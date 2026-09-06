package launch

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractNatives unpacks a native library archive into destDir.
//
// Entries whose name starts with one of the exclude prefixes are skipped —
// manifests use that to drop META-INF signatures, which the JVM would
// otherwise reject.
func ExtractNatives(archive, destDir string, exclude []string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("opening %s: %w", filepath.Base(archive), err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	for _, f := range r.File {
		if excluded(f.Name, exclude) {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}

		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}

		// Natives are already extracted after the first launch; skipping
		// matching files keeps subsequent launches instant.
		if info, err := os.Stat(target); err == nil && info.Size() == f.FileInfo().Size() {
			continue
		}
		if err := extractZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin resolves an archive entry against a destination directory and
// refuses anything that would escape it.
//
// Without this a crafted archive containing "../../.bashrc" would write
// wherever it liked — the zip-slip vulnerability.
func safeJoin(destDir, name string) (string, error) {
	// Archive paths always use forward slashes, whatever the host.
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing archive entry %q: it escapes the target directory", name)
	}

	target := filepath.Join(destDir, cleaned)
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing archive entry %q: it escapes the target directory", name)
	}
	return target, nil
}

func extractZipEntry(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	in, err := f.Open()
	if err != nil {
		return fmt.Errorf("reading %s: %w", f.Name, err)
	}
	defer in.Close()

	// Natives must stay executable on Unix; the archive carries the mode.
	mode := f.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("extracting %s: %w", f.Name, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}

func excluded(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
