package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// MetaFileName is the per-instance metadata file, stored inside the
	// instance directory so it travels with a copied or shared instance.
	MetaFileName = "instance.json"

	// MetaSchemaVersion is the schema this build writes and understands.
	MetaSchemaVersion = 1
)

// LoaderType identifies the mod loader an instance runs.
type LoaderType string

const (
	LoaderVanilla  LoaderType = "vanilla"
	LoaderFabric   LoaderType = "fabric"
	LoaderQuilt    LoaderType = "quilt"
	LoaderNeoForge LoaderType = "neoforge"
	LoaderForge    LoaderType = "forge"
)

// LoaderTypes returns the supported loaders in presentation order.
func LoaderTypes() []LoaderType {
	return []LoaderType{LoaderVanilla, LoaderNeoForge, LoaderForge, LoaderFabric, LoaderQuilt}
}

// Valid reports whether t is a loader this build knows.
func (t LoaderType) Valid() bool {
	for _, k := range LoaderTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// Display returns a human-readable loader name.
func (t LoaderType) Display() string {
	switch t {
	case LoaderVanilla:
		return "Vanilla"
	case LoaderFabric:
		return "Fabric"
	case LoaderQuilt:
		return "Quilt"
	case LoaderNeoForge:
		return "NeoForge"
	case LoaderForge:
		return "Forge"
	}
	return string(t)
}

// LoaderSpec is an instance's mod loader and, where applicable, its version.
type LoaderSpec struct {
	Type    LoaderType `json:"type"`
	Version string     `json:"version,omitempty"`
}

// String renders the loader for display, e.g. "NeoForge 21.1.248".
func (l LoaderSpec) String() string {
	if l.Type == "" {
		return LoaderVanilla.Display()
	}
	if l.Version == "" {
		return l.Type.Display()
	}
	return l.Type.Display() + " " + l.Version
}

// JavaSpec pins the Java runtime for an instance. An empty Path means the
// launcher picks a runtime matching the version's required major release.
type JavaSpec struct {
	Path      string `json:"path,omitempty"`
	Component string `json:"component,omitempty"`
}

// MemSpec is the JVM heap sizing in megabytes. Zero means use the default.
type MemSpec struct {
	MinMB int `json:"min_mb,omitempty"`
	MaxMB int `json:"max_mb,omitempty"`
}

// Default heap sizes applied when MemSpec leaves them unset.
const (
	DefaultMinMB = 1024
	DefaultMaxMB = 4096
)

// Resolved returns the heap sizes with defaults filled in.
func (m MemSpec) Resolved() (minMB, maxMB int) {
	minMB, maxMB = m.MinMB, m.MaxMB
	if maxMB <= 0 {
		maxMB = DefaultMaxMB
	}
	if minMB <= 0 {
		minMB = DefaultMinMB
	}
	// A min above max would make the JVM refuse to start.
	if minMB > maxMB {
		minMB = maxMB
	}
	return minMB, maxMB
}

// Meta is the per-instance configuration introduced in v2. Instances created
// before it exists have no file; see LoadMeta.
type Meta struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`

	// MinecraftVersion is empty on an instance that has not been configured.
	MinecraftVersion string     `json:"minecraft_version"`
	Loader           LoaderSpec `json:"loader"`
	Java             JavaSpec   `json:"java"`
	Memory           MemSpec    `json:"memory"`
	JVMArgs          []string   `json:"jvm_args,omitempty"`
	GameArgs         []string   `json:"game_args,omitempty"`

	// ResolvedVersionID is the version id actually launched, which for a
	// modded instance is the loader profile rather than MinecraftVersion.
	ResolvedVersionID string `json:"resolved_version_id,omitempty"`

	LastPlayed       time.Time `json:"last_played,omitzero"`
	TotalPlaySeconds int64     `json:"total_play_seconds,omitempty"`
	// PlaySessions counts the runs behind that total. It has only been
	// kept since the session log existed, so it can be short of the truth
	// on an instance played before that.
	PlaySessions int64     `json:"play_sessions,omitempty"`
	Created      time.Time `json:"created,omitzero"`
	Notes        string    `json:"notes,omitempty"`
}

// DefaultMeta returns the metadata an unconfigured instance is treated as
// having. It is never written to disk on its own.
func DefaultMeta(name string) Meta {
	return Meta{
		SchemaVersion: MetaSchemaVersion,
		Name:          name,
		Loader:        LoaderSpec{Type: LoaderVanilla},
	}
}

// Configured reports whether the instance knows which Minecraft version to run.
func (m Meta) Configured() bool {
	return m.MinecraftVersion != ""
}

// LoadMeta reads an instance's metadata.
//
// A missing file is not an error: instances predating v2 have none, and
// reading must never write. The returned bool reports whether a file was
// actually found.
func LoadMeta(instanceDir string) (Meta, bool, error) {
	name := filepath.Base(instanceDir)

	data, err := os.ReadFile(filepath.Join(instanceDir, MetaFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultMeta(name), false, nil
		}
		return DefaultMeta(name), false, err
	}

	meta := DefaultMeta(name)
	if err := json.Unmarshal(data, &meta); err != nil {
		return DefaultMeta(name), false, fmt.Errorf("parse %s: %w", MetaFileName, err)
	}

	// The directory name is authoritative; a stale Name in the file would
	// otherwise survive a rename.
	meta.Name = name
	if meta.Loader.Type == "" {
		meta.Loader.Type = LoaderVanilla
	}
	return meta, true, nil
}

// SaveMeta writes an instance's metadata atomically.
//
// It refuses to write over a file produced by a newer schema, so an older
// build cannot silently truncate settings it does not understand.
func SaveMeta(instanceDir string, meta Meta) error {
	if existing, found, err := LoadMeta(instanceDir); err == nil && found {
		if existing.SchemaVersion > MetaSchemaVersion {
			return fmt.Errorf(
				"%s was written by a newer version (schema %d, this build understands %d); refusing to overwrite it",
				MetaFileName, existing.SchemaVersion, MetaSchemaVersion)
		}
	}

	meta.SchemaVersion = MetaSchemaVersion
	if meta.Name == "" {
		meta.Name = filepath.Base(instanceDir)
	}
	if meta.Loader.Type == "" {
		meta.Loader.Type = LoaderVanilla
	}
	if meta.Created.IsZero() {
		meta.Created = time.Now().UTC()
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", MetaFileName, err)
	}
	data = append(data, '\n')

	final := filepath.Join(instanceDir, MetaFileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", MetaFileName, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", MetaFileName, err)
	}
	return nil
}

// GetMeta reads the metadata of a named instance.
func (m *Manager) GetMeta(name string) (Meta, error) {
	dir, err := m.InstancePath(name)
	if err != nil {
		return Meta{}, err
	}
	meta, _, err := LoadMeta(dir)
	return meta, err
}

// SetMeta writes the metadata of a named instance.
func (m *Manager) SetMeta(name string, meta Meta) error {
	dir, err := m.InstancePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("instance '%s' does not exist", name)
	}
	return SaveMeta(dir, meta)
}
