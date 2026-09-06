package launch

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// makeZip builds an archive from name/content pairs. A name may use "../" to
// exercise the containment check.
func makeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(0o755)
		entry, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractNatives(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "natives.jar")
	dest := filepath.Join(dir, "out")

	makeZip(t, archive, map[string]string{
		"liblwjgl.so":          "native code",
		"libglfw.so":           "more native code",
		"META-INF/MANIFEST.MF": "should be excluded",
		"META-INF/SIG.RSA":     "should be excluded",
	})

	if err := ExtractNatives(archive, dest, []string{"META-INF/"}); err != nil {
		t.Fatalf("ExtractNatives: %v", err)
	}

	for _, name := range []string{"liblwjgl.so", "libglfw.so"} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Errorf("%s was not extracted: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "META-INF")); !os.IsNotExist(err) {
		t.Error("excluded META-INF was extracted; the JVM rejects signed natives")
	}

	// Natives must remain executable.
	info, err := os.Stat(filepath.Join(dest, "liblwjgl.so"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("extracted native has mode %v, want the executable bit set", info.Mode().Perm())
	}
}

// TestExtractNativesRejectsZipSlip is the security guard: an archive entry
// that escapes the destination must be refused, not written.
func TestExtractNativesRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.jar")
	dest := filepath.Join(dir, "out")

	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	makeZip(t, archive, map[string]string{"../victim.txt": "overwritten"})

	if err := ExtractNatives(archive, dest, nil); err == nil {
		t.Fatal("an escaping archive entry was accepted")
	}

	content, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Error("a file outside the destination was overwritten")
	}
}

func TestSafeJoin(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")

	for _, name := range []string{"../escape", "../../etc/passwd", "a/../../escape", "/etc/passwd"} {
		if _, err := safeJoin(dest, name); err == nil {
			t.Errorf("safeJoin(%q) = nil error, want a refusal", name)
		}
	}
	for _, name := range []string{"lib.so", "sub/lib.so", "./lib.so"} {
		got, err := safeJoin(dest, name)
		if err != nil {
			t.Errorf("safeJoin(%q): %v", name, err)
			continue
		}
		if rel, _ := filepath.Rel(dest, got); rel == ".." {
			t.Errorf("safeJoin(%q) escaped the destination", name)
		}
	}
}

// TestExtractNativesSkipsExisting keeps repeat launches instant.
func TestExtractNativesSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "natives.jar")
	dest := filepath.Join(dir, "out")
	makeZip(t, archive, map[string]string{"lib.so": "native code"})

	if err := ExtractNatives(archive, dest, nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dest, "lib.so")
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	// Mark the file so a re-extraction would be visible.
	if err := os.Chtimes(target, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := ExtractNatives(archive, dest, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged native was re-extracted")
	}
}

func TestExtractNativesMissingArchive(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractNatives(filepath.Join(dir, "nope.jar"), filepath.Join(dir, "out"), nil); err == nil {
		t.Error("extracting a missing archive = nil, want an error")
	}
}
