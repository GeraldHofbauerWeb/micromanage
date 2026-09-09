package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// DefaultInstanceName is what the player's existing .minecraft becomes on
// the first run, when there are no instances yet.
const DefaultInstanceName = "Default"

// AdoptResult says what became of a .minecraft that was turned into an
// instance.
type AdoptResult struct {
	Name string
	Path string
	// Copied is true when the directory could not simply be renamed (it was
	// on another filesystem) and was copied instead; the original was then
	// moved to the backup path rather than deleted.
	Copied bool
	// Meta is what was detected from the launcher's own files; Detected is
	// false when nothing was and the instance starts unconfigured.
	Meta     Meta
	Detected bool
}

// minecraftMarkers are the things a real .minecraft holds. One of them is
// enough; an empty directory the official launcher has not run in yet is
// not worth adopting.
var minecraftMarkers = []string{
	"launcher_profiles.json", OptionsFile, "saves", "mods", "versions", "resourcepacks",
}

// CanAdopt reports whether the first run should turn .minecraft into an
// instance: there are no instances yet, and .minecraft is a real directory
// with a player's things in it rather than a symlink or an empty folder.
func (m *Manager) CanAdopt() bool {
	instances, err := m.ListInstances()
	if err != nil || len(instances) > 0 {
		return false
	}
	return m.adoptable() == nil
}

// adoptable checks the preconditions, naming the first one that fails.
func (m *Manager) adoptable() error {
	info, err := os.Lstat(m.MinecraftPath)
	if err != nil {
		return fmt.Errorf("no %s to adopt", m.MinecraftPath)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is already a link to an instance", m.MinecraftPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", m.MinecraftPath)
	}
	// Moving .minecraft would carry the instances along if they lived
	// inside it, and the link would then point into itself.
	if within(m.InstancesPath, m.MinecraftPath) {
		return fmt.Errorf("the instances directory is inside %s", m.MinecraftPath)
	}
	for _, marker := range minecraftMarkers {
		if _, err := os.Lstat(filepath.Join(m.MinecraftPath, marker)); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%s holds nothing the game made", m.MinecraftPath)
}

// AdoptMinecraft turns the current .minecraft directory into an instance
// and links .minecraft to it, so the player's existing setup — worlds,
// mods, settings, the lot — is the first instance instead of something to
// copy. The directory is renamed, which is instant and keeps every byte;
// only across filesystems is it copied, and even then the original is kept
// as the backup.
func (m *Manager) AdoptMinecraft(name string) (AdoptResult, error) {
	if name == "" {
		name = DefaultInstanceName
	}
	if err := m.adoptable(); err != nil {
		return AdoptResult{}, err
	}
	instancePath, err := m.InstancePath(name)
	if err != nil {
		return AdoptResult{}, err
	}
	if _, err := os.Lstat(instancePath); err == nil {
		return AdoptResult{}, fmt.Errorf("instance '%s' already exists", name)
	}
	if err := os.MkdirAll(m.InstancesPath, 0o755); err != nil {
		return AdoptResult{}, fmt.Errorf("failed to create instances directory: %w", err)
	}

	result := AdoptResult{Name: name, Path: instancePath}
	if err := os.Rename(m.MinecraftPath, instancePath); err != nil {
		if !crossDevice(err) {
			return AdoptResult{}, fmt.Errorf("moving %s: %w", m.MinecraftPath, err)
		}
		// Another filesystem: copy everything, then park the original as
		// the backup the way a switch does, so nothing is ever deleted here.
		if err := CopyTree(m.MinecraftPath, instancePath, CopyOptions{}); err != nil {
			os.RemoveAll(instancePath)
			return AdoptResult{}, fmt.Errorf("copying %s: %w", m.MinecraftPath, err)
		}
		if err := os.RemoveAll(m.BackupPath); err != nil {
			return AdoptResult{}, fmt.Errorf("failed to remove old backup: %w", err)
		}
		if err := os.Rename(m.MinecraftPath, m.BackupPath); err != nil {
			return AdoptResult{}, fmt.Errorf("failed to set aside %s: %w", m.MinecraftPath, err)
		}
		result.Copied = true
	}

	if err := os.Symlink(instancePath, m.MinecraftPath); err != nil {
		return result, fmt.Errorf("failed to create symlink: %w", err)
	}
	for _, dir := range essentialDirs {
		if err := os.MkdirAll(filepath.Join(instancePath, dir), 0o755); err != nil {
			return result, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// The official launcher's profile says what this setup runs; carry it
	// over so the instance is ready to play rather than "not set up".
	if det, err := DetectMeta(instancePath); err == nil && det.Confidence > ConfidenceNone {
		meta := det.Meta
		meta.Name = name
		if err := SaveMeta(instancePath, meta); err == nil {
			result.Meta, result.Detected = meta, true
		}
	}
	return result, nil
}

// crossDevice reports whether a rename failed because source and target are
// on different filesystems.
func crossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		err = linkErr.Err
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	// Windows says ERROR_NOT_SAME_DEVICE instead.
	var errno syscall.Errno
	return runtime.GOOS == "windows" && errors.As(err, &errno) && errno == 17
}

// within reports whether path is dir or lies inside it.
func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
