package gui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// pressable wraps a clickable so the pointer turns into a hand over it. Gio
// leaves the cursor alone by default, and a flat design gives no other hint
// that a row can be clicked.
func pressable(gtx layout.Context, click *widget.Clickable, w layout.Widget) layout.Dimensions {
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		dims := w(gtx)
		call := macro.Stop()
		defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		call.Add(gtx.Ops)
		return dims
	})
}

// buttonStyle is the shared shape of every button: an inset label on a
// rounded ground, with hover and press feedback from opacity alone.
type buttonStyle struct {
	bg, hoverBg, fg color.NRGBA
	border          color.NRGBA
	display         bool
	size            unit.Sp
	inset           layout.Inset
}

func (t *Theme) layoutButton(gtx layout.Context, click *widget.Clickable, st buttonStyle, content layout.Widget) layout.Dimensions {
	bg := st.bg
	if click.Hovered() {
		bg = st.hoverBg
	}
	if click.Pressed() {
		bg = mix(st.hoverBg, t.P.Bg, 0.25)
	}
	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			inner := func(gtx layout.Context) layout.Dimensions {
				return st.inset.Layout(gtx, content)
			}
			if st.border.A > 0 {
				return outlined(gtx, st.border, unit.Dp(6), inner)
			}
			return inner(gtx)
		})
	})
}

// buttonLabel renders a button's text in the style's face and size.
func (t *Theme) buttonLabel(st buttonStyle, txt string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(t.Theme, st.size, txt)
		l.Color = st.fg
		l.MaxLines = 1
		if st.display {
			l.Font.Typeface = faceDisplay
			l.Font.Weight = font.SemiBold
		} else {
			l.Font.Typeface = faceBody
			l.Font.Weight = font.Medium
		}
		return l.Layout(gtx)
	}
}

// primary is the torch: there is one per screen, and it is the thing to do.
func (t *Theme) primary(gtx layout.Context, click *widget.Clickable, icon *widget.Icon, label string) layout.Dimensions {
	st := buttonStyle{
		bg: t.P.Torch, hoverBg: mix(t.P.Torch, rgb(0xFFFFFF), 0.12), fg: t.P.TorchInk,
		display: true, size: unit.Sp(16),
		inset: layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(18), Right: unit.Dp(22)},
	}
	return t.layoutButton(gtx, click, st, func(gtx layout.Context) layout.Dimensions {
		return row(gtx, sp1,
			rigid(func(gtx layout.Context) layout.Dimensions {
				if icon == nil {
					return layout.Dimensions{}
				}
				gtx.Constraints.Min = image.Pt(gtx.Dp(20), gtx.Dp(20))
				gtx.Constraints.Max = gtx.Constraints.Min
				return icon.Layout(gtx, st.fg)
			}),
			rigid(t.buttonLabel(st, label)),
		)
	})
}

// secondary is a quiet outlined button.
func (t *Theme) secondary(gtx layout.Context, click *widget.Clickable, label string) layout.Dimensions {
	st := buttonStyle{
		bg: t.P.Raised, hoverBg: t.P.Hover, fg: t.P.Text, border: t.P.Line,
		size:  sizeBody,
		inset: layout.Inset{Top: sp2, Bottom: sp2, Left: sp3, Right: sp3},
	}
	return t.layoutButton(gtx, click, st, t.buttonLabel(st, label))
}

// ghost is a button with no ground until hovered, for actions that should
// not compete with the content around them.
func (t *Theme) ghost(gtx layout.Context, click *widget.Clickable, icon *widget.Icon, label string) layout.Dimensions {
	return t.ghostIn(gtx, click, icon, label, t.P.TextMid, t.P.Hover)
}

// danger is a ghost in the failure colour, for delete.
func (t *Theme) danger(gtx layout.Context, click *widget.Clickable, icon *widget.Icon, label string) layout.Dimensions {
	return t.ghostIn(gtx, click, icon, label, t.P.Bad, alpha(t.P.Bad, 0x28))
}

