package gui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// loginScreen is step 1: sign in, but only when there is no usable account.
type loginScreen struct {
	name       *widget.Editor
	microsoft  widget.Clickable
	offline    widget.Clickable
	copyCode   widget.Clickable
	openLink   widget.Clickable
	cancel     widget.Clickable
	useAccount []widget.Clickable
	signOut    []widget.Clickable
	continueTo widget.Clickable

	// copiedAt marks when the code was last copied, so the button can confirm
	// it happened; a clipboard write is otherwise invisible.
	copiedAt time.Time
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
	if s.microsoft.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionLoginMicrosoft{})
	}
	if s.continueTo.Clicked(gtx) {
		ctrl.Store().SetScreen(launcher.ScreenInstances)
	}

	if snap.Login.Active {
		if s.cancel.Clicked(gtx) {
			ctrl.Cancel(snap.Login.Task)
		}
		if s.copyCode.Clicked(gtx) {
			gtx.Execute(clipboard.WriteCmd{
				Type: "application/text",
				Data: io.NopCloser(strings.NewReader(snap.Login.UserCode)),
			})
			s.copiedAt = time.Now()
		}
		if s.openLink.Clicked(gtx) && snap.Login.VerificationURI != "" {
			if err := openURL(snap.Login.VerificationURI); err != nil {
				ctrl.Store().SetError(fmt.Errorf("could not open a browser: %w", err))
			}
		}
		// The panel counts the code's remaining life down, which only ticks if
		// the frame is redrawn while nothing else is happening.
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(time.Second)})
	}

	// One clickable per account, kept across frames.
	for len(s.useAccount) < len(snap.Accounts) {
		s.useAccount = append(s.useAccount, widget.Clickable{})
		s.signOut = append(s.signOut, widget.Clickable{})
	}
	for i := range snap.Accounts {
		account := snap.Accounts[i]
		if s.useAccount[i].Clicked(gtx) {
			go func() {
				_ = ctrl.Accounts.SetActive(account.UUID)
				ctrl.Dispatch(launcher.ActionRefresh{})
				ctrl.Store().SetScreen(launcher.ScreenInstances)
			}()
		}
		if s.signOut[i].Clicked(gtx) {
			ctrl.Dispatch(launcher.ActionSignOut{UUID: account.UUID})
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
					// A sign-in in flight owns the screen: the player has a
					// code to enter and a browser to switch to, and the other
					// options would only be in the way.
					if snap.Login.Active {
						return s.layoutDeviceCode(gtx, th, snap)
					}
					return s.layoutChoices(gtx, th, snap)
				}),
			)
		})
	})
}

// layoutChoices shows the accounts already signed in and the ways to add one.
func (s *loginScreen) layoutChoices(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	return column(gtx, SpaceM,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return s.layoutExisting(gtx, th, snap)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return s.layoutMicrosoft(gtx, th, snap)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "A local account plays single-player in full; "+
				"only servers running in online mode reject it.")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.editor(gtx, s.name, "Player name", "Steve")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, SpaceS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.secondary(gtx, &s.offline, "Use a local account")
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
}

// layoutMicrosoft offers the online sign-in, or explains why it is missing.
func (s *loginScreen) layoutMicrosoft(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	if !snap.MSAConfigured {
		return th.dim(gtx, "Microsoft sign-in needs an Azure application id, which is not "+
			"configured in this build. Set one under Settings → Microsoft application id "+
			"(or in MIM_MSA_CLIENT_ID) to play on online servers.")
	}

	return column(gtx, SpaceS,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.button(gtx, &s.microsoft, "Sign in with Microsoft")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "Opens microsoft.com in your browser, where you enter a "+
				"code. Needed for servers running in online mode.")
		}),
	)
}

// layoutDeviceCode shows the code the player has to enter, and where.
func (s *loginScreen) layoutDeviceCode(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	login := snap.Login

	copyLabel := "Copy code"
	if time.Since(s.copiedAt) < 3*time.Second {
		copyLabel = "Copied"
	}

	return column(gtx, SpaceM,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			where := login.VerificationURI
			if where == "" {
				where = "microsoft.com/link"
			}
			return th.label(gtx, "Open "+where+" and enter this code:")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return s.layoutCode(gtx, th, login.UserCode)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, SpaceS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.button(gtx, &s.openLink, "Open browser")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.secondary(gtx, &s.copyCode, copyLabel)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.secondary(gtx, &s.cancel, "Cancel")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, login.Step)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			remaining := time.Until(login.ExpiresAt).Round(time.Second)
			if login.ExpiresAt.IsZero() || remaining <= 0 {
				return layout.Dimensions{}
			}
			return th.dim(gtx, fmt.Sprintf("The code is valid for %s.", formatCountdown(remaining)))
		}),
	)
}

// layoutCode renders the code itself, large enough to read off the screen
// while typing it somewhere else.
func (s *loginScreen) layoutCode(gtx layout.Context, th *Theme, code string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return fill(gtx, th.P.Bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
		return border(gtx, th.P.Border, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(SpaceM).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.H4(th.Theme, code)
					l.Color = th.P.Accent
					return l.Layout(gtx)
				})
			})
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
			return row(gtx, SpaceS,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return th.clickableRow(gtx, &s.useAccount[i], active, func(gtx layout.Context) layout.Dimensions {
						return row(gtx, SpaceS,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return th.label(gtx, account.Name)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{Size: gtx.Constraints.Min}
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								// A stale Microsoft session is the one thing a
								// player has to act on, so it outranks the
								// kind badge.
								if account.NeedsReauth {
									return th.chip(gtx, "sign in again", th.P.Bad, th.P.Bg)
								}
								if account.Kind == "offline" {
									return th.chip(gtx, "local", th.P.TextDim, th.P.Bg)
								}
								return th.chip(gtx, "Microsoft", th.P.Good, th.P.Bg)
							}),
						)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.danger(gtx, &s.signOut[i], "Sign out")
				}),
			)
		}))
	}

	return column(gtx, SpaceS, children...)
}

// formatCountdown renders a duration as minutes and seconds.
func formatCountdown(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%d seconds", seconds)
	}
	return fmt.Sprintf("%d:%02d minutes", minutes, seconds)
}
