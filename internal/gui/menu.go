package gui

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
)

// menuItem is one line of a context menu. A nil do makes it inert, shown
// dimmed with its note beside it, which says why rather than hiding the
// option.
type menuItem struct {
	label  string
	icon   *widget.Icon
	danger bool
	note   string
	// divider draws a line above the item.
	divider bool
	do      func()
}

// contextMenu is a popup at the pointer, opened by a right-click or a "⋯"
// button. It closes on a choice, on a click anywhere else, and on Escape.
type contextMenu struct {
	open  bool
	at    image.Point
	title string
	items []menuItem
	rows  []widget.Clickable

	// scrim and panel are the pointer tags of the two areas: everything
	// outside the panel closes the menu; the panel swallows what lands in
	// its padding so the scrim never sees it. They are bytes rather than
	// empty structs because Go gives adjacent zero-size fields the same
	// address, and a tag is its address.
	scrim, panel byte
}

// menuWidth is the panel's width; long labels wrap rather than widen it.
const menuWidth = unit.Dp(224)

func (m *contextMenu) show(at image.Point, title string, items []menuItem) {
	m.open = true
	m.at = at
	m.title = title
	m.items = items
	for len(m.rows) < len(items) {
		m.rows = append(m.rows, widget.Clickable{})
	}
}

func (m *contextMenu) close() {
	m.open = false
	m.items = nil
}

// Layout draws the menu as a layer over the whole window. It returns the
// window's size so the caller can stack it.
func (m *contextMenu) Layout(gtx layout.Context, u *ui) layout.Dimensions {
	if !m.open {
		return layout.Dimensions{}
	}
	th := u.th

	// Any press outside the panel closes the menu, whichever button.
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &m.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			m.close()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}
	// The panel's own presses are consumed so they never reach the scrim.
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &m.panel, Kinds: pointer.Press}); !ok {
			break
		}
	}
	for i := range m.items {
		if m.items[i].do != nil && m.rows[i].Clicked(gtx) {
			do := m.items[i].do
			m.close()
			do()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}

	// The scrim: the whole window. Its area is closed again at once so
	// the panel is a sibling rather than a child — Gio hands a press to a
	// handler's ancestors as well, and a scrim that were an ancestor would
	// close the menu on the press that starts every click inside it.
	scrim := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &m.scrim)
	scrim.Pop()

	// Measure the panel, then place it so it stays inside the window: to
	// the left of the pointer when there is no room on the right, above it
	// when there is none below.
	macro := op.Record(gtx.Ops)
	panelGtx := gtx
	panelGtx.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(menuWidth), gtx.Constraints.Max.Y)}
	dims := m.layoutPanel(panelGtx, u)
	call := macro.Stop()

	pos := m.at
	margin := gtx.Dp(sp2)
	if pos.X+dims.Size.X+margin > gtx.Constraints.Max.X {
		pos.X = max(margin, pos.X-dims.Size.X)
	}
	if pos.Y+dims.Size.Y+margin > gtx.Constraints.Max.Y {
		pos.Y = max(margin, gtx.Constraints.Max.Y-dims.Size.Y-margin)
	}

	defer op.Offset(pos).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &m.panel)
	call.Add(gtx.Ops)
	_ = th
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// layoutPanel draws the raised card with its rows.
func (m *contextMenu) layoutPanel(gtx layout.Context, u *ui) layout.Dimensions {
	th := u.th
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	children := make([]layout.FlexChild, 0, len(m.items)+1)
	if m.title != "" {
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return th.smallIn(gtx, m.title, th.P.TextDim)
				})
		}))
	}
	for i := range m.items {
		i := i
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return m.layoutItem(gtx, u, &m.items[i], &m.rows[i])
		}))
	}

	return fill(gtx, th.P.Raised, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
		return outlined(gtx, th.P.Line, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
		})
	})
}

func (m *contextMenu) layoutItem(gtx layout.Context, u *ui, item *menuItem, click *widget.Clickable) layout.Dimensions {
	th := u.th
	enabled := item.do != nil

	fg := th.P.Text
	switch {
	case !enabled:
		fg = th.P.TextDim
	case item.danger:
		fg = th.P.Bad
	}
	bg := color.NRGBA{}
	if enabled && click.Hovered() {
		bg = th.P.Hover
		if item.danger {
			bg = alpha(th.P.Bad, 0x28)
		}
	}

	body := func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return fill(gtx, bg, 0, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return row(gtx, unit.Dp(10),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if item.icon == nil {
								return layout.Dimensions{Size: image.Pt(gtx.Dp(16), 0)}
							}
							gtx.Constraints.Min = image.Pt(gtx.Dp(16), gtx.Dp(16))
							gtx.Constraints.Max = gtx.Constraints.Min
							return item.icon.Layout(gtx, fg)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions {
							return th.text(gtx, item.label, sizeBody, 0, fg)
						}),
						flexFill(),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if item.note == "" {
								return layout.Dimensions{}
							}
							return th.monoIn(gtx, item.note, th.P.TextDim)
						}),
					)
				})
		})
	}

	return column(gtx, 0,
		rigid(func(gtx layout.Context) layout.Dimensions {
			if !item.divider {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return hairline(gtx, th.P.Line)
			})
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			if !enabled {
				return body(gtx)
			}
			return pressable(gtx, click, body)
		}),
	)
}
