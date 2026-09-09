package gui

import (
	"fmt"
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// railWidth is the instance list's width. Wide enough for a long pack name
// and its version line, narrow enough to leave the workbench the room.
const railWidth = unit.Dp(272)

// layoutHome is the main screen: the instances on the left, the selected one
// laid out as a workbench on the right.
func (u *ui) layoutHome(gtx layout.Context, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(railWidth)
			gtx.Constraints.Min = gtx.Constraints.Max
			return fill(gtx, th.P.Surface, 0, func(gtx layout.Context) layout.Dimensions {
				return u.rail.Layout(gtx, u, snap)
			})
		}),
		rigid(func(gtx layout.Context) layout.Dimensions { return vline(gtx, th.P.LineDim) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = gtx.Constraints.Max
			return u.bench.Layout(gtx, u, snap)
		}),
	)
}

// --- rail ---

// railState holds the instance list's widgets.
type railState struct {
	list *widget.List
	rows []widget.Clickable
	add  widget.Clickable
}

func newRail() railState { return railState{list: newList()} }

func (r *railState) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	for len(r.rows) < len(snap.Instances) {
		r.rows = append(r.rows, widget.Clickable{})
	}
	for i := range snap.Instances {
		if r.rows[i].Clicked(gtx) {
			u.ctrl.Dispatch(launcher.ActionSelect{Name: snap.Instances[i].Name})
		}
	}
	if r.add.Clicked(gtx) {
		u.dialogs.openCreate(snap, "")
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: sp3, Bottom: sp2, Left: sp3, Right: sp2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions {
						return th.smallIn(gtx, fmt.Sprintf("INSTANCES · %d", len(snap.Instances)), th.P.TextDim)
					}),
					flexFill(),
					rigid(func(gtx layout.Context) layout.Dimensions { return th.iconButton(gtx, &r.add, u.ic.Add) }),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(snap.Instances) == 0 {
				return layout.UniformInset(sp3).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return th.wrapped(gtx, "No instances yet. Press + to make one.", th.P.TextDim)
				})
			}
			return th.list(gtx, r.list, len(snap.Instances), func(gtx layout.Context, i int) layout.Dimensions {
				return r.layoutRow(gtx, u, snap, i)
			})
		}),
	)
}

// layoutRow draws one instance: its loader-coloured mark, its name, and
// what it runs.
func (r *railState) layoutRow(gtx layout.Context, u *ui, snap launcher.Snapshot, i int) layout.Dimensions {
	th := u.th
	inst := snap.Instances[i]
	selected := inst.Name == snap.Selected
	running := snap.Game.Running && snap.Game.Instance == inst.Name

	return th.selectableRow(gtx, &r.rows[i], selected, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, unit.Dp(12),
				rigid(func(gtx layout.Context) layout.Dimensions {
					c := th.loaderColor(inst.Loader.Type)
					if !inst.Configured {
						c = th.P.TextDim
					}
					return slab(gtx, c, unit.Dp(22))
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return column(gtx, unit.Dp(2),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if selected {
								return th.bodyMedium(gtx, inst.Name)
							}
							return th.text(gtx, inst.Name, sizeBody, 100, th.P.TextMid)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if !inst.Configured {
								return th.monoIn(gtx, "not set up", th.P.TextDim)
							}
							return th.monoIn(gtx, inst.MinecraftVersion+" · "+inst.Loader.Type.Display(), th.P.TextDim)
						}),
					)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					switch {
					case running:
						return dot(gtx, th.P.Torch, unit.Dp(8))
					case inst.IsActive:
						return dot(gtx, th.P.Good, unit.Dp(6))
					}
					return layout.Dimensions{}
				}),
			)
		})
	})
}

// --- workbench ---

// benchTab is one tab of the workbench: a content kind, or the settings.
type benchTab struct {
	kind     instance.ContentKind
	settings bool
}

func (t benchTab) label() string {
	if t.settings {
		return "Settings"
	}
	return t.kind.Label()
}

