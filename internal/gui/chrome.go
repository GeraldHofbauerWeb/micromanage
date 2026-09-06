package gui

import (
	"fmt"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// navState holds the persistent widgets of the window frame.
type navState struct {
	home     widget.Clickable
	settings widget.Clickable
	account  widget.Clickable
	refresh  widget.Clickable
}

func newNavState() navState { return navState{} }

// layoutTopBar draws the title row: navigation on the left, the signed-in
// account on the right.
func (u *ui) layoutTopBar(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	th := u.th

	if u.nav.home.Clicked(gtx) {
		u.ctrl.Store().SetScreen(launcher.ScreenInstances)
	}
	if u.nav.settings.Clicked(gtx) {
		u.ctrl.Store().SetScreen(launcher.ScreenSettings)
	}
	if u.nav.refresh.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionRefresh{})
	}
	if u.nav.account.Clicked(gtx) {
		u.ctrl.Store().SetScreen(launcher.ScreenLogin)
	}

	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	return fill(gtx, th.P.Panel, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Top: SpaceS, Bottom: SpaceS, Left: SpaceM, Right: SpaceM,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, SpaceS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.heading(gtx, "Minecraft")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.dim(gtx, "Instance Manager")
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Min}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if snap.Screen == launcher.ScreenInstances {
						return layout.Dimensions{}
					}
					return th.secondary(gtx, &u.nav.home, "Instances")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.secondary(gtx, &u.nav.refresh, "Refresh")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.secondary(gtx, &u.nav.settings, "Settings")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					label := "Sign in"
					if snap.HasAccount {
						label = snap.Active.Name
					}
					return th.secondary(gtx, &u.nav.account, label)
				}),
			)
		})
	})
}

// layoutStatusBar draws the bottom strip: the current task, the running game,
// or the last message.
func (u *ui) layoutStatusBar(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	th := u.th

	// A rigid child is only as wide as its content, so the bar has to claim
	// the full width itself or it renders as a stub behind the text.
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	return fill(gtx, th.P.Panel, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Top: SpaceS, Bottom: SpaceS, Left: SpaceM, Right: SpaceM,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			switch {
			case snap.Err != nil:
				return th.coloured(gtx, snap.Err.Error(), th.P.Bad)
			case snap.Game.Running:
				return th.coloured(gtx, fmt.Sprintf(
					"Minecraft is running — %s (pid %d)", snap.Game.Instance, snap.Game.PID), th.P.Good)
			case snap.Task.Running() && snap.Task.Label != "":
				return th.dim(gtx, snap.Task.Label+" — "+taskDetail(snap.Task))
			case snap.Status != "":
				return th.dim(gtx, snap.Status)
			default:
				return th.dim(gtx, fmt.Sprintf("%d instances", len(snap.Instances)))
			}
		})
	})
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

// errorBanner shows an error prominently inside a screen.
func (t *Theme) errorBanner(gtx layout.Context, err error) layout.Dimensions {
	if err == nil {
		return layout.Dimensions{}
	}
	return fill(gtx, withAlpha(t.P.Bad, 0x30), unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(SpaceS).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return t.coloured(gtx, err.Error(), t.P.Bad)
		})
	})
}
