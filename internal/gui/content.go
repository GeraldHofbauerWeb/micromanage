package gui

import (
	"fmt"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// kindState is the widgets of one content tab: its filter, its list, and
// one set of buttons per visible row.
type kindState struct {
	list   *widget.List
	filter *widget.Editor
	folder widget.Clickable
	rows   []contentRow

	// confirming names the entry whose Delete was pressed once; the row
	// then asks before anything is removed.
	confirming string
}

type contentRow struct {
	open, reveal, toggle, del, confirm, keep widget.Clickable
}

// Layout draws a content tab: a filter above a list of entries with their
// actions.
func (k *kindState) Layout(gtx layout.Context, u *ui, snap launcher.Snapshot, inst instance.Instance, kind instance.ContentKind) layout.Dimensions {
	th := u.th

	if k.folder.Clicked(gtx) {
		dir, err := u.ctrl.Manager.ContentDir(inst.Name, kind)
		if err == nil {
			u.ctrl.Dispatch(launcher.ActionOpen{Path: dir})
		}
	}

	all := snap.ContentOf(kind)
	entries := filterEntries(all, k.filter.Text())
	for len(k.rows) < len(entries) {
		k.rows = append(k.rows, contentRow{})
	}

	for i := range entries {
		e := entries[i]
		r := &k.rows[i]
		switch {
		case r.open.Clicked(gtx):
			u.ctrl.Dispatch(launcher.ActionOpen{Path: e.Path})
		case r.reveal.Clicked(gtx):
			u.ctrl.Dispatch(launcher.ActionReveal{Path: e.Path})
		case r.toggle.Clicked(gtx):
			u.ctrl.Dispatch(launcher.ActionSetEnabled{Name: inst.Name, Kind: kind, File: e.Name, Enabled: e.Disabled})
		case r.del.Clicked(gtx):
			k.confirming = e.Name
		case r.keep.Clicked(gtx):
			k.confirming = ""
		case r.confirm.Clicked(gtx):
			k.confirming = ""
			u.ctrl.Dispatch(launcher.ActionDeleteContent{Name: inst.Name, Kind: kind, File: e.Name})
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: sp3, Bottom: sp2, Left: sp4, Right: sp4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, sp2,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(360)))
						return th.input(gtx, k.filter, "Filter "+strings.ToLower(kind.Label()), u.ic.Search)
					}),
					flexFill(),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if len(all) == 0 || len(entries) == len(all) {
							return layout.Dimensions{}
						}
						return th.monoIn(gtx, fmt.Sprintf("%d of %d", len(entries), len(all)), th.P.TextDim)
					}),
					rigid(func(gtx layout.Context) layout.Dimensions {
						return th.ghost(gtx, &k.folder, u.ic.Folder, "Open folder")
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(all) == 0 {
				return k.layoutEmpty(gtx, u, kind)
			}
			if len(entries) == 0 {
				return layout.Inset{Top: sp3, Left: sp4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return th.small(gtx, "Nothing matches the filter.")
				})
			}
			return layout.Inset{Left: sp3, Right: sp3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.list(gtx, k.list, len(entries), func(gtx layout.Context, i int) layout.Dimensions {
					return k.layoutRow(gtx, u, kind, entries[i], &k.rows[i])
				})
			})
		}),
	)
}

// layoutEmpty says what would fill the tab and where to put it.
func (k *kindState) layoutEmpty(gtx layout.Context, u *ui, kind instance.ContentKind) layout.Dimensions {
	th := u.th
	var msg string
	switch kind {
	case instance.ContentMods:
		msg = "No mods. Drop .jar files into the mods folder and they show up here."
	case instance.ContentConfig:
		msg = "No configuration yet. Mods write theirs on the first run."
	case instance.ContentSaves:
		msg = "No worlds yet. The first one appears after you play."
	case instance.ContentResourcePacks:
		msg = "No resource packs. Drop .zip files into the resourcepacks folder."
	case instance.ContentShaderPacks:
		msg = "No shader packs. Drop .zip files into the shaderpacks folder."
	case instance.ContentScreenshots:
		msg = "No screenshots. F2 in the game takes one."
	case instance.ContentLogs:
		msg = "No logs yet. The game writes one per session."
	case instance.ContentCrashReports:
		msg = "No crash reports. That is the good kind of empty."
	default:
		msg = "Nothing here."
	}
	return layout.Inset{Top: sp3, Left: sp4, Right: sp4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return th.wrapped(gtx, msg, th.P.TextDim)
	})
}

