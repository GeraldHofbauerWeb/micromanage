package gui

import (
	"fmt"
	"image"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

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
	rows []*railRow
	add  widget.Clickable
}

// railRow is one instance's widgets: the row itself, its "⋯" button, and
// the pointer tag that hears the right-click the row's clickable ignores.
type railRow struct {
	click widget.Clickable
	more  widget.Clickable
	tag   struct{}
}

func newRail() railState { return railState{list: newList()} }

func (r *railState) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	for len(r.rows) < len(snap.Instances) {
		r.rows = append(r.rows, &railRow{})
	}
	for i := range snap.Instances {
		inst := snap.Instances[i]
		rr := r.rows[i]
		if rr.click.Clicked(gtx) {
			u.dispatch(launcher.ActionSelect{Name: inst.Name})
		}
		if rr.more.Clicked(gtx) {
			u.openInstanceMenu(snap, inst)
		}
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &rr.tag, Kinds: pointer.Press})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press && e.Buttons == pointer.ButtonSecondary {
				// A right-click selects too, so the workbench and the
				// menu are about the same instance.
				u.dispatch(launcher.ActionSelect{Name: inst.Name})
				u.openInstanceMenu(snap, inst)
			}
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
	rr := r.rows[i]
	selected := inst.Name == snap.Selected
	running := snap.Game.Running && snap.Game.Instance == inst.Name
	showMore := rr.click.Hovered() || rr.more.Hovered() || selected

	// The tag goes on before the row so the row's own clickable, drawn
	// inside it, hands the press on to it as well.
	macro := op.Record(gtx.Ops)
	dims := th.selectableRow(gtx, &rr.click, selected, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: sp3, Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
				rigid(func(gtx layout.Context) layout.Dimensions {
					// The "⋯" is the discoverable way to the menu the
					// right-click opens; it appears when the row does.
					if !showMore {
						return layout.Dimensions{Size: image.Pt(gtx.Dp(unit.Dp(32)), 0)}
					}
					return th.iconButton(gtx, &rr.more, u.ic.More)
				}),
			)
		})
	})
	call := macro.Stop()

	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &rr.tag)
	call.Add(gtx.Ops)
	return dims
}

// openInstanceMenu shows what can be done to an instance from the list,
// at the pointer.
func (u *ui) openInstanceMenu(snap launcher.Snapshot, inst instance.Instance) {
	u.menu.show(u.pointer, inst.Name, u.instanceMenu(snap, inst))
}

// instanceMenu builds the items. The same things live on the workbench;
// here they are one click from the list, whichever instance is selected.
func (u *ui) instanceMenu(snap launcher.Snapshot, inst instance.Instance) []menuItem {
	running := snap.Game.Running && snap.Game.Instance == inst.Name
	name := inst.Name

	play := menuItem{label: "Play", icon: u.ic.Play}
	switch {
	case running:
		play = menuItem{label: "Stop game", icon: u.ic.Stop, do: func() { u.dispatch(launcher.ActionStopGame{}) }}
	case !snap.HasAccount:
		play.note = "sign in first"
	case !inst.Configured:
		play.note = "not set up"
	case taskActive(snap.Task) && snap.Task.Kind == launcher.TaskLaunch:
		play.note = "launching"
	default:
		play.do = func() { u.dispatch(launcher.ActionLaunch{Name: name}) }
	}

	settingsTab := len(benchTabs()) - 1
	openTab := func(tab int) func() {
		return func() {
			u.dispatch(launcher.ActionSelect{Name: name})
			u.bench.shownFor = name
			u.bench.tab = tab
		}
	}

	del := menuItem{label: "Delete…", icon: u.ic.Delete, danger: true, divider: true}
	if inst.IsActive {
		del.note = "active"
	} else {
		del.do = func() { u.dialogs.openDelete(inst) }
	}

	return []menuItem{
		play,
		{label: "Overview", icon: u.ic.Info, do: openTab(0)},
		{label: "Settings", icon: u.ic.Settings, do: openTab(settingsTab)},
		{label: "Instance folder", icon: u.ic.Folder, do: func() { u.dispatch(launcher.ActionOpen{Path: inst.Path}) }},
		{label: "Duplicate…", icon: u.ic.Add, divider: true, do: func() {
			// The dialog takes the version from the selected instance's
			// metadata, so ask for the state as it is now.
			u.dialogs.openCreate(u.currentSnapshot(snap), name)
		}},
		del,
	}
}

