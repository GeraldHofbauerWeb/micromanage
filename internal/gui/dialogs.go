package gui

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launcher"
)

// dialogs holds the modal states. At most one is open at a time.
type dialogs struct {
	scrim widget.Clickable

	create createDialog
	del    deleteDialog
	pick   picker
}

func newDialogs() dialogs {
	return dialogs{create: newCreateDialog(), pick: newPicker()}
}

func (d *dialogs) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	switch {
	case d.pick.open:
		// The picker sits above whatever opened it; that dialog keeps its
		// state and returns when the choice is made.
		if d.scrim.Clicked(gtx) {
			d.pick.open = false
		}
		return u.th.modal(gtx, &d.scrim, unit.Dp(460), func(gtx layout.Context) layout.Dimensions {
			return d.pick.Layout(gtx, u, snap)
		})
	case d.create.open:
		if d.scrim.Clicked(gtx) {
			d.create.open = false
		}
		return u.th.modal(gtx, &d.scrim, unit.Dp(540), func(gtx layout.Context) layout.Dimensions {
			return d.create.Layout(gtx, u, snap)
		})
	case d.del.open:
		if d.scrim.Clicked(gtx) {
			d.del.open = false
		}
		return u.th.modal(gtx, &d.scrim, unit.Dp(460), func(gtx layout.Context) layout.Dimensions {
			return d.del.Layout(gtx, u)
		})
	}
	return layout.Dimensions{}
}

func (d *dialogs) openCreate(snap launcher.Snapshot, cloneFrom string) {
	d.create.show(snap, cloneFrom)
}

func (d *dialogs) openDelete(inst instance.Instance) {
	d.del.inst = inst
	d.del.open = true
}

// pickMinecraft opens the release list.
func (d *dialogs) pickMinecraft(u *ui, current string, onPick func(string)) {
	d.pick.show(u, "Minecraft version", pickMinecraft, "", "", current, true, onPick)
}

// pickLoaderVersion opens one loader's list for a game version.
func (d *dialogs) pickLoaderVersion(u *ui, kind instance.LoaderType, mc, current string, onPick func(string)) {
	d.pick.show(u, kind.Display()+" for "+mc, pickLoader, kind, mc, current, true, onPick)
}

// pickInstance opens the instance list.
func (d *dialogs) pickInstance(u *ui, current string, onPick func(string)) {
	d.pick.show(u, "Copy which instance?", pickInstances, "", "", current, false, onPick)
}

// --- new instance ---

// createDialog makes an instance. Empty is the default because cloning
// costs gigabytes and the shared store already supplies everything a fresh
// instance needs; duplicating an existing one is a click away.
type createDialog struct {
	open bool

	name *widget.Editor

	version       string
	pickVersion   widget.Clickable
	loaderChoices []widget.Clickable
	loaderVersion string
	pickLoaderVer widget.Clickable
	loader        instance.LoaderType

	fromScratch, fromClone widget.Clickable
	clone                  bool
	cloneSource            string
	pickSource             widget.Clickable

	confirm, cancel widget.Clickable
}

func newCreateDialog() createDialog {
	return createDialog{
		name:   newEditor(),
		loader: instance.LoaderVanilla,
	}
}

// show opens the dialog. With a clone source it starts as a duplicate of
// that instance, taking its version and loader along.
func (d *createDialog) show(snap launcher.Snapshot, cloneFrom string) {
	d.open = true
	d.name.SetText("")
	d.loader = instance.LoaderVanilla
	d.loaderVersion = ""
	d.version = ""
	d.clone = cloneFrom != ""
	d.cloneSource = cloneFrom

	if snap.Editing.MinecraftVersion != "" {
		d.version = snap.Editing.MinecraftVersion
		if cloneFrom != "" {
			d.loader = snap.Editing.Loader.Type
			d.loaderVersion = snap.Editing.Loader.Version
			d.name.SetText(cloneFrom + "-copy")
		}
	}
}

