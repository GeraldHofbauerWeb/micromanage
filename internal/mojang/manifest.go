package mojang

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/download"
)

// VersionManifestURL lists every published Minecraft version.
const VersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

// Manifest is the published version list.
type Manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []ManifestVersion `json:"versions"`
}

// ManifestVersion is one entry of the version list.
type ManifestVersion struct {
	ID              string `json:"id"`
	Type            string `json:"type"` // release, snapshot, old_beta, old_alpha
	URL             string `json:"url"`
	Time            string `json:"time"`
	ReleaseTime     string `json:"releaseTime"`
	SHA1            string `json:"sha1"`
	ComplianceLevel int    `json:"complianceLevel"`
}

// Find returns the manifest entry for a version id.
func (m *Manifest) Find(id string) (ManifestVersion, bool) {
	for _, v := range m.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return ManifestVersion{}, false
}

// OfType returns the versions of one release type, newest first. The manifest
// already arrives in that order, so this only filters.
func (m *Manifest) OfType(kind string) []ManifestVersion {
	var out []ManifestVersion
	for _, v := range m.Versions {
		if v.Type == kind {
			out = append(out, v)
		}
	}
	return out
}

// Client fetches and caches Minecraft metadata.
type Client struct {
	Downloader *download.Downloader

	// VersionsDir holds one directory per version id, mirroring the layout the
	// official launcher uses so harvested files drop straight in.
	VersionsDir string
	// CacheDir holds the version manifest and other non-version metadata.
	CacheDir string
}

// NewClient returns a client storing metadata under the given directories.
func NewClient(d *download.Downloader, versionsDir, cacheDir string) *Client {
	if d == nil {
		d = download.New()
	}
	return &Client{Downloader: d, VersionsDir: versionsDir, CacheDir: cacheDir}
}

// manifestPath is where the version list is cached.
func (c *Client) manifestPath() string {
	return filepath.Join(c.CacheDir, "version_manifest_v2.json")
}

// VersionPath is the on-disk location of a version manifest.
func (c *Client) VersionPath(id string) string {
	return filepath.Join(c.VersionsDir, id, id+".json")
}

// Manifest fetches the version list, falling back to the cached copy when the
// network is unavailable so an offline launch of an installed version works.
func (c *Client) Manifest(ctx context.Context) (*Manifest, error) {
	data, err := c.Downloader.GetJSON(ctx, VersionManifestURL, "")
	if err != nil {
		cached, readErr := os.ReadFile(c.manifestPath())
		if readErr != nil {
			return nil, fmt.Errorf("fetching the version manifest: %w", err)
		}
		data = cached
	} else if writeErr := writeFileAtomic(c.manifestPath(), data); writeErr != nil {
		// A cache we cannot write is not worth failing the launch over.
		_ = writeErr
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing the version manifest: %w", err)
	}
	if len(m.Versions) == 0 {
		return nil, fmt.Errorf("the version manifest is empty")
	}
	return &m, nil
}

// Version returns a version manifest, reading the local copy when present and
// otherwise downloading it via the version list.
//
// Loader profiles only ever exist locally — they are written by the loader's
// installer or harvested from an existing instance — so a local hit must be
// tried before the manifest is consulted.
func (c *Client) Version(ctx context.Context, id string) (*Version, error) {
	if data, err := os.ReadFile(c.VersionPath(id)); err == nil {
		v, err := ParseVersion(data)
		if err == nil {
			return v, nil
		}
		// A corrupt local file should not be fatal for a vanilla version we
		// can re-fetch; fall through.
	}

	manifest, err := c.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := manifest.Find(id)
	if !ok {
		return nil, fmt.Errorf("unknown version %q (and no local manifest for it)", id)
	}

	data, err := c.Downloader.GetJSON(ctx, entry.URL, entry.SHA1)
	if err != nil {
		return nil, fmt.Errorf("fetching version %s: %w", id, err)
	}
	if err := writeFileAtomic(c.VersionPath(id), data); err != nil {
		return nil, fmt.Errorf("caching version %s: %w", id, err)
	}
	return ParseVersion(data)
}

// Resolve returns a version with its inheritsFrom chain merged in.
func (c *Client) Resolve(ctx context.Context, id string) (*Version, error) {
	return Resolve(ctx, id, c.Version)
}

// LocalVersions lists the version ids present on disk.
func (c *Client) LocalVersions() []string {
	entries, err := os.ReadDir(c.VersionsDir)
	if err != nil {
		return nil
	}

	var ids []string
	for _, e := range entries {
		// The launcher drops loose files such as jre_manifest.json into
		// versions/, so only directories with a matching manifest count.
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(c.VersionPath(e.Name())); err != nil {
			continue
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids
}

// writeFileAtomic writes data via a temporary file and a rename.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
