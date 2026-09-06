package gui

import (
	"fmt"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// instancesScreen is the home page: choose an instance (step 2), choose the
// system (step 3) and launch (step 5).
type instancesScreen struct {
	list  *widget.List
	rows  []widget.Clickable
	setUp []widget.Clickable

	play    widget.Clickable
	edit    widget.Clickable
	stop    widget.Clickable
	cancel  widget.Clickable
	newInst widget.Clickable

	loaderChoices []widget.Clickable
	resetLoader   widget.Clickable
	makeDefault   widget.Clickable

	create createDialog
}

func newInstancesScreen() instancesScreen {
	return instancesScreen{
		list:   newList(),
		create: newCreateDialog(),
	}
}

func (s *instancesScreen) Layout(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	if s.create.open {
		return s.create.Layout(gtx, th, ctrl, snap)
	}
	if s.newInst.Clicked(gtx) {
		s.create.show(snap)
	}

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return s.layoutList(gtx, th, ctrl, snap)
		}),
		hspacer(SpaceM),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(360)))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return s.layoutSide(gtx, th, ctrl, snap)
		}),
	)
}

// layoutList renders the instance cards.
func (s *instancesScreen) layoutList(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	for len(s.rows) < len(snap.Instances) {
		s.rows = append(s.rows, widget.Clickable{})
		s.setUp = append(s.setUp, widget.Clickable{})
	}

	for i := range snap.Instances {
		if s.rows[i].Clicked(gtx) {
			ctrl.Dispatch(launcher.ActionSelect{Name: snap.Instances[i].Name})
		}
		if s.setUp[i].Clicked(gtx) {
			name := snap.Instances[i].Name
			ctrl.Dispatch(launcher.ActionSelect{Name: name})
			ctrl.Dispatch(launcher.ActionDetect{Name: name})
			ctrl.Store().SetScreen(launcher.ScreenEdit)
		}
	}

	return column(gtx, SpaceS,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, SpaceS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.heading(gtx, "Instances")
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Min}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.button(gtx, &s.newInst, "New instance")
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(snap.Instances) == 0 {
				return th.dim(gtx, "No instances yet. Create one to get started.")
			}
			return th.scrollList(gtx, s.list, len(snap.Instances), func(gtx layout.Context, i int) layout.Dimensions {
				return layout.Inset{Bottom: SpaceS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return s.layoutCard(gtx, th, snap, i)
				})
			})
		}),
	)
}

// layoutCard renders one instance.
func (s *instancesScreen) layoutCard(gtx layout.Context, th *Theme, snap launcher.Snapshot, i int) layout.Dimensions {
	inst := snap.Instances[i]
	selected := inst.Name == snap.Selected

	return th.clickableRow(gtx, &s.rows[i], selected, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, SpaceXS,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, inst.Name)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !inst.IsActive {
							return layout.Dimensions{}
						}
						return th.chip(gtx, "active", th.P.Good, th.P.Bg)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: gtx.Constraints.Min}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if inst.Configured {
							return layout.Dimensions{}
						}
						// An instance from before instance.json existed, with
						// the detected settings one click away.
						return th.secondary(gtx, &s.setUp[i], "Set up")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !inst.Configured {
					return th.coloured(gtx, "not configured yet", th.P.Warn)
				}
				return th.dim(gtx, fmt.Sprintf("%s · %s", inst.MinecraftVersion, inst.Loader))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				detail := fmt.Sprintf("%d mods · %d configs · %d saves",
					inst.ModCount, inst.ConfigCount, inst.SaveCount)
				if !inst.LastPlayed.IsZero() {
					detail += " · played " + humaniseSince(inst.LastPlayed)
				}
				return th.dim(gtx, detail)
			}),
		)
	})
}

