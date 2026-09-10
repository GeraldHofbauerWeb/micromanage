//go:build gui_screenshot

package gui

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launcher"
)

// Screenshot renders one frame of the interface offscreen and writes it as a
// PNG.
//
// It exists because the launcher targets Wayland, where no screenshot tool is
// guaranteed to be present, and because rendering a frame is a real check that
// the layout code runs: a nil dereference or a bad constraint fails here
// rather than in front of a user. It is behind a build tag so the normal
// binary carries no offscreen GPU code.
//
// One artefact to expect: with no input source attached, gtx.Enabled() is
// false, so Gio desaturates button backgrounds exactly as it would for a
// disabled control. Buttons look washed out here and are full strength in a
// real window; it is not a palette bug.
func Screenshot(path string, width, height int, snap launcher.Snapshot, setup func(*ui)) error {
	win, err := headless.NewWindow(width, height)
	if err != nil {
		return fmt.Errorf("creating an offscreen surface: %w", err)
	}
	defer win.Release()

	ui := newUI(nil)
	if setup != nil {
		setup(ui)
	}

	var ops op.Ops
	gtx := layout.Context{
		Ops: &ops,
		Constraints: layout.Constraints{
			Max: image.Pt(width, height),
		},
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	ui.layoutSnapshot(gtx, snap)

	if err := win.Frame(&ops); err != nil {
		return fmt.Errorf("rendering: %w", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := win.Screenshot(img); err != nil {
		return fmt.Errorf("reading back the frame: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
