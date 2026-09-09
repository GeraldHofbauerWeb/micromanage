package gui

import (
	"fmt"
	"image"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/GeraldHofbauerWeb/micromanage/internal/instance"
	"github.com/GeraldHofbauerWeb/micromanage/internal/launch"
	"github.com/GeraldHofbauerWeb/micromanage/internal/launcher"
)

// overview is the workbench's first tab: one card per kind of content,
// each a count and a detail, each a way into its tab. It answers "what is
// in here?" before the player has clicked anything.
type overview struct {
	list  *widget.List
	cards []widget.Clickable
}

func newOverview() overview {
	return overview{list: newList(), cards: make([]widget.Clickable, len(instance.ContentKinds()))}
}

// cardWidth is the width a card is laid out at; the grid wraps to however
// many fit.
const cardWidth = unit.Dp(200)

func (o *overview) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot, inst instance.Instance) layout.Dimensions {
	th := u.th
	kinds := instance.ContentKinds()
	for i := range kinds {
		if o.cards[i].Clicked(gtx) {
			u.bench.tab = i + 1
		}
	}

	return layout.Inset{Top: sp3, Left: sp4, Right: sp4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return th.list(gtx, o.list, 2, func(gtx layout.Context, i int) layout.Dimensions {
			if i == 0 {
				return layout.Inset{Bottom: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return o.layoutGrid(gtx, u, snap, kinds)
				})
			}
			return layout.Inset{Bottom: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return o.layoutFacts(gtx, u, snap, inst)
			})
		})
	})
}

// layoutGrid wraps the cards into rows of whatever fits.
func (o *overview) layoutGrid(gtx layout.Context, u *ui, snap launcher.Snapshot, kinds []instance.ContentKind) layout.Dimensions {
	gap := gtx.Dp(sp3)
	perRow := max(1, (gtx.Constraints.Max.X+gap)/(gtx.Dp(cardWidth)+gap))
	cellW := (gtx.Constraints.Max.X - gap*(perRow-1)) / perRow

	var rows []layout.FlexChild
	for start := 0; start < len(kinds); start += perRow {
		end := min(start+perRow, len(kinds))
		start, end := start, end
		rows = append(rows, rigid(func(gtx layout.Context) layout.Dimensions {
			var cells []layout.FlexChild
			for i := start; i < end; i++ {
				i := i
				cells = append(cells, rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = cellW
					gtx.Constraints.Min.X = cellW
					return o.layoutCard(gtx, u, snap, kinds[i], &o.cards[i])
				}))
			}
			return row(gtx, sp3, cells...)
		}))
	}
	return column(gtx, sp3, rows...)
}

// layoutCard is one kind: its name, how many, and one line about them.
func (o *overview) layoutCard(gtx layout.Context, u *ui, snap launcher.Snapshot, kind instance.ContentKind, click *widget.Clickable) layout.Dimensions {
	th := u.th
	entries := snap.ContentOf(kind)
	count := len(entries)
	detail := ""

	switch kind {
	case instance.ContentMods:
		off := 0
		for _, e := range entries {
			if e.Disabled {
				off++
			}
		}
		count -= off
		if off > 0 {
			detail = fmt.Sprintf("%d switched off", off)
		}
	case instance.ContentSaves, instance.ContentScreenshots, instance.ContentLogs, instance.ContentCrashReports:
		if newest, ok := newestOf(entries); ok {
			detail = "newest " + humaniseSince(newest.ModTime)
			if kind == instance.ContentSaves {
				detail = newest.Label() + " · " + humaniseSince(newest.ModTime)
			}
		}
	default:
		if size := totalSize(entries); size > 0 {
			detail = launch.FormatBytes(size)
		}
	}

	bg := th.P.Surface
	border := th.P.LineDim
	if click.Hovered() {
		bg, border = th.P.Raised, th.P.Line
	}
	return pressable(gtx, click, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, bg, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			return outlined(gtx, border, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: sp3, Bottom: sp3, Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return column(gtx, unit.Dp(4),
						rigid(func(gtx layout.Context) layout.Dimensions {
							return th.smallIn(gtx, kind.Label(), th.P.TextMid)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(th.Theme, unit.Sp(30), fmt.Sprint(count))
							l.Font.Typeface = faceDisplay
							l.Font.Weight = font.SemiBold
							l.Color = th.P.Text
							if count == 0 {
								l.Color = th.P.TextDim
							}
							return l.Layout(gtx)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if detail == "" {
								return layout.Dimensions{Size: image.Pt(0, gtx.Sp(sizeSmall))}
							}
							return th.small(gtx, detail)
						}),
					)
				})
			})
		})
	})
}

// layoutFacts lists what the instance is, as a small table.
func (o *overview) layoutFacts(gtx layout.Context, u *ui, snap launcher.Snapshot, inst instance.Instance) layout.Dimensions {
	th := u.th
	meta := snap.Editing
	minMB, maxMB := meta.Memory.Resolved()

	type fact struct{ k, v string }
	facts := []fact{
		{"Minecraft", orDash(meta.MinecraftVersion)},
		{"Loader", meta.Loader.String()},
		{"Memory", fmt.Sprintf("%d – %d MB", minMB, maxMB)},
		{"Java", orDash(meta.Java.Path)},
	}
	if !meta.LastPlayed.IsZero() {
		facts = append(facts, fact{"Last played", humaniseSince(meta.LastPlayed)})
	}
	if meta.TotalPlaySeconds > 0 {
		played := formatPlaytime(time.Duration(meta.TotalPlaySeconds) * time.Second)
		if meta.PlaySessions > 0 {
			played += fmt.Sprintf("  (%d sessions)", meta.PlaySessions)
		}
		facts = append(facts, fact{"Time played", played})
	}
	if !meta.Created.IsZero() {
		facts = append(facts, fact{"Created", meta.Created.Local().Format("2 Jan 2006")})
	}
	facts = append(facts, fact{"Folder", inst.Path})
	if meta.Notes != "" {
		facts = append(facts, fact{"Notes", meta.Notes})
	}

	children := make([]layout.FlexChild, 0, len(facts))
	for _, f := range facts {
		f := f
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return row(gtx, sp3,
				rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(110))
					return th.small(gtx, f.k)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return th.monoIn(gtx, f.v, th.P.TextMid) }),
			)
		}))
	}
	return th.card(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, sp2, children...)
	})
}

func newestOf(entries []instance.Entry) (instance.Entry, bool) {
	var best instance.Entry
	found := false
	for _, e := range entries {
		if !found || e.ModTime.After(best.ModTime) {
			best, found = e, true
		}
	}
	return best, found
}

func totalSize(entries []instance.Entry) int64 {
	var n int64
	for _, e := range entries {
		n += e.Size
	}
	return n
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// formatPlaytime renders a duration as hours and minutes.
func formatPlaytime(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case d < time.Minute:
		// A launch that was closed again straight away; minutes would
		// round it to nothing at all.
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}
