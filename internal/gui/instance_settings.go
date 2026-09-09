package gui

import (
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// instanceSettings is the workbench's last tab: what the instance runs and
// with how much, plus the things you do to an instance as a whole.
type instanceSettings struct {
	list *widget.List

	// Editors are seeded when the edited instance changes; doing it every
	// frame would overwrite whatever is being typed.
	seededFor string
	version   *widget.Editor
	loaderVer *widget.Editor
	minRAM    *widget.Editor
	maxRAM    *widget.Editor
	javaPath  *widget.Editor
	jvmArgs   *widget.Editor
	notes     *widget.Editor

	loaderChoices []widget.Clickable
	loader        instance.LoaderType

	save, detect widget.Clickable

	rename, duplicate, remove widget.Clickable
	newName                   *widget.Editor
}

func newInstanceSettings() instanceSettings {
	return instanceSettings{
		list:      newList(),
		version:   newEditor(),
		loaderVer: newEditor(),
		minRAM:    newEditor(),
		maxRAM:    newEditor(),
		javaPath:  newEditor(),
		jvmArgs:   &widget.Editor{},
		notes:     &widget.Editor{},
		newName:   newEditor(),
	}
}

func (s *instanceSettings) seed(snap launcher.Snapshot) {
	m := snap.Editing
	s.seededFor = snap.Selected
	s.version.SetText(m.MinecraftVersion)
	s.loader = m.Loader.Type
	if s.loader == "" {
		s.loader = instance.LoaderVanilla
	}
	s.loaderVer.SetText(m.Loader.Version)
	minMB, maxMB := m.Memory.Resolved()
	s.minRAM.SetText(strconv.Itoa(minMB))
	s.maxRAM.SetText(strconv.Itoa(maxMB))
	s.javaPath.SetText(m.Java.Path)
	s.jvmArgs.SetText(strings.Join(m.JVMArgs, " "))
	s.notes.SetText(m.Notes)
	s.newName.SetText(snap.Selected)
}

// collect reads the editors back into metadata.
func (s *instanceSettings) collect(snap launcher.Snapshot) instance.Meta {
	m := snap.Editing
	m.MinecraftVersion = strings.TrimSpace(s.version.Text())
	m.Loader = instance.LoaderSpec{Type: s.loader, Version: strings.TrimSpace(s.loaderVer.Text())}
	if s.loader == instance.LoaderVanilla {
		m.Loader.Version = ""
	}
	m.Memory = instance.MemSpec{MinMB: atoiOr(s.minRAM.Text(), 0), MaxMB: atoiOr(s.maxRAM.Text(), 0)}
	m.Java.Path = strings.TrimSpace(s.javaPath.Text())
	m.JVMArgs = strings.Fields(s.jvmArgs.Text())
	m.Notes = s.notes.Text()

	// The stored version id belongs to the old loader choice; clearing it
	// makes the launcher rebuild it from the version and loader.
	if m.Loader != snap.Editing.Loader || m.MinecraftVersion != snap.Editing.MinecraftVersion {
		m.ResolvedVersionID = ""
	}
	return m
}

func (s *instanceSettings) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot, inst instance.Instance) layout.Dimensions {
	th := u.th
	if s.seededFor != snap.Selected {
		s.seed(snap)
	}

	loaders := instance.LoaderTypes()
	for len(s.loaderChoices) < len(loaders) {
		s.loaderChoices = append(s.loaderChoices, widget.Clickable{})
	}
	for i, lt := range loaders {
		if s.loaderChoices[i].Clicked(gtx) {
			s.loader = lt
		}
	}
	if s.save.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionSaveMeta{Name: snap.Selected, Meta: s.collect(snap)})
	}
	if s.detect.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionDetect{Name: snap.Selected})
		s.seededFor = ""
	}
	if s.rename.Clicked(gtx) {
		if name := strings.TrimSpace(s.newName.Text()); name != "" && name != snap.Selected {
			u.ctrl.Dispatch(launcher.ActionRename{Name: snap.Selected, NewName: name})
		}
	}
	if s.duplicate.Clicked(gtx) {
		u.dialogs.openCreate(snap, snap.Selected)
	}
	if s.remove.Clicked(gtx) {
		u.dialogs.openDelete(inst)
	}

	sections := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions { return s.layoutGame(gtx, u, snap, loaders) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutJava(gtx, u) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutInstance(gtx, u, snap, inst) },
	}

	return layout.Inset{Left: sp4, Right: sp4, Top: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(720)))
		return th.list(gtx, s.list, len(sections), func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Inset{Bottom: sp3}.Layout(gtx, sections[i])
		})
	})
}

func (s *instanceSettings) layoutGame(gtx layout.Context, u *ui, snap launcher.Snapshot, loaders []instance.LoaderType) layout.Dimensions {
	th := u.th
	configured := snap.EditingOK && snap.Editing.Configured()

	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Game") }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if configured {
					return layout.Dimensions{}
				}
				return th.notice(gtx, u.ic.Info, "This instance has no settings yet. Detect them from what the "+
					"official launcher left on disk, or fill them in by hand.", th.P.Torch)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp3,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.field(gtx, s.version, "Minecraft version", "1.21.1")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if s.loader == instance.LoaderVanilla {
							return layout.Dimensions{}
						}
						return th.field(gtx, s.loaderVer, s.loader.Display()+" version", "21.1.248")
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return column(gtx, unit.Dp(6),
					rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, "Mod loader") }),
					rigid(func(gtx layout.Context) layout.Dimensions {
						children := make([]layout.FlexChild, 0, len(loaders))
						for i, lt := range loaders {
							i, lt := i, lt
							children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
								return th.pill(gtx, &s.loaderChoices[i], lt == s.loader, lt.Display())
							}))
						}
						return row(gtx, unit.Dp(6), children...)
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.save, "Save") }),
					rigid(func(gtx layout.Context) layout.Dimensions {
						return th.ghost(gtx, &s.detect, u.ic.Search, "Detect from disk")
					}),
				)
			}),
		)
	})
}

func (s *instanceSettings) layoutJava(gtx layout.Context, u *ui) layout.Dimensions {
	th := u.th
	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Java") }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp3,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.field(gtx, s.minRAM, "Minimum memory (MB)", "1024")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.field(gtx, s.maxRAM, "Maximum memory (MB)", "8192")
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.field(gtx, s.javaPath, "Java executable (empty picks one automatically)", "")
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.field(gtx, s.jvmArgs, "JVM flags (empty uses the official launcher's)", "-XX:+UseG1GC …")
			}),
			rigid(func(gtx layout.Context) layout.Dimensions { return th.field(gtx, s.notes, "Notes", "") }),
			rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.save, "Save") }),
		)
	})
}

func (s *instanceSettings) layoutInstance(gtx layout.Context, u *ui, snap launcher.Snapshot, inst instance.Instance) layout.Dimensions {
	th := u.th
	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Instance") }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.field(gtx, s.newName, "Name", "")
					}),
					hspacer(sp2),
					rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.rename, "Rename") }),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.wrapped(gtx, "Folder: "+inst.Path, th.P.TextDim)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.LineDim) }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &s.duplicate, u.ic.Add, "Duplicate") }),
					flexFill(),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if inst.IsActive {
							return th.smallIn(gtx, "The active instance cannot be deleted", th.P.TextDim)
						}
						return th.danger(gtx, &s.remove, u.ic.Delete, "Delete instance")
					}),
				)
			}),
		)
	})
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return fallback
}
