package gui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// loginScreen signs a player in, and lists who already is.
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

	// copiedAt marks when the code was last copied, so the button can
	// confirm it happened; a clipboard write is otherwise invisible.
	copiedAt time.Time
}

func newLoginScreen() loginScreen {
	return loginScreen{name: newEditor()}
}

func (s *loginScreen) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	ctrl := u.ctrl

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
		ctrl.Dispatch(launcher.ActionLoginOffline{Name: strings.TrimSpace(s.name.Text())})
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
			ctrl.Dispatch(launcher.ActionOpen{Path: snap.Login.VerificationURI})
		}
		// The panel counts the code's remaining life down, which only ticks
		// if the frame is redrawn while nothing else is happening.
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(time.Second)})
	}

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

		return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return column(gtx, sp3,
				rigid(func(gtx layout.Context) layout.Dimensions {
					if snap.Login.Active {
						return th.display(gtx, "Enter the code")
					}
					if snap.HasAccount {
						return th.display(gtx, "Accounts")
					}
					return th.display(gtx, "Who is playing?")
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if snap.Err == nil {
						return layout.Dimensions{}
					}
					return th.notice(gtx, u.ic.Warning, snap.Err.Error(), th.P.Bad)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if snap.Login.Active {
						return s.layoutDeviceCode(gtx, u, snap)
					}
					return s.layoutChoices(gtx, u, snap)
				}),
			)
		})
	})
}

func (s *loginScreen) layoutChoices(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	return column(gtx, sp3,
		rigid(func(gtx layout.Context) layout.Dimensions { return s.layoutExisting(gtx, u, snap) }),
		rigid(func(gtx layout.Context) layout.Dimensions { return s.layoutMicrosoft(gtx, u, snap) }),
		rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.LineDim) }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, "A local account plays single-player in full. "+
				"Only servers running in online mode turn it away.", th.P.TextDim)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return th.field(gtx, s.name, "Player name", "Steve")
				}),
				hspacer(sp2),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.offline, "Play locally") }),
			)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			if !snap.HasAccount {
				return layout.Dimensions{}
			}
			return th.ghost(gtx, &s.continueTo, u.ic.Back, "Back to instances")
		}),
	)
}

func (s *loginScreen) layoutMicrosoft(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	if !snap.MSAConfigured {
		return th.notice(gtx, u.ic.Info, "Microsoft sign-in needs an Azure application id, which this build "+
			"does not have. Set one under Settings → Microsoft application id (or in MIM_MSA_CLIENT_ID) "+
			"to play on online servers.", th.P.TextMid)
	}
	return column(gtx, sp2,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.primary(gtx, &s.microsoft, u.ic.Account, "Sign in with Microsoft")
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, "Opens microsoft.com in your browser, where you enter a short code. "+
				"Needed for online servers.", th.P.TextDim)
		}),
	)
}

// layoutDeviceCode shows the code the player has to enter, and where.
func (s *loginScreen) layoutDeviceCode(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	login := snap.Login

	copyLabel := "Copy code"
	if time.Since(s.copiedAt) < 3*time.Second {
		copyLabel = "Copied"
	}

	return column(gtx, sp3,
		rigid(func(gtx layout.Context) layout.Dimensions {
			where := login.VerificationURI
			if where == "" {
				where = "microsoft.com/link"
			}
			return th.mid(gtx, "Open "+where+" and type this code:")
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return fill(gtx, th.P.Bg, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
				return outlined(gtx, th.P.Line, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(sp4).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(th.Theme, unit.Sp(34), login.UserCode)
							l.Font.Typeface = faceMono
							l.Font.Weight = font.Medium
							l.Color = th.P.Torch
							return l.Layout(gtx)
						})
					})
				})
			})
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.primary(gtx, &s.openLink, u.ic.OpenInNew, "Open browser")
				}),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.copyCode, copyLabel) }),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &s.cancel, nil, "Cancel") }),
			)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, login.Step) }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			remaining := time.Until(login.ExpiresAt).Round(time.Second)
			if login.ExpiresAt.IsZero() || remaining <= 0 {
				return layout.Dimensions{}
			}
			return th.small(gtx, fmt.Sprintf("The code is valid for %s.", formatCountdown(remaining)))
		}),
	)
}

// layoutExisting lists the accounts already signed in.
func (s *loginScreen) layoutExisting(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	if len(snap.Accounts) == 0 {
		return layout.Dimensions{}
	}

	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, "Signed in") }),
	}
	for i, account := range snap.Accounts {
		i, account := i, account
		active := account.UUID == snap.Active.UUID
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					border := th.P.LineDim
					if active {
						border = th.P.Sky
					}
					return pressable(gtx, &s.useAccount[i], func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return fill(gtx, th.P.Raised, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
							return outlined(gtx, border, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(9), Bottom: unit.Dp(9), Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									gtx.Constraints.Min.X = gtx.Constraints.Max.X
									return row(gtx, sp2,
										rigid(func(gtx layout.Context) layout.Dimensions { return th.bodyMedium(gtx, account.Name) }),
										flexFill(),
										rigid(func(gtx layout.Context) layout.Dimensions {
											switch {
											case account.NeedsReauth:
												return th.chip(gtx, "sign in again", th.P.Bad)
											case account.Kind == auth.KindOffline:
												return th.chip(gtx, "local", th.P.TextMid)
											}
											return th.chip(gtx, "Microsoft", th.P.Good)
										}),
									)
								})
							})
						})
					})
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.danger(gtx, &s.signOut[i], u.ic.SignOut, "Sign out")
				}),
			)
		}))
	}
	return column(gtx, sp2, children...)
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
