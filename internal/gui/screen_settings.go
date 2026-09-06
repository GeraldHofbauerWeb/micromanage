package gui

import (
	"fmt"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// settingsScreen shows the manager configuration, the detected Java runtimes
// and the storage panel.
type settingsScreen struct {
	list *widget.List

	seeded  bool
	fields  map[string]*widget.Editor
	saves   map[string]*widget.Clickable
	harvest widget.Clickable
	scan    widget.Clickable
	repair  widget.Clickable
}

func newSettingsScreen() settingsScreen {
	return settingsScreen{
		list:   newList(),
		fields: map[string]*widget.Editor{},
		saves:  map[string]*widget.Clickable{},
	}
}

func (s *settingsScreen) Layout(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	// One editor and button per editable key, created once.
	for _, key := range instance.ConfigKeys() {
		if !key.Editable {
			continue
		}
		if _, ok := s.fields[key.Key]; !ok {
			s.fields[key.Key] = newEditor()
			s.saves[key.Key] = &widget.Clickable{}
		}
	}
	if !s.seeded && len(snap.Config) > 0 {
		for key, ed := range s.fields {
			ed.SetText(snap.Config[key])
		}
		s.seeded = true
	}

	for key, click := range s.saves {
		if click.Clicked(gtx) {
			ctrl.Dispatch(launcher.ActionSetConfig{Key: key, Value: s.fields[key].Text()})
		}
	}
	if s.harvest.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionHarvest{})
	}
	if s.scan.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionScanStorage{})
	}

	sections := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions { return s.layoutPaths(gtx, th) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutStorage(gtx, th, snap) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutJava(gtx, th, snap) },
	}

	return th.scrollList(gtx, s.list, len(sections), func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Inset{Bottom: SpaceM}.Layout(gtx, sections[i])
	})
}

func (s *settingsScreen) layoutPaths(gtx layout.Context, th *Theme) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.heading(gtx, "Paths")
		}),
	}
	for _, key := range instance.ConfigKeys() {
		if !key.Editable {
			continue
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return column(gtx, SpaceXS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.editor(gtx, s.fields[key.Key], key.Label, "")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return row(gtx, SpaceS,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return th.dim(gtx, key.Description)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.secondary(gtx, s.saves[key.Key], "Apply")
						}),
					)
				}),
			)
		}))
	}
	return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, SpaceM, children...)
	})
}

func (s *settingsScreen) layoutStorage(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.heading(gtx, "Storage")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx,
				"Instances each carry their own copy of the game files. Consolidating "+
					"hard-links them into one shared store, which costs no extra space "+
					"and leaves the instances untouched.")
		}),
	}

	if len(snap.Reclaimable) > 0 {
		var total int64
		for _, inst := range snap.Instances {
			size, ok := snap.Reclaimable[inst.Name]
			if !ok {
				continue
			}
			total += size
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceS,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.dim(gtx, inst.Name)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, launch.FormatBytes(size))
					}),
				)
			}))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.label(gtx, "Shareable in total: "+launch.FormatBytes(total))
		}))
	}

	if snap.Task.Running() && snap.Task.Kind == launcher.TaskHarvest {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.dim(gtx, taskDetail(snap.Task))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.progressBar(gtx, taskFraction(snap.Task))
			}),
		)
	}

	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return row(gtx, SpaceS,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.secondary(gtx, &s.scan, "Measure")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.button(gtx, &s.harvest, "Consolidate")
			}),
		)
	}))

	return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, SpaceS, children...)
	})
}

func (s *settingsScreen) layoutJava(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.heading(gtx, "Java")
		}),
	}

	if len(snap.Runtimes) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.coloured(gtx, "No Java runtime found.", th.P.Bad)
		}))
	}

	for _, rt := range snap.Runtimes {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return column(gtx, 0,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return row(gtx, SpaceS,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if rt.Broken {
								return th.chip(gtx, "unusable", th.P.Bad, th.P.Bg)
							}
							return th.chip(gtx, fmt.Sprintf("Java %d", rt.Major), th.P.Good, th.P.Bg)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if rt.Broken {
								return th.coloured(gtx, rt.Reason, th.P.Bad)
							}
							return th.label(gtx, rt.String())
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Dimensions{Size: gtx.Constraints.Min}
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.dim(gtx, rt.Source)
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.dim(gtx, rt.Path)
				}),
			)
		}))
	}

	return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, SpaceS, children...)
	})
}
