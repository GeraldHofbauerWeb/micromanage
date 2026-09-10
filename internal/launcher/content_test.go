package launcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
)

// TestSelectListsContentAndTogglingRefreshesIt walks the path the workbench
// takes: pick an instance, see its mods, switch one off, see the change.
func TestSelectListsContentAndTogglingRefreshesIt(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)

	dir := filepath.Join(m.InstancesPath, "pack")
	for _, sub := range []string{"mods", "config/jei"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"mods/create.jar", "mods/jei.jar", "config/jei/jei-client.toml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "pack"})
	waitFor(t, ctrl, "state", func(s Snapshot) bool {
		return s.Selected == "pack" && len(s.ContentOf(instance.ContentMods)) == 2
	})
	snap := ctrl.Store().Snapshot()
	if got := snap.ContentOf(instance.ContentConfig); len(got) != 1 || got[0].Name != "jei/jei-client.toml" {
		t.Errorf("configs = %+v", got)
	}

	ctrl.Dispatch(ActionSetEnabled{Name: "pack", Kind: instance.ContentMods, File: "jei.jar", Enabled: false})
	waitFor(t, ctrl, "state", func(s Snapshot) bool {
		mods := s.ContentOf(instance.ContentMods)
		return len(mods) == 2 && mods[1].Disabled
	})
	waitFor(t, ctrl, "state", func(s Snapshot) bool {
		inst, ok := s.SelectedInstance()
		return ok && inst.ModCount == 1 && inst.DisabledMods == 1
	})

	ctrl.Dispatch(ActionDeleteContent{Name: "pack", Kind: instance.ContentConfig, File: "jei/jei-client.toml"})
	waitFor(t, ctrl, "state", func(s Snapshot) bool {
		return len(s.ContentOf(instance.ContentConfig)) == 0
	})
	if _, err := os.Stat(filepath.Join(dir, "config/jei/jei-client.toml")); !os.IsNotExist(err) {
		t.Error("config file still on disk")
	}
}

// TestRenameFollowsTheSelection: the renamed instance stays selected under
// its new name, with its content listed against that name.
func TestRenameFollowsTheSelection(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	if err := os.MkdirAll(filepath.Join(m.InstancesPath, "old", "mods"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "old"})
	waitFor(t, ctrl, "state", func(s Snapshot) bool { return s.ContentFor == "old" })

	ctrl.Dispatch(ActionRename{Name: "old", NewName: "new"})
	waitFor(t, ctrl, "state", func(s Snapshot) bool {
		_, ok := s.SelectedInstance()
		return ok && s.Selected == "new" && s.ContentFor == "new"
	})
}

// isolateInstances gives the manager its own instances directory and
// symlink target, so the store's own directories are not listed as instances.
func isolateInstances(t *testing.T, ctrl *Controller) *instance.Manager {
	t.Helper()
	m := ctrl.Manager
	m.InstancesPath = filepath.Join(m.AppDir, "instances")
	m.MinecraftPath = filepath.Join(m.AppDir, "minecraft")
	if err := os.MkdirAll(m.InstancesPath, 0o755); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestContentOfHidesAStaleListing guards against showing the previous
// instance's mods under a newly selected name.
func TestContentOfHidesAStaleListing(t *testing.T) {
	s := NewStore()
	s.SetSelected("a")
	s.SetContent("a", map[instance.ContentKind][]instance.Entry{
		instance.ContentMods: {{Name: "x.jar"}},
	})
	if got := s.Snapshot().ContentOf(instance.ContentMods); len(got) != 1 {
		t.Fatalf("content for a = %v", got)
	}
	s.SetSelected("b")
	if got := s.Snapshot().ContentOf(instance.ContentMods); got != nil {
		t.Errorf("stale content shown for b: %v", got)
	}
	_ = time.Second
}
