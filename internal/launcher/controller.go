package launcher

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/desktop"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/loader"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/mojang"
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
	// ActionLoginMicrosoft signs in with a Microsoft account through the
	// device code flow. It stays in flight until the player finishes in their
	// browser, so cancelling it is a normal outcome rather than an error.
	ActionLoginMicrosoft struct{}
	// ActionSignOut forgets an account and its stored tokens.
	ActionSignOut struct{ UUID string }
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
	// ActionRename gives an instance a new name.
	ActionRename struct{ Name, NewName string }
	// ActionLoadContent lists everything an instance holds.
	ActionLoadContent struct{ Name string }
	// ActionSetEnabled switches a mod on or off.
	ActionSetEnabled struct {
		Name    string
		Kind    instance.ContentKind
		File    string
		Enabled bool
	}
	// ActionDeleteContent removes one mod, config file, world, pack or log.
	ActionDeleteContent struct {
		Name string
		Kind instance.ContentKind
		File string
	}
	// ActionOpen hands a file, folder or link to the desktop.
	ActionOpen struct{ Path string }
	// ActionReveal shows a file in the file manager.
	ActionReveal struct{ Path string }
	// ActionLaunch starts the selected instance.
	ActionLaunch struct{ Name string }
	// ActionStopGame asks a running game to close.
	ActionStopGame struct{}
	// ActionHarvest builds the shared store from the instances.
	ActionHarvest struct{}
	// ActionScanStorage measures what each instance could share.
	ActionScanStorage struct{}
	// ActionReclaim removes from the instances the game files the shared
	// store already holds.
	ActionReclaim struct{}
	// ActionListVersions fetches a version list: the Minecraft releases
	// when Kind is empty, otherwise one loader's releases for MC.
	ActionListVersions struct {
		Kind instance.LoaderType
		MC   string
	}
	// ActionInstallLoader installs an instance's loader profile into the
	// store ahead of its first launch.
	ActionInstallLoader struct{ Name string }
	// ActionSetConfig changes a manager configuration key.
	ActionSetConfig struct{ Key, Value string }
)

func (ActionRefresh) isAction()        {}
func (ActionSelect) isAction()         {}
func (ActionLoginOffline) isAction()   {}
func (ActionLoginMicrosoft) isAction() {}
func (ActionSignOut) isAction()        {}
func (ActionCreate) isAction()         {}
func (ActionDelete) isAction()         {}
func (ActionSaveMeta) isAction()       {}
func (ActionDetect) isAction()         {}
func (ActionRename) isAction()         {}
func (ActionLoadContent) isAction()    {}
func (ActionSetEnabled) isAction()     {}
func (ActionDeleteContent) isAction()  {}
func (ActionOpen) isAction()           {}
func (ActionReveal) isAction()         {}
func (ActionLaunch) isAction()         {}
func (ActionStopGame) isAction()       {}
func (ActionHarvest) isAction()        {}
func (ActionScanStorage) isAction()    {}
func (ActionReclaim) isAction()        {}
func (ActionListVersions) isAction()   {}
func (ActionInstallLoader) isAction()  {}
func (ActionSetConfig) isAction()      {}

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
	// MSAClientID is the Azure application id Microsoft sign-in runs against.
	// Empty leaves the launcher able to make local accounts only.
	MSAClientID string
	// MSAEndpoints overrides the sign-in services, which only a test does.
	MSAEndpoints auth.Endpoints
	Version      string
	// Loaders lists and installs mod loaders. Its endpoints are replaceable
	// for tests.
	Loaders *loader.Client

	store  *Store
	events chan Event

	nextTask atomic.Int64

	mu      sync.Mutex
	cancels map[TaskID]context.CancelFunc
	game    *launch.Process
}

