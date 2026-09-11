package launch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestHarvestCopySharesNoStorage fills the store from a directory the
// launcher does not own. Every file has to arrive as a copy of its own, so
// nothing done to one side ever shows on the other.
func TestHarvestCopySharesNoStorage(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(filepath.Join(root, "app"))
	source := filepath.Join(root, ".minecraft")

	files := []string{
		filepath.Join("versions", "1.21.1", "1.21.1.json"),
		filepath.Join("libraries", "org", "lwjgl", "lwjgl.jar"),
		filepath.Join("assets", "indexes", "17.json"),
	}
	for _, rel := range files {
		path := filepath.Join(source, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := HarvestWith(context.Background(), layout, source, HarvestOptions{Copy: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesLinked != 0 || stats.FilesCopied != int64(len(files)) {
		t.Errorf("stats = %+v, want %d copies and no links", stats, len(files))
	}

	targets := map[string]string{
		files[0]: layout.VersionJSON("1.21.1"),
		files[1]: filepath.Join(layout.Libraries(), "org", "lwjgl", "lwjgl.jar"),
		files[2]: filepath.Join(layout.Assets(), "indexes", "17.json"),
	}
	for rel, target := range targets {
		src, err := os.Stat(filepath.Join(source, rel))
		if err != nil {
			t.Fatal(err)
		}
		dst, err := os.Stat(target)
		if err != nil {
			t.Fatalf("%s did not reach the store: %v", rel, err)
		}
		if os.SameFile(src, dst) {
			t.Errorf("%s shares its storage with the store", rel)
		}
	}
}
