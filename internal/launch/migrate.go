package launch

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// HarvestStats summarises what a harvest moved into the shared store.
type HarvestStats struct {
	FilesLinked  int64
	FilesCopied  int64
	FilesSkipped int64
	BytesShared  int64
	// BytesReclaimable is what the source copies now occupy redundantly.
	BytesReclaimable int64
	Errors           []string
}

// HarvestProgress reports a harvest in flight.
type HarvestProgress struct {
	Source  string
	Files   int64
	Bytes   int64
	Current string
}

// harvestDirs are the subtrees of an instance that belong in the shared store.
// Everything else is user data and is never touched.
var harvestDirs = []string{"libraries", "versions", "assets", "runtime"}

// HarvestOptions tunes a harvest.
type HarvestOptions struct {
	// Copy writes real copies instead of hard links. A directory the launcher
	// does not own — the official launcher's .minecraft — must not share its
	// files' storage with the store, or a change on either side would show
	// on the other.
	Copy bool
}

// Harvest copies the shareable content of an instance into the store.
//
// It is deliberately additive: files are hard-linked where the filesystem
// allows it, which costs no extra bytes, and copied otherwise. The instance is
// only read. Freeing the redundant copies is a separate, explicit step, so a
// harvest can never lose data.
//
// This is what makes an existing modded instance launchable without running a
// loader installer at all: its version profile and processed libraries are
// already on disk.
func Harvest(ctx context.Context, layout *Layout, instanceDir string, progress func(HarvestProgress)) (HarvestStats, error) {
	return HarvestWith(ctx, layout, instanceDir, HarvestOptions{}, progress)
}

// HarvestWith is Harvest with options.
func HarvestWith(ctx context.Context, layout *Layout, instanceDir string, opts HarvestOptions, progress func(HarvestProgress)) (HarvestStats, error) {
	var stats HarvestStats
	var files, bytes atomic.Int64

	for _, sub := range harvestDirs {
		src := filepath.Join(instanceDir, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}

		dst, err := layout.harvestTarget(sub)
		if err != nil {
			return stats, err
		}

		err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// One unreadable subdirectory should not abandon the rest.
				stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", path, err))
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}

			rel, err := filepath.Rel(src, path)
			if err != nil {
				return nil
			}
			target := filepath.Join(dst, rel)

			info, err := d.Info()
			if err != nil {
				return nil
			}

			// Already in the store with the same size: nothing to do, but the
			// instance copy is now redundant.
			if existing, err := os.Stat(target); err == nil {
				if existing.Size() == info.Size() {
					stats.FilesSkipped++
					stats.BytesReclaimable += info.Size()
					return nil
				}
				return nil
			}

			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", rel, err))
				return nil
			}

			// A hard link shares the data outright; it only works within one
			// filesystem, so copying is the fallback.
			if !opts.Copy && os.Link(path, target) == nil {
				stats.FilesLinked++
			} else if err := copyPreservingMode(path, target, info.Mode().Perm()); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", rel, err))
				return nil
			} else {
				stats.FilesCopied++
			}

			stats.BytesShared += info.Size()
			stats.BytesReclaimable += info.Size()

			n := files.Add(1)
			b := bytes.Add(info.Size())
			if progress != nil && n%200 == 0 {
				progress(HarvestProgress{
					Source:  filepath.Base(instanceDir),
					Files:   n,
					Bytes:   b,
					Current: rel,
				})
			}
			return nil
		})
		if err != nil {
			return stats, err
		}
	}

	// Runtimes copied by older versions of this tool lost their executable
	// bit; the store must not inherit that.
	if err := repairStoreRuntimes(layout.Runtimes()); err != nil {
		stats.Errors = append(stats.Errors, err.Error())
	}

	return stats, nil
}

// harvestTarget maps an instance subdirectory onto its place in the store.
func (l *Layout) harvestTarget(sub string) (string, error) {
	switch sub {
	case "libraries":
		return l.Libraries(), nil
	case "versions":
		return l.Versions(), nil
	case "assets":
		return l.Assets(), nil
	case "runtime":
		return l.Runtimes(), nil
	}
	return "", fmt.Errorf("unknown harvest directory %q", sub)
}

// copyPreservingMode copies a file, keeping its permissions.
func copyPreservingMode(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// repairStoreRuntimes restores the executable bit inside harvested runtimes.
func repairStoreRuntimes(runtimesDir string) error {
	if _, err := os.Stat(runtimesDir); err != nil {
		return nil
	}

	return filepath.WalkDir(runtimesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) != "bin" && d.Name() != "jspawnhelper" {
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
		return os.Chmod(path, mode|((mode&0o444)>>2))
	})
}

// ReclaimStats summarises what a reclaim removed, or would remove.
type ReclaimStats struct {
	Files int64
	Bytes int64
}

// Reclaim deletes from an instance the game files the shared store already
// holds: the same path under libraries/, versions/, assets/ or runtime/ with
// the same size. With dryRun it only counts. Directories left empty are
// removed afterwards.
//
// It is the second half of a harvest. Six instances made by the official
// launcher each carry their own gigabytes of the same assets and the same two
// Java runtimes; once the store has them, those copies do nothing but occupy
// the disk. Nothing the store does not have is touched, so a reclaim can
// never lose data the launcher needs — and user data lives outside these
// four directories entirely.
func Reclaim(ctx context.Context, layout *Layout, instanceDir string, dryRun bool) (ReclaimStats, error) {
	var stats ReclaimStats

	for _, sub := range harvestDirs {
		src := filepath.Join(instanceDir, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst, err := layout.harvestTarget(sub)
		if err != nil {
			return stats, err
		}

		var emptied []string
		err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if path != src {
					emptied = append(emptied, path)
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			stored, err := os.Stat(filepath.Join(dst, rel))
			if err != nil || stored.Size() != info.Size() {
				return nil
			}
			// A hard link to the store's own file is the same bytes; it
			// still counts as a copy to remove, since it pins the store's
			// layout to the instance's.
			if !dryRun {
				if err := os.Remove(path); err != nil {
					return nil
				}
			}
			stats.Files++
			stats.Bytes += info.Size()
			return nil
		})
		if err != nil {
			return stats, err
		}

		if !dryRun {
			// Deepest first, so a directory is tried after its children.
			for i := len(emptied) - 1; i >= 0; i-- {
				_ = os.Remove(emptied[i])
			}
			_ = os.Remove(src)
		}
	}
	return stats, nil
}

// Reclaimable reports how much of an instance the shared store already
// holds, so a UI can show the saving before anything is removed.
func Reclaimable(layout *Layout, instanceDir string) (int64, error) {
	stats, err := Reclaim(context.Background(), layout, instanceDir, true)
	return stats.Bytes, err
}

// FormatBytes renders a byte count for display.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// HarvestedVersions lists the version ids a harvest brought into the store,
// which is how a modded instance becomes launchable without an installer.
func HarvestedVersions(layout *Layout) []string {
	entries, err := os.ReadDir(layout.Versions())
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if _, err := os.Stat(layout.VersionJSON(id)); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// IsLoaderProfile reports whether a version id names a mod loader profile
// rather than a plain Minecraft release.
func IsLoaderProfile(id string) bool {
	return strings.HasPrefix(id, "neoforge-") ||
		strings.Contains(id, "-forge-") ||
		strings.HasPrefix(id, "fabric-loader-") ||
		strings.HasPrefix(id, "quilt-loader-")
}
