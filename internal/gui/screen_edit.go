package gui

import (
	"fmt"
	"strconv"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// editTab identifies the visible tab of the edit screen.
type editTab int

const (
	tabMods editTab = iota
	tabConfigs
	tabSaves
	tabSettings
)

func (t editTab) label() string {
	switch t {
	case tabMods:
		return "Mods"
	case tabConfigs:
		return "Configs"
	case tabSaves:
		return "Saves"
	default:
		return "Settings"
	}
}

func (t editTab) kind() instance.FileKind {
	switch t {
	case tabConfigs:
		return instance.KindConfig
	case tabSaves:
		return instance.KindSave
	default:
		return instance.KindMod
	}
}

// editScreen is step 4: edit the instance.
type editScreen struct {
	tab      editTab
	tabs     [4]widget.Clickable
	back     widget.Clickable
	fileList *widget.List
	deletes  []widget.Clickable

	// settings fields, seeded when the edited instance changes
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

	save   widget.Clickable
	detect widget.Clickable

	// info is the file listing for the current tab, refreshed on demand.
	info      *instance.InstanceInfo
	infoFor   string
	infoTab   editTab
	pendingIn bool
}

func newEditScreen() editScreen {
	return editScreen{
		fileList:  newList(),
		version:   newEditor(),
		loaderVer: newEditor(),
		minRAM:    newEditor(),
		maxRAM:    newEditor(),
		javaPath:  newEditor(),
		jvmArgs:   &widget.Editor{},
		notes:     &widget.Editor{},
	}
}

func (s *editScreen) Layout(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	if snap.Selected == "" {
		ctrl.Store().SetScreen(launcher.ScreenInstances)
		return layout.Dimensions{}
	}

	// Seed the editors only when the instance changes; doing it every frame
	// would overwrite whatever is being typed.
	if s.seededFor != snap.Selected {
		s.seed(snap)
	}

	for i := range s.tabs {
		if s.tabs[i].Clicked(gtx) {
			s.tab = editTab(i)
			s.info = nil
		}
	}
	if s.back.Clicked(gtx) {
		ctrl.Store().SetScreen(launcher.ScreenInstances)
	}
	if s.detect.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionDetect{Name: snap.Selected})
		s.seededFor = ""
	}
	if s.save.Clicked(gtx) {
		ctrl.Dispatch(launcher.ActionSaveMeta{Name: snap.Selected, Meta: s.collect(snap)})
	}

	return column(gtx, SpaceM,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, SpaceS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.heading(gtx, snap.Selected)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Min}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.secondary(gtx, &s.back, "Back")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, 4)
			for i := range s.tabs {
				tab := editTab(i)
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.clickableRow(gtx, &s.tabs[i], tab == s.tab, func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, tab.label())
					})
				}))
			}
			return row(gtx, SpaceXS, children...)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if s.tab == tabSettings {
				return s.layoutSettings(gtx, th, snap)
			}
			return s.layoutFiles(gtx, th, ctrl, snap)
		}),
	)
}

// seed fills the editors from the stored metadata.
func (s *editScreen) seed(snap launcher.Snapshot) {
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
	s.jvmArgs.SetText(joinArgs(m.JVMArgs))
	s.notes.SetText(m.Notes)
}

// collect reads the editors back into metadata.
func (s *editScreen) collect(snap launcher.Snapshot) instance.Meta {
	m := snap.Editing
	m.MinecraftVersion = s.version.Text()
	m.Loader = instance.LoaderSpec{Type: s.loader, Version: s.loaderVer.Text()}
	m.Memory = instance.MemSpec{
		MinMB: atoiOr(s.minRAM.Text(), 0),
		MaxMB: atoiOr(s.maxRAM.Text(), 0),
	}
	m.Java.Path = s.javaPath.Text()
	m.JVMArgs = splitArgs(s.jvmArgs.Text())
	m.Notes = s.notes.Text()

	// The stored version id belongs to the old loader choice; clearing it
	// makes the launcher rebuild it from the version and loader.
	if m.Loader != snap.Editing.Loader || m.MinecraftVersion != snap.Editing.MinecraftVersion {
		m.ResolvedVersionID = ""
	}
	return m
}

