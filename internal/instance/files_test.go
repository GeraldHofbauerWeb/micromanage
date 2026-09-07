package instance

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestManager builds a Manager rooted in a temp dir, without touching the
// user's real configuration.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	return &Manager{
		HomeDir:       root,
		AppDir:        filepath.Join(root, "app"),
		ConfigFile:    filepath.Join(root, "app", "config.json"),
		InstancesPath: filepath.Join(root, "instances"),
		MinecraftPath: filepath.Join(root, ".minecraft"),
		BackupPath:    filepath.Join(root, "backup"),
	}
}

// mkInstance creates an instance directory with the standard content dirs.
func mkInstance(t *testing.T, m *Manager, name string) string {
	t.Helper()
	path := filepath.Join(m.InstancesPath, name)
	for _, d := range essentialDirs {
		if err := os.MkdirAll(filepath.Join(path, d), 0o755); err != nil {
			t.Fatalf("mkInstance: %v", err)
		}
	}
	return path
}

func TestDeleteInstanceFileRejectsTraversal(t *testing.T) {
	m := newTestManager(t)
	mkInstance(t, m, "pack")

	// A file outside the instance that must survive every attempt below.
	outside := filepath.Join(m.InstancesPath, "secret.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	bad := []string{
		"../../etc/passwd",
		"../secret.txt",
		"..",
		".",
		"",
		"sub/dir/x",
		"/etc/passwd",
		"mods/../../secret.txt",
	}
	for _, name := range bad {
		t.Run("name="+name, func(t *testing.T) {
			if err := m.DeleteInstanceFile("pack", KindMod, name); err == nil {
				t.Fatalf("DeleteInstanceFile(%q) = nil, want an error", name)
			}
		})
	}

	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the instance was affected: %v", err)
	}
}

func TestDeleteInstanceFileRejectsBadInstanceName(t *testing.T) {
	m := newTestManager(t)
	mkInstance(t, m, "pack")

	for _, name := range []string{"../pack", "..", "", "a/b"} {
		if err := m.DeleteInstanceFile(name, KindMod, "x.jar"); err == nil {
			t.Errorf("DeleteInstanceFile(instance=%q) = nil, want an error", name)
		}
	}
}

func TestDeleteInstanceFileRemovesEachKind(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")

	cases := []struct {
		kind  FileKind
		dir   string
		name  string
		isDir bool
	}{
		{KindMod, "mods", "cool-mod.jar", false},
		{KindConfig, "config", "settings.toml", false},
		{KindSave, "saves", "My World", true},
	}

	for _, tc := range cases {
		target := filepath.Join(path, tc.dir, tc.name)
		if tc.isDir {
			if err := os.MkdirAll(filepath.Join(target, "region"), 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := m.DeleteInstanceFile("pack", tc.kind, tc.name); err != nil {
			t.Fatalf("DeleteInstanceFile(%s): %v", tc.kind, err)
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Errorf("%s still present after delete", target)
		}
	}
}

func TestDeleteInstanceFileSymlinkEscape(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")

	outsideDir := filepath.Join(m.InstancesPath, "vault")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outsideDir, "important.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A symlink inside mods/ pointing at a directory outside the instance.
	link := filepath.Join(path, "mods", "escape")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Deleting the link itself is legitimate and must not follow it.
	if err := m.DeleteInstanceFile("pack", KindMod, "escape"); err != nil {
		t.Fatalf("deleting the symlink failed: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("delete followed the symlink and destroyed data outside the instance: %v", err)
	}
}

func TestDeleteInstanceFileUnknownKind(t *testing.T) {
	m := newTestManager(t)
	mkInstance(t, m, "pack")

	if err := m.DeleteInstanceFile("pack", FileKind("screenshot"), "x.png"); err == nil {
		t.Fatal("unknown kind accepted, want an error")
	}
}

func TestCanDeleteRejectsActiveInstance(t *testing.T) {
	m := newTestManager(t)
	target := mkInstance(t, m, "pack")
	mkInstance(t, m, "other")

	if err := os.Symlink(target, m.MinecraftPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := m.CanDelete("pack"); err == nil {
		t.Fatal("CanDelete(active) = nil, want an error")
	}
	if err := m.CanDelete("other"); err != nil {
		t.Fatalf("CanDelete(inactive) = %v, want nil", err)
	}
	if err := m.CanDelete("missing"); err == nil {
		t.Fatal("CanDelete(missing) = nil, want an error")
	}
}

func TestConfigKeysAreOrderedAndEditableFlagged(t *testing.T) {
	keys := ConfigKeys()
	want := []string{"minecraft-path", "instances-path", "backup-path", "msa-client-id", "app-dir", "config-file"}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k.Key != want[i] {
			t.Errorf("key %d = %q, want %q", i, k.Key, want[i])
		}
	}

	// The two derived keys are rejected by UpdateConfig, so they must not be
	// offered as editable anywhere.
	for _, k := range []string{"app-dir", "config-file"} {
		if IsEditableConfigKey(k) {
			t.Errorf("%q reported as editable, but UpdateConfig rejects it", k)
		}
	}
	for _, k := range []string{"minecraft-path", "instances-path", "backup-path", "msa-client-id"} {
		if !IsEditableConfigKey(k) {
			t.Errorf("%q reported as read-only, but UpdateConfig accepts it", k)
		}
	}
	if IsEditableConfigKey("nonsense") {
		t.Error("unknown key reported as editable")
	}
}
