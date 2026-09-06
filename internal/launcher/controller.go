package launcher

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
)

// Action is a request from the UI. Every action is handled off the render
// goroutine.
type Action interface{ isAction() }

type (
	// ActionRefresh reloads instances, accounts, runtimes and config.
	ActionRefresh struct{}
	// ActionSelect changes the selected instance and loads its metadata.
	ActionSelect struct{ Name string }
	// ActionLoginOffline adds and activates a local account.
	ActionLoginOffline struct{ Name string }
	// ActionCreate makes a new instance.
	ActionCreate struct {
		Name    string
		Version string
		Loader  instance.LoaderSpec
		Clone   string
	}
	// ActionDelete removes an instance.
	ActionDelete struct{ Name string }
	// ActionSaveMeta writes an instance's settings.
	ActionSaveMeta struct {
		Name string
		Meta instance.Meta
	}
	// ActionDetect fills an instance's settings from what is on disk.
	ActionDetect struct{ Name string }
	// ActionDeleteFile removes one mod, config or save.
	ActionDeleteFile struct {
		Name string
		Kind instance.FileKind
		File string
	}
	// ActionSetLoaderOverride changes the loader for the next launch only.
	ActionSetLoaderOverride struct {
		Spec instance.LoaderSpec
		Set  bool
	}
	// ActionLaunch starts the selected instance.
	ActionLaunch struct{ Name string }
	// ActionStopGame asks a running game to close.
	ActionStopGame struct{}
	// ActionHarvest builds the shared store from the instances.
	ActionHarvest struct{}
	// ActionScanStorage measures what each instance could share.
	ActionScanStorage struct{}
	// ActionSetConfig changes a manager configuration key.
	ActionSetConfig struct{ Key, Value string }
)

func (ActionRefresh) isAction()           {}
func (ActionSelect) isAction()            {}
func (ActionLoginOffline) isAction()      {}
func (ActionCreate) isAction()            {}
func (ActionDelete) isAction()            {}
func (ActionSaveMeta) isAction()          {}
func (ActionDetect) isAction()            {}
func (ActionDeleteFile) isAction()        {}
func (ActionSetLoaderOverride) isAction() {}
func (ActionLaunch) isAction()            {}
func (ActionStopGame) isAction()          {}
func (ActionHarvest) isAction()           {}
func (ActionScanStorage) isAction()       {}
func (ActionSetConfig) isAction()         {}

// Event is a state change produced by a worker.
type Event struct {
	// Apply mutates the store. Running it on the pump goroutine keeps all
	// writes serialised without the workers touching the lock directly.
	Apply func(*Store)
	// Terminal marks an event worth an immediate repaint, as opposed to
	// progress that can wait for the next tick.
	Terminal bool
}

// Controller turns actions into work and work into events.
//
// Every exported method returns immediately: nothing the render loop can call
// is allowed to block, because a blocked render loop is a frozen window.
type Controller struct {
	Manager    *instance.Manager
	Layout     *launch.Layout
	Accounts   *AccountStore
	LauncherID string
	Version    string

	store  *Store
	events chan Event

	nextTask atomic.Int64

	mu      sync.Mutex
	cancels map[TaskID]context.CancelFunc
	game    *launch.Process
}

// NewController wires a controller against the core packages.
func NewController(m *instance.Manager, store *Store, accounts *AccountStore, version string) *Controller {
	return &Controller{
		Manager:  m,
		Layout:   launch.NewLayout(m.AppDir),
		Accounts: accounts,
		Version:  version,
		store:    store,
		// Buffered so a burst of progress never blocks a worker.
		events:  make(chan Event, 256),
		cancels: map[TaskID]context.CancelFunc{},
	}
}

// Events is the stream the pump drains.
func (c *Controller) Events() <-chan Event { return c.events }

// Store gives the render loop read access.
func (c *Controller) Store() *Store { return c.store }

// emit queues an event, dropping progress rather than blocking if the pump has
// fallen behind. Terminal events always get through.
func (c *Controller) emit(e Event) {
	if e.Terminal {
		c.events <- e
		return
	}
	select {
	case c.events <- e:
	default:
	}
}

