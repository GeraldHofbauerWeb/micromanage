package instance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func optionsManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m := &Manager{
		AppDir:        root,
		InstancesPath: filepath.Join(root, "instances"),
		MinecraftPath: filepath.Join(root, "minecraft"),
		BackupPath:    filepath.Join(root, "backup"),
	}
	dir := filepath.Join(m.InstancesPath, "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func writeOptions(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, OptionsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSaveListRestoreOptions(t *testing.T) {
	m, dir := optionsManager(t)

	if _, err := m.SaveOptions("pack", "nothing yet"); err == nil {
		t.Fatal("saving without an options.txt should fail")
	}

	writeOptions(t, dir, "renderDistance:12\nfov:0.0\nkey_key.jump:key.keyboard.space\n")
	first, err := m.SaveOptions("pack", "Sebi's keybinds / v1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Label != "Sebi's keybinds - v1" {
		t.Errorf("label = %q, want the slash replaced", first.Label)
	}
	if !strings.HasSuffix(first.Name, "--Sebi's keybinds - v1.txt") {
		t.Errorf("file name = %q", first.Name)
	}

	// Two saves in the same second must not collide.
	second, err := m.SaveOptions("pack", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Name == first.Name {
		t.Fatalf("second snapshot reused the name %q", first.Name)
	}
	if second.Label != "" {
		t.Errorf("unlabelled snapshot got label %q", second.Label)
	}

	list, err := m.ListOptionsSnapshots("pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d snapshots, want 2: %+v", len(list), list)
	}
	if time.Since(list[0].Time) > time.Minute {
		t.Errorf("snapshot time %v was not read back from the name", list[0].Time)
	}

	// The game changes a setting; restoring the snapshot puts it back and
	// keeps the changed file as a snapshot of its own.
	writeOptions(t, dir, "renderDistance:32\nfov:0.0\nkey_key.jump:key.keyboard.space\nnewSetting:true\n")
	changes, err := m.OptionsDiff("pack", first.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Key != "newSetting" || changes[0].New != "" ||
		changes[1].Key != "renderDistance" || changes[1].Old != "32" || changes[1].New != "12" {
		t.Errorf("diff = %+v", changes)
	}

	kept, ok, err := m.RestoreOptions("pack", first.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || kept.Label != "before restore" {
		t.Errorf("restore kept %+v (ok=%v), want a 'before restore' snapshot", kept, ok)
	}
	got, _ := os.ReadFile(filepath.Join(dir, OptionsFile))
	if !strings.HasPrefix(string(got), "renderDistance:12\n") || strings.Contains(string(got), "newSetting") {
		t.Errorf("options.txt after restore = %q", got)
	}

	// Restoring what is already in place keeps nothing extra.
	if _, ok, err := m.RestoreOptions("pack", first.Name); err != nil || ok {
		t.Errorf("restoring again: ok=%v err=%v; the current file matched, nothing to keep", ok, err)
	}
	// "latest" is the safety copy, and restoring it brings the change back.
	if _, _, err := m.RestoreOptions("pack", LatestSnapshot); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, OptionsFile)); !strings.Contains(string(got), "newSetting") {
		t.Errorf("options.txt after restoring latest = %q", got)
	}

	if err := m.DeleteOptionsSnapshot("pack", "../"+OptionsFile); err == nil {
		t.Error("a path was accepted as a snapshot name")
	}
	if err := m.DeleteOptionsSnapshot("pack", second.Name); err != nil {
		t.Fatal(err)
	}
	list, _ = m.ListOptionsSnapshots("pack")
	if len(list) != 3 {
		t.Errorf("after delete %d snapshots remain, want 3 (first + two safety copies)", len(list))
	}
}

func TestDiffOptionsHandlesValuesWithColons(t *testing.T) {
	changes := DiffOptions([]byte("lastServer:play.example.org:25565\nlang:en_us"),
		[]byte("lastServer:play.example.org:25566\nlang:en_us\n"))
	if len(changes) != 1 || changes[0].Old != "play.example.org:25565" || changes[0].New != "play.example.org:25566" {
		t.Errorf("diff = %+v", changes)
	}
}
