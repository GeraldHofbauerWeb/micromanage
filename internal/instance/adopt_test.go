package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdoptMinecraftMovesItAndLinksBack(t *testing.T) {
	root := t.TempDir()
	m := &Manager{
		AppDir:        root,
		InstancesPath: filepath.Join(root, "instances"),
		MinecraftPath: filepath.Join(root, ".minecraft"),
		BackupPath:    filepath.Join(root, "backup"),
	}

	if m.CanAdopt() {
		t.Fatal("nothing to adopt yet, but CanAdopt said yes")
	}
	if err := os.MkdirAll(m.MinecraftPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if m.CanAdopt() {
		t.Fatal("an empty .minecraft is not worth adopting")
	}

	for _, sub := range []string{"saves/World", "mods", "versions/1.21.1"} {
		if err := os.MkdirAll(filepath.Join(m.MinecraftPath, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.MinecraftPath, OptionsFile), []byte("fov:0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profiles := `{"profiles":{"a":{"name":"Latest release","type":"custom","lastVersionId":"1.21.1","javaArgs":"-Xmx6G","lastUsed":"2026-09-01T10:00:00.000Z"}}}`
	if err := os.WriteFile(filepath.Join(m.MinecraftPath, "launcher_profiles.json"), []byte(profiles), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.CanAdopt() {
		t.Fatal("a populated .minecraft with no instances should be adoptable")
	}

	res, err := m.AdoptMinecraft("")
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != DefaultInstanceName || res.Copied {
		t.Errorf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "saves", "World")); err != nil {
		t.Errorf("the world did not come along: %v", err)
	}
	if target, err := os.Readlink(m.MinecraftPath); err != nil || target != res.Path {
		t.Errorf(".minecraft -> %q (%v), want %q", target, err, res.Path)
	}
	if !res.Detected || res.Meta.MinecraftVersion != "1.21.1" || res.Meta.Memory.MaxMB != 6144 {
		t.Errorf("detected meta = %+v", res.Meta)
	}
	if meta, found, err := LoadMeta(res.Path); err != nil || !found || meta.Name != DefaultInstanceName {
		t.Errorf("saved meta = %+v found=%v err=%v", meta, found, err)
	}

	instances, err := m.ListInstances()
	if err != nil || len(instances) != 1 || !instances[0].IsActive || instances[0].SaveCount != 1 {
		t.Errorf("instances = %+v (%v)", instances, err)
	}
	if m.CanAdopt() {
		t.Error("adopting twice must not be offered")
	}
	if _, err := m.AdoptMinecraft(""); err == nil {
		t.Error("adopting a symlink should fail")
	}
}

func TestAdoptRefusesInstancesInsideMinecraft(t *testing.T) {
	root := t.TempDir()
	m := &Manager{
		AppDir:        root,
		MinecraftPath: filepath.Join(root, ".minecraft"),
		InstancesPath: filepath.Join(root, ".minecraft", "instances"),
		BackupPath:    filepath.Join(root, "backup"),
	}
	if err := os.MkdirAll(filepath.Join(m.MinecraftPath, "saves"), 0o755); err != nil {
		t.Fatal(err)
	}
	if m.CanAdopt() {
		t.Error("instances inside .minecraft must not be adoptable")
	}
	if _, err := m.AdoptMinecraft("x"); err == nil {
		t.Error("adopting should have failed")
	}
}
