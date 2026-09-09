package gui

import (
	"fmt"
	"sort"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// settingsScreen shows the manager configuration, the Java runtimes and the
// storage panel.
type settingsScreen struct {
	list *widget.List

	seeded bool
	fields map[string]*widget.Editor
	saves  map[string]*widget.Clickable

	harvest, scan, reclaim, reclaimYes, reclaimNo widget.Clickable
	confirmingReclaim                             bool
}

func newSettingsScreen() settingsScreen {
	return settingsScreen{
		list:   newList(),
		fields: map[string]*widget.Editor{},
		saves:  map[string]*widget.Clickable{},
	}
}

func (s *settingsScreen) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	ctrl := u.ctrl

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
			ctrl.Dispatch(launcher.ActionSetConfig{Key: key, Value: strings.TrimSpace(s.fields[key].Text())})
		}
	}
	if s.harvest.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionHarvest{})
	}
	if s.scan.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionScanStorage{})
	}
	if s.reclaim.Clicked(gtx) {
		s.confirmingReclaim = true
	}
	if s.reclaimNo.Clicked(gtx) {
		s.confirmingReclaim = false
	}
	if s.reclaimYes.Clicked(gtx) {
		s.confirmingReclaim = false
		ctrl.Dispatch(launcher.ActionReclaim{})
	}

	sections := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions { return s.layoutStorage(gtx, u, snap) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutJava(gtx, u, snap) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutPaths(gtx, u) },
	}

	return layout.Inset{Left: sp4, Right: sp4, Top: sp4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(860)))
		return th.list(gtx, s.list, len(sections), func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Inset{Bottom: sp3}.Layout(gtx, sections[i])
		})
	})
}

func (s *settingsScreen) layoutPaths(gtx layout.Context, u *ui) layout.Dimensions {
	th := u.th
	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Paths and sign-in") }),
	}
	for _, key := range instance.ConfigKeys() {
		key := key
		if !key.Editable {
			continue
		}
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return column(gtx, unit.Dp(5),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return th.field(gtx, s.fields[key.Key], key.Label, "")
						}),
						hspacer(sp2),
						rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, s.saves[key.Key], "Apply") }),
					)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.wrapped(gtx, key.Description, th.P.TextDim) }),
			)
		}))
	}
	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3, children...)
	})
}

func (s *settingsScreen) layoutStorage(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th

	var total int64
	measured := false
	for _, inst := range snap.Instances {
		if size, ok := snap.Reclaimable[inst.Name]; ok {
			measured = true
			total += size
		}
	}

	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Storage") }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, "Instances made by the official launcher each carry their own copy of the "+
				"game files and Java. Consolidating links them into one shared store, which costs no space. "+
				"Freeing then removes the copies the store already holds; mods, configs and worlds are never touched.",
				th.P.TextMid)
		}),
	}

	if measured {
		for _, inst := range snap.Instances {
			inst := inst
			size, ok := snap.Reclaimable[inst.Name]
			if !ok {
				continue
			}
			children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp2,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return th.mid(gtx, inst.Name) }),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if size == 0 {
							return th.monoIn(gtx, "nothing to free", th.P.TextDim)
						}
						return th.mono(gtx, launch.FormatBytes(size))
					}),
				)
			}))
		}
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return th.bodyMedium(gtx, "Held twice in total") }),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.monoIn(gtx, launch.FormatBytes(total), th.P.Text)
				}),
			)
		}))
	}

	if taskActive(snap.Task) && (snap.Task.Kind == launcher.TaskHarvest || snap.Task.Kind == launcher.TaskReclaim) {
		children = append(children,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, taskDetail(snap.Task)) }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.progress(gtx, taskFraction(snap.Task), unit.Dp(3))
			}),
		)
	}

	children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
		if s.confirmingReclaim {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.smallIn(gtx, fmt.Sprintf("Remove %s of copies from the instances?", launch.FormatBytes(total)), th.P.Torch)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.primary(gtx, &s.reclaimYes, nil, "Free it") }),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &s.reclaimNo, nil, "Keep") }),
			)
		}
		return row(gtx, sp2,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.scan, "Measure") }),
			rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.harvest, "Consolidate") }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if !measured || total == 0 {
					return layout.Dimensions{}
				}
				return th.ghost(gtx, &s.reclaim, u.ic.Delete, "Free "+launch.FormatBytes(total))
			}),
		)
	}))

	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3, children...)
	})
}

// runtimeGroup is one Java version and every place it was found.
type runtimeGroup struct {
	major     int
	label     string
	broken    bool
	reason    string
	sources   []string
	path      string
	instances int
}

// groupRuntimes folds the same JDK found in the store and in six instances
// into one line. A list of fifteen identical entries said nothing except
// that the official launcher likes copies.
func groupRuntimes(runtimes []java.Runtime) []runtimeGroup {
	index := map[string]int{}
	var groups []runtimeGroup
	for _, rt := range runtimes {
		key := fmt.Sprintf("%d|%s|%v|%s", rt.Major, rt.String(), rt.Broken, rt.Reason)
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, runtimeGroup{
				major: rt.Major, label: rt.String(), broken: rt.Broken, reason: rt.Reason, path: rt.Path,
			})
		}
		g := &groups[i]
		if strings.HasPrefix(rt.Source, "instance ") {
			g.instances++
		} else if g.sources == nil || !contains(g.sources, rt.Source) {
			g.sources = append(g.sources, rt.Source)
		}
		// Prefer the store's path as the representative.
		if rt.Source == "shared store" {
			g.path = rt.Path
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].broken != groups[j].broken {
			return !groups[i].broken
		}
		return groups[i].major > groups[j].major
	})
	return groups
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (g runtimeGroup) where() string {
	parts := append([]string(nil), g.sources...)
	switch g.instances {
	case 0:
	case 1:
		parts = append(parts, "1 instance")
	default:
		parts = append(parts, fmt.Sprintf("%d instances", g.instances))
	}
	return strings.Join(parts, ", ")
}

func (s *settingsScreen) layoutJava(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Java") }),
	}

	if len(snap.Runtimes) == 0 {
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return th.notice(gtx, u.ic.Warning, "No Java runtime found. Consolidating an instance made by the official launcher brings its Java along.", th.P.Bad)
		}))
	}

	for _, g := range groupRuntimes(snap.Runtimes) {
		g := g
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return column(gtx, unit.Dp(3),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return row(gtx, sp2,
						rigid(func(gtx layout.Context) layout.Dimensions {
							if g.broken {
								return th.chip(gtx, "unusable", th.P.Bad)
							}
							return th.chip(gtx, fmt.Sprintf("Java %d", g.major), th.P.Good)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if g.broken {
								return th.text(gtx, g.reason, sizeBody, 0, th.P.Bad)
							}
							return th.bodyMedium(gtx, g.label)
						}),
						flexFill(),
						rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, g.where()) }),
					)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.monoIn(gtx, g.path, th.P.TextDim) }),
			)
		}))
	}

	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3, children...)
	})
}
