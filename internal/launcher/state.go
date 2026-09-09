// Package launcher holds the launcher's application logic: what the user can
// ask for, what runs in the background, and the state a UI renders.
//
// It deliberately knows nothing about Gio. Keeping the decisions in their own
// package makes them testable with no display present, and turns "the UI layer
// leaked into the logic" from a convention into a compile error.
package launcher

import (
	"sync"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/loader"
)

// Screen identifies the visible page.
type Screen int

const (
	ScreenLogin Screen = iota
	ScreenInstances
	ScreenSettings
)

// TaskID identifies one background operation.
type TaskID int64

// TaskKind says what a task is doing, so the UI can label it.
type TaskKind string

const (
	TaskRefresh TaskKind = "refresh"
	TaskLaunch  TaskKind = "launch"
	TaskCreate  TaskKind = "create"
	TaskDelete  TaskKind = "delete"
	TaskHarvest TaskKind = "harvest"
	TaskReclaim TaskKind = "reclaim"
	TaskInstall TaskKind = "install"
	TaskDetect  TaskKind = "detect"
	TaskLogin   TaskKind = "login"
)

// LoginState is an interactive Microsoft sign-in in progress.
//
// The device code flow makes the wait visible on purpose: the player has to
// read a code off this screen and type it into a browser, so the UI needs the
// code, where to enter it, and how long it stays valid.
type LoginState struct {
	Active          bool
	Task            TaskID
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	// Step names the stage of the chain currently running, so the wait after
	// the browser part says what it is doing.
	Step string
}

// Task is the observable state of a background operation.
type Task struct {
	ID      TaskID
	Kind    TaskKind
	Label   string
	Phase   string
	Message string
	// Steps records the phases already finished, for a checklist display.
	Steps    []string
	Progress download.Progress
	Started  time.Time
	Done     bool
	Err      error
}

// Running reports whether the task is still in flight.
func (t Task) Running() bool { return !t.Done && t.Err == nil }

// GameState describes a running game.
type GameState struct {
	Instance string
	PID      int
	LogPath  string
	Started  time.Time
	Running  bool
	ExitErr  error
	// Tail is the most recent output, refreshed as the game runs.
	Tail []string
}

// Snapshot is an immutable view of the store, taken once per frame.
type Snapshot struct {
	Screen     Screen
	Accounts   []auth.Account
	Active     auth.Account
	HasAccount bool

	Instances []instance.Instance
	Selected  string
	Editing   instance.Meta
	EditingOK bool

	// Content is what the selected instance holds, per kind, and ContentFor
	// names the instance it was listed for so a stale listing is never shown
	// against a newly selected one.
	Content    map[instance.ContentKind][]instance.Entry
	ContentFor string
	// ProfileInstalled reports whether the selected instance's version
	// profile is in the store; a loader that is not gets installed on the
	// first launch.
	ProfileInstalled bool

	// MCVersions lists the Minecraft releases, newest first, once asked for.
	MCVersions []string
	// LoaderVersions holds the releases of one loader for one Minecraft
	// version, keyed by VersionsKey. VersionsPending marks a list on its way.
	LoaderVersions  map[string][]loader.Version
	VersionsPending map[string]bool

	Runtimes []java.Runtime
	Config   map[string]string

	Task  Task
	Game  GameState
	Login LoginState

	// MSAConfigured reports whether this build can offer Microsoft sign-in at
	// all; without an Azure application id it can only make local accounts.
	MSAConfigured bool

	Status string
	Err    error

	// Reclaimable is the measured shareable size per instance, filled in
	// lazily by a storage scan.
	Reclaimable map[string]int64
	StoreSize   int64
}

// Store holds the application state. It is written by the pump goroutine and
// read by the render loop, so every access takes the lock and Snapshot returns
// copies rather than views.
type Store struct {
	mu sync.RWMutex

	screen     Screen
	accounts   []auth.Account
	activeAcct string

	instances        []instance.Instance
	selected         string
	editing          instance.Meta
	editingOK        bool
	content          map[instance.ContentKind][]instance.Entry
	contentFor       string
	profileInstalled bool

	mcVersions      []string
	loaderVersions  map[string][]loader.Version
	versionsPending map[string]bool

	runtimes []java.Runtime
	config   map[string]string

	task  Task
	game  GameState
	login LoginState

	msaConfigured bool

	status      string
	err         error
	reclaimable map[string]int64
	storeSize   int64
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		screen:          ScreenInstances,
		config:          map[string]string{},
		reclaimable:     map[string]int64{},
		loaderVersions:  map[string][]loader.Version{},
		versionsPending: map[string]bool{},
	}
}