// currentSnapshot is the live state when there is a controller, or the
// one given when there is not.
func (u *ui) currentSnapshot(fallback launcher.Snapshot) launcher.Snapshot {
	if u.ctrl == nil {
		return fallback
	}
	return u.ctrl.Store().Snapshot()
}

// --- workbench ---

// benchTab is one tab of the workbench: the overview, a content kind, or
// the settings.
type benchTab struct {
	kind     instance.ContentKind
	overview bool
	settings bool
}

func (t benchTab) label() string {
	switch {
	case t.overview:
		return "Overview"
	case t.settings:
		return "Settings"
	}
	return t.kind.Short()
}

// benchTabs lists the tabs in order. The overview comes first and is where
// a newly selected instance opens; the content tabs follow in
// ContentKinds order, which the overview's cards index into.
func benchTabs() []benchTab {
	tabs := []benchTab{{overview: true}}
	for _, k := range instance.ContentKinds() {
		tabs = append(tabs, benchTab{kind: k})
	}
	return append(tabs, benchTab{settings: true})
}

// workbench shows the selected instance and everything in it.
type workbench struct {
	tab    int
	tabs   []widget.Clickable
	tabRow widget.List

	play, stop, cancel, folder, newFirst widget.Clickable

	content  map[instance.ContentKind]*kindState
	settings instanceSettings
	overview overview

	// shownFor is the instance the tab was chosen for; a new selection
	// returns to the overview.
	shownFor string
}

func newWorkbench() workbench {
	w := workbench{
		tabs:     make([]widget.Clickable, len(benchTabs())),
		tabRow:   widget.List{List: layout.List{Axis: layout.Horizontal}},
		content:  map[instance.ContentKind]*kindState{},
		settings: newInstanceSettings(),
		overview: newOverview(),
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

	if w.shownFor != inst.Name {
		w.shownFor = inst.Name
		w.tab = 0
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
			switch {
			case tab.overview:
				return w.overview.Layout(gtx, u, snap, inst)
			case tab.settings:
				return w.settings.Layout(gtx, u, snap, inst)
			}
			return w.content[tab.kind].Layout(gtx, u, snap, inst, tab.kind)
		}),
	)
}

// layoutEmpty is the workbench with nothing on it: the mark, the name, and
// the one thing to do next. It is the first thing a new player sees and
// the resting state between instances, so it is the icon at a size where
// its three blocks read as what they are, and little else.
func (w *workbench) layoutEmpty(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	if w.newFirst.Clicked(gtx) {
		u.dialogs.openCreate(snap, "")
	}
	adopting := taskActive(snap.Task) && snap.Task.Kind == launcher.TaskAdopt

	var active string
	for _, inst := range snap.Instances {
		if inst.IsActive {
			active = inst.Name
		}
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions { return mark(gtx, unit.Dp(132)) }),
			spacer(sp4),
			rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th.Theme, unit.Sp(32), "Instance Manager")
				l.Font.Typeface = faceDisplay
				l.Font.Weight = font.Bold
				l.Color = th.P.Text
				return l.Layout(gtx)
			}),
			spacer(sp2),
			rigid(func(gtx layout.Context) layout.Dimensions {
				switch {
				case adopting:
					return th.mid(gtx, "Moving your .minecraft in as the first instance…")
				case len(snap.Instances) == 0:
					return th.mid(gtx, "Make your first instance to get started.")
				}
				return th.mid(gtx, "Pick an instance on the left, or right-click one.")
			}),
			spacer(sp4),
			rigid(func(gtx layout.Context) layout.Dimensions {
				switch {
				case adopting:
					gtx.Constraints.Max.X = gtx.Dp(unit.Dp(220))
					return th.progress(gtx, taskFraction(snap.Task), unit.Dp(3))
				case len(snap.Instances) == 0:
					return th.primary(gtx, &w.newFirst, u.ic.Add, "New instance")
				}
				line := fmt.Sprintf("%d instances", len(snap.Instances))
				if active != "" {
					line += " · " + active + " is active"
				}
				return th.monoIn(gtx, line, th.P.TextDim)
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
						if !snap.ProfileInstalled && snap.EditingOK {
							line += " · installs on first play"
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
	case taskActive(snap.Task) && (snap.Task.Kind == launcher.TaskLaunch ||
		snap.Task.Kind == launcher.TaskInstall || snap.Task.Kind == launcher.TaskCreate):
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
		if !tab.settings && !tab.overview {
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

	// The row scrolls sideways rather than truncating: on a narrow window
	// the last tab is still reachable, and on a wide one nothing moves.
	return layout.Inset{Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return w.tabRow.Layout(gtx, len(children), func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx, children[i])
		})
	})
}
