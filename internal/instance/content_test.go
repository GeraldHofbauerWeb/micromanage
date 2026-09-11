package instance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListContentMods(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	writeFile(t, filepath.Join(path, "mods", "Zeta.jar"), "z")
	writeFile(t, filepath.Join(path, "mods", "alpha.jar"), "a")
	writeFile(t, filepath.Join(path, "mods", "beta.jar.disabled"), "b")
	writeFile(t, filepath.Join(path, "mods", ".hidden"), "")
	if err := os.MkdirAll(filepath.Join(path, "mods", "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := m.ListContent("pack", ContentMods)
	if err != nil {
		t.Fatal(err)
	}
	// Case-insensitive, disabled marker ignored for ordering, directories
	// and dotfiles left out.
	want := []string{"alpha.jar", "beta.jar.disabled", "Zeta.jar"}
	if !equal(names(got), want) {
		t.Errorf("mods = %v, want %v", names(got), want)
	}
	if !got[1].Disabled || got[0].Disabled {
		t.Errorf("disabled flags wrong: %+v", got)
	}
	if got[1].DisplayName() != "beta.jar" {
		t.Errorf("DisplayName = %q", got[1].DisplayName())
	}
}

func TestListContentConfigIsRecursive(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	writeFile(t, filepath.Join(path, "config", "jei", "jei-client.toml"), "")
	writeFile(t, filepath.Join(path, "config", "create-common.toml"), "")

	got, err := m.ListContent("pack", ContentConfig)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create-common.toml", "jei/jei-client.toml"}
	if !equal(names(got), want) {
		t.Errorf("configs = %v, want %v", names(got), want)
	}
}

func TestListContentWorldsAreDirectories(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	old := time.Now().Add(-48 * time.Hour)
	writeFile(t, filepath.Join(path, "saves", "Survival", "level.dat"), "")
	writeFile(t, filepath.Join(path, "saves", "stray.txt"), "")
	if err := os.Chtimes(filepath.Join(path, "saves", "Survival", "level.dat"), old, old); err != nil {
		t.Fatal(err)
	}

	got, err := m.ListContent("pack", ContentSaves)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(names(got), []string{"Survival"}) {
		t.Fatalf("worlds = %v", names(got))
	}
	if !got[0].IsDir {
		t.Error("a world should list as a directory")
	}
	if got[0].ModTime.Sub(old).Abs() > time.Second {
		t.Errorf("world time = %v, want level.dat's %v", got[0].ModTime, old)
	}
}

func TestListContentMissingDirIsEmpty(t *testing.T) {
	m := newTestManager(t)
	mkInstance(t, m, "pack")
	got, err := m.ListContent("pack", ContentScreenshots)
	if err != nil || len(got) != 0 {
		t.Errorf("missing screenshots dir: got %v, %v; want empty and no error", got, err)
	}
	if _, err := m.ListContent("pack", ContentKind("nonsense")); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestListContentNewestFirst(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	older := time.Now().Add(-time.Hour)
	writeFile(t, filepath.Join(path, "logs", "latest.log"), "")
	writeFile(t, filepath.Join(path, "logs", "2026-09-08-1.log.gz"), "")
	if err := os.Chtimes(filepath.Join(path, "logs", "2026-09-08-1.log.gz"), older, older); err != nil {
		t.Fatal(err)
	}
	got, err := m.ListContent("pack", ContentLogs)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(names(got), []string{"latest.log", "2026-09-08-1.log.gz"}) {
		t.Errorf("logs = %v, want newest first", names(got))
	}
}

func TestSetContentEnabledRenames(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	writeFile(t, filepath.Join(path, "mods", "jei.jar"), "x")

	newName, err := m.SetContentEnabled("pack", ContentMods, "jei.jar", false)
	if err != nil {
		t.Fatal(err)
	}
	if newName != "jei.jar.disabled" {
		t.Errorf("disabled name = %q", newName)
	}
	if _, err := os.Stat(filepath.Join(path, "mods", "jei.jar.disabled")); err != nil {
		t.Fatal("the mod was not renamed")
	}

	// Disabling again is a no-op, not an error.
	if again, err := m.SetContentEnabled("pack", ContentMods, "jei.jar.disabled", false); err != nil || again != "jei.jar.disabled" {
		t.Errorf("second disable: %q, %v", again, err)
	}

	back, err := m.SetContentEnabled("pack", ContentMods, "jei.jar.disabled", true)
	if err != nil || back != "jei.jar" {
		t.Fatalf("enable: %q, %v", back, err)
	}
	if _, err := os.Stat(filepath.Join(path, "mods", "jei.jar")); err != nil {
		t.Fatal("the mod was not renamed back")
	}

	// Only mods toggle.
	if _, err := m.SetContentEnabled("pack", ContentSaves, "world", false); err == nil {
		t.Error("a world was switched off")
	}
}

func TestDeleteContentNested(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	target := filepath.Join(path, "config", "jei", "jei-client.toml")
	writeFile(t, target, "")

	if err := m.DeleteContent("pack", ContentConfig, "jei/jei-client.toml"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("nested config still present")
	}
	// A nested name is only valid where the listing is recursive.
	if err := m.DeleteContent("pack", ContentMods, "a/b.jar"); err == nil {
		t.Error("nested mod name accepted")
	}
	if err := m.DeleteContent("pack", ContentConfig, "../instance.json"); err == nil {
		t.Error("traversal accepted")
	}
}

func TestRenameInstanceMovesItAndItsName(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "old")
	if err := SaveMeta(path, DefaultMeta("old")); err != nil {
		t.Fatal(err)
	}

	if err := m.RenameInstance("old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(m.MinecraftPath); !os.IsNotExist(err) {
		t.Errorf("renaming an instance touched %s: %v", m.MinecraftPath, err)
	}
	if _, err := os.Stat(filepath.Join(m.InstancesPath, "new", "mods")); err != nil {
		t.Error("renamed instance missing")
	}
	meta, _, err := LoadMeta(filepath.Join(m.InstancesPath, "new"))
	if err != nil || meta.Name != "new" {
		t.Errorf("meta name = %q, %v", meta.Name, err)
	}

	mkInstance(t, m, "taken")
	if err := m.RenameInstance("new", "taken"); err == nil {
		t.Error("rename onto an existing instance accepted")
	}
	if err := m.RenameInstance("new", "../escape"); err == nil {
		t.Error("rename to a path accepted")
	}
}

func TestCountModsSplitsDisabled(t *testing.T) {
	m := newTestManager(t)
	path := mkInstance(t, m, "pack")
	writeFile(t, filepath.Join(path, "mods", "a.jar"), "")
	writeFile(t, filepath.Join(path, "mods", "b.jar"), "")
	writeFile(t, filepath.Join(path, "mods", "c.jar.disabled"), "")

	list, err := m.ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ModCount != 2 || list[0].DisabledMods != 1 {
		t.Errorf("counts = %+v", list)
	}
}
