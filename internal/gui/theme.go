package gui

import (
	"image"
	"image/color"

	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Palette is the launcher's colour scheme. It is dark deliberately: this sits
// next to a game window, and a white panel between sessions is unpleasant.
type Palette struct {
	Bg        color.NRGBA
	Panel     color.NRGBA
	PanelHigh color.NRGBA
	Border    color.NRGBA
	Text      color.NRGBA
	TextDim   color.NRGBA
	Accent    color.NRGBA
	AccentDim color.NRGBA
	Good      color.NRGBA
	Warn      color.NRGBA
	Bad       color.NRGBA
}

// Theme bundles the Gio material theme with our palette and metrics.
type Theme struct {
	*material.Theme
	P Palette
}

// NewTheme builds the application theme.
func NewTheme() *Theme {
	p := Palette{
		Bg:        rgb(0x16181D),
		Panel:     rgb(0x1E2128),
		PanelHigh: rgb(0x272B34),
		Border:    rgb(0x343945),
		Text:      rgb(0xE6E8EC),
		TextDim:   rgb(0x9AA0AC),
		Accent:    rgb(0x5B8DEF),
		AccentDim: rgb(0x33507F),
		Good:      rgb(0x4CAF7D),
		Warn:      rgb(0xD8A657),
		Bad:       rgb(0xE05C5C),
	}

	base := material.NewTheme()
	base.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	base.Palette.Bg = p.Bg
	base.Palette.Fg = p.Text
	base.Palette.ContrastBg = p.Accent
	base.Palette.ContrastFg = rgb(0xFFFFFF)

	return &Theme{Theme: base, P: p}
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
}

// withAlpha returns c at the given opacity.
func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// Spacing constants keep the layout consistent without magic numbers spread
// through the screens.
var (
	SpaceXS = unit.Dp(4)
	SpaceS  = unit.Dp(8)
	SpaceM  = unit.Dp(14)
	SpaceL  = unit.Dp(22)
)

// fill paints a rounded rectangle behind the widget.
func fill(gtx layout.Context, c color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := w(gtx)
	call := macro.Stop()

	rect := clip.RRect{
		Rect: image.Rectangle{Max: dims.Size},
		SE:   gtx.Dp(radius), SW: gtx.Dp(radius),
		NE: gtx.Dp(radius), NW: gtx.Dp(radius),
	}
	defer rect.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	call.Add(gtx.Ops)
	return dims
}

// border draws a one-pixel outline around a widget.
func border(gtx layout.Context, c color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	return widget.Border{
		Color:        c,
		CornerRadius: radius,
		Width:        unit.Dp(1),
	}.Layout(gtx, w)
}

// panel is the standard container: a filled, outlined, padded box.
func (t *Theme) panel(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return fill(gtx, t.P.Panel, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
		return border(gtx, t.P.Border, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(SpaceM).Layout(gtx, w)
		})
	})
}

// label renders body text.
func (t *Theme) label(gtx layout.Context, txt string) layout.Dimensions {
	l := material.Body1(t.Theme, txt)
	l.Color = t.P.Text
	return l.Layout(gtx)
}

// dim renders secondary text.
func (t *Theme) dim(gtx layout.Context, txt string) layout.Dimensions {
	l := material.Body2(t.Theme, txt)
	l.Color = t.P.TextDim
	return l.Layout(gtx)
}

// heading renders a section title.
func (t *Theme) heading(gtx layout.Context, txt string) layout.Dimensions {
	l := material.H6(t.Theme, txt)
	l.Color = t.P.Text
	return l.Layout(gtx)
}

// coloured renders text in a specific colour.
func (t *Theme) coloured(gtx layout.Context, txt string, c color.NRGBA) layout.Dimensions {
	l := material.Body2(t.Theme, txt)
	l.Color = c
	return l.Layout(gtx)
}

// spacer inserts vertical space.
func spacer(h unit.Dp) layout.FlexChild {
	return layout.Rigid(layout.Spacer{Height: h}.Layout)
}

// hspacer inserts horizontal space.
func hspacer(w unit.Dp) layout.FlexChild {
	return layout.Rigid(layout.Spacer{Width: w}.Layout)
}