// layoutRow draws one entry: state, name, size and date, then its actions.
func (k *kindState) layoutRow(gtx layout.Context, u *ui, kind instance.ContentKind, e instance.Entry, r *contentRow) layout.Dimensions {
	th := u.th
	confirming := k.confirming == e.Name

	return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		bg := th.P.Bg
		bg.A = 0
		if confirming {
			bg = alpha(th.P.Bad, 0x14)
		}
		return fill(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(10), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, sp2,
					rigid(func(gtx layout.Context) layout.Dimensions {
						if !kind.Toggleable() {
							return layout.Dimensions{}
						}
						c := th.P.Good
						if e.Disabled {
							c = th.P.Line
						}
						return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return dot(gtx, c, unit.Dp(7))
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						name := e.DisplayName()
						if e.Disabled {
							return th.text(gtx, name, sizeBody, 0, th.P.TextDim)
						}
						return th.body(gtx, name)
					}),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if confirming {
							return row(gtx, sp1,
								rigid(func(gtx layout.Context) layout.Dimensions {
									what := "Delete"
									if e.IsDir {
										what = "Delete the whole folder"
									}
									return th.smallIn(gtx, what+"?", th.P.Bad)
								}),
								rigid(func(gtx layout.Context) layout.Dimensions { return th.danger(gtx, &r.confirm, nil, "Yes, delete") }),
								rigid(func(gtx layout.Context) layout.Dimensions { return th.ghost(gtx, &r.keep, nil, "Keep") }),
							)
						}
						return row(gtx, sp1,
							rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Right: sp2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return th.monoIn(gtx, entryMeta(e), th.P.TextDim)
								})
							}),
							rigid(func(gtx layout.Context) layout.Dimensions {
								switch {
								case e.IsDir:
									return th.ghost(gtx, &r.open, u.ic.Folder, "Open")
								case kind == instance.ContentConfig || kind == instance.ContentLogs || kind == instance.ContentCrashReports:
									return th.ghost(gtx, &r.open, u.ic.Edit, "Edit")
								default:
									return th.ghost(gtx, &r.open, u.ic.OpenInNew, "Open")
								}
							}),
							rigid(func(gtx layout.Context) layout.Dimensions {
								if e.IsDir {
									return layout.Dimensions{}
								}
								return th.ghost(gtx, &r.reveal, u.ic.Launch, "Show")
							}),
							rigid(func(gtx layout.Context) layout.Dimensions {
								if !kind.Toggleable() {
									return layout.Dimensions{}
								}
								if e.Disabled {
									return th.ghost(gtx, &r.toggle, u.ic.Eye, "Turn on")
								}
								return th.ghost(gtx, &r.toggle, u.ic.EyeOff, "Turn off")
							}),
							rigid(func(gtx layout.Context) layout.Dimensions { return th.danger(gtx, &r.del, u.ic.Delete, "") }),
						)
					}),
				)
			})
		})
	})
}

// entryMeta is the size and age of an entry, as data.
func entryMeta(e instance.Entry) string {
	parts := []string{}
	if !e.IsDir && e.Size > 0 {
		parts = append(parts, launch.FormatBytes(e.Size))
	}
	if !e.ModTime.IsZero() {
		parts = append(parts, humaniseSince(e.ModTime))
	}
	return strings.Join(parts, " · ")
}

// filterEntries keeps the entries whose name contains the filter, ignoring
// case. Every word of the filter has to match somewhere.
func filterEntries(entries []instance.Entry, filter string) []instance.Entry {
	words := strings.Fields(strings.ToLower(filter))
	if len(words) == 0 {
		return entries
	}
	out := make([]instance.Entry, 0, len(entries))
	for _, e := range entries {
		name := strings.ToLower(e.Name)
		ok := true
		for _, w := range words {
			if !strings.Contains(name, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, e)
		}
	}
	return out
}
