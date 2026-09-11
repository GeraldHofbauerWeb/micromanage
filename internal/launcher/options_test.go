package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
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

// TestFirstRefreshOnlyOffersTheImport is the first run: no instances and a
// real .minecraft. The refresh offers to import it and does nothing more.
func TestFirstRefreshOnlyOffersTheImport(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	writeTestFile(t, filepath.Join(m.MinecraftPath, instance.OptionsFile), "fov:0.0\n")

	ctrl.Dispatch(ActionRefresh{})
	snap := waitFor(t, ctrl, "the import offered", func(s Snapshot) bool { return s.CanImport })
	if len(snap.Instances) != 0 || snap.Task.ID != 0 {
		t.Errorf("a refresh did more than offer: instances %+v, task %+v", snap.Instances, snap.Task)
	}
	if info, err := os.Lstat(m.MinecraftPath); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf(".minecraft is not the plain directory it was: %v", err)
	}
}

// TestImportCopiesMinecraft is the wizard's import: a copy of .minecraft
// becomes the instance Default, selected and remembered, and .minecraft
// keeps every file it had.
func TestImportCopiesMinecraft(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	for rel, content := range map[string]string{
		"saves/Home/level.dat":          "level",
		"screenshots/shot.png":          "png",
		instance.OptionsFile:            "fov:0.0\n",
		"versions/1.21.1/1.21.1.json":   `{"id":"1.21.1"}`,
		"libraries/org/lwjgl/lwjgl.jar": "lib",
	} {
		writeTestFile(t, filepath.Join(m.MinecraftPath, filepath.FromSlash(rel)), content)
	}

	ctrl.Dispatch(ActionImport{IncludeSaves: true})
	snap := waitFor(t, ctrl, "the import", func(s Snapshot) bool {
		return s.Task.Kind == TaskImport && s.Task.Done && s.Selected == instance.DefaultInstanceName
	})
	if snap.Task.Err != nil {
		t.Fatal(snap.Task.Err)
	}
	if len(snap.Instances) != 1 || snap.Instances[0].SaveCount != 1 {
		t.Errorf("instances = %+v", snap.Instances)
	}
	if snap.LastInstance != instance.DefaultInstanceName {
		t.Errorf("last instance = %q", snap.LastInstance)
	}

	path := filepath.Join(m.InstancesPath, instance.DefaultInstanceName)
	if _, err := os.Stat(filepath.Join(path, "screenshots")); !os.IsNotExist(err) {
		t.Errorf("screenshots were not asked for: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctrl.Layout.Libraries(), "org", "lwjgl", "lwjgl.jar")); err != nil {
		t.Errorf("the game files did not reach the store: %v", err)
	}
	for _, rel := range []string{"saves/Home/level.dat", "screenshots/shot.png", instance.OptionsFile, "versions/1.21.1/1.21.1.json", "libraries/org/lwjgl/lwjgl.jar"} {
		if _, err := os.Stat(filepath.Join(m.MinecraftPath, filepath.FromSlash(rel))); err != nil {
			t.Errorf(".minecraft lost %s: %v", rel, err)
		}
	}
}

// TestTheLastInstanceOutlivesTheSession selects an instance, starts over
// with a new controller on the same configuration, and expects the start
// screen to offer that instance again; deleting it forgets it.
func TestTheLastInstanceOutlivesTheSession(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	m.ConfigFile = filepath.Join(m.AppDir, "config.json")
	for _, name := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(m.InstancesPath, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "b"})
	waitFor(t, ctrl, "b remembered", func(s Snapshot) bool { return s.LastInstance == "b" })

	again := newTestController(t, nil)
	again.Manager, again.Layout = &instance.Manager{
		AppDir:        m.AppDir,
		ConfigFile:    m.ConfigFile,
		InstancesPath: m.InstancesPath,
		MinecraftPath: m.MinecraftPath,
	}, ctrl.Layout
	if err := again.Manager.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	again.Dispatch(ActionRefresh{})
	waitFor(t, again, "b offered after a restart", func(s Snapshot) bool {
		return s.LastInstance == "b" && s.Selected == ""
	})

	again.Dispatch(ActionDelete{Name: "b"})
	waitFor(t, again, "b forgotten", func(s Snapshot) bool {
		return len(s.Instances) == 1 && s.LastInstance == ""
	})
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