// Snapshot copies the state for one frame of rendering.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := Snapshot{
		Screen:           s.screen,
		Accounts:         append([]auth.Account(nil), s.accounts...),
		Instances:        append([]instance.Instance(nil), s.instances...),
		Selected:         s.selected,
		Editing:          s.editing,
		EditingOK:        s.editingOK,
		ContentFor:       s.contentFor,
		Content:          make(map[instance.ContentKind][]instance.Entry, len(s.content)),
		ProfileInstalled: s.profileInstalled,
		MCVersions:       append([]string(nil), s.mcVersions...),
		LoaderVersions:   make(map[string][]loader.Version, len(s.loaderVersions)),
		VersionsPending:  make(map[string]bool, len(s.versionsPending)),
		Runtimes:         append([]java.Runtime(nil), s.runtimes...),
		Config:           make(map[string]string, len(s.config)),
		Task:             s.task,
		Game:             s.game,
		Login:            s.login,
		MSAConfigured:    s.msaConfigured,
		Status:           s.status,
		Err:              s.err,
		Reclaimable:      make(map[string]int64, len(s.reclaimable)),
		StoreSize:        s.storeSize,
	}
	snap.Task.Steps = append([]string(nil), s.task.Steps...)
	snap.Game.Tail = append([]string(nil), s.game.Tail...)
	for k, v := range s.config {
		snap.Config[k] = v
	}
	for k, v := range s.reclaimable {
		snap.Reclaimable[k] = v
	}
	for k, v := range s.content {
		snap.Content[k] = append([]instance.Entry(nil), v...)
	}
	for k, v := range s.loaderVersions {
		snap.LoaderVersions[k] = append([]loader.Version(nil), v...)
	}
	for k, v := range s.versionsPending {
		snap.VersionsPending[k] = v
	}

	for _, a := range s.accounts {
		if a.UUID == s.activeAcct {
			snap.Active = a
			snap.HasAccount = true
			break
		}
	}
	return snap
}

// --- mutations, all called from the pump goroutine ---

func (s *Store) SetScreen(screen Screen) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.screen = screen
}

func (s *Store) SetInstances(list []instance.Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances = list

	// Keep the selection valid across a refresh.
	if s.selected != "" {
		for _, inst := range list {
			if inst.Name == s.selected {
				return
			}
		}
		s.selected = ""
	}
}

func (s *Store) SetSelected(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selected = name
}

// SetContent publishes the listing of one instance, replacing whatever was
// listed for any other.
func (s *Store) SetContent(name string, content map[instance.ContentKind][]instance.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contentFor = name
	s.content = content
}

func (s *Store) SetEditing(meta instance.Meta, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editing, s.editingOK = meta, ok
}

// SetProfileInstalled records whether the selected instance can launch
// without an install first.
func (s *Store) SetProfileInstalled(ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profileInstalled = ok
}

// SetMCVersions publishes the Minecraft release list.
func (s *Store) SetMCVersions(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mcVersions = ids
}

// SetLoaderVersions publishes one loader's releases for one game version.
func (s *Store) SetLoaderVersions(key string, versions []loader.Version) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaderVersions[key] = versions
	delete(s.versionsPending, key)
}

// SetVersionsPending marks a list as being fetched.
func (s *Store) SetVersionsPending(key string, pending bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pending {
		s.versionsPending[key] = true
	} else {
		delete(s.versionsPending, key)
	}
}

// VersionsKey names a loader's list for one game version. The Minecraft
// release list itself uses MCVersionsKey.
func VersionsKey(kind instance.LoaderType, mc string) string {
	return string(kind) + "|" + mc
}

// MCVersionsKey is the pending marker for the Minecraft release list.
const MCVersionsKey = "minecraft"

func (s *Store) SetAccounts(list []auth.Account, active string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts, s.activeAcct = list, active
}

// SetLogin publishes the state of an interactive sign-in.
func (s *Store) SetLogin(login LoginState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.login = login
}

// UpdateLogin changes the sign-in in flight, ignoring an update from one that
// has already been superseded or cancelled.
func (s *Store) UpdateLogin(id TaskID, apply func(*LoginState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.login.Active || s.login.Task != id {
		return
	}
	apply(&s.login)
}

// SetMSAConfigured records whether Microsoft sign-in is available.
func (s *Store) SetMSAConfigured(ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msaConfigured = ok
}

func (s *Store) SetRuntimes(list []java.Runtime) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimes = list
}

func (s *Store) SetConfig(cfg map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}

func (s *Store) SetStatus(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.err = msg, nil
}

func (s *Store) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *Store) SetTask(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.task = t
}

// UpdateTask applies a change to the current task, ignoring updates from a
// task that has already been superseded.
func (s *Store) UpdateTask(id TaskID, apply func(*Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.task.ID != id {
		return
	}
	apply(&s.task)
}

func (s *Store) SetGame(g GameState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.game = g
}

func (s *Store) UpdateGame(apply func(*GameState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	apply(&s.game)
}

func (s *Store) SetReclaimable(name string, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimable[name] = size
}

func (s *Store) SetStoreSize(size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeSize = size
}

// SelectedInstance returns the selected instance from a snapshot.
func (s Snapshot) SelectedInstance() (instance.Instance, bool) {
	for _, inst := range s.Instances {
		if inst.Name == s.Selected {
			return inst, true
		}
	}
	return instance.Instance{}, false
}

// ContentOf returns the listed entries of one kind for the selected
// instance, or nil while the listing is for another instance.
func (s Snapshot) ContentOf(kind instance.ContentKind) []instance.Entry {
	if s.ContentFor != s.Selected {
		return nil
	}
	return s.Content[kind]
}
