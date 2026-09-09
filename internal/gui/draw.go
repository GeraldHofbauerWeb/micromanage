package gui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// fill paints a rounded rectangle behind a widget.
func fill(gtx layout.Context, c color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := w(gtx)
	call := macro.Stop()

	r := gtx.Dp(radius)
	defer clip.RRect{Rect: image.Rectangle{Max: dims.Size}, SE: r, SW: r, NE: r, NW: r}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	call.Add(gtx.Ops)
	return dims
}

// fillMax paints the whole available area and lays the widget out on it.
func fillMax(gtx layout.Context, c color.NRGBA, w layout.Widget) layout.Dimensions {
	gtx.Constraints.Min = gtx.Constraints.Max
	return fill(gtx, c, 0, w)
}

// outlined draws a one-pixel border around a widget.
func outlined(gtx layout.Context, c color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	return widget.Border{Color: c, CornerRadius: radius, Width: unit.Dp(1)}.Layout(gtx, w)
}

// rect paints a solid rectangle of a fixed size.
func rect(gtx layout.Context, c color.NRGBA, size image.Point) layout.Dimensions {
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: size}
}

// hairline draws a horizontal one-pixel line across the available width.
func hairline(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	return rect(gtx, c, image.Pt(gtx.Constraints.Max.X, 1))
}

// vline draws a vertical one-pixel line down the available height.
func vline(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	return rect(gtx, c, image.Pt(1, gtx.Constraints.Max.Y))
}

// dot paints a small filled circle, for status.
func dot(gtx layout.Context, c color.NRGBA, d unit.Dp) layout.Dimensions {
	px := gtx.Dp(d)
	defer clip.Ellipse{Max: image.Pt(px, px)}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(px, px)}
}

// slab draws the launcher's mark at a given size: three stacked isometric
// slabs, the top face in the given colour. On the rail it stands for an
// instance, coloured by its loader, so a row of them reads like a shelf of
// packs rather than a list of file names.
//
// The geometry is the icon's, scaled: each slab is a rhombus top face over
// two side faces, and the three are offset vertically.
func slab(gtx layout.Context, top color.NRGBA, size unit.Dp) layout.Dimensions {
	s := float32(gtx.Dp(size))
	w := s          // full width
	h := s * 0.5    // top face height (isometric 2:1)
	d := s * 0.12   // slab thickness
	gap := s * 0.24 // vertical distance between slabs
	total := h + d + 2*gap

	left := mix(top, rgb(0x000000), 0.45)
	right := mix(top, rgb(0x000000), 0.62)
	dimTop := mix(top, rgb(0x0C0E12), 0.55)

	draw := func(y float32, faceTop, faceL, faceR color.NRGBA) {
		// Top face.
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(w/2, y))
		p.LineTo(f32.Pt(w, y+h/2))
		p.LineTo(f32.Pt(w/2, y+h))
		p.LineTo(f32.Pt(0, y+h/2))
		p.Close()
		paint.FillShape(gtx.Ops, faceTop, clip.Outline{Path: p.End()}.Op())

		// Left side.
		var l clip.Path
		l.Begin(gtx.Ops)
		l.MoveTo(f32.Pt(0, y+h/2))
		l.LineTo(f32.Pt(w/2, y+h))
		l.LineTo(f32.Pt(w/2, y+h+d))
		l.LineTo(f32.Pt(0, y+h/2+d))
		l.Close()
		paint.FillShape(gtx.Ops, faceL, clip.Outline{Path: l.End()}.Op())

		// Right side.
		var r clip.Path
		r.Begin(gtx.Ops)
		r.MoveTo(f32.Pt(w/2, y+h))
		r.LineTo(f32.Pt(w, y+h/2))
		r.LineTo(f32.Pt(w, y+h/2+d))
		r.LineTo(f32.Pt(w/2, y+h+d))
		r.Close()
		paint.FillShape(gtx.Ops, faceR, clip.Outline{Path: r.End()}.Op())
	}

	// Bottom slab first so the upper ones overlap it.
	grey := rgb(0x2E3440)
	draw(2*gap, dimTop, mix(grey, rgb(0x000000), 0.3), mix(grey, rgb(0x000000), 0.5))
	draw(gap, dimTop, mix(grey, rgb(0x000000), 0.3), mix(grey, rgb(0x000000), 0.5))
	draw(0, top, left, right)

	return layout.Dimensions{Size: image.Pt(int(w), int(total))}
}