func (c *Controller) setStatus(msg string) {
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetStatus(msg) }})
}

func (c *Controller) fail(err error) {
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetError(err) }})
}

// Dispatch starts an action and returns at once.
func (c *Controller) Dispatch(a Action) TaskID {
	id := TaskID(c.nextTask.Add(1))

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancels[id] = cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.cancels, id)
			c.mu.Unlock()
			cancel()
		}()
		c.run(ctx, id, a)
	}()

	return id
}

// Cancel stops a running task. A running game is deliberately not affected;
// stopping that is a separate, explicit action.
func (c *Controller) Cancel(id TaskID) {
	c.mu.Lock()
	cancel := c.cancels[id]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// beginTask publishes a task as started.
func (c *Controller) beginTask(id TaskID, kind TaskKind, label string) {
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetTask(Task{ID: id, Kind: kind, Label: label, Started: time.Now()})
	}})
}

// finishTask publishes the outcome.
func (c *Controller) finishTask(id TaskID, err error) {
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.UpdateTask(id, func(t *Task) {
			t.Done = true
			t.Err = err
		})
		if err != nil {
			s.SetError(err)
		}
	}})
}

func (c *Controller) run(ctx context.Context, id TaskID, a Action) {
	switch action := a.(type) {
	case ActionRefresh:
		c.doRefresh(ctx)
	case ActionSelect:
		c.doSelect(action.Name)
	case ActionLoginOffline:
		c.doLoginOffline(action.Name)
	case ActionCreate:
		c.doCreate(ctx, id, action)
	case ActionDelete:
		c.doDelete(ctx, action.Name)
	case ActionSaveMeta:
		c.doSaveMeta(action)
	case ActionDetect:
		c.doDetect(action.Name)
	case ActionDeleteFile:
		c.doDeleteFile(action)
	case ActionSetLoaderOverride:
		c.emit(Event{Terminal: true, Apply: func(s *Store) {
			s.SetLoaderOverride(action.Spec, action.Set)
		}})
	case ActionLaunch:
		c.doLaunch(ctx, id, action.Name)
	case ActionStopGame:
		c.doStopGame()
	case ActionHarvest:
		c.doHarvest(ctx, id)
	case ActionScanStorage:
		c.doScanStorage(ctx)
	case ActionSetConfig:
		c.doSetConfig(ctx, action)
	}
}

func (c *Controller) doRefresh(ctx context.Context) {
	instances, err := c.Manager.ListInstances()
	if err != nil {
		c.fail(err)
		return
	}

	roots := make([]string, 0, len(instances))
	for _, inst := range instances {
		roots = append(roots, inst.Path)
	}
	detector := &java.Detector{SharedRuntimes: c.Layout.Runtimes(), ExtraRoots: roots}
	runtimes := detector.Detect(ctx)

	accounts, active := c.Accounts.List()
	config := c.Manager.GetConfig()

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetInstances(instances)
		s.SetRuntimes(runtimes)
		s.SetAccounts(accounts, active)
		s.SetConfig(config)

		// Land on the login screen only when there is genuinely no account.
		if len(accounts) == 0 {
			s.SetScreen(ScreenLogin)
		}
	}})
}

func (c *Controller) doSelect(name string) {
	meta, err := c.Manager.GetMeta(name)
	ok := err == nil
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetSelected(name)
		s.SetEditing(meta, ok)
	}})
}

func (c *Controller) doLoginOffline(name string) {
	account, err := auth.NewOfflineAccount(name)
	if err != nil {
		c.fail(err)
		return
	}
	if err := c.Accounts.Add(account); err != nil {
		c.fail(err)
		return
	}

	accounts, active := c.Accounts.List()
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetAccounts(accounts, active)
		s.SetScreen(ScreenInstances)
		s.SetStatus("Signed in as " + account.Name)
	}})
}