// NewController wires a controller against the core packages.
func NewController(m *instance.Manager, store *Store, accounts *AccountStore, version string) *Controller {
	layout := launch.NewLayout(m.AppDir)
	return &Controller{
		Manager:  m,
		Layout:   layout,
		Accounts: accounts,
		Version:  version,
		Loaders:  loader.NewClient(layout, download.New()),
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
	case ActionLoginMicrosoft:
		c.doLoginMicrosoft(ctx, id)
	case ActionSignOut:
		c.doSignOut(action.UUID)
	case ActionCreate:
		c.doCreate(ctx, id, action)
	case ActionDelete:
		c.doDelete(ctx, action.Name)
	case ActionSaveMeta:
		c.doSaveMeta(action)
	case ActionDetect:
		c.doDetect(action.Name)
	case ActionRename:
		c.doRename(ctx, action)
	case ActionLoadContent:
		c.doLoadContent(action.Name)
	case ActionSetEnabled:
		c.doSetEnabled(action)
	case ActionDeleteContent:
		c.doDeleteContent(action)
	case ActionOpen:
		if err := desktop.Open(action.Path); err != nil {
			c.fail(fmt.Errorf("could not open %s: %w", filepath.Base(action.Path), err))
		}
	case ActionReveal:
		if err := desktop.Reveal(action.Path); err != nil {
			c.fail(fmt.Errorf("could not show %s: %w", filepath.Base(action.Path), err))
		}
	case ActionLaunch:
		c.doLaunch(ctx, id, action.Name)
	case ActionStopGame:
		c.doStopGame()
	case ActionHarvest:
		c.doHarvest(ctx, id)
	case ActionReclaim:
		c.doReclaim(ctx, id)
	case ActionListVersions:
		c.doListVersions(ctx, action)
	case ActionInstallLoader:
		c.doInstallLoader(ctx, id, action.Name)
	case ActionScanStorage:
		c.doScanStorage(ctx)
	case ActionSetConfig:
		c.doSetConfig(ctx, action)
	}
}

// doRefresh reloads everything the window shows. It publishes in two steps:
// the instance list is a few directory reads and appears at once, while the
// Java scan may have to fork a JVM per runtime and follows when it is done.
// Nobody should look at an empty window because a runtime was slow to answer.
func (c *Controller) doRefresh(ctx context.Context) {
	instances, err := c.Manager.ListInstances()
	if err != nil {
		c.fail(err)
		return
	}

	accounts, active := c.Accounts.List()
	config := c.Manager.GetConfig()

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetInstances(instances)
		s.SetAccounts(accounts, active)
		s.SetConfig(config)
		s.SetMSAConfigured(c.MSAConfigured())

		// Land on the login screen only when there is genuinely no account.
		if len(accounts) == 0 {
			s.SetScreen(ScreenLogin)
		}
	}})

	runtimes := c.detector(instances).Detect(ctx)
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetRuntimes(runtimes)
	}})
}

// detector builds a Java detector over the store and the given instances,
// sharing the probe cache so a runtime is only ever asked once.
func (c *Controller) detector(instances []instance.Instance) *java.Detector {
	roots := make([]string, 0, len(instances))
	for _, inst := range instances {
		roots = append(roots, inst.Path)
	}
	return java.NewDetector(c.Layout.Runtimes(), c.Layout.Cache(), roots)
}

func (c *Controller) doSelect(name string) {
	meta, err := c.Manager.GetMeta(name)
	ok := err == nil
	installed := c.profileInstalled(meta)
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetSelected(name)
		s.SetEditing(meta, ok)
		s.SetProfileInstalled(installed)
	}})
	c.doLoadContent(name)
}

// profileInstalled reports whether an instance's version is in the store.
// Vanilla counts as installed: the launcher fetches it itself.
func (c *Controller) profileInstalled(meta instance.Meta) bool {
	id, err := versionIDFor(meta, meta.Loader)
	if err != nil {
		return false
	}
	return !launch.IsLoaderProfile(id) || c.Loaders.Installed(id)
}

// doListVersions fetches a version list on demand and publishes it. The
// pending marker lets a picker say "loading" instead of "nothing".
func (c *Controller) doListVersions(ctx context.Context, a ActionListVersions) {
	if a.Kind == "" || a.Kind == instance.LoaderVanilla {
		c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetVersionsPending(MCVersionsKey, true) }})
		client := mojang.NewClient(c.Loaders.Downloader, c.Layout.Versions(), c.Layout.Cache())
		manifest, err := client.Manifest(ctx)
		if err != nil {
			c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetVersionsPending(MCVersionsKey, false) }})
			c.fail(err)
			return
		}
		var ids []string
		for _, v := range manifest.OfType("release") {
			ids = append(ids, v.ID)
		}
		c.emit(Event{Terminal: true, Apply: func(s *Store) {
			s.SetMCVersions(ids)
			s.SetVersionsPending(MCVersionsKey, false)
		}})
		return
	}

	key := VersionsKey(a.Kind, a.MC)
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetVersionsPending(key, true) }})
	versions, err := c.Loaders.Versions(ctx, a.Kind, a.MC)
	if err != nil {
		c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetVersionsPending(key, false) }})
		c.fail(fmt.Errorf("listing %s versions for %s: %w", a.Kind.Display(), a.MC, err))
		return
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetLoaderVersions(key, versions) }})
}