// progress draws a thin determinate bar; fraction 0 draws only the track.
func (t *Theme) progress(gtx layout.Context, fraction float32, height unit.Dp) layout.Dimensions {
	h := gtx.Dp(height)
	w := gtx.Constraints.Max.X
	rect(gtx, t.P.Raised, image.Pt(w, h))
	if fraction > 0 {
		if fraction > 1 {
			fraction = 1
		}
		rect(gtx, t.P.Torch, image.Pt(int(float32(w)*fraction), h))
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// --- layout helpers ---

func spacer(h unit.Dp) layout.FlexChild  { return layout.Rigid(layout.Spacer{Height: h}.Layout) }
func hspacer(w unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Width: w}.Layout) }

// flexFill is a flexed child that takes whatever room is left and draws nothing.
func flexFill() layout.FlexChild {
	return layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
	})
}

// row lays widgets out horizontally, vertically centred, with a gap.
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

// column lays widgets out vertically with a gap.
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

// wide makes a widget claim the full available width.
func wide(w layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return w(gtx)
	}
}

// rigid wraps a widget as a rigid flex child.
func rigid(w layout.Widget) layout.FlexChild { return layout.Rigid(w) }

// none is an empty widget.
func none(layout.Context) layout.Dimensions { return layout.Dimensions{} }

// mark draws the launcher's icon at a given size: the rounded dark tile
// with three stacked instances, the top one in the accent. It is the same
// geometry as packaging/minecraft-instance-manager.svg, scaled, so the
// window and the application menu agree on what this program looks like.
func mark(gtx layout.Context, size unit.Dp) layout.Dimensions {
	s := float32(gtx.Dp(size))
	at := func(v float32) float32 { return v / 512 * s }

	// The tile.
	radius := int(at(112))
	tile := image.Rectangle{Max: image.Pt(int(s), int(s))}
	paint.FillShape(gtx.Ops, rgb(0x1E2128),
		clip.RRect{Rect: tile, SE: radius, SW: radius, NE: radius, NW: radius}.Op(gtx.Ops))
	border := clip.RRect{Rect: tile, SE: radius, SW: radius, NE: radius, NW: radius}.Path(gtx.Ops)
	paint.FillShape(gtx.Ops, rgb(0x343945), clip.Stroke{Path: border, Width: max(1, at(3))}.Op())

	// One instance: a rhombus top face over two side faces, 300 wide and
	// 150 tall on top, 36 deep, centred at x=256 with its top at y.
	slabAt := func(y float32, top, left, right color.NRGBA) clip.PathSpec {
		cx, w, h, d := at(256), at(150), at(75), at(36)
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(cx, at(y)))
		p.LineTo(f32.Pt(cx+w, at(y)+h))
		p.LineTo(f32.Pt(cx, at(y)+2*h))
		p.LineTo(f32.Pt(cx-w, at(y)+h))
		p.Close()
		face := p.End()
		paint.FillShape(gtx.Ops, top, clip.Outline{Path: face}.Op())

		var l clip.Path
		l.Begin(gtx.Ops)
		l.MoveTo(f32.Pt(cx-w, at(y)+h))
		l.LineTo(f32.Pt(cx, at(y)+2*h))
		l.LineTo(f32.Pt(cx, at(y)+2*h+d))
		l.LineTo(f32.Pt(cx-w, at(y)+h+d))
		l.Close()
		paint.FillShape(gtx.Ops, left, clip.Outline{Path: l.End()}.Op())

		var r clip.Path
		r.Begin(gtx.Ops)
		r.MoveTo(f32.Pt(cx, at(y)+2*h))
		r.LineTo(f32.Pt(cx+w, at(y)+h))
		r.LineTo(f32.Pt(cx+w, at(y)+h+d))
		r.LineTo(f32.Pt(cx, at(y)+2*h+d))
		r.Close()
		paint.FillShape(gtx.Ops, right, clip.Outline{Path: r.End()}.Op())
		return face
	}

	slabAt(261, rgb(0x2E5C39), rgb(0x24492D), rgb(0x1C3A24))
	slabAt(173, rgb(0x3E7A4B), rgb(0x2E5C39), rgb(0x24492D))
	active := slabAt(85, rgb(0x5B8DEF), rgb(0x3F66B5), rgb(0x33507F))
	// The hairline that keeps the active block's shape at small sizes.
	paint.FillShape(gtx.Ops, rgb(0x8FB2F5), clip.Stroke{Path: active, Width: max(1, at(3))}.Op())

	return layout.Dimensions{Size: image.Pt(int(s), int(s))}
}