func (c *Controller) doCreate(ctx context.Context, id TaskID, a ActionCreate) {
	c.beginTask(id, TaskCreate, "Creating "+a.Name)

	opts := instance.CreateOptions{Ctx: ctx, CloneFrom: a.Clone}
	if a.Clone != "" {
		opts.Progress = func(copied, total int64, current string) {
			c.emit(Event{Apply: func(s *Store) {
				s.UpdateTask(id, func(t *Task) {
					t.Phase = "copying"
					t.Progress = download.Progress{BytesDone: copied, BytesTotal: total, Current: current}
				})
			}})
		}
	}

	if err := c.Manager.CreateInstanceWithOptions(a.Name, opts); err != nil {
		c.finishTask(id, err)
		return
	}

	// A brand-new instance knows its version from the dialog, not from disk.
	meta := instance.DefaultMeta(a.Name)
	meta.MinecraftVersion = a.Version
	meta.Loader = a.Loader
	meta.ResolvedVersionID = a.Version
	if err := c.Manager.SetMeta(a.Name, meta); err != nil {
		c.finishTask(id, err)
		return
	}

	c.finishTask(id, nil)
	c.doRefresh(ctx)
	c.doSelect(a.Name)
}

func (c *Controller) doDelete(ctx context.Context, name string) {
	if err := c.Manager.DeleteInstance(name); err != nil {
		c.fail(err)
		return
	}
	c.setStatus("Deleted " + name)
	c.doRefresh(ctx)
}

func (c *Controller) doSaveMeta(a ActionSaveMeta) {
	if err := c.Manager.SetMeta(a.Name, a.Meta); err != nil {
		c.fail(err)
		return
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetEditing(a.Meta, true)
		s.SetStatus("Saved " + a.Name)
	}})
}

func (c *Controller) doDetect(name string) {
	dir, err := c.Manager.InstancePath(name)
	if err != nil {
		c.fail(err)
		return
	}
	det, err := instance.DetectMeta(dir)
	if err != nil {
		c.fail(err)
		return
	}
	if det.Confidence == instance.ConfidenceNone {
		c.fail(fmt.Errorf("could not work out what %s runs; set it by hand", name))
		return
	}

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetEditing(det.Meta, true)
		s.SetStatus(fmt.Sprintf("Detected %s / %s from %s — review and save",
			det.Meta.MinecraftVersion, det.Meta.Loader, det.Source))
	}})
}

func (c *Controller) doDeleteFile(a ActionDeleteFile) {
	if err := c.Manager.DeleteInstanceFile(a.Name, a.Kind, a.File); err != nil {
		c.fail(err)
		return
	}
	c.setStatus("Deleted " + a.File)
}

func (c *Controller) doSetConfig(ctx context.Context, a ActionSetConfig) {
	if err := c.Manager.UpdateConfig(a.Key, a.Value); err != nil {
		c.fail(err)
		return
	}
	c.doRefresh(ctx)
}

func (c *Controller) doStopGame() {
	c.mu.Lock()
	proc := c.game
	c.mu.Unlock()
	if proc == nil {
		return
	}
	// Give the game a chance to save before it is killed.
	_ = proc.Stop(10 * time.Second)
}

func (c *Controller) doScanStorage(ctx context.Context) {
	instances, err := c.Manager.ListInstances()
	if err != nil {
		return
	}
	for _, inst := range instances {
		if ctx.Err() != nil {
			return
		}
		size, err := launch.Reclaimable(inst.Path)
		if err != nil {
			continue
		}
		name := inst.Name
		c.emit(Event{Apply: func(s *Store) { s.SetReclaimable(name, size) }})
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetStatus("Storage scan complete") }})
}

