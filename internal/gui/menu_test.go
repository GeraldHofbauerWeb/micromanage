package gui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// frame lays the interface out once against a router, so queued pointer
// events reach the widgets exactly as they would from a window.
func frame(u *ui, r *input.Router, ops *op.Ops, snap launcher.Snapshot) {
	ops.Reset()
	gtx := layout.Context{
		Ops:         ops,
		Constraints: layout.Constraints{Max: image.Pt(1180, 760)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
		Source:      r.Source(),
	}
	u.layoutSnapshot(gtx, snap)
	r.Frame(ops)
}

func press(r *input.Router, at image.Point, button pointer.Buttons) {
	pos := f32.Pt(float32(at.X), float32(at.Y))
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: button, Position: pos},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos},
	)
}

// TestRightClickOpensTheInstanceMenu presses the secondary button on the
// instance list and expects the menu for that instance, at the pointer.
func TestRightClickOpensTheInstanceMenu(t *testing.T) {
	snap := launcher.Snapshot{
		Screen:     launcher.ScreenInstances,
		HasAccount: true,
		Instances: []instance.Instance{
			{Name: "alpha", Path: "/tmp/alpha", Configured: true, MinecraftVersion: "1.21.1"},
			{Name: "beta", Path: "/tmp/beta", Configured: true, MinecraftVersion: "1.21.1", IsActive: true},
		},
	}
	u := newUI(nil)
	var r input.Router
	var ops op.Ops
	frame(u, &r, &ops, snap)

	// Walk down the rail until a row answers; the first to do so is the
	// first instance.
	var hit image.Point
	for y := 60; y < 400 && !u.menu.open; y += 8 {
		hit = image.Pt(120, y)
		press(&r, hit, pointer.ButtonSecondary)
		frame(u, &r, &ops, snap)
	}
	if !u.menu.open {
		t.Fatal("a right-click on the instance list opened no menu")
	}
	if u.menu.title != "alpha" {
		t.Errorf("menu is for %q, want alpha", u.menu.title)
	}
	if u.menu.at != hit {
		t.Errorf("menu at %v, pointer was at %v", u.menu.at, hit)
	}
	if len(u.menu.items) < 2 || u.menu.items[0].label != "Play" || u.menu.items[0].do == nil {
		t.Errorf("first item = %+v, want an enabled Play", u.menu.items)
	}
	if item := u.menu.items[1]; item.label != "Set active" || item.do == nil {
		t.Errorf("second item = %+v, want an enabled Set active", item)
	}
	last := u.menu.items[len(u.menu.items)-1]
	if last.label != "Delete…" || last.do == nil {
		t.Errorf("last item = %+v, want an enabled Delete", last)
	}

	// A click on an item runs it and closes the menu. Walk down the panel
	// from its top-left corner until the Settings item answers.
	settingsTab := len(benchTabs()) - 1
	frame(u, &r, &ops, snap)
	ran := 0
	for y := hit.Y + 4; y < hit.Y+300 && u.bench.tab != settingsTab; y += 6 {
		press(&r, image.Pt(hit.X+40, y), pointer.ButtonPrimary)
		frame(u, &r, &ops, snap)
		if !u.menu.open && u.bench.tab != settingsTab {
			// An item above Settings ran and closed the menu; open it
			// again at the same spot and keep walking.
			ran++
			u.pointer = hit
			u.openInstanceMenu(snap, snap.Instances[0])
			frame(u, &r, &ops, snap)
		}
	}
	if u.bench.tab != settingsTab {
		t.Fatalf("no click on the panel reached the Settings item (%d items ran)", ran)
	}
	if ran < 2 {
		t.Errorf("only %d items ran before Settings; Play and Set active come first", ran)
	}
	if u.menu.open {
		t.Error("the menu stayed open after an item ran")
	}
	if u.bench.shownFor != "alpha" || u.bench.tab != settingsTab {
		t.Errorf("after the Settings item: shownFor=%q tab=%d", u.bench.shownFor, u.bench.tab)
	}

	// The menu draws over everything; a click far from it closes it.
	u.pointer = hit
	u.openInstanceMenu(snap, snap.Instances[0])
	frame(u, &r, &ops, snap)
	press(&r, image.Pt(900, 700), pointer.ButtonPrimary)
	frame(u, &r, &ops, snap)
	if u.menu.open {
		t.Error("a click outside the menu left it open")
	}

	// The active instance cannot be deleted; its item says so.
	u.pointer = image.Pt(10, 10)
	u.openInstanceMenu(snap, snap.Instances[1])
	last = u.menu.items[len(u.menu.items)-1]
	if last.do != nil || last.note != "active" {
		t.Errorf("delete item for the active instance = %+v", last)
	}
	if item := u.menu.items[1]; item.do != nil || item.note == "" {
		t.Errorf("set active for the active instance = %+v, want inert with a note", item)
	}
}