func (t *Theme) ghostIn(gtx layout.Context, click *widget.Clickable, icon *widget.Icon, label string, fg, hover color.NRGBA) layout.Dimensions {
	st := buttonStyle{
		bg: color.NRGBA{}, hoverBg: hover, fg: fg,
		size:  sizeSmall,
		inset: layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(10)},
	}
	if label == "" {
		st.inset.Right = unit.Dp(8)
	}
	if click.Hovered() {
		st.fg = t.P.Text
		if fg == t.P.Bad {
			st.fg = t.P.Bad
		}
	}
	return t.layoutButton(gtx, click, st, func(gtx layout.Context) layout.Dimensions {
		return row(gtx, unit.Dp(6),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if icon == nil {
					return layout.Dimensions{}
				}
				gtx.Constraints.Min = image.Pt(gtx.Dp(16), gtx.Dp(16))
				gtx.Constraints.Max = gtx.Constraints.Min
				return icon.Layout(gtx, st.fg)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if label == "" {
					return layout.Dimensions{}
				}
				return t.buttonLabel(st, label)(gtx)
			}),
		)
	})
}

// iconButton is a square ghost holding one glyph, for the bars.
func (t *Theme) iconButton(gtx layout.Context, click *widget.Clickable, icon *widget.Icon) layout.Dimensions {
	fg := t.P.TextMid
	bg := color.NRGBA{}
	if click.Hovered() {
		fg, bg = t.P.Text, t.P.Hover
	}
	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(7)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Pt(gtx.Dp(18), gtx.Dp(18))
				gtx.Constraints.Max = gtx.Constraints.Min
				return icon.Layout(gtx, fg)
			})
		})
	})
}

// chip is a small pill carrying one word of state.
func (t *Theme) chip(gtx layout.Context, txt string, c color.NRGBA) layout.Dimensions {
	return fill(gtx, alpha(c, 0x22), unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(7), Right: unit.Dp(7)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return t.text(gtx, txt, unit.Sp(11.5), font.Medium, c)
			})
	})
}

// tag is a mono label on a dark pill, for versions and counts.
func (t *Theme) tag(gtx layout.Context, txt string) layout.Dimensions {
	return fill(gtx, t.P.Bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return t.monoIn(gtx, txt, t.P.TextMid)
			})
	})
}

// input is a bare text field, optionally led by an icon.
func (t *Theme) input(gtx layout.Context, ed *widget.Editor, hint string, icon *widget.Icon) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	border := t.P.Line
	if gtx.Focused(ed) {
		border = t.P.Sky
	}
	return fill(gtx, t.P.Bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
		return outlined(gtx, border, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return row(gtx, sp2,
						rigid(func(gtx layout.Context) layout.Dimensions {
							if icon == nil {
								return layout.Dimensions{}
							}
							gtx.Constraints.Min = image.Pt(gtx.Dp(16), gtx.Dp(16))
							gtx.Constraints.Max = gtx.Constraints.Min
							return icon.Layout(gtx, t.P.TextDim)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							e := material.Editor(t.Theme, ed, hint)
							e.Font.Typeface = faceBody
							e.TextSize = sizeBody
							e.Color = t.P.Text
							e.HintColor = t.P.TextDim
							return e.Layout(gtx)
						}),
					)
				})
		})
	})
}

// field is a labelled input.
func (t *Theme) field(gtx layout.Context, ed *widget.Editor, label, hint string) layout.Dimensions {
	return column(gtx, unit.Dp(5),
		rigid(func(gtx layout.Context) layout.Dimensions { return t.small(gtx, label) }),
		rigid(func(gtx layout.Context) layout.Dimensions { return t.input(gtx, ed, hint, nil) }),
	)
}