// doInstallLoader installs an instance's loader as its own task.
func (c *Controller) doInstallLoader(ctx context.Context, id TaskID, name string) {
	meta, err := c.Manager.GetMeta(name)
	if err != nil {
		c.fail(err)
		return
	}
	c.beginTask(id, TaskInstall, "Installing "+meta.Loader.String())
	if _, err := c.ensureLoader(ctx, id, meta); err != nil {
		c.finishTask(id, err)
		return
	}
	c.finishTask(id, nil)
	c.setStatus(meta.Loader.String() + " is installed")
	c.doSelect(name)
}

// ensureLoader makes sure an instance's version can be launched, installing
// the loader when its profile is missing, and returns the version id.
func (c *Controller) ensureLoader(ctx context.Context, id TaskID, meta instance.Meta) (string, error) {
	versionID, err := versionIDFor(meta, meta.Loader)
	if err != nil {
		return "", err
	}
	if !launch.IsLoaderProfile(versionID) || c.Loaders.Installed(versionID) {
		return versionID, nil
	}

	c.step(id, "Installing "+meta.Loader.String())

	// Forge-style installers are Java programs; any usable runtime does.
	javaPath := ""
	if meta.Loader.Type == instance.LoaderForge || meta.Loader.Type == instance.LoaderNeoForge {
		instances, err := c.Manager.ListInstances()
		if err != nil {
			return "", err
		}
		runtimes := c.store.Snapshot().Runtimes
		if len(runtimes) == 0 {
			runtimes = c.detector(instances).Detect(ctx)
		}
		sel, err := java.Select(runtimes, java.Requirement{})
		if err != nil {
			return "", fmt.Errorf("installing %s: %w", meta.Loader, err)
		}
		javaPath = sel.Runtime.Path
	}

	progress := func(line string) {
		c.emit(Event{Apply: func(s *Store) {
			s.UpdateTask(id, func(t *Task) { t.Message = line })
		}})
	}
	installed, err := c.Loaders.Install(ctx, meta.Loader.Type, meta.MinecraftVersion, meta.Loader.Version, javaPath, progress)
	if err != nil {
		return "", err
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.UpdateTask(id, func(t *Task) { t.Message = "" })
		if s.Snapshot().Selected == meta.Name {
			s.SetProfileInstalled(true)
		}
	}})
	return installed, nil
}

// doLoadContent lists every kind of content at once. Eight directory reads
// and one walk of config/ cost a few milliseconds, so there is nothing to
// gain from listing lazily and a lot of state to lose track of.
func (c *Controller) doLoadContent(name string) {
	if name == "" {
		return
	}
	content := make(map[instance.ContentKind][]instance.Entry, len(instance.ContentKinds()))
	for _, kind := range instance.ContentKinds() {
		entries, err := c.Manager.ListContent(name, kind)
		if err != nil {
			c.fail(err)
			return
		}
		content[kind] = entries
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetContent(name, content)
	}})
}

// refreshInstances reloads the instance list only, for after a change that
// touched an instance's contents. The Java scan is not repeated.
func (c *Controller) refreshInstances() {
	instances, err := c.Manager.ListInstances()
	if err != nil {
		c.fail(err)
		return
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetInstances(instances) }})
}

func (c *Controller) doRename(ctx context.Context, a ActionRename) {
	if err := c.Manager.RenameInstance(a.Name, a.NewName); err != nil {
		c.fail(err)
		return
	}
	c.setStatus("Renamed " + a.Name + " to " + a.NewName)
	c.doRefresh(ctx)
	c.doSelect(a.NewName)
}

func (c *Controller) doSetEnabled(a ActionSetEnabled) {
	newName, err := c.Manager.SetContentEnabled(a.Name, a.Kind, a.File, a.Enabled)
	if err != nil {
		c.fail(err)
		return
	}
	if a.Enabled {
		c.setStatus("Enabled " + newName)
	} else {
		c.setStatus("Disabled " + strings.TrimSuffix(newName, instance.DisabledSuffix))
	}
	c.doLoadContent(a.Name)
	c.refreshInstances()
}

