package gui

import (
	"os"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// invalidateInterval bounds how often the window repaints in response to
// background progress.
//
// An asset download emits progress ten times a second and a harvest far more
// often; calling Invalidate per event would spend the whole frame budget on
// repaints. Terminal events bypass this and repaint immediately.
const invalidateInterval = 30 * time.Millisecond

// Options configures the launcher window.
type Options struct {
	Version    string
	LauncherID string
	// MSAClientID is the Azure application id Microsoft sign-in runs against.
	// It is resolved at startup from, in order, the MIM_MSA_CLIENT_ID
	// environment variable, the manager configuration, and the id compiled
	// into the build — so a fork or a test build needs no recompile.
	MSAClientID string
}

// Run opens the launcher window and blocks until it closes.
func Run(opts Options) error {
	manager, err := instance.NewManager()
	if err != nil {
		return err
	}

	accounts, err := launcher.NewAccountStore(manager.AppDir)
	if err != nil {
		return err
	}

	store := launcher.NewStore()
	ctrl := launcher.NewController(manager, store, accounts, opts.Version)
	ctrl.LauncherID = opts.LauncherID
	ctrl.MSAClientID = resolveMSAClientID(opts.MSAClientID, manager)
	store.SetMSAConfigured(ctrl.MSAConfigured())

	w := new(app.Window)
	w.Option(
		app.Title("Minecraft Instance Manager"),
		app.Size(unit.Dp(1100), unit.Dp(720)),
		app.MinSize(unit.Dp(820), unit.Dp(560)),
	)

	go pump(ctrl, w)
	ctrl.Dispatch(launcher.ActionRefresh{})

	ui := newUI(ctrl)

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// resolveMSAClientID picks the Azure application id to sign in with.
//
// The environment wins so a player can try a different registration without a
// rebuild, then the saved configuration, then whatever the build was stamped
// with.
func resolveMSAClientID(compiledIn string, manager *instance.Manager) string {
	if id := strings.TrimSpace(os.Getenv("MIM_MSA_CLIENT_ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(manager.GetConfig()["msa-client-id"]); id != "" {
		return id
	}
	return strings.TrimSpace(compiledIn)
}

// pump drains controller events into the store and coalesces repaints.
//
// Applying every event immediately keeps the state current; batching the
// repaint is what stops thousands of progress updates from starving the
// render loop.
func pump(ctrl *launcher.Controller, w *app.Window) {
	ticker := time.NewTicker(invalidateInterval)
	defer ticker.Stop()

	dirty := false
	for {
		select {
		case e, ok := <-ctrl.Events():
			if !ok {
				return
			}
			if e.Apply != nil {
				e.Apply(ctrl.Store())
			}
			if e.Terminal {
				dirty = false
				w.Invalidate()
			} else {
				dirty = true
			}
		case <-ticker.C:
			if dirty {
				dirty = false
				w.Invalidate()
			}
		}
	}
}

// RunMain is the entry point for the GUI binary: it starts the window on its
// own goroutine and hands the main one to Gio, which several platforms
// require.
func RunMain(opts Options) {
	go func() {
		if err := Run(opts); err != nil {
			// Reported on stderr because there may be no window to show it in.
			os.Stderr.WriteString("minecraft-instance-manager: " + err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

// ui holds every persistent widget. In immediate mode a widget's state lives
// between frames, so nothing here may be constructed inside Layout — an editor
// rebuilt each frame would lose what the user typed.
type ui struct {
	ctrl  *launcher.Controller
	th    *Theme
	nav   navState
	login loginScreen
	list  instancesScreen
	edit  editScreen
	set   settingsScreen
}

func newUI(ctrl *launcher.Controller) *ui {
	th := NewTheme()
	return &ui{
		ctrl:  ctrl,
		th:    th,
		nav:   newNavState(),
		login: newLoginScreen(),
		list:  newInstancesScreen(),
		edit:  newEditScreen(),
		set:   newSettingsScreen(),
	}
}

// Layout draws one frame, taking the snapshot once at the top so every widget
// in the frame sees the same state.
func (u *ui) Layout(gtx layout.Context) layout.Dimensions {
	return u.layoutSnapshot(gtx, u.ctrl.Store().Snapshot())
}

// layoutSnapshot draws a frame from a given snapshot. Splitting it out lets the
// offscreen renderer draw a state without a live controller behind it.
func (u *ui) layoutSnapshot(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	return fill(gtx, u.th.P.Bg, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.layoutTopBar(gtx, snap)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(SpaceM).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return u.layoutContent(gtx, snap)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.layoutStatusBar(gtx, snap)
			}),
		)
	})
}

func (u *ui) layoutContent(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	// A flex child gets no minimum on the cross axis, so a screen that centres
	// itself would otherwise centre inside its own width and sit on the left.
	gtx.Constraints.Min = gtx.Constraints.Max

	switch snap.Screen {
	case launcher.ScreenLogin:
		return u.login.Layout(gtx, u.th, u.ctrl, snap)
	case launcher.ScreenEdit:
		return u.edit.Layout(gtx, u.th, u.ctrl, snap)
	case launcher.ScreenSettings:
		return u.set.Layout(gtx, u.th, u.ctrl, snap)
	default:
		return u.list.Layout(gtx, u.th, u.ctrl, snap)
	}
}