// list renders a vertical list with a slim scrollbar.
func (t *Theme) list(gtx layout.Context, l *widget.List, n int, item layout.ListElement) layout.Dimensions {
	ls := material.List(t.Theme, l)
	ls.Indicator.MinorWidth = unit.Dp(5)
	ls.Indicator.Color = t.P.Line
	ls.Indicator.HoverColor = t.P.TextDim
	ls.Track.Color = color.NRGBA{}
	return ls.Layout(gtx, n, item)
}

// selectableRow is a full-width row that highlights on hover and marks the
// selection with a sky bar on its left edge.
func (t *Theme) selectableRow(gtx layout.Context, click *widget.Clickable, selected bool, w layout.Widget) layout.Dimensions {
	bg := color.NRGBA{}
	switch {
	case selected:
		bg = t.P.Raised
	case click.Hovered():
		bg = alpha(t.P.Hover, 0x90)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, 0, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Stack{}.Layout(gtx,
				layout.Stacked(w),
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					if !selected {
						return layout.Dimensions{}
					}
					return rect(gtx, t.P.Sky, image.Pt(gtx.Dp(3), gtx.Constraints.Min.Y))
				}),
			)
		})
	})
}

// pill is a selectable option in a row of choices.
func (t *Theme) pill(gtx layout.Context, click *widget.Clickable, selected bool, label string) layout.Dimensions {
	bg, fg, border := color.NRGBA{}, t.P.TextMid, t.P.Line
	if selected {
		bg, fg, border = alpha(t.P.Sky, 0x2A), t.P.Text, t.P.Sky
	} else if click.Hovered() {
		bg, fg = t.P.Hover, t.P.Text
	}
	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			return outlined(gtx, border, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: sp3, Right: sp3}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return t.text(gtx, label, sizeSmall, font.Medium, fg)
					})
			})
		})
	})
}

// card is a padded surface with a border.
func (t *Theme) card(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return fill(gtx, t.P.Surface, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
		return outlined(gtx, t.P.LineDim, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(sp4).Layout(gtx, w)
		})
	})
}

// notice is an inline message in a colour, for errors and warnings.
func (t *Theme) notice(gtx layout.Context, icon *widget.Icon, msg string, c color.NRGBA) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return fill(gtx, alpha(c, 0x1C), unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: sp2, Bottom: sp2, Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
					rigid(func(gtx layout.Context) layout.Dimensions {
						if icon == nil {
							return layout.Dimensions{}
						}
						return layout.Inset{Right: sp2, Top: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min = image.Pt(gtx.Dp(16), gtx.Dp(16))
							gtx.Constraints.Max = gtx.Constraints.Min
							return icon.Layout(gtx, c)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return t.wrapped(gtx, msg, c)
					}),
				)
			})
	})
}

// modal dims the screen and centres a panel over it. The scrim swallows
// clicks so the panel is the only thing that responds.
func (t *Theme) modal(gtx layout.Context, scrim *widget.Clickable, width unit.Dp, panel layout.Widget) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return scrim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return fillMax(gtx, alpha(rgb(0x000000), 0x99), none)
			})
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// Centre within the whole window, not within the panel's own size.
			gtx.Constraints.Min = gtx.Constraints.Max
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(sp4)*2, gtx.Dp(width))
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				// A clickable ground so a click inside the panel never
				// reaches the scrim.
				return fill(gtx, t.P.Surface, unit.Dp(10), func(gtx layout.Context) layout.Dimensions {
					return outlined(gtx, t.P.Line, unit.Dp(10), func(gtx layout.Context) layout.Dimensions {
						defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
						return layout.UniformInset(sp4).Layout(gtx, panel)
					})
				})
			})
		}),
	)
}

func newList() *widget.List {
	return &widget.List{List: layout.List{Axis: layout.Vertical}}
}

func newEditor() *widget.Editor {
	return &widget.Editor{SingleLine: true, Submit: true}
}
