package instance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultInstanceName is what an imported .minecraft is called unless the
// player names it otherwise.
const DefaultInstanceName = "Default"

// minecraftMarkers are the things a real .minecraft holds. One of them is
// enough; an empty directory the official launcher has not run in yet is
// not worth importing.
var minecraftMarkers = []string{
	"launcher_profiles.json", OptionsFile, "saves", "mods", "versions", "resourcepacks",
}

// ImportOptions controls how .minecraft is copied into a new instance.
type ImportOptions struct {
	// Name is the new instance's name; empty means DefaultInstanceName.
	Name string
	// IncludeSaves and IncludeScreenshots copy the worlds and screenshots
	// too. Mods, configs, packs and options.txt always come along.
	IncludeSaves       bool
	IncludeScreenshots bool

	Ctx      context.Context
	Progress func(copied, total int64, current string)
}

// ImportResult says what an import made.
type ImportResult struct {
	Name string
	Path string
	// Meta is what was detected from the official launcher's files; Detected
	// is false when nothing was and the instance starts unconfigured.
	Meta     Meta
	Detected bool
}

// CanImport reports why .minecraft cannot be imported, or nil when it can: it
// has to be a real directory with a player's things in it, and must not hold
// the instances directory.
func (m *Manager) CanImport() error {
	info, err := os.Lstat(m.MinecraftPath)
	if err != nil {
		return fmt.Errorf("no %s to import", m.MinecraftPath)
	}
	if isLinkInfo(m.MinecraftPath, info) {
		return fmt.Errorf("%s is a link, not a directory to import", m.MinecraftPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", m.MinecraftPath)
	}
	// A copy of .minecraft would then contain the instances, the new one
	// included.
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

// ImportMinecraft copies the player's .minecraft into a new instance, so the
// worlds, mods and settings already there are ready to play here too.
// .minecraft is only read: the official launcher keeps it exactly as it was,
// and the two go their own ways from here.
//
// The game files the official launcher downloaded (versions, libraries,
// assets, runtimes) are left out; they belong in the shared store, which the
// caller fills from .minecraft separately.
func (m *Manager) ImportMinecraft(o ImportOptions) (ImportResult, error) {
	name := o.Name
	if name == "" {
		name = DefaultInstanceName
	}
	if err := m.CanImport(); err != nil {
		return ImportResult{}, err
	}
	instancePath, err := m.InstancePath(name)
	if err != nil {
		return ImportResult{}, err
	}
	if _, err := os.Lstat(instancePath); err == nil {
		return ImportResult{}, fmt.Errorf("instance '%s' already exists", name)
	}

	err = m.CreateInstanceWithOptions(name, CreateOptions{
		CloneFromMinecraftDir: true,
		IncludeSaves:          o.IncludeSaves,
		IncludeScreenshots:    o.IncludeScreenshots,
		Ctx:                   o.Ctx,
		Progress:              o.Progress,
	})
	if err != nil {
		// The directory did not exist before, so whatever is there now is a
		// partial copy of this import and nothing else.
		os.RemoveAll(instancePath)
		return ImportResult{}, err
	}

	result := ImportResult{Name: name, Path: instancePath}
	// The official launcher's profile says what this setup runs. It is read
	// from .minecraft itself, because the copy leaves versions/ out.
	if det, err := DetectMeta(m.MinecraftPath); err == nil && det.Confidence > ConfidenceNone {
		meta := det.Meta
		meta.Name = name
		if err := SaveMeta(instancePath, meta); err == nil {
			result.Meta, result.Detected = meta, true
		}
	}
	return result, nil
}

// within reports whether path is dir or lies inside it.
func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