func (c *Controller) doDeleteContent(a ActionDeleteContent) {
	if err := c.Manager.DeleteContent(a.Name, a.Kind, a.File); err != nil {
		c.fail(err)
		return
	}
	c.setStatus("Deleted " + a.File)
	c.doLoadContent(a.Name)
	c.refreshInstances()
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

// MSAConfigured reports whether Microsoft sign-in can be offered.
func (c *Controller) MSAConfigured() bool { return c.MSAClientID != "" }

// msa builds a sign-in client whose progress is reported against one task.
func (c *Controller) msa(id TaskID) *auth.MSA {
	client := auth.NewMSA(c.MSAClientID)
	if c.MSAEndpoints.Token != "" {
		client.Endpoints = c.MSAEndpoints
	}
	client.Observer = func(step string) {
		c.emit(Event{Terminal: true, Apply: func(s *Store) {
			s.UpdateLogin(id, func(l *LoginState) { l.Step = step })
			s.UpdateTask(id, func(t *Task) { t.Message = step })
		}})
	}
	return client
}

// doLoginMicrosoft runs the device code flow from the code to a stored
// account, publishing the code as soon as there is one to show.
func (c *Controller) doLoginMicrosoft(ctx context.Context, id TaskID) {
	client := c.msa(id)
	if !client.Configured() {
		c.fail(auth.ErrNotConfigured)
		return
	}

	c.beginTask(id, TaskLogin, "Signing in with Microsoft")

	// A refusal from the last attempt would otherwise sit in the banner for
	// the whole of this one, which reads as the new attempt having failed
	// before it even asked for a code.
	c.setStatus("Signing in with Microsoft")

	code, err := client.StartDeviceCode(ctx)
	if err != nil {
		c.endLogin(id, err)
		return
	}

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetLogin(LoginState{
			Active:          true,
			Task:            id,
			UserCode:        code.UserCode,
			VerificationURI: code.VerificationURI,
			ExpiresAt:       code.ExpiresAt,
			Step:            "Waiting for you to enter the code",
		})
		s.SetScreen(ScreenLogin)
	}})

	tokens, err := client.WaitForToken(ctx, code)
	if err != nil {
		c.endLogin(id, err)
		return
	}

	account, err := client.SignIn(ctx, tokens)
	if err != nil {
		c.endLogin(id, err)
		return
	}
	if err := c.Accounts.Add(account); err != nil {
		c.endLogin(id, err)
		return
	}

	accounts, active := c.Accounts.List()
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetLogin(LoginState{})
		s.SetAccounts(accounts, active)
		s.SetScreen(ScreenInstances)
		s.SetStatus("Signed in as " + account.Name)
	}})
	c.finishTask(id, nil)
}

// endLogin clears a sign-in that did not produce an account. A cancelled one
// is the player changing their mind, not a failure, so it reports no error.
func (c *Controller) endLogin(id TaskID, err error) {
	cancelled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)

	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetLogin(LoginState{})
		if cancelled {
			s.SetStatus("Sign-in cancelled")
		}
	}})

	if cancelled {
		c.emit(Event{Terminal: true, Apply: func(s *Store) {
			s.UpdateTask(id, func(t *Task) { t.Done = true })
		}})
		return
	}
	c.finishTask(id, err)
}

// doSignOut forgets an account, so a wrong or expired one can be replaced.
func (c *Controller) doSignOut(uuid string) {
	if err := c.Accounts.Remove(uuid); err != nil {
		c.fail(err)
		return
	}

	accounts, active := c.Accounts.List()
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetAccounts(accounts, active)
		s.SetStatus("Signed out")
		if len(accounts) == 0 {
			s.SetScreen(ScreenLogin)
		}
	}})
}

// ensureSession renews an expired Microsoft session before it is launched
// with. Tokens last a day, so without this every launch on the second day
// would fail inside the game rather than in the launcher.
func (c *Controller) ensureSession(ctx context.Context, id TaskID, account auth.Account) (auth.Account, error) {
	if account.Kind != auth.KindMSA || account.Usable() {
		return account, nil
	}

	client := c.msa(id)
	if !client.Configured() {
		return account, auth.ErrNotConfigured
	}

	c.step(id, "Renewing the Microsoft session")
	fresh, err := client.RefreshAccount(ctx, account)
	if err != nil {
		if errors.Is(err, auth.ErrReauth) {
			// Mark it so the sign-in screen can say which account went stale
			// instead of failing again at the next launch.
			if markErr := c.Accounts.MarkNeedsReauth(account.UUID); markErr != nil {
				return account, markErr
			}
			accounts, active := c.Accounts.List()
			c.emit(Event{Terminal: true, Apply: func(s *Store) {
				s.SetAccounts(accounts, active)
			}})
			return account, fmt.Errorf("%s has to sign in with Microsoft again: %w", account.Name, err)
		}
		return account, err
	}

	if err := c.Accounts.Add(fresh); err != nil {
		return account, err
	}
	accounts, active := c.Accounts.List()
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetAccounts(accounts, active)
	}})
	return fresh, nil
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
	if a.Loader.Type == "" {
		meta.Loader.Type = instance.LoaderVanilla
	}
	meta.ResolvedVersionID = loader.VersionID(meta.Loader.Type, a.Version, a.Loader.Version)
	if err := c.Manager.SetMeta(a.Name, meta); err != nil {
		c.finishTask(id, err)
		return
	}

	c.doRefresh(ctx)
	c.doSelect(a.Name)

	// A modded instance is only useful once its loader is in the store, so
	// the install is part of creating it rather than a surprise at launch.
	if _, err := c.ensureLoader(ctx, id, meta); err != nil {
		c.finishTask(id, err)
		return
	}
	c.finishTask(id, nil)
	c.doSelect(a.Name)
}

