package instance

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// copyBufSize is the reusable buffer size for streaming copies. Copying via
// io.Copy with a fixed buffer keeps memory constant regardless of file size —
// instances here contain multi-hundred-megabyte jars and world region files.
const copyBufSize = 1 << 20 // 1 MiB

// CopyOptions configures CopyTree.
type CopyOptions struct {
	// Ctx cancels the copy. A cancelled copy leaves the partial destination in
	// place for the caller to remove; CopyTree itself does not delete it.
	Ctx context.Context

	// Skip reports whether an entry should be excluded. rel is the path
	// relative to the copy root, always using the OS separator. Skipping a
	// directory skips its entire subtree.
	Skip func(rel string, d fs.DirEntry) bool

	// Progress, if set, is called as bytes are copied. total is the size
	// computed by the sizing pass, so callers can render a real percentage
	// instead of an indeterminate spinner.
	Progress func(copied, total int64, current string)
}

// DefaultCreateSkip excludes everything a new instance has no use for: content
// the shared store supplies (versions, libraries, assets, runtimes) and pure
// launcher cruft. Only top-level entries are considered, so an instance's own
// config/logs or a mod named "assets.jar" are unaffected.
func DefaultCreateSkip(rel string, d fs.DirEntry) bool {
	if strings.ContainsRune(rel, filepath.Separator) {
		return false
	}

	if d.IsDir() {
		switch rel {
		case "versions", "libraries", "assets", "runtime", "bin",
			"logs", "crash-reports", "webcache2", "launcher", ".mixin.out":
			return true
		}
		return false
	}

	switch rel {
	case "usercache.json", "usernamecache.json", "clientId_v2.txt",
		"launcher_cef_log.txt", "launcher_ui_state.json":
		return true
	}
	for _, glob := range []string{"bootstrap_log*.txt", "launcher_log*.txt"} {
		if ok, _ := filepath.Match(glob, rel); ok {
			return true
		}
	}
	return false
}

// CopyTree recursively copies src into dst.
//
// It differs from a naive walk in three ways that matter here: files are
// streamed rather than read whole into memory, the source file mode is
// preserved (a hardcoded 0644 silently strips the executable bit from bundled
// Java runtimes), and symlinks are recreated instead of followed.
func CopyTree(src, dst string, o CopyOptions) error {
	ctx := o.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	total, err := treeSize(ctx, src, o.Skip)
	if err != nil {
		return err
	}

	buf := make([]byte, copyBufSize)
	var copied int64

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if o.Skip != nil && o.Skip(rel, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			mode := fs.FileMode(0o755)
			if info, err := d.Info(); err == nil {
				mode = info.Mode().Perm()
			}
			return os.MkdirAll(target, mode)

		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read symlink %s: %w", rel, err)
			}
			// A leftover target would make os.Symlink fail; the tree is being
			// built fresh, so removing it is safe.
			_ = os.Remove(target)
			if err := os.Symlink(link, target); err != nil {
				return fmt.Errorf("create symlink %s: %w", rel, err)
			}
			return nil

		case !d.Type().IsRegular():
			// Sockets, devices, FIFOs — nothing an instance needs.
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := copyFileStream(ctx, path, target, info.Mode().Perm(), buf); err != nil {
			return err
		}

		copied += info.Size()
		if o.Progress != nil {
			o.Progress(copied, total, rel)
		}
		return nil
	})
}

// treeSize sums the bytes CopyTree will actually write, so progress reporting
// has a real denominator.
func treeSize(ctx context.Context, src string, skip func(string, fs.DirEntry) bool) (int64, error) {
	var total int64
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip != nil && skip(rel, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// copyFileStream copies one regular file, preserving mode.
func copyFileStream(ctx context.Context, src, dst string, mode fs.FileMode, buf []byte) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.CopyBuffer(out, &ctxReader{ctx: ctx, r: in}, buf); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return err
	}

	// O_CREATE only applies the mode when the file did not exist; an overwrite
	// would otherwise keep the old permissions.
	return os.Chmod(dst, mode)
}

// ctxReader aborts a long read when the context is cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// cloneSource resolves the directory a new instance should be populated from,
// or "" when an empty skeleton was requested.
func (m *Manager) cloneSource(o CreateOptions) (string, error) {
	switch {
	case o.CloneFrom != "":
		src, err := m.InstancePath(o.CloneFrom)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(src); err != nil {
			return "", fmt.Errorf("cannot clone '%s': %w", o.CloneFrom, err)
		}
		return src, nil

	case o.CloneFromMinecraftDir:
		info, err := os.Lstat(m.MinecraftPath)
		if err != nil {
			// Nothing to clone from is not an error; the skeleton stands alone.
			return "", nil
		}
		// A link here would be one into an instance, the way versions before
		// 2 left it; copying through it would duplicate that instance under
		// the official launcher's name.
		if isLinkInfo(m.MinecraftPath, info) {
			return "", fmt.Errorf("%s is a link, not a directory to copy", m.MinecraftPath)
		}
		return m.MinecraftPath, nil
	}
	return "", nil
}

// cloneSkip builds the exclusion predicate for a clone: the shared-store and
// cruft defaults, plus the two directories that dominate a clone's size unless
// the caller opted into them.
func cloneSkip(o CreateOptions) func(string, fs.DirEntry) bool {
	return func(rel string, d fs.DirEntry) bool {
		if DefaultCreateSkip(rel, d) {
			return true
		}
		if strings.ContainsRune(rel, filepath.Separator) {
			return false
		}
		if !o.IncludeSaves && rel == "saves" {
			return true
		}
		if !o.IncludeScreenshots && rel == "screenshots" {
			return true
		}
		return false
	}
}
