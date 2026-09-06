package gui

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// createDialog is the "new instance" modal. Creating from scratch is the
// default because cloning costs gigabytes and the shared store already
// supplies everything a fresh instance needs.
type createDialog struct {
	open bool

	name    *widget.Editor
	version *widget.Editor

	loaderChoices []widget.Clickable
	loaderVersion *widget.Editor
	loader        instance.LoaderType

	fromScratch widget.Clickable
	fromClone   widget.Clickable
	clone       bool
	cloneSource string
	cloneRows   []widget.Clickable

	confirm widget.Clickable
	cancel  widget.Clickable
}

func newCreateDialog() createDialog {
	return createDialog{
		name:          newEditor(),
		version:       newEditor(),
		loaderVersion: newEditor(),
		loader:        instance.LoaderVanilla,
	}
}

// show opens the dialog, pre-filling the version from the selected instance so
// the common case of "another one like this" takes one field.
func (d *createDialog) show(snap launcher.Snapshot) {
	d.open = true
	d.name.SetText("")
	d.loader = instance.LoaderVanilla
	d.loaderVersion.SetText("")
	d.clone = false
	d.cloneSource = ""

	if snap.Editing.MinecraftVersion != "" {
		d.version.SetText(snap.Editing.MinecraftVersion)
	}
}

func (d *createDialog) Layout(gtx layout.Context, th *Theme, ctrl *launcher.Controller, snap launcher.Snapshot) layout.Dimensions {
	loaders := instance.LoaderTypes()
	for len(d.loaderChoices) < len(loaders) {
		d.loaderChoices = append(d.loaderChoices, widget.Clickable{})
	}
	for len(d.cloneRows) < len(snap.Instances) {
		d.cloneRows = append(d.cloneRows, widget.Clickable{})
	}

	for i, lt := range loaders {
		if d.loaderChoices[i].Clicked(gtx) {
			d.loader = lt
		}
	}
	if d.fromScratch.Clicked(gtx) {
		d.clone = false
	}
	if d.fromClone.Clicked(gtx) {
		d.clone = true
	}
	for i := range snap.Instances {
		if d.cloneRows[i].Clicked(gtx) {
			d.cloneSource = snap.Instances[i].Name
		}
	}
	if d.cancel.Clicked(gtx) {
		d.open = false
	}
	if d.confirm.Clicked(gtx) {
		action := launcher.ActionCreate{
			Name:    d.name.Text(),
			Version: d.version.Text(),
			Loader:  instance.LoaderSpec{Type: d.loader, Version: d.loaderVersion.Text()},
		}
		if d.clone {
			action.Clone = d.cloneSource
		}
		ctrl.Dispatch(action)
		d.open = false
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(560)))
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		return th.panel(gtx, func(gtx layout.Context) layout.Dimensions {
			return column(gtx, SpaceM,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.heading(gtx, "New instance")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.editor(gtx, d.name, "Name", "my-modpack")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.editor(gtx, d.version, "Minecraft version", "1.21.1")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return d.layoutLoader(gtx, th, loaders)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return d.layoutSource(gtx, th, snap)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return row(gtx, SpaceS,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.button(gtx, &d.confirm, "Create")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.secondary(gtx, &d.cancel, "Cancel")
						}),
					)
				}),
			)
		})
	})
}

func (d *createDialog) layoutLoader(gtx layout.Context, th *Theme, loaders []instance.LoaderType) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "System")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, SpaceXS, loaderChips(gtx, th, loaders, d.loader, d.loaderChoices)...)
		}),
	}
	if d.loader != instance.LoaderVanilla {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.editor(gtx, d.loaderVersion, d.loader.Display()+" version", "21.1.248")
		}))
	}
	return column(gtx, SpaceXS, children...)
}

func (d *createDialog) layoutSource(gtx layout.Context, th *Theme, snap launcher.Snapshot) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "Content")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, SpaceS,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.clickableRow(gtx, &d.fromScratch, !d.clone, func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, "Empty")
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.clickableRow(gtx, &d.fromClone, d.clone, func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, "Clone an instance")
					})
				}),
			)
		}),
	}

	if !d.clone {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.dim(gtx, "Created instantly; game files come from the shared store.")
		}))
		return column(gtx, SpaceXS, children...)
	}

	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return th.dim(gtx, "Mods and configs are copied. Saves and screenshots are not.")
	}))
	for i, inst := range snap.Instances {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: SpaceXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.clickableRow(gtx, &d.cloneRows[i], inst.Name == d.cloneSource,
					func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, inst.Name)
					})
			})
		}))
	}
	return column(gtx, SpaceXS, children...)
}

// loaderChips renders the loader selector as a row of pills.
func loaderChips(gtx layout.Context, th *Theme, loaders []instance.LoaderType,
	current instance.LoaderType, clicks []widget.Clickable) []layout.FlexChild {

	children := make([]layout.FlexChild, 0, len(loaders))
	for i, lt := range loaders {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.clickableRow(gtx, &clicks[i], lt == current, func(gtx layout.Context) layout.Dimensions {
				return th.label(gtx, lt.Display())
			})
		}))
	}
	return children
}