func (c *Controller) doDelete(ctx context.Context, name string) {
	if err := c.Manager.DeleteInstance(name); err != nil {
		c.fail(err)
		return
	}
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		if s.Snapshot().Selected == name {
			s.SetSelected("")
			s.SetContent("", nil)
		}
		s.SetStatus("Deleted " + name)
	}})
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
		size, err := launch.Reclaimable(c.Layout, inst.Path)
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

// doReclaim frees the copies the store has made redundant, one instance at
// a time, then measures again so the storage panel shows what is left.
func (c *Controller) doReclaim(ctx context.Context, id TaskID) {
	c.beginTask(id, TaskReclaim, "Freeing space")

	instances, err := c.Manager.ListInstances()
	if err != nil {
		c.finishTask(id, err)
		return
	}

	var total launch.ReclaimStats
	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			c.finishTask(id, err)
			return
		}
		name := inst.Name
		c.emit(Event{Apply: func(s *Store) {
			s.UpdateTask(id, func(t *Task) { t.Phase = name })
		}})
		stats, err := launch.Reclaim(ctx, c.Layout, inst.Path, false)
		if err != nil {
			c.finishTask(id, err)
			return
		}
		total.Files += stats.Files
		total.Bytes += stats.Bytes
		c.emit(Event{Apply: func(s *Store) { s.SetReclaimable(name, 0) }})
	}

	c.finishTask(id, nil)
	c.setStatus(fmt.Sprintf("Freed %s across %d files", launch.FormatBytes(total.Bytes), total.Files))
	c.doRefresh(ctx)
}

// doLaunch runs the whole path from an instance to a running game.
func (c *Controller) doLaunch(ctx context.Context, id TaskID, name string) {
	started := time.Now()
	c.beginTask(id, TaskLaunch, "Launching "+name)

	account, ok := c.Accounts.Active()
	if !ok {
		c.finishTask(id, fmt.Errorf("no account selected"))
		return
	}
	account, err := c.ensureSession(ctx, id, account)
	if err != nil {
		c.finishTask(id, err)
		return
	}

	meta, err := c.Manager.GetMeta(name)
	if err != nil {
		c.finishTask(id, err)
		return
	}

	loader := meta.Loader
	versionID, err := c.ensureLoader(ctx, id, meta)
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
		ExtraJVMArgs:  launch.JVMArgsFor(meta.JVMArgs),
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

	elapsed := time.Since(started)
	c.emit(Event{Terminal: true, Apply: func(s *Store) {
		s.SetGame(GameState{
			Instance: name,
			PID:      proc.Cmd.Process.Pid,
			LogPath:  proc.LogPath,
			Started:  proc.Started,
			Running:  true,
		})
		s.SetStatus(fmt.Sprintf("%s handed to Java in %s", name, formatElapsed(elapsed)))
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
//
// The runtimes found by the last refresh are tried first: they are what the
// settings screen shows, and a launch should not have to rediscover them.
// Only when none of them fits is the machine scanned again, in case one was
// installed since.
func (c *Controller) selectJava(ctx context.Context, prepared *launch.Prepared,
	meta instance.Meta, loader instance.LoaderSpec) (java.Selection, error) {

	var req java.Requirement
	if jv := prepared.Version.JavaVersion; jv != nil {
		req = java.Requirement{Major: jv.MajorVersion, Component: jv.Component}
	}
	// Forge pins itself to the JVM it was built against.
	if loader.Type == instance.LoaderForge {
		req.Strict = true
	}

	instances, err := c.Manager.ListInstances()
	if err != nil {
		return java.Selection{}, err
	}
	detector := c.detector(instances)

	if meta.Java.Path != "" {
		return detector.Resolve(ctx, req, meta.Java.Path)
	}
	if known := c.store.Snapshot().Runtimes; len(known) > 0 {
		if sel, err := java.Select(known, req); err == nil {
			return sel, nil
		}
	}

	runtimes := detector.Detect(ctx)
	c.emit(Event{Terminal: true, Apply: func(s *Store) { s.SetRuntimes(runtimes) }})
	return java.Select(runtimes, req)
}

// formatElapsed renders a launch duration at the precision it deserves.
func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
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
