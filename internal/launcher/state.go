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
)

// Screen identifies the visible page.
type Screen int

const (
	ScreenLogin Screen = iota
	ScreenInstances
	ScreenEdit
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

	// LoaderOverride is a per-launch choice that is not written to the
	// instance. Empty means the instance's own default is used.
	LoaderOverride instance.LoaderSpec
	HasOverride    bool

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

	instances      []instance.Instance
	selected       string
	editing        instance.Meta
	editingOK      bool
	loaderOverride instance.LoaderSpec
	hasOverride    bool

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
		screen:      ScreenInstances,
		config:      map[string]string{},
		reclaimable: map[string]int64{},
	}
}

// Snapshot copies the state for one frame of rendering.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := Snapshot{
		Screen:         s.screen,
		Accounts:       append([]auth.Account(nil), s.accounts...),
		Instances:      append([]instance.Instance(nil), s.instances...),
		Selected:       s.selected,
		Editing:        s.editing,
		EditingOK:      s.editingOK,
		LoaderOverride: s.loaderOverride,
		HasOverride:    s.hasOverride,
		Runtimes:       append([]java.Runtime(nil), s.runtimes...),
		Config:         make(map[string]string, len(s.config)),
		Task:           s.task,
		Game:           s.game,
		Login:          s.login,
		MSAConfigured:  s.msaConfigured,
		Status:         s.status,
		Err:            s.err,
		Reclaimable:    make(map[string]int64, len(s.reclaimable)),
		StoreSize:      s.storeSize,
	}
	snap.Task.Steps = append([]string(nil), s.task.Steps...)
	snap.Game.Tail = append([]string(nil), s.game.Tail...)
	for k, v := range s.config {
		snap.Config[k] = v
	}
	for k, v := range s.reclaimable {
		snap.Reclaimable[k] = v
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
	if s.selected != name {
		// A new selection drops a launch-only loader override, which belonged
		// to the instance being left.
		s.loaderOverride = instance.LoaderSpec{}
		s.hasOverride = false
	}
	s.selected = name
}

func (s *Store) SetEditing(meta instance.Meta, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editing, s.editingOK = meta, ok
}

func (s *Store) SetLoaderOverride(spec instance.LoaderSpec, has bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaderOverride, s.hasOverride = spec, has
}

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

// EffectiveLoader returns the loader a launch would use: the override when one
// is set, otherwise the instance's own default.
func (s Snapshot) EffectiveLoader() instance.LoaderSpec {
	if s.HasOverride {
		return s.LoaderOverride
	}
	return s.Editing.Loader
}