func (c *Controller) doHarvest(ctx context.Context, id TaskID) {
	c.beginTask(id, TaskHarvest, "Building the shared store")

	instances, err := c.Manager.ListInstances()
	if err != nil {
		c.finishTask(id, err)
		return
	}

	var shared int64
	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			c.finishTask(id, err)
			return
		}
		name := inst.Name
		c.emit(Event{Apply: func(s *Store) {
			s.UpdateTask(id, func(t *Task) { t.Phase = name })
		}})

		stats, err := launch.Harvest(ctx, c.Layout, inst.Path, func(p launch.HarvestProgress) {
			c.emit(Event{Apply: func(s *Store) {
				s.UpdateTask(id, func(t *Task) {
					t.Message = fmt.Sprintf("%d files, %s", p.Files, launch.FormatBytes(p.Bytes))
				})
			}})
		})
		if err != nil {
			c.finishTask(id, err)
			return
		}
		shared += stats.BytesShared
	}

	c.finishTask(id, nil)
	c.setStatus("Shared store now holds " + launch.FormatBytes(shared) + " of harvested content")
	c.doRefresh(ctx)
}

// doLaunch runs the whole path from an instance to a running game.
func (c *Controller) doLaunch(ctx context.Context, id TaskID, name string) {
	c.beginTask(id, TaskLaunch, "Launching "+name)

	account, ok := c.Accounts.Active()
	if !ok {
		c.finishTask(id, fmt.Errorf("no account selected"))
		return
	}

	meta, err := c.Manager.GetMeta(name)
	if err != nil {
		c.finishTask(id, err)
		return
	}

	snap := c.store.Snapshot()
	loader := meta.Loader
	if snap.HasOverride {
		loader = snap.LoaderOverride
	}

	versionID, err := versionIDFor(meta, loader)
	if err != nil {
		c.finishTask(id, err)
		return
	}

	// gameDir is the ~/.minecraft symlink, so the instance has to be active.
	if c.Manager.GetActiveInstance() != name {
		c.step(id, "Activating "+name)
		if err := c.Manager.SwitchInstance(name); err != nil {
			c.finishTask(id, err)
			return
		}
	}

	d := download.New()
	prep := launch.NewPreparer(c.Layout, d)
	prep.Observer = func(e launch.Event) {
		c.emit(Event{Apply: func(s *Store) {
			s.UpdateTask(id, func(t *Task) {
				t.Phase = string(e.Phase)
				t.Progress = e.Progress
				if e.Message != "" {
					t.Message = e.Message
				}
				if e.Done {
					t.Steps = append(t.Steps, string(e.Phase))
					t.Message = ""
					t.Progress = download.Progress{}
				}
			})
		}})
	}

	prepared, err := prep.Prepare(ctx, versionID)
	if err != nil {
		c.finishTask(id, err)
		return
	}

	c.step(id, "Selecting Java")
	selection, err := c.selectJava(ctx, prepared, meta, loader)
	if err != nil {
		c.finishTask(id, err)
		return
	}
	if selection.Warning != "" {
		c.setStatus(selection.Warning)
	}

	minMB, maxMB := meta.Memory.Resolved()
	args := launch.BuildArgs(prepared, launch.Options{
		Session: launch.Session{
			PlayerName:  account.Name,
			UUID:        account.UUID,
			AccessToken: account.AccessToken(),
			XUID:        account.EffectiveXUID(),
			UserType:    account.UserType(),
			ClientID:    c.LauncherID,
		},
		GameDir:       c.Manager.MinecraftPath,
		LauncherName:  "minecraft-instance-manager",
		LauncherVer:   c.Version,
		MinMB:         minMB,
		MaxMB:         maxMB,
		ExtraJVMArgs:  meta.JVMArgs,
		ExtraGameArgs: meta.GameArgs,
	}, prep.Platform)

	// The game must outlive this task's context, or cancelling the launch
	// progress would kill a running game.
	proc, err := launch.Start(context.Background(), launch.Spec{
		JavaPath: selection.Runtime.Path,
		Args:     args,
		GameDir:  c.Manager.MinecraftPath,
		LogDir:   filepath.Join(c.Manager.AppDir, "logs"),
		Name:     name,
	})
	if err != nil {
		c.finishTask(id, err)
		return
	}

	c.mu.Lock()
	c.game = proc
	c.mu.Unlock()

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetGame(GameState{
			Instance: name,
			PID:      proc.Cmd.Process.Pid,
			LogPath:  proc.LogPath,
			Started:  proc.Started,
			Running:  true,
		})
	}})
	c.finishTask(id, nil)

	go c.watchGame(name, proc)
}