func (s *editScreen) layoutSettings(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	loaders := instance.LoaderTypes()
	for len(s.loaderChoices) < len(loaders) {
		s.loaderChoices = append(s.loaderChoices, widget.Clickable{})
	}
	for i, lt := range loaders {
		if s.loaderChoices[i].Clicked(gtx) {
			s.loader = lt
		}
	}

	return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
		return column(gtx, SpaceM,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if snap.EditingOK && snap.Editing.Configured() {
					return layout.Dimensions{}
				}
				return column(gtx, SpaceXS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.coloured(gtx,
							"This instance has no settings yet.", th.P.Warn)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.secondary(gtx, &s.detect, "Detect from disk")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.editor(gtx, s.version, "Minecraft version", "1.21.1")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return column(gtx, SpaceXS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.dim(gtx, "System")
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return row(gtx, SpaceXS, loaderChips(gtx, th, loaders, s.loader, s.loaderChoices)...)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if s.loader == instance.LoaderVanilla {
					return layout.Dimensions{}
				}
				return th.editor(gtx, s.loaderVer, s.loader.Display()+" version", "21.1.248")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceM,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.editor(gtx, s.minRAM, "Minimum RAM (MB)", "1024")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return th.editor(gtx, s.maxRAM, "Maximum RAM (MB)", "8192")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.editor(gtx, s.javaPath, "Java path (empty for automatic)", "")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.editor(gtx, s.jvmArgs, "JVM arguments", "-XX:+UseG1GC")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.editor(gtx, s.notes, "Notes", "")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, SpaceS,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.button(gtx, &s.save, "Save")
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.secondary(gtx, &s.detect, "Detect from disk")
					}),
				)
			}),
		)
	})
}

// layoutFiles lists the mods, configs or saves of the instance.
func (s *editScreen) layoutFiles(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	// Refresh the listing when the instance or tab changes. Reading a
	// directory is cheap enough to do inline; anything slower would have to
	// move to the controller.
	if s.info == nil || s.infoFor != snap.Selected || s.infoTab != s.tab {
		info, err := ctrl.Manager.GetInstanceInfo(snap.Selected)
		if err == nil {
			s.info = info
		} else {
			s.info = &instance.InstanceInfo{}
		}
		s.infoFor, s.infoTab = snap.Selected, s.tab
	}

	files := s.filesForTab()
	for len(s.deletes) < len(files) {
		s.deletes = append(s.deletes, widget.Clickable{})
	}
	for i := range files {
		if s.deletes[i].Clicked(gtx) {
			ctrl.Dispatch(launcher.ActionDeleteFile{Name: snap.Selected, Kind: s.tab.kind(), File: files[i]})
			s.info = nil
		}
	}

	if len(files) == 0 {
		return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, fmt.Sprintf("No %s in this instance.", s.tab.label()))
		})
	}

	return th.scrollList(gtx, s.fileList, len(files), func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Inset{Bottom: SpaceXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return fill(gtx, th.P.Panel, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{
					Top: SpaceS, Bottom: SpaceS, Left: SpaceM, Right: SpaceS,
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return row(gtx, SpaceS,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return th.label(gtx, files[i])
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.danger(gtx, &s.deletes[i], "Delete")
						}),
					)
				})
			})
		})
	})
}

func (s *editScreen) filesForTab() []string {
	if s.info == nil {
		return nil
	}
	switch s.tab {
	case tabConfigs:
		return s.info.ConfigsDir
	case tabSaves:
		return s.info.SavesDir
	default:
		return s.info.ModsDir
	}
}

// --- helpers ---

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func splitArgs(s string) []string {
	var out []string
	for _, f := range splitFields(s) {
		out = append(out, f)
	}
	return out
}

// splitFields splits on whitespace without pulling in strings for one call.
func splitFields(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}
