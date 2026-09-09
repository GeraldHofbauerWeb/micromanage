package launch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestReclaimRemovesOnlyWhatTheStoreHolds: a file the store has, same size,
// goes; a file it lacks or holds at a different size stays; user data is
// never looked at.
func TestReclaimRemovesOnlyWhatTheStoreHolds(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(root)
	inst := filepath.Join(root, "instances", "pack")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// In the store and in the instance, identical: redundant.
	write(filepath.Join(layout.Libraries(), "com", "x", "x-1.jar"), "same-bytes")
	write(filepath.Join(inst, "libraries", "com", "x", "x-1.jar"), "same-bytes")
	// In the store at a different size: not the same file, keep it.
	write(filepath.Join(layout.Assets(), "objects", "ab", "abcd"), "long-version")
	write(filepath.Join(inst, "assets", "objects", "ab", "abcd"), "short")
	// Only in the instance: keep it.
	write(filepath.Join(inst, "versions", "custom", "custom.json"), "{}")
	// User data, never touched even if it looked shared.
	write(filepath.Join(inst, "mods", "cool.jar"), "same-bytes")

	dry, err := Reclaim(context.Background(), layout, inst, true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Files != 1 || dry.Bytes != int64(len("same-bytes")) {
		t.Errorf("dry run = %+v, want 1 file of %d bytes", dry, len("same-bytes"))
	}
	if _, err := os.Stat(filepath.Join(inst, "libraries", "com", "x", "x-1.jar")); err != nil {
		t.Fatal("dry run removed a file")
	}

	got, err := Reclaim(context.Background(), layout, inst, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != dry {
		t.Errorf("real run = %+v, dry run said %+v", got, dry)
	}

	if _, err := os.Stat(filepath.Join(inst, "libraries")); !os.IsNotExist(err) {
		t.Error("the emptied libraries tree was not removed")
	}
	for _, keep := range []string{
		"assets/objects/ab/abcd", "versions/custom/custom.json", "mods/cool.jar",
	} {
		if _, err := os.Stat(filepath.Join(inst, filepath.FromSlash(keep))); err != nil {
			t.Errorf("%s was removed, but the store does not hold it", keep)
		}
	}

	size, err := Reclaimable(layout, inst)
	if err != nil || size != 0 {
		t.Errorf("after reclaim, reclaimable = %d, %v; want 0", size, err)
	}
}