func (d *createDialog) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	loaders := instance.LoaderTypes()
	for len(d.loaderChoices) < len(loaders) {
		d.loaderChoices = append(d.loaderChoices, widget.Clickable{})
	}

	for i, lt := range loaders {
		if d.loaderChoices[i].Clicked(gtx) && d.loader != lt {
			d.loader = lt
			// A version belongs to one loader; the next one starts fresh.
			d.loaderVersion = ""
		}
	}
	if d.pickVersion.Clicked(gtx) {
		u.dialogs.pickMinecraft(u, d.version, func(v string) {
			if v != d.version {
				d.loaderVersion = ""
			}
			d.version = v
		})
	}
	if d.pickLoaderVer.Clicked(gtx) {
		u.dialogs.pickLoaderVersion(u, d.loader, d.version, d.loaderVersion, func(v string) { d.loaderVersion = v })
	}
	if d.fromScratch.Clicked(gtx) {
		d.clone = false
	}
	if d.fromClone.Clicked(gtx) {
		d.clone = true
	}
	if d.pickSource.Clicked(gtx) {
		u.dialogs.pickInstance(u, d.cloneSource, func(v string) { d.cloneSource = v })
	}
	if d.cancel.Clicked(gtx) {
		d.open = false
	}

	ready := strings.TrimSpace(d.name.Text()) != "" && d.version != "" &&
		(d.loader == instance.LoaderVanilla || d.loaderVersion != "") &&
		(!d.clone || d.cloneSource != "")
	if ready && d.confirm.Clicked(gtx) {
		action := launcher.ActionCreate{
			Name:    strings.TrimSpace(d.name.Text()),
			Version: d.version,
			Loader:  instance.LoaderSpec{Type: d.loader, Version: d.loaderVersion},
		}
		if d.clone {
			action.Clone = d.cloneSource
		}
		u.ctrl.Dispatch(action)
		d.open = false
	}

	return column(gtx, sp3,
		rigid(func(gtx layout.Context) layout.Dimensions {
			if d.clone && d.cloneSource != "" {
				return th.display(gtx, "Duplicate "+d.cloneSource)
			}
			return th.display(gtx, "New instance")
		}),
		rigid(func(gtx layout.Context) layout.Dimensions { return th.field(gtx, d.name, "Name", "my-modpack") }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return column(gtx, unit.Dp(6),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, "Mod loader") }),
				rigid(func(gtx layout.Context) layout.Dimensions {
					children := make([]layout.FlexChild, 0, len(loaders))
					for i, lt := range loaders {
						i, lt := i, lt
						children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
							return th.pill(gtx, &d.loaderChoices[i], lt == d.loader, lt.Display())
						}))
					}
					return row(gtx, unit.Dp(6), children...)
				}),
			)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp3,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return th.selectField(gtx, u, &d.pickVersion, "Minecraft version", d.version, "Choose a release")
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if d.loader == instance.LoaderVanilla {
						return layout.Dimensions{}
					}
					placeholder := "Choose a version"
					if d.version == "" {
						placeholder = "Minecraft version first"
					}
					return th.selectField(gtx, u, &d.pickLoaderVer, d.loader.Display()+" version", d.loaderVersion, placeholder)
				}),
			)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			if d.loader == instance.LoaderVanilla {
				return layout.Dimensions{}
			}
			return th.wrapped(gtx, d.loader.Display()+" is installed into the shared store when the instance is created.", th.P.TextDim)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions { return d.layoutSource(gtx, u, snap) }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					if !ready {
						return th.secondary(gtx, &d.confirm, "Create")
					}
					return th.primary(gtx, &d.confirm, nil, "Create")
				}),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &d.cancel, nil, "Cancel") }),
			)
		}),
	)
}

func (d *createDialog) layoutSource(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions { return th.small(gtx, "Start from") }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, unit.Dp(6),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.pill(gtx, &d.fromScratch, !d.clone, "Empty") }),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.pill(gtx, &d.fromClone, d.clone, "A copy of an instance")
				}),
			)
		}),
	}

	if !d.clone {
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, "Ready at once. The game files come from the shared store.", th.P.TextDim)
		}))
		return column(gtx, unit.Dp(6), children...)
	}

	children = append(children,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.selectField(gtx, u, &d.pickSource, "Instance to copy", d.cloneSource, "Choose an instance")
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, "Mods, configs and packs are copied. Worlds and screenshots are not.", th.P.TextDim)
		}),
	)
	return column(gtx, unit.Dp(6), children...)
}

// --- delete instance ---

// deleteDialog asks before an instance and everything in it goes.
type deleteDialog struct {
	open            bool
	inst            instance.Instance
	confirm, cancel widget.Clickable
}

func (d *deleteDialog) Layout(gtx layout.Context, u *ui) layout.Dimensions {
	th := u.th
	if d.cancel.Clicked(gtx) {
		d.open = false
	}
	if d.confirm.Clicked(gtx) {
		d.open = false
		u.ctrl.Dispatch(launcher.ActionDelete{Name: d.inst.Name})
	}

	return column(gtx, sp3,
		rigid(func(gtx layout.Context) layout.Dimensions { return th.display(gtx, "Delete "+d.inst.Name+"?") }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, "Its mods, configs, worlds and screenshots are removed from disk. "+
				"There is no undo. The shared game files stay.", th.P.TextMid)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return th.wrapped(gtx, d.inst.Path, th.P.TextDim)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.layoutButton(gtx, &d.confirm, buttonStyle{
						bg: th.P.Bad, hoverBg: mix(th.P.Bad, rgb(0xFFFFFF), 0.1), fg: rgb(0xFFFFFF),
						size: sizeBody, inset: layout.Inset{Top: sp2, Bottom: sp2, Left: sp3, Right: sp3},
					}, th.buttonLabel(buttonStyle{fg: rgb(0xFFFFFF), size: sizeBody}, "Delete "+d.inst.Name))
				}),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &d.cancel, nil, "Keep it") }),
			)
		}),
	)
}
