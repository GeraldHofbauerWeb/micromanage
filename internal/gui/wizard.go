package gui

import (
	"image"
	"path/filepath"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launcher"
)

// wizardStep is where the first-start wizard is.
type wizardStep int

const (
	// wizardChoose offers the two ways to begin.
	wizardChoose wizardStep = iota
	// wizardImport asks what to bring along from .minecraft.
	wizardImport
)

// wizard is the start screen before there is any instance: the first one is
// either a copy of the official launcher's .minecraft or an empty one, made
// with the usual dialog.
type wizard struct {
	step wizardStep

	chooseImport, chooseEmpty widget.Clickable

	name                     *widget.Editor
	saves, screenshots       bool
	toggleSaves, toggleShots widget.Clickable
	start, back, cancel      widget.Clickable
}

func newWizard() wizard {
	w := wizard{name: newEditor(), saves: true, screenshots: true}
	w.name.SetText(instance.DefaultInstanceName)
	return w
}

// headline is the line under the title while the wizard is showing.
func (w *wizard) headline(snap launcher.Snapshot) string {
	switch {
	case importing(snap):
		return "Copying your " + minecraftDir(snap) + " into a new instance…"
	case w.step == wizardImport && snap.CanImport:
		return "Your " + minecraftDir(snap) + " is copied, never changed."
	case snap.CanImport:
		return "Bring your Minecraft along, or start fresh."
	}
	return "Make your first instance to get started."
}

// Layout draws the part under the hero: the two ways to begin, the import's
// options, or the import in progress.
func (w *wizard) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	if w.chooseImport.Clicked(gtx) {
		w.step = wizardImport
	}
	if w.chooseEmpty.Clicked(gtx) {
		u.dialogs.openCreate(snap, "")
	}
	if w.back.Clicked(gtx) {
		w.step = wizardChoose
	}
	if w.toggleSaves.Clicked(gtx) {
		w.saves = !w.saves
	}
	if w.toggleShots.Clicked(gtx) {
		w.screenshots = !w.screenshots
	}
	if w.cancel.Clicked(gtx) && u.ctrl != nil {
		u.ctrl.Cancel(snap.Task.ID)
	}
	name := strings.TrimSpace(w.name.Text())
	if w.start.Clicked(gtx) && name != "" {
		u.dispatch(launcher.ActionImport{Name: name, IncludeSaves: w.saves, IncludeScreenshots: w.screenshots})
		w.step = wizardChoose
	}
	if !snap.CanImport {
		w.step = wizardChoose
	}

	switch {
	case importing(snap):
		return w.layoutProgress(gtx, u, snap)
	case w.step == wizardImport:
		return w.layoutImport(gtx, u, snap, name != "")
	case !snap.CanImport:
		// Nothing to import: the one way to begin is the dialog.
		return th.primary(gtx, &w.chooseEmpty, u.ic.Add, "New instance")
	}

	card := func(click *widget.Clickable, icon *widget.Icon, title, text string, lead bool) layout.FlexChild {
		return rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(290))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return choiceCard(gtx, th, click, icon, title, text, lead)
		})
	}
	return row(gtx, sp3,
		card(&w.chooseImport, u.ic.Download, "Import your "+minecraftDir(snap),
			"Copies your worlds, mods and settings into a new instance. The official launcher keeps its folder exactly as it is.", true),
		card(&w.chooseEmpty, u.ic.Add, "Start empty",
			"A fresh instance with the version and mod loader you pick. Ready at once.", false),
	)
}

// layoutImport asks for a name and what to bring along.
func (w *wizard) layoutImport(gtx layout.Context, u *ui, snap launcher.Snapshot, ready bool) layout.Dimensions {
	th := u.th
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(460))
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, sp3,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Import your "+minecraftDir(snap)) }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.field(gtx, w.name, "Instance name", instance.DefaultInstanceName)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return column(gtx, unit.Dp(6),
					rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, "Also copy") }),
					rigid(func(gtx layout.Context) layout.Dimensions {
						return row(gtx, unit.Dp(6),
							rigid(func(gtx layout.Context) layout.Dimensions { return th.pill(gtx, &w.toggleSaves, w.saves, "Worlds") }),
							rigid(func(gtx layout.Context) layout.Dimensions {
								return th.pill(gtx, &w.toggleShots, w.screenshots, "Screenshots")
							}),
						)
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.wrapped(gtx, "Mods, configs, resource and shader packs and the game options always come along; "+
					"the game files go into the shared store, and the version is read from the official launcher. "+
					snap.Config["minecraft-path"]+" is only read.", th.P.TextDim)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions {
						if !ready {
							return th.secondary(gtx, &w.start, "Import")
						}
						return th.primary(gtx, &w.start, u.ic.Download, "Import")
					}),
					rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &w.back, nil, "Back") }),
				)
			}),
		)
	})
}

// layoutProgress shows the import running: its bar, what it is on, and a
// way to stop it, which leaves no half-made instance behind.
func (w *wizard) layoutProgress(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(360))
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.progress(gtx, taskFraction(snap.Task), unit.Dp(3))
		}),
		spacer(sp2),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.monoIn(gtx, truncateMiddle(taskDetail(snap.Task), 56), th.P.TextDim)
		}),
		spacer(sp3),
		rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &w.cancel, nil, "Cancel") }),
	)
}

// choiceCard is a card that is a button: an icon, a title and what choosing
// it does. The lead card carries the accent border.
func choiceCard(gtx layout.Context, th *Theme, click *widget.Clickable, icon *widget.Icon, title, text string, lead bool) layout.Dimensions {
	bg, border := th.P.Surface, th.P.LineDim
	if lead {
		border = th.P.Sky
	}
	if click.Hovered() {
		bg = th.P.Hover
	}
	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			return outlined(gtx, border, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(sp4).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return column(gtx, sp2,
						rigid(func(gtx layout.Context) layout.Dimensions {
							return row(gtx, sp2,
								rigid(func(gtx layout.Context) layout.Dimensions {
									gtx.Constraints.Min = image.Pt(gtx.Dp(18), gtx.Dp(18))
									gtx.Constraints.Max = gtx.Constraints.Min
									c := th.P.TextMid
									if lead {
										c = th.P.Sky
									}
									return icon.Layout(gtx, c)
								}),
								rigid(func(gtx layout.Context) layout.Dimensions { return th.bodyMedium(gtx, title) }),
							)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions { return th.wrapped(gtx, text, th.P.TextMid) }),
					)
				})
			})
		})
	})
}

// importing reports whether an import is running.
func importing(snap launcher.Snapshot) bool {
	return taskActive(snap.Task) && snap.Task.Kind == launcher.TaskImport
}

// minecraftDir names the official launcher's directory the way the player
// knows it: ".minecraft", or "minecraft" on macOS.
func minecraftDir(snap launcher.Snapshot) string {
	if p := snap.Config["minecraft-path"]; p != "" {
		return filepath.Base(p)
	}
	return ".minecraft"
}

// truncateMiddle shortens a line to about max runes, keeping both ends.
func truncateMiddle(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	half := (max - 1) / 2
	return string(r[:half]) + "…" + string(r[len(r)-half:])
}
