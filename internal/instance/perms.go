package instance

import (
	"io/fs"
	"os"
	"path/filepath"
)

// RuntimeFix records one executable inside a bundled Java runtime whose
// executable bit was missing.
type RuntimeFix struct {
	Instance string      // instance the runtime belongs to
	Path     string      // absolute path of the affected file
	OldMode  fs.FileMode // permissions before the repair
	NewMode  fs.FileMode // permissions after (equal to OldMode when dryRun)
	Err      error       // non-nil if the chmod failed
}

// needsExec reports whether a file inside a Java runtime is supposed to be
// executable. Mojang ships these with the bit set; instances created by
// copying lost it, because the old copy helper hardcoded mode 0644.
func needsExec(path string, d fs.DirEntry) bool {
	if !d.Type().IsRegular() {
		return false
	}
	if filepath.Base(filepath.Dir(path)) == "bin" {
		return true
	}
	// The JVM execs this helper to spawn subprocesses; it lives under lib/.
	return d.Name() == "jspawnhelper"
}

// RepairRuntimePermissions restores the executable bit on Java runtimes bundled
// inside instances. With dryRun set nothing is modified and the result only
// reports what would change.
//
// Without this, an instance whose runtime was copied has a java binary it
// cannot execute, and the only symptom is that the instance mysteriously has no
// suitable Java despite one sitting right there on disk.
func (m *Manager) RepairRuntimePermissions(dryRun bool) ([]RuntimeFix, error) {
	entries, err := os.ReadDir(m.InstancesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var fixes []RuntimeFix
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runtimeDir := filepath.Join(m.InstancesPath, entry.Name(), "runtime")
		if _, err := os.Stat(runtimeDir); err != nil {
			continue
		}
		fixes = append(fixes, repairRuntimeTree(entry.Name(), runtimeDir, dryRun)...)
	}
	return fixes, nil
}

func repairRuntimeTree(instance, runtimeDir string, dryRun bool) []RuntimeFix {
	var fixes []RuntimeFix

	// Walk errors on individual entries are skipped rather than aborting: a
	// single unreadable subdirectory should not hide every other repair.
	_ = filepath.WalkDir(runtimeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !needsExec(path, d) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mode := info.Mode().Perm()
		if mode&0o111 != 0 {
			return nil
		}

		fix := RuntimeFix{
			Instance: instance,
			Path:     path,
			OldMode:  mode,
			NewMode:  mode,
		}
		if !dryRun {
			// Mirror read access into execute: 0644 becomes 0755.
			newMode := mode | ((mode & 0o444) >> 2)
			if err := os.Chmod(path, newMode); err != nil {
				fix.Err = err
			} else {
				fix.NewMode = newMode
			}
		}
		fixes = append(fixes, fix)
		return nil
	})

	return fixes
}
