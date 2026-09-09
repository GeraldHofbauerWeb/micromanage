package instance

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ContentKind names one of the directories inside an instance that hold what
// a player actually manages: the mods, their configuration, the worlds, the
// packs, and what the game leaves behind.
//
// The value is the directory name, so a kind can be joined onto an instance
// path directly.
type ContentKind string

const (
	ContentMods          ContentKind = "mods"
	ContentConfig        ContentKind = "config"
	ContentSaves         ContentKind = "saves"
	ContentResourcePacks ContentKind = "resourcepacks"
	ContentShaderPacks   ContentKind = "shaderpacks"
	ContentScreenshots   ContentKind = "screenshots"
	ContentLogs          ContentKind = "logs"
	ContentCrashReports  ContentKind = "crash-reports"
)

// ContentKinds returns every kind in presentation order: what a player
// changes most often first, what the game writes for them last.
func ContentKinds() []ContentKind {
	return []ContentKind{
		ContentMods, ContentConfig, ContentSaves,
		ContentResourcePacks, ContentShaderPacks,
		ContentScreenshots, ContentLogs, ContentCrashReports,
	}
}

// Valid reports whether k is a kind this build knows.
func (k ContentKind) Valid() bool {
	for _, known := range ContentKinds() {
		if known == k {
			return true
		}
	}
	return false
}

// Dir is the directory name inside an instance.
func (k ContentKind) Dir() string { return string(k) }

// Label is the kind's name as shown to a player.
func (k ContentKind) Label() string {
	switch k {
	case ContentMods:
		return "Mods"
	case ContentConfig:
		return "Configs"
	case ContentSaves:
		return "Worlds"
	case ContentResourcePacks:
		return "Resource packs"
	case ContentShaderPacks:
		return "Shader packs"
	case ContentScreenshots:
		return "Screenshots"
	case ContentLogs:
		return "Logs"
	case ContentCrashReports:
		return "Crash reports"
	}
	return string(k)
}

// Short is a name that fits a tab; Label is for anywhere with room.
func (k ContentKind) Short() string {
	switch k {
	case ContentResourcePacks:
		return "Resources"
	case ContentShaderPacks:
		return "Shaders"
	case ContentCrashReports:
		return "Crashes"
	}
	return k.Label()
}

// Recursive reports whether the kind's directory is listed as a tree. Mod
// configuration is the one case: mods nest their files in subdirectories,
// and a player looking for one wants a flat, searchable list of paths.
func (k ContentKind) Recursive() bool { return k == ContentConfig }

// Toggleable reports whether entries of this kind can be switched off
// without deleting them.
func (k ContentKind) Toggleable() bool { return k == ContentMods }

// NewestFirst reports whether the kind is best read in reverse
// chronological order, which is true for everything the game writes.
func (k ContentKind) NewestFirst() bool {
	switch k {
	case ContentScreenshots, ContentLogs, ContentCrashReports:
		return true
	}
	return false
}

// DisabledSuffix is appended to a mod's file name to keep it out of the
// game's classpath. It is the convention every other launcher uses, so a
// mod switched off here shows as off there too.
const DisabledSuffix = ".disabled"

// Entry is one file or directory inside a content directory.
type Entry struct {
	// Name is the path relative to the kind's directory, with forward
	// slashes, which is what a player sees and what the mutating methods
	// accept back.
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
	IsDir   bool
	// Disabled reports a mod carrying DisabledSuffix.
	Disabled bool
	// Title is the name a mod gives itself in its manifest, filled in by
	// whoever reads jars; empty means the file name is all there is.
	Title string
	// Version is the mod's own version, when its manifest states one.
	Version string
}

// Label is the best name to show: the manifest's title, else the file name
// without the disabled marker.
func (e Entry) Label() string {
	if e.Title != "" {
		return e.Title
	}
	return e.DisplayName()
}

// DisplayName is the name without the disabled marker.
func (e Entry) DisplayName() string {
	return strings.TrimSuffix(e.Name, DisabledSuffix)
}

// ContentDir returns the absolute directory holding a kind of content.
func (m *Manager) ContentDir(instanceName string, kind ContentKind) (string, error) {
	instancePath, err := m.InstancePath(instanceName)
	if err != nil {
		return "", err
	}
	if !kind.Valid() {
		return "", fmt.Errorf("unknown content type: %s", kind)
	}
	return filepath.Join(instancePath, kind.Dir()), nil
}

// ListContent lists what an instance holds of one kind. A directory that does
// not exist yet lists as empty rather than failing: a fresh instance has no
// screenshots, and that is not an error.
func (m *Manager) ListContent(instanceName string, kind ContentKind) ([]Entry, error) {
	dir, err := m.ContentDir(instanceName, kind)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	if kind.Recursive() {
		entries, err = listTree(dir)
	} else {
		entries, err = listFlat(dir, kind)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	if kind.NewestFirst() {
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].ModTime.After(entries[j].ModTime)
		})
	} else {
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := strings.ToLower(entries[i].DisplayName()), strings.ToLower(entries[j].DisplayName())
			if a != b {
				return a < b
			}
			return entries[i].Name < entries[j].Name
		})
	}
	return entries, nil
}

