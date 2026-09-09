package gui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// statsWidth keeps the panel to a readable column even on a wide window;
// the bars carry the comparison, and a bar two feet long says no more than
// a short one.
const statsWidth = unit.Dp(560)

// statsRows is how many instances the panel names. The rest are added up
// into one last line, so the totals still agree with the headline.
const statsRows = 5

// statsPanel is the playtime summary on the start screen: what has been
// played, per instance and per day.
type statsPanel struct {
	rows []widget.Clickable
}

// Layout draws the panel. Compact leaves out the daily chart, for a window
// too short to hold it under the hero.
func (p *statsPanel) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot, compact bool) layout.Dimensions {
	th := u.th
	stats := snap.Stats
	if !stats.Played() {
		return layout.Dimensions{}
	}

	for len(p.rows) < len(stats.Instances) {
		p.rows = append(p.rows, widget.Clickable{})
	}
	for i, play := range stats.Instances {
		if i < len(p.rows) && p.rows[i].Clicked(gtx) {
			u.dispatch(launcher.ActionSelect{Name: play.Name})
		}
	}

	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(statsWidth))
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.smallIn(gtx, "PLAYTIME", th.P.TextDim)
				}),
				flexFill(),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if stats.Last.IsZero() {
						return layout.Dimensions{}
					}
					return th.monoIn(gtx, "last played "+humaniseSince(stats.Last), th.P.TextDim)
				}),
			)
		}),
		spacer(sp2),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(th.Theme, sizeDisplay, formatPlaytime(stats.Total))
					l.Font.Typeface = faceDisplay
					l.Font.Weight = font.Bold
					l.Color = th.P.Text
					return l.Layout(gtx)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					return th.smallIn(gtx, statsSubtitle(stats), th.P.TextDim)
				}),
			)
		}),
	}

	if !compact && stats.InWindow() > 0 {
		children = append(children,
			spacer(sp4),
			rigid(func(gtx layout.Context) layout.Dimensions { return p.layoutDays(gtx, u, stats) }),
		)
	}

	children = append(children, spacer(sp3))
	shown := stats.Instances
	if len(shown) > statsRows {
		shown = shown[:statsRows]
	}
	for i, play := range shown {
		i, play := i, play
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutRow(gtx, u, &p.rows[i], play)
		}))
	}
	if rest := stats.Instances[len(shown):]; len(rest) > 0 {
		var total time.Duration
		for _, play := range rest {
			total += play.Total
		}
		children = append(children, spacer(sp1), rigid(func(gtx layout.Context) layout.Dimensions {
			return th.smallIn(gtx, fmt.Sprintf("%d more · %s", len(rest), formatPlaytime(total)), th.P.TextDim)
		}))
	}

	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// statsSubtitle says what the headline figure is made of.
func statsSubtitle(stats instance.PlayStats) string {
	line := "across " + count(len(stats.Instances), "instance")
	if stats.Sessions > 0 {
		line += " · " + count(stats.Sessions, "session")
	}
	if stats.Longest > 0 {
		line += " · longest " + formatPlaytime(stats.Longest)
	}
	return line
}

// count says how many, with the word in the right number.
func count(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// layoutDays is the daily chart: one column per day, today on the right.
func (p *statsPanel) layoutDays(gtx layout.Context, u *ui, stats instance.PlayStats) layout.Dimensions {
	th := u.th
	busiest := stats.BusiestDay()

	columns := make([]layout.FlexChild, 0, len(stats.Days)*2)
	for i, day := range stats.Days {
		day := day
		if i > 0 {
			columns = append(columns, hspacer(sp1))
		}
		share := 0.0
		if busiest.Total > 0 {
			share = float64(day.Total) / float64(busiest.Total)
		}
		c := th.P.Sky
		if day.Day.Equal(busiest.Day) {
			c = th.P.Torch
		}
		columns = append(columns, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return dayColumn(gtx, c, share, unit.Dp(52))
		}))
	}

	return column(gtx, sp2,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx, columns...)
				}),
				// The days nothing was played draw nothing, so the chart
				// stands on a line rather than on a row of grey blocks.
				rigid(func(gtx layout.Context) layout.Dimensions { return hairline(gtx, th.P.Line) }),
			)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			first, last := "", "today"
			if len(stats.Days) > 0 {
				first = stats.Days[0].Day.Format("2 Jan")
			}
			return row(gtx, sp2,
				rigid(func(gtx layout.Context) layout.Dimensions { return th.monoIn(gtx, first, th.P.TextDim) }),
				flexFill(),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if busiest.Total == 0 {
						return layout.Dimensions{}
					}
					return th.monoIn(gtx, busiest.Day.Format("Mon 2 Jan")+" · "+formatPlaytime(busiest.Total), th.P.Torch)
				}),
				flexFill(),
				rigid(func(gtx layout.Context) layout.Dimensions { return th.monoIn(gtx, last, th.P.TextDim) }),
			)
		}),
	)
}

// layoutRow is one instance's share, as a bar in its loader's colour.
func (p *statsPanel) layoutRow(gtx layout.Context, u *ui, click *widget.Clickable, play instance.InstancePlay) layout.Dimensions {
	th := u.th
	c := th.loaderColor(play.Loader.Type)
	fg := th.P.TextMid
	if click.Hovered() {
		fg = th.P.Text
	}

	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp3,
				rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(130))
					gtx.Constraints.Max.X = gtx.Constraints.Min.X
					return th.text(gtx, play.Name, sizeSmall, font.Normal, fg)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return statBar(gtx, c, th.P.Raised, play.Share, unit.Dp(8))
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(76))
					return th.monoIn(gtx, formatPlaytime(play.Total), th.P.TextMid)
				}),
			)
		})
	})
}

// statBar is a horizontal bar: the track, and the share filled in.
func statBar(gtx layout.Context, fg, track color.NRGBA, share float64, height unit.Dp) layout.Dimensions {
	w, h := gtx.Constraints.Max.X, gtx.Dp(height)
	rect(gtx, track, image.Pt(w, h))
	if n := barLength(gtx, share, w); n > 0 {
		rect(gtx, fg, image.Pt(n, h))
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// dayColumn is a vertical bar, growing from the baseline.
func dayColumn(gtx layout.Context, fg color.NRGBA, share float64, height unit.Dp) layout.Dimensions {
	w, h := gtx.Constraints.Max.X, gtx.Dp(height)
	if n := barLength(gtx, share, h); n > 0 {
		defer op.Offset(image.Pt(0, h-n)).Push(gtx.Ops).Pop()
		rect(gtx, fg, image.Pt(w, n))
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// barLength turns a share into pixels, keeping a played day visible even
// when it is a rounding error next to a long one.
func barLength(gtx layout.Context, share float64, full int) int {
	if share <= 0 {
		return 0
	}
	n := int(share*float64(full) + 0.5)
	if smallest := gtx.Dp(unit.Dp(3)); n < smallest {
		n = smallest
	}
	return min(n, full)
}
