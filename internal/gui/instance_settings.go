package gui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launch"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launcher"
)

// instanceSettings is the workbench's last tab: what the instance runs and
// with how much, plus the things you do to an instance as a whole.
type instanceSettings struct {
	list *widget.List

	// Editors are seeded when the edited instance changes; doing it every
	// frame would overwrite whatever is being typed.
	seededFor string
	version   string
	loaderVer string
	pickVer   widget.Clickable
	pickLdr   widget.Clickable
	install   widget.Clickable
	minRAM    *widget.Editor
	maxRAM    *widget.Editor
	javaPath  *widget.Editor
	jvmArgs   *widget.Editor
	notes     *widget.Editor

	loaderChoices []widget.Clickable
	loader        instance.LoaderType

	save, detect widget.Clickable

	rename, duplicate, remove, activate widget.Clickable
	newName                             *widget.Editor

	// The game options card: a label for the next snapshot, the buttons on
	// the file, and one Restore/Delete pair per snapshot.
	optLabel                  *widget.Editor
	optSave, optEdit, optShow widget.Clickable
	optRows                   []optionsRow
	// optConfirming names the snapshot whose Delete was pressed once.
	optConfirming string
}

type optionsRow struct {
	restore, del, confirm, keep widget.Clickable
}

func newInstanceSettings() instanceSettings {
	return instanceSettings{
		list:     newList(),
		minRAM:   newEditor(),
		maxRAM:   newEditor(),
		javaPath: newEditor(),
		jvmArgs:  &widget.Editor{},
		notes:    &widget.Editor{},
		newName:  newEditor(),
		optLabel: newEditor(),
	}
}

func (s *instanceSettings) seed(snap launcher.Snapshot) {
	m := snap.Editing
	s.seededFor = snap.Selected
	s.version = m.MinecraftVersion
	s.loader = m.Loader.Type
	if s.loader == "" {
		s.loader = instance.LoaderVanilla
	}
	s.loaderVer = m.Loader.Version
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
	m.MinecraftVersion = s.version
	m.Loader = instance.LoaderSpec{Type: s.loader, Version: s.loaderVer}
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
		if s.loaderChoices[i].Clicked(gtx) && s.loader != lt {
			s.loader = lt
			s.loaderVer = ""
		}
	}
	if s.pickVer.Clicked(gtx) {
		u.dialogs.pickMinecraft(u, s.version, func(v string) {
			if v != s.version {
				s.loaderVer = ""
			}
			s.version = v
		})
	}
	if s.pickLdr.Clicked(gtx) {
		u.dialogs.pickLoaderVersion(u, s.loader, s.version, s.loaderVer, func(v string) { s.loaderVer = v })
	}
	if s.install.Clicked(gtx) {
		u.ctrl.Dispatch(launcher.ActionInstallLoader{Name: snap.Selected})
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
	if s.activate.Clicked(gtx) {
		u.dispatch(launcher.ActionSetActive{Name: snap.Selected})
	}
	s.updateOptions(gtx, u, snap)

	sections := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions { return s.layoutGame(gtx, u, snap, loaders) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutJava(gtx, u) },
		func(gtx layout.Context) layout.Dimensions { return s.layoutOptions(gtx, u, snap) },
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
				return row(gtx, sp3,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.selectField(gtx, u, &s.pickVer, "Minecraft version", s.version, "Choose a release")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if s.loader == instance.LoaderVanilla {
							return layout.Dimensions{}
						}
						placeholder := "Choose a version"
						if s.version == "" {
							placeholder = "Minecraft version first"
						}
						return th.selectField(gtx, u, &s.pickLdr, s.loader.Display()+" version", s.loaderVer, placeholder)
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				saved := s.version == snap.Editing.MinecraftVersion && s.loader == snap.Editing.Loader.Type &&
					(s.loader == instance.LoaderVanilla || s.loaderVer == snap.Editing.Loader.Version)
				if !saved || snap.ProfileInstalled || !configured || s.loader == instance.LoaderVanilla {
					return layout.Dimensions{}
				}
				return th.notice(gtx, u.ic.Download, snap.Editing.Loader.String()+" is not installed yet. "+
					"It is installed on the first Play, or now.", th.P.Torch)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions { return th.secondary(gtx, &s.save, "Save") }),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if snap.ProfileInstalled || !configured || snap.Editing.Loader.Type == instance.LoaderVanilla {
							return layout.Dimensions{}
						}
						return th.ghost(gtx, &s.install, u.ic.Download, "Install "+snap.Editing.Loader.String())
					}),
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
			rigid(func(gtx layout.Context) layout.Dimensions {
				if inst.IsActive {
					return th.wrapped(gtx, "This is the active instance: .minecraft points to it, so the "+
						"official launcher starts it too.", th.P.TextDim)
				}
				return th.wrapped(gtx, "Not the active instance. Setting it active points .minecraft at it.", th.P.TextDim)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.LineDim) }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &s.duplicate, u.ic.Add, "Duplicate") }),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if inst.IsActive {
							return layout.Dimensions{}
						}
						return th.ghost(gtx, &s.activate, u.ic.Check, "Set active")
					}),
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

