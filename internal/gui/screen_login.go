package gui

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// loginScreen is step 1: sign in, but only when there is no usable account.
type loginScreen struct {
	name       *widget.Editor
	signIn     widget.Clickable
	offline    widget.Clickable
	useAccount []widget.Clickable
	continueTo widget.Clickable
}

func newLoginScreen() loginScreen {
	return loginScreen{name: newEditor()}
}

func (s *loginScreen) Layout(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	// Enter in the name field is the same as pressing the button.
	submitted := false
	for {
		ev, ok := s.name.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if s.offline.Clicked(gtx) || submitted {
		ctrl.Dispatch(launcher.ActionLoginOffline{Name: s.name.Text()})
	}
	if s.continueTo.Clicked(gtx) {
		ctrl.Store().SetScreen(launcher.ScreenInstances)
	}

	// One clickable per account, kept across frames.
	for len(s.useAccount) < len(snap.Accounts) {
		s.useAccount = append(s.useAccount, widget.Clickable{})
	}
	for i := range snap.Accounts {
		if s.useAccount[i].Clicked(gtx) {
			account := snap.Accounts[i]
			go func() {
				_ = ctrl.Accounts.SetActive(account.UUID)
				ctrl.Dispatch(launcher.ActionRefresh{})
				ctrl.Store().SetScreen(launcher.ScreenInstances)
			}()
		}
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(520)))
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
			return column(gtx, SpaceM,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.heading(gtx, "Sign in")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.errorBanner(gtx, snap.Err)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return s.layoutExisting(gtx, th, snap)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.dim(gtx,
						"Microsoft sign-in needs an Azure application id, which is not "+
							"configured in this build. A local account plays single-player "+
							"in full; only online servers reject it.")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.editor(gtx, s.name, "Player name", "Steve")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return row(gtx, SpaceS,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.button(gtx, &s.offline, "Use a local account")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !snap.HasAccount {
								return layout.Dimensions{}
							}
							return th.secondary(gtx, &s.continueTo, "Back to instances")
						}),
					)
				}),
			)
		})
	})
}

// layoutExisting lists the accounts already signed in.
func (s *loginScreen) layoutExisting(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	if len(snap.Accounts) == 0 {
		return layout.Dimensions{}
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "Signed in")
		}),
	}
	for i, account := range snap.Accounts {
		active := account.UUID == snap.Active.UUID
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.clickableRow(gtx, &s.useAccount[i], active, func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, account.Name)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: gtx.Constraints.Min}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if account.Kind == "offline" {
							return th.chip(gtx, "local", th.P.TextDim, th.P.Bg)
						}
						return th.chip(gtx, "Microsoft", th.P.Good, th.P.Bg)
					}),
				)
			})
		}))
	}

	return column(gtx, SpaceS, children...)
}