// layoutSide renders the launch panel for the selected instance.
func (s *instancesScreen) layoutSide(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	inst, ok := snap.SelectedInstance()
	if !ok {
		return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "Select an instance to play it.")
		})
	}

	if s.edit.Clicked(gtx) {
		ctrl.Store().SetScreen(launcher.ScreenEdit)
	}
	if s.play.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionLaunch{Name: inst.Name})
	}
	if s.stop.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionStopGame{})
	}
	if s.cancel.Clicked(gtx) {
		ctrl.Cancel(snap.Task.ID)
	}

	return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, SpaceM,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.heading(gtx, inst.Name)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.layoutLoaderChoice(gtx, th, ctrl, snap)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.layoutActivity(gtx, th, snap)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						switch {
						case snap.Game.Running:
							return th.secondary(gtx, &s.stop, "Stop game")
						case snap.Task.Running() && snap.Task.Kind == launcher.TaskLaunch:
							return th.secondary(gtx, &s.cancel, "Cancel")
						case !snap.HasAccount:
							return th.dim(gtx, "Sign in to play")
						case !inst.Configured:
							return th.dim(gtx, "Set the version first")
						default:
							return th.primary(gtx, &s.play, "Play")
						}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.secondary(gtx, &s.edit, "Edit")
					}),
				)
			}),
		)
	})
}

// layoutLoaderChoice is step 3: the system to launch with, defaulting to the
// instance's own and overridable for this launch only.
func (s *instancesScreen) layoutLoaderChoice(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	loaders := instance.LoaderTypes()
	for len(s.loaderChoices) < len(loaders) {
		s.loaderChoices = append(s.loaderChoices, widget.Clickable{})
	}

	current := snap.EffectiveLoader()
	for i, lt := range loaders {
		if s.loaderChoices[i].Clicked(gtx) {
			spec := instance.LoaderSpec{Type: lt}
			// Keep the version when returning to the instance's own loader,
			// since that is the combination we know is installed.
			if lt == snap.Editing.Loader.Type {
				spec.Version = snap.Editing.Loader.Version
			}
			ctrl.Dispatch(launcher.ActionSetLoaderOverride{Spec: spec, Set: lt != snap.Editing.Loader.Type})
		}
	}
	if s.resetLoader.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionSetLoaderOverride{Set: false})
	}
	if s.makeDefault.Clicked(gtx) {
		meta := snap.Editing
		meta.Loader = snap.LoaderOverride
		meta.ResolvedVersionID = ""
		ctrl.Dispatch(launcher.ActionSaveMeta{Name: snap.Selected, Meta: meta})
		ctrl.Dispatch(launcher.ActionSetLoaderOverride{Set: false})
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "System")
		}),
	}
	for i, lt := range loaders {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			selected := lt == current.Type
			label := lt.Display()
			if selected && current.Version != "" {
				label += " " + current.Version
			}
			return layout.Inset{Bottom: SpaceXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.clickableRow(gtx, &s.loaderChoices[i], selected, func(gtx layout.Context) layout.Dimensions {
					return th.label(gtx, label)
				})
			})
		}))
	}

	if snap.HasOverride {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.coloured(gtx, fmt.Sprintf("differs from the default (%s)", snap.Editing.Loader), th.P.Warn)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.secondary(gtx, &s.resetLoader, "Use default")
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.secondary(gtx, &s.makeDefault, "Make default")
					}),
				)
			}),
		)
	}

	return column(gtx, SpaceXS, children...)
}

// layoutActivity renders launch progress, or the running game's log tail.
func (s *instancesScreen) layoutActivity(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	switch {
	case snap.Game.Running:
		return column(gtx, SpaceXS,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.coloured(gtx, "Running", th.P.Good)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(snap.Game.Tail) == 0 {
					return layout.Dimensions{}
				}
				return th.dim(gtx, lastLine(snap.Game.Tail))
			}),
		)

	case snap.Game.ExitErr != nil:
		return th.coloured(gtx, "Last run failed: "+snap.Game.ExitErr.Error(), th.P.Bad)

	case snap.Task.Running() && snap.Task.Kind == launcher.TaskLaunch:
		return column(gtx, SpaceXS,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.dim(gtx, taskDetail(snap.Task))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.progressBar(gtx, taskFraction(snap.Task))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(snap.Task.Steps) == 0 {
					return layout.Dimensions{}
				}
				return th.dim(gtx, "done: "+joinSteps(snap.Task.Steps))
			}),
		)
	}
	return layout.Dimensions{}
}

// --- helpers ---

func lastLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	if len(line) > 70 {
		return line[:70] + "…"
	}
	return line
}

func joinSteps(steps []string) string {
	out := ""
	for i, s := range steps {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// humaniseSince renders a timestamp as a rough age.
func humaniseSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