// benchTabs lists the tabs in order.
func benchTabs() []benchTab {
	var tabs []benchTab
	for _, k := range instance.ContentKinds() {
		tabs = append(tabs, benchTab{kind: k})
	}
	return append(tabs, benchTab{settings: true})
}

// workbench shows the selected instance and everything in it.
type workbench struct {
	tab  int
	tabs []widget.Clickable

	play, stop, cancel, folder, newFirst widget.Clickable

	content  map[instance.ContentKind]*kindState
	settings instanceSettings
}

func newWorkbench() workbench {
	w := workbench{
		tabs:     make([]widget.Clickable, len(benchTabs())),
		content:  map[instance.ContentKind]*kindState{},
		settings: newInstanceSettings(),
	}
	for _, k := range instance.ContentKinds() {
		w.content[k] = &kindState{list: newList(), filter: newEditor()}
	}
	return w
}

func (w *workbench) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	inst, ok := snap.SelectedInstance()
	if !ok {
		return w.layoutEmpty(gtx, u, snap)
	}

	for i := range w.tabs {
		if w.tabs[i].Clicked(gtx) {
			w.tab = i
		}
	}
	if w.play.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionLaunch{Name: inst.Name})
	}
	if w.stop.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionStopGame{})
	}
	if w.cancel.Clicked(gtx) {
		u.ctrl.Cancel(snap.Task.ID)
	}
	if w.folder.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionOpen{Path: inst.Path})
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions { return w.layoutHeader(gtx, u, snap, inst) }),
		rigid(func(gtx layout.Context) layout.Dimensions { return w.layoutActivity(gtx, u, snap) }),
		rigid(func(gtx layout.Context) layout.Dimensions { return w.layoutTabs(gtx, u, snap) }),
		rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.LineDim) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = gtx.Constraints.Max
			tab := benchTabs()[w.tab]
			if tab.settings {
				return w.settings.Layout(gtx, u, snap, inst)
			}
			return w.content[tab.kind].Layout(gtx, u, snap, inst, tab.kind)
		}),
	)
}

// layoutEmpty is the workbench with nothing on it.
func (w *workbench) layoutEmpty(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	if w.newFirst.Clicked(gtx) {
		u.dialogs.openCreate(snap, "")
	}
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions { return slab(gtx, th.P.Line, unit.Dp(56)) }),
			spacer(sp4),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if len(snap.Instances) == 0 {
					return th.mid(gtx, "Make your first instance to get started.")
				}
				return th.mid(gtx, "Pick an instance on the left.")
			}),
			spacer(sp3),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if len(snap.Instances) > 0 {
					return layout.Dimensions{}
				}
				return th.primary(gtx, &w.newFirst, u.ic.Add, "New instance")
			}),
		)
	})
}

// layoutHeader is the nameplate: mark, name, what it runs, and Play.
func (w *workbench) layoutHeader(gtx layout.Context, u *ui, snap launcher.Snapshot, inst instance.Instance) layout.Dimensions {
	th := u.th
	running := snap.Game.Running && snap.Game.Instance == inst.Name
	launching := taskActive(snap.Task) && snap.Task.Kind == launcher.TaskLaunch

	return layout.Inset{Top: sp4, Bottom: sp3, Left: sp4, Right: sp4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return row(gtx, sp3,
			rigid(func(gtx layout.Context) layout.Dimensions {
				c := th.loaderColor(inst.Loader.Type)
				if !inst.Configured {
					c = th.P.TextDim
				}
				return slab(gtx, c, unit.Dp(40))
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return column(gtx, unit.Dp(5),
					rigid(func(gtx layout.Context) layout.Dimensions {
						return row(gtx, sp2,
							rigid(func(gtx layout.Context) layout.Dimensions { return th.display(gtx, inst.Name) }),
							rigid(func(gtx layout.Context) layout.Dimensions {
								switch {
								case running:
									return th.chip(gtx, "running", th.P.Torch)
								case inst.IsActive:
									return th.chip(gtx, "active", th.P.Good)
								}
								return layout.Dimensions{}
							}),
						)
					}),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if !inst.Configured {
							return th.smallIn(gtx, "Not set up yet — open Settings to choose a version, or detect it from disk.", th.P.Torch)
						}
						line := inst.MinecraftVersion + " · " + inst.Loader.String()
						if !inst.LastPlayed.IsZero() {
							line += " · played " + humaniseSince(inst.LastPlayed)
						}
						return th.mono(gtx, line)
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.ghost(gtx, &w.folder, u.ic.Folder, "Instance folder")
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				switch {
				case running:
					return th.secondary(gtx, &w.stop, "Stop game")
				case launching:
					return th.secondary(gtx, &w.cancel, "Cancel")
				case !snap.HasAccount:
					return th.smallIn(gtx, "Sign in to play", th.P.TextDim)
				case !inst.Configured:
					return layout.Dimensions{}
				default:
					return th.primary(gtx, &w.play, u.ic.Play, "Play")
				}
			}),
		)
	})
}

