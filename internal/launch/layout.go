// Package launch prepares and starts Minecraft.
package launch

import (
	"path/filepath"
)

// Layout is the shared store: everything downloaded once and used by every
// instance. Keeping it out of the instances is what stops each one carrying
// its own copy of the assets, libraries and runtimes — the reason six
// instances on this machine occupied 50 GB.
//
// The instance directory keeps only user data (mods, config, saves, ...), and
// remains reachable through the ~/.minecraft symlink exactly as before,
// because every shared path below is passed explicitly on the command line.
type Layout struct {
	Root string
}

// NewLayout returns the shared store rooted under an application directory.
func NewLayout(appDir string) *Layout {
	return &Layout{Root: filepath.Join(appDir, "shared")}
}

// Versions holds one directory per version id, mirroring the official
// launcher's layout so harvested files drop straight in.
func (l *Layout) Versions() string { return filepath.Join(l.Root, "versions") }

// Libraries is a plain maven repository tree.
func (l *Layout) Libraries() string { return filepath.Join(l.Root, "libraries") }

// Assets holds indexes/ and the content-addressed objects/.
func (l *Layout) Assets() string { return filepath.Join(l.Root, "assets") }

// Natives holds the extracted native libraries for one version.
func (l *Layout) Natives(versionID string) string {
	return filepath.Join(l.Root, "natives", versionID)
}

// Runtimes holds managed and harvested Java runtimes, by component name.
func (l *Layout) Runtimes() string { return filepath.Join(l.Root, "runtimes") }

// Loaders caches mod loader installers and their install profiles.
func (l *Layout) Loaders() string { return filepath.Join(l.Root, "loaders") }

// Cache holds metadata that belongs to no single version, such as the version
// manifest.
func (l *Layout) Cache() string { return filepath.Join(l.Root, "cache") }

// LogConfigs holds the log4j configuration files versions reference.
func (l *Layout) LogConfigs() string { return filepath.Join(l.Assets(), "log_configs") }

// VirtualAssets is where a legacy "virtual" asset index is materialised.
func (l *Layout) VirtualAssets() string {
	return filepath.Join(l.Assets(), "virtual", "legacy")
}

// VersionJSON is a version's manifest path.
func (l *Layout) VersionJSON(id string) string {
	return filepath.Join(l.Versions(), id, id+".json")
}

// VersionJar is a version's client jar path.
func (l *Layout) VersionJar(id string) string {
	return filepath.Join(l.Versions(), id, id+".jar")
}

// LibraryPath resolves a library's repository-relative path against the store.
func (l *Layout) LibraryPath(rel string) string {
	return filepath.Join(l.Libraries(), filepath.FromSlash(rel))
}
