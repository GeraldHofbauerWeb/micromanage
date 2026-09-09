package gui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/micromanage/internal/instance"
	"github.com/GeraldHofbauerWeb/micromanage/internal/launcher"
)

// invalidateInterval bounds how often the window repaints in response to
// background progress.
//
// An asset download emits progress ten times a second and a harvest far more
// often; calling Invalidate per event would spend the whole frame budget on
// repaints. Terminal events bypass this and repaint immediately.
const invalidateInterval = 30 * time.Millisecond

// appID is the desktop identity of the window: it has to match the name of
// the installed .desktop file (see packaging/), or nothing links the two.
const appID = "micromanage"

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

	// The desktop matches a window to its .desktop file by this id — the
	// Wayland app_id, the X11 class — so it has to be the file's name. Left
	// to itself Gio uses the binary's name, and the dock then shows
	// "micromanage-launcher" under a blank icon.
	app.ID = appID

	w := new(app.Window)
	w.Option(
		app.Title("MicroManage"),
		app.Size(unit.Dp(1180), unit.Dp(760)),
		app.MinSize(unit.Dp(880), unit.Dp(560)),
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
			os.Stderr.WriteString("micromanage: " + err.Error() + "\n")
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
	ctrl    *launcher.Controller
	th      *Theme
	ic      iconSet
	version string

	// top bar
	home, brand, refresh, settings, account widget.Clickable

	rail      railState
	bench     workbench
	login     loginScreen
	setScreen settingsScreen
	dialogs   dialogs
	menu      contextMenu

	// pointer is where the pointer last was, in window coordinates. A
	// right-click knows only its own row; the menu it opens has to be
	// placed in the window.
	pointer    image.Point
	pointerTag byte
}

func newUI(ctrl *launcher.Controller) *ui {
	version := "dev"
	if ctrl != nil {
		version = ctrl.Version
	}
	return &ui{
		ctrl:      ctrl,
		version:   version,
		th:        NewTheme(),
		ic:        loadIcons(),
		rail:      newRail(),
		bench:     newWorkbench(),
		login:     newLoginScreen(),
		setScreen: newSettingsScreen(),
		dialogs:   newDialogs(),
	}
}

// Layout draws one frame, taking the snapshot once at the top so every widget
// in the frame sees the same state.
func (u *ui) Layout(gtx layout.Context) layout.Dimensions {
	return u.layoutSnapshot(gtx, u.ctrl.Store().Snapshot())
}

// goHome returns to the start screen: the instance list with nothing
// selected.
func (u *ui) goHome(snap launcher.Snapshot) {
	if u.ctrl == nil {
		return
	}
	u.ctrl.Store().SetScreen(launcher.ScreenInstances)
	if snap.Selected != "" {
		u.ctrl.Store().SetSelected("")
		u.ctrl.Store().SetContent("", nil)
	}
}

// dispatch hands an action to the controller. The offscreen renderer has
// none, and a menu item pressed there should do nothing rather than crash.
func (u *ui) dispatch(a launcher.Action) {
	if u.ctrl != nil {
		u.ctrl.Dispatch(a)
	}
}

// trackPointer remembers the pointer's window position. The handler is
// registered before anything else in the frame, so every later widget's
// events reach it too, whatever they do with them.
func (u *ui) trackPointer(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &u.pointerTag,
			Kinds: pointer.Move | pointer.Press | pointer.Drag | pointer.Enter})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			u.pointer = e.Position.Round()
		}
	}
	event.Op(gtx.Ops, &u.pointerTag)
}

// layoutSnapshot draws a frame from a given snapshot. Splitting it out lets
// the offscreen renderer draw a state without a live controller behind it.
func (u *ui) layoutSnapshot(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	u.trackPointer(gtx)

	// Bar clicks are read before layout so the frame already reflects them.
	if u.home.Clicked(gtx) {
		u.ctrl.Store().SetScreen(launcher.ScreenInstances)
	}
	if u.brand.Clicked(gtx) {
		u.goHome(snap)
	}
	if u.settings.Clicked(gtx) {
		u.ctrl.Store().SetScreen(launcher.ScreenSettings)
	}
	if u.refresh.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionRefresh{})
	}
	if u.account.Clicked(gtx) {
		u.ctrl.Store().SetScreen(launcher.ScreenLogin)
	}

	return fillMax(gtx, th.P.Bg, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					rigid(func(gtx layout.Context) layout.Dimensions { return u.layoutTopBar(gtx, snap) }),
					rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.LineDim) }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = gtx.Constraints.Max
						switch snap.Screen {
						case launcher.ScreenLogin:
							return u.login.Layout(gtx, u, snap)
						case launcher.ScreenSettings:
							return u.setScreen.Layout(gtx, u, snap)
						default:
							return u.layoutHome(gtx, snap)
						}
					}),
					rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.LineDim) }),
					rigid(func(gtx layout.Context) layout.Dimensions { return u.layoutStatusBar(gtx, snap) }),
				)
			}),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				return u.dialogs.Layout(gtx, u, snap)
			}),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				return u.menu.Layout(gtx, u)
			}),
		)
	})
}