// layoutActivity shows a launch in progress, or how the last one ended.
func (w *workbench) layoutActivity(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	inset := layout.Inset{Bottom: sp3, Left: sp4, Right: sp4}

	switch {
	case taskActive(snap.Task) && snap.Task.Kind == launcher.TaskLaunch:
		return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return column(gtx, unit.Dp(6),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return row(gtx, sp2,
						rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, taskDetail(snap.Task)) }),
						flexFill(),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if len(snap.Task.Steps) == 0 {
								return layout.Dimensions{}
							}
							return th.monoIn(gtx, fmt.Sprintf("%d steps done", len(snap.Task.Steps)), th.P.TextDim)
						}),
					)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.progress(gtx, taskFraction(snap.Task), unit.Dp(3))
				}),
			)
		})
	case snap.Game.ExitErr != nil && !snap.Game.Running:
		return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return th.notice(gtx, u.ic.Warning, "The game ended with an error: "+snap.Game.ExitErr.Error()+
				" — the Logs and Crash reports tabs have the details.", th.P.Bad)
		})
	}
	return layout.Dimensions{}
}

// layoutTabs draws the row of content tabs with their counts.
func (w *workbench) layoutTabs(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	tabs := benchTabs()

	children := make([]layout.FlexChild, 0, len(tabs)*2)
	for i, tab := range tabs {
		i, tab := i, tab
		selected := i == w.tab
		count := -1
		if !tab.settings {
			if entries := snap.ContentOf(tab.kind); entries != nil {
				count = len(entries)
			}
		}
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			fg := th.P.TextMid
			if selected {
				fg = th.P.Text
			} else if w.tabs[i].Hovered() {
				fg = th.P.Text
			}
			return pressable(gtx, &w.tabs[i], func(gtx layout.Context) layout.Dimensions {
				// The label decides the width; the underline follows it.
				macro := op.Record(gtx.Ops)
				dims := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return row(gtx, unit.Dp(5),
							rigid(func(gtx layout.Context) layout.Dimensions {
								return th.text(gtx, tab.label(), sizeSmall, 100, fg)
							}),
							rigid(func(gtx layout.Context) layout.Dimensions {
								if count < 0 {
									return layout.Dimensions{}
								}
								return th.monoIn(gtx, fmt.Sprint(count), th.P.TextDim)
							}),
						)
					})
				call := macro.Stop()
				call.Add(gtx.Ops)

				underline := gtx.Dp(unit.Dp(2))
				if selected {
					defer op.Offset(image.Pt(0, dims.Size.Y)).Push(gtx.Ops).Pop()
					rect(gtx, th.P.Sky, image.Pt(dims.Size.X, underline))
				}
				return layout.Dimensions{Size: image.Pt(dims.Size.X, dims.Size.Y+underline)}
			})
		}))
	}

	return layout.Inset{Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx, children...)
	})
}
