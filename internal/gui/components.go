package gui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// button renders a filled action button.
func (t *Theme) button(gtx layout.Context, click *widget.Clickable, label string) layout.Dimensions {
	b := material.Button(t.Theme, click, label)
	b.Background = t.P.Accent
	b.Color = rgb(0xFFFFFF)
	b.CornerRadius = unit.Dp(6)
	b.Inset = layout.Inset{Top: SpaceS, Bottom: SpaceS, Left: SpaceM, Right: SpaceM}
	return b.Layout(gtx)
}

// primary renders the one button a screen is built around.
func (t *Theme) primary(gtx layout.Context, click *widget.Clickable, label string) layout.Dimensions {
	b := material.Button(t.Theme, click, label)
	b.Background = t.P.Accent
	b.Color = rgb(0xFFFFFF)
	b.CornerRadius = unit.Dp(8)
	b.TextSize = unit.Sp(17)
	b.Inset = layout.Inset{Top: SpaceM, Bottom: SpaceM, Left: unit.Dp(34), Right: unit.Dp(34)}
	return b.Layout(gtx)
}

// secondary renders a quieter button.
func (t *Theme) secondary(gtx layout.Context, click *widget.Clickable, label string) layout.Dimensions {
	b := material.Button(t.Theme, click, label)
	b.Background = t.P.PanelHigh
	b.Color = t.P.Text
	b.CornerRadius = unit.Dp(6)
	b.Inset = layout.Inset{Top: SpaceS, Bottom: SpaceS, Left: SpaceM, Right: SpaceM}
	return b.Layout(gtx)
}

// danger renders a destructive button.
func (t *Theme) danger(gtx layout.Context, click *widget.Clickable, label string) layout.Dimensions {
	b := material.Button(t.Theme, click, label)
	b.Background = t.P.Bad
	b.Color = rgb(0xFFFFFF)
	b.CornerRadius = unit.Dp(6)
	b.Inset = layout.Inset{Top: SpaceXS, Bottom: SpaceXS, Left: SpaceS, Right: SpaceS}
	b.TextSize = unit.Sp(13)
	return b.Layout(gtx)
}

// chip renders a small status pill.
func (t *Theme) chip(gtx layout.Context, text string, fg, bg color.NRGBA) layout.Dimensions {
	return fill(gtx, bg, unit.Dp(10), func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Top: unit.Dp(3), Bottom: unit.Dp(3), Left: SpaceS, Right: SpaceS,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(t.Theme, text)
			l.Color = fg
			l.TextSize = unit.Sp(12)
			return l.Layout(gtx)
		})
	})
}

// editor renders a labelled single-line text field.
func (t *Theme) editor(gtx layout.Context, ed *widget.Editor, label, hint string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return t.dim(gtx, label)
		}),
		spacer(SpaceXS),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// A text field should occupy the width it is given, not shrink to
			// whatever happens to be typed in it.
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return fill(gtx, t.P.Bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
				return border(gtx, t.P.Border, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{
						Top: SpaceS, Bottom: SpaceS, Left: SpaceS, Right: SpaceS,
					}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						e := material.Editor(t.Theme, ed, hint)
						e.Color = t.P.Text
						e.HintColor = t.P.TextDim
						return e.Layout(gtx)
					})
				})
			})
		}),
	)
}

// progressBar renders a determinate or indeterminate bar.
func (t *Theme) progressBar(gtx layout.Context, fraction float32) layout.Dimensions {
	height := gtx.Dp(unit.Dp(6))
	width := gtx.Constraints.Max.X

	// Track.
	defer clip.RRect{
		Rect: image.Rect(0, 0, width, height),
		SE:   height / 2, SW: height / 2, NE: height / 2, NW: height / 2,
	}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: t.P.PanelHigh}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	if fraction > 0 {
		if fraction > 1 {
			fraction = 1
		}
		filled := int(float32(width) * fraction)
		if filled > 0 {
			defer clip.RRect{
				Rect: image.Rect(0, 0, filled, height),
				SE:   height / 2, SW: height / 2, NE: height / 2, NW: height / 2,
			}.Push(gtx.Ops).Pop()
			paint.ColorOp{Color: t.P.Accent}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
		}
	}

	return layout.Dimensions{Size: image.Pt(width, height)}
}

// row lays widgets out horizontally with a gap between them.
func row(gtx layout.Context, gap unit.Dp, children ...layout.FlexChild) layout.Dimensions {
	spaced := make([]layout.FlexChild, 0, len(children)*2)
	for i, c := range children {
		if i > 0 {
			spaced = append(spaced, hspacer(gap))
		}
		spaced = append(spaced, c)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, spaced...)
}

// column lays widgets out vertically with a gap between them.
func column(gtx layout.Context, gap unit.Dp, children ...layout.FlexChild) layout.Dimensions {
	spaced := make([]layout.FlexChild, 0, len(children)*2)
	for i, c := range children {
		if i > 0 {
			spaced = append(spaced, spacer(gap))
		}
		spaced = append(spaced, c)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, spaced...)
}

// clickableRow makes a whole row selectable and highlights it when chosen.
func (t *Theme) clickableRow(gtx layout.Context, click *widget.Clickable, selected bool, w layout.Widget) layout.Dimensions {
	bg := t.P.Panel
	if selected {
		bg = t.P.PanelHigh
	} else if click.Hovered() {
		bg = withAlpha(t.P.PanelHigh, 0xA0)
	}

	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			outline := t.P.Border
			if selected {
				outline = t.P.Accent
			}
			return border(gtx, outline, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(SpaceM).Layout(gtx, w)
			})
		})
	})
}

// scrollList renders a vertical list with a scrollbar.
func (t *Theme) scrollList(gtx layout.Context, list *widget.List, n int, item layout.ListElement) layout.Dimensions {
	l := material.List(t.Theme, list)
	l.Indicator.MinorWidth = unit.Dp(6)
	l.Indicator.Color = t.P.Border
	l.Indicator.HoverColor = t.P.TextDim
	return l.Layout(gtx, n, item)
}

// newList returns a vertical list widget.
func newList() *widget.List {
	return &widget.List{List: layout.List{Axis: layout.Vertical}}
}

// newEditor returns a single-line editor.
func newEditor() *widget.Editor {
	return &widget.Editor{SingleLine: true, Submit: true}
}
