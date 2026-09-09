package gui

import (
	"fmt"
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/micromanage/internal/instance"
	"github.com/GeraldHofbauerWeb/micromanage/internal/launcher"
)

// pickerSource says where a picker's options come from. Options are read
// from the snapshot every frame, so a list that arrives while the picker is
// open simply appears.
type pickerSource int

const (
	pickInstances pickerSource = iota
	pickMinecraft
	pickLoader
)

// pickerOption is one row of a picker.
type pickerOption struct {
	value  string
	detail string
	dim    bool
}

// picker is a modal list with a filter, replacing a dropdown. A dropdown
// that holds a hundred Forge builds is not a dropdown any more; a filtered
// list with the newest on top is what the choice actually is.
type picker struct {
	open   bool
	title  string
	source pickerSource
	kind   instance.LoaderType
	mc     string
	// current is the value already chosen, shown selected.
	current string
	// allowCustom lets what was typed into the filter be used as the value,
	// for a version the list does not know yet.
	allowCustom bool
	onPick      func(string)

	filter *widget.Editor
	list   *widget.List
	rows   []widget.Clickable
	custom widget.Clickable
	cancel widget.Clickable
}

func newPicker() picker {
	return picker{filter: newEditor(), list: newList()}
}

// show opens the picker and asks the controller for the list it needs.
func (p *picker) show(u *ui, title string, source pickerSource, kind instance.LoaderType, mc, current string, allowCustom bool, onPick func(string)) {
	p.open = true
	p.title = title
	p.source = source
	p.kind = kind
	p.mc = mc
	p.current = current
	p.allowCustom = allowCustom
	p.onPick = onPick
	p.filter.SetText("")
	p.list.Position = layout.Position{}

	// The offscreen renderer has no controller; the list is then whatever
	// the snapshot already holds.
	if u.ctrl == nil {
		return
	}
	switch source {
	case pickMinecraft:
		u.ctrl.Dispatch(launcher.ActionListVersions{})
	case pickLoader:
		u.ctrl.Dispatch(launcher.ActionListVersions{Kind: kind, MC: mc})
	}
}

// options builds the rows from the snapshot.
func (p *picker) options(snap launcher.Snapshot) (opts []pickerOption, pending bool) {
	switch p.source {
	case pickInstances:
		for _, inst := range snap.Instances {
			detail := "not set up"
			if inst.Configured {
				detail = inst.MinecraftVersion + " · " + inst.Loader.String()
			}
			opts = append(opts, pickerOption{value: inst.Name, detail: detail})
		}
	case pickMinecraft:
		pending = snap.VersionsPending[launcher.MCVersionsKey]
		for i, id := range snap.MCVersions {
			detail := ""
			if i == 0 {
				detail = "latest release"
			}
			opts = append(opts, pickerOption{value: id, detail: detail})
		}
	case pickLoader:
		key := launcher.VersionsKey(p.kind, p.mc)
		pending = snap.VersionsPending[key]
		for i, v := range snap.LoaderVersions[key] {
			detail := "for " + p.mc
			switch {
			case !v.Stable:
				detail = "pre-release · " + detail
			case i == 0:
				detail = "latest · " + detail
			}
			opts = append(opts, pickerOption{value: v.Version, detail: detail, dim: !v.Stable})
		}
	}
	return opts, pending
}

func (p *picker) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot) layout.Dimensions {
	th := u.th
	if p.cancel.Clicked(gtx) {
		p.open = false
	}

	all, pending := p.options(snap)
	filter := strings.TrimSpace(p.filter.Text())
	shown := all
	if filter != "" {
		shown = shown[:0:0]
		needle := strings.ToLower(filter)
		for _, o := range all {
			if strings.Contains(strings.ToLower(o.value), needle) {
				shown = append(shown, o)
			}
		}
	}
	for len(p.rows) < len(shown) {
		p.rows = append(p.rows, widget.Clickable{})
	}
	for i := range shown {
		if p.rows[i].Clicked(gtx) {
			p.open = false
			p.onPick(shown[i].value)
		}
	}
	exact := false
	for _, o := range all {
		if o.value == filter {
			exact = true
		}
	}
	if p.allowCustom && filter != "" && !exact && p.custom.Clicked(gtx) {
		p.open = false
		p.onPick(filter)
	}

	return column(gtx, sp3,
		rigid(func(gtx layout.Context) layout.Dimensions { return th.display(gtx, p.title) }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			hint := "Filter"
			if p.allowCustom {
				hint = "Filter, or type a version"
			}
			return th.input(gtx, p.filter, hint, u.ic.Search)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			if !p.allowCustom || filter == "" || exact {
				return layout.Dimensions{}
			}
			return th.ghost(gtx, &p.custom, u.ic.Check, "Use \""+filter+"\"")
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(unit.Dp(380)))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			switch {
			case pending && len(all) == 0:
				return th.small(gtx, "Fetching the list…")
			case len(all) == 0 && snap.Err != nil:
				return th.notice(gtx, u.ic.Warning, snap.Err.Error(), th.P.Bad)
			case len(all) == 0:
				return th.small(gtx, "Nothing to choose from.")
			case len(shown) == 0:
				return th.small(gtx, "Nothing matches.")
			}
			return th.list(gtx, p.list, len(shown), func(gtx layout.Context, i int) layout.Dimensions {
				return p.layoutRow(gtx, u, shown[i], &p.rows[i])
			})
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &p.cancel, nil, "Cancel") }),
				flexFill(),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if len(all) == 0 {
						return layout.Dimensions{}
					}
					return th.monoIn(gtx, fmt.Sprintf("%d", len(all)), th.P.TextDim)
				}),
			)
		}),
	)
}

func (p *picker) layoutRow(gtx layout.Context, u *ui, o pickerOption, click *widget.Clickable) layout.Dimensions {
	th := u.th
	selected := o.value == p.current
	return th.selectableRow(gtx, click, selected, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					if o.dim {
						return th.monoIn(gtx, o.value, th.P.TextDim)
					}
					return th.monoIn(gtx, o.value, th.P.Text)
				}),
				flexFill(),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if o.detail == "" {
						return layout.Dimensions{}
					}
					return th.small(gtx, o.detail)
				}),
			)
		})
	})
}

// selectField is a labelled button showing a chosen value; pressing it
// opens a picker. It stands where a dropdown would.
func (t *Theme) selectField(gtx layout.Context, u *ui, click *widget.Clickable, label, value, placeholder string) layout.Dimensions {
	return column(gtx, unit.Dp(5),
		rigid(func(gtx layout.Context) layout.Dimensions { return t.small(gtx, label) }),
		rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			border := t.P.Line
			if click.Hovered() {
				border = t.P.TextDim
			}
			return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
				return fill(gtx, t.P.Bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
					return outlined(gtx, border, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return row(gtx, sp2,
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										if value == "" {
											return t.text(gtx, placeholder, sizeBody, 0, t.P.TextDim)
										}
										return t.body(gtx, value)
									}),
									rigid(func(gtx layout.Context) layout.Dimensions {
										gtx.Constraints.Min = image.Pt(gtx.Dp(18), gtx.Dp(18))
										gtx.Constraints.Max = gtx.Constraints.Min
										return u.ic.DropDown.Layout(gtx, t.P.TextMid)
									}),
								)
							})
					})
				})
			})
		}),
	)
}