// listFlat reads one directory level, keeping what makes sense for the kind:
// worlds are directories, mods are files, packs come both ways.
func listFlat(dir string, kind ContentKind) ([]Entry, error) {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		isDir := d.IsDir()
		switch kind {
		case ContentSaves:
			if !isDir {
				continue
			}
		case ContentMods, ContentScreenshots, ContentLogs, ContentCrashReports:
			if isDir {
				continue
			}
		}
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}

		e := Entry{
			Name:  d.Name(),
			Path:  filepath.Join(dir, d.Name()),
			IsDir: isDir,
		}
		if info, err := d.Info(); err == nil {
			e.Size = info.Size()
			e.ModTime = info.ModTime()
		}
		if isDir {
			e.Size = 0
			// A world's own modification time says when it was last played
			// far better than the directory's, which only changes when the
			// game adds a file.
			if kind == ContentSaves {
				if info, err := os.Stat(filepath.Join(e.Path, "level.dat")); err == nil {
					e.ModTime = info.ModTime()
				}
			}
		}
		if kind.Toggleable() {
			e.Disabled = strings.HasSuffix(e.Name, DisabledSuffix)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// listTree flattens a directory tree into relative paths.
func listTree(root string) ([]Entry, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	var entries []Entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable subdirectory should not hide the rest.
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		e := Entry{Name: filepath.ToSlash(rel), Path: path}
		if info, err := d.Info(); err == nil {
			e.Size = info.Size()
			e.ModTime = info.ModTime()
		}
		entries = append(entries, e)
		return nil
	})
	return entries, err
}

// ContentSummary is the per-kind count shown beside an instance.
type ContentSummary struct {
	Kind     ContentKind
	Count    int
	Disabled int
}

// SetContentEnabled switches a mod on or off by renaming it, and returns the
// name it now has. Switching a mod that is already in the wanted state is a
// no-op rather than an error, so a double click cannot fail.
func (m *Manager) SetContentEnabled(instanceName string, kind ContentKind, name string, enabled bool) (string, error) {
	if !kind.Toggleable() {
		return "", fmt.Errorf("%s cannot be switched off; delete them instead", kind.Label())
	}
	target, err := m.contentPath(instanceName, kind, name)
	if err != nil {
		return "", err
	}

	disabled := strings.HasSuffix(name, DisabledSuffix)
	if enabled == !disabled {
		return name, nil
	}

	var newName string
	if enabled {
		newName = strings.TrimSuffix(name, DisabledSuffix)
	} else {
		newName = name + DisabledSuffix
	}
	newPath := filepath.Join(filepath.Dir(target), filepath.Base(newName))

	if _, err := os.Lstat(target); err != nil {
		return "", fmt.Errorf("%s does not exist", name)
	}
	if _, err := os.Lstat(newPath); err == nil {
		return "", fmt.Errorf("%s already exists", newName)
	}
	if err := os.Rename(target, newPath); err != nil {
		return "", err
	}
	return newName, nil
}

// DeleteContent removes one entry of a kind. A world is a directory: deleting
// one removes everything in it.
func (m *Manager) DeleteContent(instanceName string, kind ContentKind, name string) error {
	target, err := m.contentPath(instanceName, kind, name)
	if err != nil {
		return err
	}
	// Lstat, not Stat: a dangling symlink still exists and should be removable.
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", name)
	}
	return os.RemoveAll(target)
}

// contentPath resolves an entry name inside a kind's directory, refusing
// anything that would land outside it.
//
// Names from a recursive listing contain slashes, so each segment is checked
// on its own and the resolved path is checked again against the directory,
// which rules out "..", absolute paths and symlink tricks in the name itself.
func (m *Manager) contentPath(instanceName string, kind ContentKind, name string) (string, error) {
	dir, err := m.ContentDir(instanceName, kind)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("file name cannot be empty")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || strings.Contains(name, "\x00") {
		return "", fmt.Errorf("invalid file name: %q", name)
	}
	segments := strings.Split(filepath.ToSlash(name), "/")
	if len(segments) > 1 && !kind.Recursive() {
		return "", fmt.Errorf("invalid file name: %q must be a plain name, not a path", name)
	}
	for _, seg := range segments {
		if err := validateName("file name", seg); err != nil {
			return "", err
		}
	}

	target := filepath.Join(dir, filepath.FromSlash(name))
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid file name: %q escapes the instance directory", name)
	}
	return target, nil
}

// RenameInstance moves an instance to a new name. The active instance is
// re-linked afterwards, so renaming what is currently playing is safe; the
// game is only started through the link anyway.
func (m *Manager) RenameInstance(oldName, newName string) error {
	oldPath, err := m.InstancePath(oldName)
	if err != nil {
		return err
	}
	newPath, err := m.InstancePath(newName)
	if err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	if _, err := os.Stat(oldPath); err != nil {
		return fmt.Errorf("instance '%s' does not exist", oldName)
	}
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("instance '%s' already exists", newName)
	}

	wasActive := m.GetActiveInstance() == oldName
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renaming the instance: %w", err)
	}

	if wasActive {
		if err := os.Remove(m.MinecraftPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("re-linking %s: %w", m.MinecraftPath, err)
		}
		if err := os.Symlink(newPath, m.MinecraftPath); err != nil {
			return fmt.Errorf("re-linking %s: %w", m.MinecraftPath, err)
		}
	}

	// The metadata carries the name too; LoadMeta treats the directory as
	// authoritative, but a file that agrees with it is less surprising.
	if meta, found, err := LoadMeta(newPath); err == nil && found {
		meta.Name = newName
		_ = SaveMeta(newPath, meta)
	}
	return nil
}
