package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GeraldHofbauerWeb/micromanage/internal/instance"
)

// TestOptionsSnapshotsFollowTheSelection walks the settings card: save the
// game options, change them, restore, and see the list keep up.
func TestOptionsSnapshotsFollowTheSelection(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)

	dir := filepath.Join(m.InstancesPath, "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, instance.OptionsFile), []byte("fov:0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "pack"})
	snap := waitFor(t, ctrl, "options", func(s Snapshot) bool { return s.OptionsLoaded() })
	if !snap.Options.Exists || len(snap.OptionsSnapshots) != 0 {
		t.Fatalf("options = %+v, snapshots = %+v", snap.Options, snap.OptionsSnapshots)
	}

	ctrl.Dispatch(ActionSaveOptions{Name: "pack", Label: "good"})
	waitFor(t, ctrl, "one snapshot", func(s Snapshot) bool { return len(s.OptionsSnapshots) == 1 })

	if err := os.WriteFile(filepath.Join(dir, instance.OptionsFile), []byte("fov:1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl.Dispatch(ActionRestoreOptions{Name: "pack", Snapshot: instance.LatestSnapshot})
	snap = waitFor(t, ctrl, "restore kept a copy", func(s Snapshot) bool { return len(s.OptionsSnapshots) == 2 })
	if snap.OptionsSnapshots[0].Label != "before restore" {
		t.Errorf("newest snapshot = %+v", snap.OptionsSnapshots[0])
	}
	if got, _ := os.ReadFile(filepath.Join(dir, instance.OptionsFile)); string(got) != "fov:0.0\n" {
		t.Errorf("options.txt = %q after restore", got)
	}

	ctrl.Dispatch(ActionDeleteOptionsSnapshot{Name: "pack", Snapshot: snap.OptionsSnapshots[0].Name})
	waitFor(t, ctrl, "deleted", func(s Snapshot) bool { return len(s.OptionsSnapshots) == 1 })

	// Another selection must not show pack's snapshots.
	if err := os.MkdirAll(filepath.Join(m.InstancesPath, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "other"})
	snap = waitFor(t, ctrl, "other", func(s Snapshot) bool { return s.Selected == "other" && s.OptionsLoaded() })
	if snap.Options.Exists || len(snap.OptionsSnapshots) != 0 {
		t.Errorf("other shows pack's options: %+v %+v", snap.Options, snap.OptionsSnapshots)
	}
}

// TestFirstRefreshAdoptsMinecraft is the first run: no instances, a real
// .minecraft, and after the refresh it is the instance Default, selected.
func TestFirstRefreshAdoptsMinecraft(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	m.BackupPath = filepath.Join(m.AppDir, "backup")

	if err := os.MkdirAll(filepath.Join(m.MinecraftPath, "saves", "Home"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.MinecraftPath, instance.OptionsFile), []byte("fov:0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctrl.Dispatch(ActionRefresh{})
	snap := waitFor(t, ctrl, "adoption", func(s Snapshot) bool {
		return s.Selected == instance.DefaultInstanceName && len(s.Instances) == 1 && s.Task.Done
	})
	if snap.Task.Kind != TaskAdopt || snap.Task.Err != nil {
		t.Errorf("task = %+v", snap.Task)
	}
	if !snap.Instances[0].IsActive || snap.Instances[0].SaveCount != 1 {
		t.Errorf("instance = %+v", snap.Instances[0])
	}
	if target, err := os.Readlink(m.MinecraftPath); err != nil || filepath.Base(target) != instance.DefaultInstanceName {
		t.Errorf(".minecraft -> %q (%v)", target, err)
	}

	// A second refresh finds an instance and leaves things alone.
	ctrl.Dispatch(ActionRefresh{})
	snap = waitFor(t, ctrl, "second refresh", func(s Snapshot) bool { return len(s.Instances) == 1 })
	if snap.Err != nil {
		t.Errorf("second refresh failed: %v", snap.Err)
	}
}

// TestSetActiveMovesTheLink points .minecraft at another instance.
func TestSetActiveMovesTheLink(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	m.BackupPath = filepath.Join(m.AppDir, "backup")
	for _, name := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(m.InstancesPath, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(m.InstancesPath, "a"), m.MinecraftPath); err != nil {
		t.Fatal(err)
	}

	ctrl.Dispatch(ActionSetActive{Name: "b"})
	snap := waitFor(t, ctrl, "b active", func(s Snapshot) bool {
		for _, inst := range s.Instances {
			if inst.Name == "b" && inst.IsActive {
				return true
			}
		}
		return false
	})
	if snap.Err != nil {
		t.Fatal(snap.Err)
	}
	if target, _ := os.Readlink(m.MinecraftPath); filepath.Base(target) != "b" {
		t.Errorf(".minecraft -> %q", target)
	}
}