// updateOptions reads the options card's clicks.
func (s *instanceSettings) updateOptions(gtx layout.Context, u *ui, snap launcher.Snapshot) {
	name := snap.Selected
	if s.optSave.Clicked(gtx) {
		u.dispatch(launcher.ActionSaveOptions{Name: name, Label: strings.TrimSpace(s.optLabel.Text())})
		s.optLabel.SetText("")
	}
	if s.optEdit.Clicked(gtx) {
		u.dispatch(launcher.ActionOpen{Path: snap.Options.Path})
	}
	if s.optShow.Clicked(gtx) {
		u.dispatch(launcher.ActionReveal{Path: snap.Options.Path})
	}
	for len(s.optRows) < len(snap.OptionsSnapshots) {
		s.optRows = append(s.optRows, optionsRow{})
	}
	for i, snapshot := range snap.OptionsSnapshots {
		r := &s.optRows[i]
		switch {
		case r.restore.Clicked(gtx):
			u.dispatch(launcher.ActionRestoreOptions{Name: name, Snapshot: snapshot.Name})
		case r.del.Clicked(gtx):
			s.optConfirming = snapshot.Name
		case r.keep.Clicked(gtx):
			s.optConfirming = ""
		case r.confirm.Clicked(gtx):
			s.optConfirming = ""
			u.dispatch(launcher.ActionDeleteOptionsSnapshot{Name: name, Snapshot: snapshot.Name})
		}
	}
}

// layoutOptions is the game options card: the file the game keeps its
// settings in, a way to keep a copy of it, and the copies kept so far.
func (s *instanceSettings) layoutOptions(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	loaded := snap.OptionsLoaded()
	opt := snap.Options

	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp3,
			rigid(func(gtx layout.Context) layout.Dimensions { return th.title(gtx, "Game options") }),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return th.wrapped(gtx, "Keybinds, video settings, the resource pack order — everything set "+
					"in-game lives in options.txt. A snapshot keeps a copy of it that can be put back later.", th.P.TextDim)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions {
						return th.monoIn(gtx, instance.OptionsFile, th.P.Text)
					}),
					rigid(func(gtx layout.Context) layout.Dimensions {
						switch {
						case !loaded:
							return layout.Dimensions{}
						case !opt.Exists:
							return th.smallIn(gtx, "not written yet — the game makes it on the first run", th.P.TextDim)
						}
						return th.monoIn(gtx, launch.FormatBytes(opt.Size)+" · changed "+humaniseSince(opt.ModTime), th.P.TextDim)
					}),
					flexFill(),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if !opt.Exists {
							return layout.Dimensions{}
						}
						return row(gtx, sp1,
							rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &s.optEdit, u.ic.Edit, "Edit") }),
							rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &s.optShow, u.ic.Launch, "Show") }),
						)
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if !opt.Exists {
					return layout.Dimensions{}
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.field(gtx, s.optLabel, "Label for the snapshot (optional)", "e.g. keybinds sorted out")
					}),
					hspacer(sp2),
					rigid(func(gtx layout.Context) layout.Dimensions {
						return th.secondary(gtx, &s.optSave, "Save snapshot")
					}),
				)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if !loaded || len(snap.OptionsSnapshots) == 0 {
					return layout.Dimensions{}
				}
				return hairline(gtx, th.P.LineDim)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if !loaded || len(snap.OptionsSnapshots) == 0 {
					return layout.Dimensions{}
				}
				rows := make([]layout.FlexChild, 0, len(snap.OptionsSnapshots))
				for i := range snap.OptionsSnapshots {
					i := i
					rows = append(rows, rigid(func(gtx layout.Context) layout.Dimensions {
						return s.layoutSnapshotRow(gtx, u, snap.OptionsSnapshots[i], &s.optRows[i])
					}))
				}
				return column(gtx, unit.Dp(2), rows...)
			}),
		)
	})
}

// layoutSnapshotRow is one saved copy: what it was saved as, when, how far
// it is from the current file, and what to do with it.
func (s *instanceSettings) layoutSnapshotRow(gtx layout.Context, u *ui, snap instance.OptionsSnapshot, r *optionsRow) layout.Dimensions {
	th := u.th
	confirming := s.optConfirming == snap.Name

	meta := snap.Time.Format("2 Jan 2006 15:04") + " · " + launch.FormatBytes(snap.Size)
	switch {
	case snap.Changes == 0:
		meta += " · same as now"
	case snap.Changes == 1:
		meta += " · 1 setting differs"
	case snap.Changes > 1:
		meta += fmt.Sprintf(" · %d settings differ", snap.Changes)
	}

	bg := color.NRGBA{}
	if confirming {
		bg = alpha(th.P.Bad, 0x14)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return fill(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(6), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, sp2,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return column(gtx, unit.Dp(2),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if snap.Label == "" {
								return th.text(gtx, snap.Time.Format("2 Jan 2006 15:04"), sizeBody, 0, th.P.Text)
							}
							return th.text(gtx, snap.Label, sizeBody, 100, th.P.Text)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions { return th.monoIn(gtx, meta, th.P.TextDim) }),
					)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if confirming {
						return row(gtx, sp1,
							rigid(func(gtx layout.Context) layout.Dimensions { return th.smallIn(gtx, "Delete the snapshot?", th.P.Bad) }),
							rigid(func(gtx layout.Context) layout.Dimensions { return th.danger(gtx, &r.confirm, nil, "Yes, delete") }),
							rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &r.keep, nil, "Keep") }),
						)
					}
					return row(gtx, sp1,
						rigid(func(gtx layout.Context) layout.Dimensions {
							if snap.Changes == 0 {
								return th.smallIn(gtx, "in use", th.P.TextDim)
							}
							return th.ghost(gtx, &r.restore, u.ic.Refresh, "Restore")
						}),
						rigid(func(gtx layout.Context) layout.Dimensions { return th.danger(gtx, &r.del, u.ic.Delete, "") }),
					)
				}),
			)
		})
	})
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return fallback
}