// watchGame keeps the log tail fresh and records the exit.
func (c *Controller) watchGame(name string, proc *launch.Process) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for proc.Running() {
		select {
		case <-proc.Done():
		case <-ticker.C:
		}
		tail := proc.Log.Tail(200)
		c.emit(Event{Apply: func(s *Store) {
			s.UpdateGame(func(g *GameState) { g.Tail = tail })
		}})
	}

	err := proc.Wait()
	tail := proc.Log.Tail(200)

	c.mu.Lock()
	if c.game == proc {
		c.game = nil
	}
	c.mu.Unlock()

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.UpdateGame(func(g *GameState) {
			g.Running = false
			g.ExitErr = err
			g.Tail = tail
		})
	}})

	// Play statistics are best-effort; a failure here must not surface.
	if meta, metaErr := c.Manager.GetMeta(name); metaErr == nil {
		meta.LastPlayed = proc.Started.UTC()
		meta.TotalPlaySeconds += int64(time.Since(proc.Started).Seconds())
		_ = c.Manager.SetMeta(name, meta)
	}
}

// step records a finished phase on the current task.
func (c *Controller) step(id TaskID, label string) {
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.UpdateTask(id, func(t *Task) {
			t.Phase = label
			t.Steps = append(t.Steps, label)
		})
	}})
}

// selectJava resolves the runtime for a prepared version.
func (c *Controller) selectJava(ctx context.Context, prepared *launch.Prepared,
	meta instance.Meta, loader instance.LoaderSpec) (java.Selection, error) {

	instances, err := c.Manager.ListInstances()
	if err != nil {
		return java.Selection{}, err
	}
	roots := make([]string, 0, len(instances))
	for _, inst := range instances {
		roots = append(roots, inst.Path)
	}

	var req java.Requirement
	if jv := prepared.Version.JavaVersion; jv != nil {
		req = java.Requirement{Major: jv.MajorVersion, Component: jv.Component}
	}
	// Forge pins itself to the JVM it was built against.
	if loader.Type == instance.LoaderForge {
		req.Strict = true
	}

	detector := &java.Detector{SharedRuntimes: c.Layout.Runtimes(), ExtraRoots: roots}
	return detector.Resolve(ctx, req, meta.Java.Path)
}

// versionIDFor works out which version id to launch for a loader choice.
//
// The stored ResolvedVersionID only applies to the instance's own loader; once
// the user overrides it, the id has to be rebuilt from the loader and the game
// version.
func versionIDFor(meta instance.Meta, loader instance.LoaderSpec) (string, error) {
	if loader == meta.Loader && meta.ResolvedVersionID != "" {
		return meta.ResolvedVersionID, nil
	}
	if meta.MinecraftVersion == "" {
		return "", fmt.Errorf("instance %q has no Minecraft version set", meta.Name)
	}

	switch loader.Type {
	case instance.LoaderVanilla, "":
		return meta.MinecraftVersion, nil
	case instance.LoaderNeoForge:
		if loader.Version == "" {
			return "", fmt.Errorf("no NeoForge version chosen")
		}
		return "neoforge-" + loader.Version, nil
	case instance.LoaderForge:
		if loader.Version == "" {
			return "", fmt.Errorf("no Forge version chosen")
		}
		return meta.MinecraftVersion + "-forge-" + loader.Version, nil
	case instance.LoaderFabric:
		if loader.Version == "" {
			return "", fmt.Errorf("no Fabric loader version chosen")
		}
		return "fabric-loader-" + loader.Version + "-" + meta.MinecraftVersion, nil
	case instance.LoaderQuilt:
		if loader.Version == "" {
			return "", fmt.Errorf("no Quilt loader version chosen")
		}
		return "quilt-loader-" + loader.Version + "-" + meta.MinecraftVersion, nil
	}
	return "", fmt.Errorf("unknown loader %q", loader.Type)
}