// layoutTopBar draws the title row: the mark and the wordmark on the left,
// the ways out of the current screen on the right.
func (u *ui) layoutTopBar(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	return fill(gtx, th.P.Surface, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: sp2, Bottom: sp2, Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					if snap.Screen != launcher.ScreenInstances {
						return th.ghost(gtx, &u.home, u.ic.Back, "Instances")
					}
					// The mark and the wordmark are the way home: back to the
					// start screen with nothing selected.
					bg := color.NRGBA{}
					if u.brand.Hovered() {
						bg = th.P.Hover
					}
					return pressable(gtx, &u.brand, func(gtx layout.Context) layout.Dimensions {
						return fill(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(10)}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									return row(gtx, unit.Dp(10),
										rigid(func(gtx layout.Context) layout.Dimensions { return slab(gtx, th.P.Sky, unit.Dp(20)) }),
										rigid(func(gtx layout.Context) layout.Dimensions { return th.brand(gtx, "MicroManage", th.P.Text) }),
									)
								})
						})
					})
				}),
				flexFill(),
				rigid(func(gtx layout.Context) layout.Dimensions { return u.iconButton(gtx, &u.refresh, u.ic.Refresh) }),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if snap.Screen == launcher.ScreenSettings {
						return layout.Dimensions{}
					}
					return u.iconButton(gtx, &u.settings, u.ic.Settings)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					label, c := "Sign in", th.P.Torch
					if snap.HasAccount {
						label = snap.Active.Name
						c = th.P.Good
						if snap.Active.NeedsReauth {
							c = th.P.Bad
						}
					}
					return th.layoutButton(gtx, &u.account, buttonStyle{
						hoverBg: th.P.Hover, fg: th.P.Text, size: sizeSmall,
						inset: layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(12)},
					}, func(gtx layout.Context) layout.Dimensions {
						return row(gtx, sp2,
							rigid(func(gtx layout.Context) layout.Dimensions { return dot(gtx, c, unit.Dp(8)) }),
							rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, label, sizeSmall, 100, th.P.Text) }),
						)
					})
				}),
			)
		})
	})
}

func (u *ui) iconButton(gtx layout.Context, click *widget.Clickable, icon *widget.Icon) layout.Dimensions {
	return u.th.iconButton(gtx, click, icon)
}

// layoutStatusBar draws the bottom strip: what is happening, or what last
// happened, and a progress bar while a task runs.
func (u *ui) layoutStatusBar(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	return fill(gtx, th.P.Surface, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, sp3,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					switch {
					case snap.Err != nil:
						return row(gtx, unit.Dp(6),
							rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min = image.Pt(gtx.Dp(14), gtx.Dp(14))
								gtx.Constraints.Max = gtx.Constraints.Min
								return u.ic.Warning.Layout(gtx, th.P.Bad)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return th.text(gtx, snap.Err.Error(), sizeSmall, 0, th.P.Bad)
							}),
						)
					case snap.Game.Running:
						return row(gtx, unit.Dp(6),
							rigid(func(gtx layout.Context) layout.Dimensions { return dot(gtx, th.P.Torch, unit.Dp(7)) }),
							rigid(func(gtx layout.Context) layout.Dimensions {
								return th.text(gtx, fmt.Sprintf("%s is running", snap.Game.Instance), sizeSmall, 100, th.P.Text)
							}),
							rigid(func(gtx layout.Context) layout.Dimensions {
								if len(snap.Game.Tail) == 0 {
									return layout.Dimensions{}
								}
								return th.monoIn(gtx, "· "+lastLine(snap.Game.Tail), th.P.TextDim)
							}),
						)
					case taskActive(snap.Task):
						line := snap.Task.Label
						if detail := taskDetail(snap.Task); detail != "" && !strings.HasPrefix(detail, snap.Task.Label) {
							line += " · " + detail
						} else if detail != "" {
							line = detail
						}
						return th.text(gtx, line, sizeSmall, 0, th.P.TextMid)
					case snap.Status != "":
						return th.text(gtx, snap.Status, sizeSmall, 0, th.P.TextMid)
					default:
						return th.text(gtx, fmt.Sprintf("%d instances", len(snap.Instances)), sizeSmall, 0, th.P.TextDim)
					}
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if !taskActive(snap.Task) {
						return layout.Dimensions{}
					}
					gtx.Constraints.Max.X = gtx.Dp(unit.Dp(180))
					return th.progress(gtx, taskFraction(snap.Task), unit.Dp(4))
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.monoIn(gtx, u.version, th.P.TextDim)
				}),
			)
		})
	})
}

// taskActive reports whether a real task is in flight. The zero Task counts
// as running by its own definition, which is not the same as there being one.
func taskActive(t launcher.Task) bool {
	return t.ID != 0 && t.Running()
}

// taskDetail renders the phase and progress of a task in one line.
func taskDetail(t launcher.Task) string {
	detail := t.Phase
	if detail == "" {
		detail = "working"
	}
	if t.Message != "" {
		detail += " · " + t.Message
	}
	if t.Progress.FilesTotal > 0 {
		detail += fmt.Sprintf(" · %d/%d", t.Progress.FilesDone, t.Progress.FilesTotal)
	}
	return detail
}

// taskFraction converts a task's progress into a bar fraction.
func taskFraction(t launcher.Task) float32 {
	switch {
	case t.Progress.BytesTotal > 0:
		return float32(t.Progress.BytesDone) / float32(t.Progress.BytesTotal)
	case t.Progress.FilesTotal > 0:
		return float32(t.Progress.FilesDone) / float32(t.Progress.FilesTotal)
	}
	return 0
}

func lastLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	if len(line) > 90 {
		return line[:90] + "…"
	}
	return line
}

// humaniseSince renders a timestamp as a rough age.
func humaniseSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return t.Format("2 Jan 2006")
	}
}
