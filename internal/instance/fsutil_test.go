package instance

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopyTreePreservesMode is the regression test for the bug that left
// bundled Java runtimes non-executable: the previous copy helper ended in
// os.WriteFile(dst, data, 0644), discarding the source mode entirely.
func TestCopyTreePreservesMode(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")

	binDir := filepath.Join(src, "runtime", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(binDir, "java")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(src, "options.txt")
	if err := os.WriteFile(plain, []byte("fov:70\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyTree(src, dst, CopyOptions{}); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	gotExe := statMode(t, filepath.Join(dst, "runtime", "bin", "java"))
	if gotExe != 0o755 {
		t.Errorf("executable copied as %v, want -rwxr-xr-x (0755)", gotExe)
	}
	if got := statMode(t, filepath.Join(dst, "options.txt")); got != 0o644 {
		t.Errorf("regular file copied as %v, want 0644", got)
	}
}

// TestCopyTreeOverwritePreservesMode covers the case where the destination
// already exists: O_CREATE does not apply a mode to an existing file.
func TestCopyTreeOverwritePreservesMode(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "java"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "java"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyTree(src, dst, CopyOptions{}); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	if got := statMode(t, filepath.Join(dst, "java")); got != 0o755 {
		t.Errorf("overwritten file has mode %v, want 0755", got)
	}
}

func TestCopyTreeRecreatesSymlinks(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")

	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := CopyTree(src, dst, CopyOptions{}); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	info, err := os.Lstat(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Error("symlink was dereferenced into a regular file")
	}
	target, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "real.txt" {
		t.Errorf("symlink target = %q, want %q", target, "real.txt")
	}
}

func TestDefaultCreateSkip(t *testing.T) {
	cases := []struct {
		rel  string
		dir  bool
		skip bool
	}{
		// Shared-store content and launcher cruft at the top level.
		{"versions", true, true},
		{"libraries", true, true},
		{"assets", true, true},
		{"runtime", true, true},
		{"webcache2", true, true},
		{"bootstrap_log.txt", false, true},
		{"bootstrap_log3.txt", false, true},
		{"launcher_log9.txt", false, true},
		{"usercache.json", false, true},
		// Kept: user data.
		{"mods", true, false},
		{"config", true, false},
		{"saves", true, false},
		{"options.txt", false, false},
		{"launcher_profiles.json", false, false},
		// Nested entries are never matched by name, so an instance's own
		// config/logs directory and a mod called assets.jar both survive.
		{filepath.Join("config", "logs"), true, false},
		{filepath.Join("mods", "assets.jar"), false, false},
		{filepath.Join("saves", "world", "versions"), true, false},
	}

	for _, tc := range cases {
		got := DefaultCreateSkip(tc.rel, fakeEntry{name: filepath.Base(tc.rel), dir: tc.dir})
		if got != tc.skip {
			t.Errorf("DefaultCreateSkip(%q, dir=%v) = %v, want %v", tc.rel, tc.dir, got, tc.skip)
		}
	}
}

func TestCopyTreeSkipsExcludedSubtrees(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")

	mustWrite(t, filepath.Join(src, "mods", "a.jar"), "keep")
	mustWrite(t, filepath.Join(src, "libraries", "deep", "b.jar"), "drop")
	mustWrite(t, filepath.Join(src, "saves", "world", "level.dat"), "drop")
	mustWrite(t, filepath.Join(src, "options.txt"), "keep")

	opts := CopyOptions{Skip: cloneSkip(CreateOptions{})}
	if err := CopyTree(src, dst, opts); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	mustExist(t, filepath.Join(dst, "mods", "a.jar"))
	mustExist(t, filepath.Join(dst, "options.txt"))
	mustNotExist(t, filepath.Join(dst, "libraries"))
	mustNotExist(t, filepath.Join(dst, "saves"))
}

func TestCloneSkipHonoursIncludeFlags(t *testing.T) {
	withSaves := cloneSkip(CreateOptions{IncludeSaves: true})
	if withSaves("saves", fakeEntry{name: "saves", dir: true}) {
		t.Error("saves skipped despite IncludeSaves")
	}
	if !withSaves("screenshots", fakeEntry{name: "screenshots", dir: true}) {
		t.Error("screenshots kept despite IncludeScreenshots being false")
	}

	withShots := cloneSkip(CreateOptions{IncludeScreenshots: true})
	if withShots("screenshots", fakeEntry{name: "screenshots", dir: true}) {
		t.Error("screenshots skipped despite IncludeScreenshots")
	}
}

func TestCopyTreeReportsProgressToCompletion(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	mustWrite(t, filepath.Join(src, "a.txt"), strings.Repeat("x", 100))
	mustWrite(t, filepath.Join(src, "b", "c.txt"), strings.Repeat("y", 50))

	var lastCopied, lastTotal int64
	opts := CopyOptions{Progress: func(copied, total int64, _ string) {
		lastCopied, lastTotal = copied, total
	}}
	if err := CopyTree(src, dst, opts); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	if lastTotal != 150 {
		t.Errorf("total = %d, want 150", lastTotal)
	}
	if lastCopied != lastTotal {
		t.Errorf("final progress %d/%d, want them equal", lastCopied, lastTotal)
	}
}

func TestCopyTreeHonoursCancellation(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	mustWrite(t, filepath.Join(src, "a.txt"), "data")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := CopyTree(src, dst, CopyOptions{Ctx: ctx}); err == nil {
		t.Fatal("CopyTree with a cancelled context = nil, want an error")
	}
}

// --- helpers ---

func statMode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be absent", path)
	}
}

// fakeEntry is a minimal fs.DirEntry for exercising the skip predicates
// without touching the filesystem.
type fakeEntry struct {
	name string
	dir  bool
}

func (f fakeEntry) Name() string { return f.name }
func (f fakeEntry) IsDir() bool  { return f.dir }
func (f fakeEntry) Type() fs.FileMode {
	if f.dir {
		return fs.ModeDir
	}
	return 0
}
func (f fakeEntry) Info() (fs.FileInfo, error) { return nil, nil }
