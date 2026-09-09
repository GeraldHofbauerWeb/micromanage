package mojang

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GeraldHofbauerWeb/micromanage/internal/download"
)

// ResourcesURL is the CDN serving asset objects by digest.
const ResourcesURL = "https://resources.download.minecraft.net/"

// AssetIndexFile is the object list a version's asset index points at.
type AssetIndexFile struct {
	Objects map[string]AssetObject `json:"objects"`

	// Virtual marks pre-1.7.3 indexes whose objects must be materialised into
	// a directory tree under their logical names.
	Virtual bool `json:"virtual,omitempty"`
	// MapToResources marks pre-1.6 indexes, where that tree lives in the game
	// directory as resources/ instead.
	MapToResources bool `json:"map_to_resources,omitempty"`
}

// AssetObject is one content-addressed asset.
type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// RelPath is an object's location under assets/objects: the first two hex
// characters of its digest, then the digest itself.
func (o AssetObject) RelPath() string {
	if len(o.Hash) < 2 {
		return o.Hash
	}
	return filepath.Join(o.Hash[:2], o.Hash)
}

// URL is where the object is served from.
func (o AssetObject) URL() string {
	if len(o.Hash) < 2 {
		return ResourcesURL + o.Hash
	}
	return ResourcesURL + o.Hash[:2] + "/" + o.Hash
}

// AssetIndexPath is where a version's asset index is cached.
func AssetIndexPath(assetsDir, indexID string) string {
	return filepath.Join(assetsDir, "indexes", indexID+".json")
}

// FetchAssetIndex downloads and caches a version's asset index.
func (c *Client) FetchAssetIndex(ctx context.Context, assetsDir string, idx *AssetIndex) (*AssetIndexFile, error) {
	if idx == nil {
		return nil, fmt.Errorf("version has no asset index")
	}

	path := AssetIndexPath(assetsDir, idx.ID)
	data, err := os.ReadFile(path)
	if err != nil || (idx.SHA1 != "" && download.FileSHA1(path) != idx.SHA1) {
		data, err = c.Downloader.GetJSON(ctx, idx.URL, idx.SHA1)
		if err != nil {
			return nil, fmt.Errorf("fetching asset index %s: %w", idx.ID, err)
		}
		if err := writeFileAtomic(path, data); err != nil {
			return nil, fmt.Errorf("caching asset index %s: %w", idx.ID, err)
		}
	}

	var index AssetIndexFile
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parsing asset index %s: %w", idx.ID, err)
	}
	return &index, nil
}

// DownloadItems returns the download list for every object in an index.
//
// A modern index holds several thousand objects totalling most of a gigabyte,
// which is exactly why the store is shared between instances rather than
// duplicated into each one.
func (f *AssetIndexFile) DownloadItems(assetsDir string) []download.Item {
	objectsDir := filepath.Join(assetsDir, "objects")

	// Objects are content-addressed, so the same file appears under several
	// logical names; fetch each digest once.
	seen := make(map[string]bool, len(f.Objects))
	items := make([]download.Item, 0, len(f.Objects))

	for _, obj := range f.Objects {
		if obj.Hash == "" || seen[obj.Hash] {
			continue
		}
		seen[obj.Hash] = true
		items = append(items, download.Item{
			URL:  obj.URL(),
			Path: filepath.Join(objectsDir, obj.RelPath()),
			SHA1: obj.Hash,
			Size: obj.Size,
		})
	}
	return items
}

// MaterialiseVirtual writes the legacy directory tree that pre-1.7.3 versions
// expect, linking or copying each object to its logical name.
func (f *AssetIndexFile) MaterialiseVirtual(assetsDir, targetDir string) error {
	objectsDir := filepath.Join(assetsDir, "objects")

	for name, obj := range f.Objects {
		dst := filepath.Join(targetDir, filepath.FromSlash(name))
		if info, err := os.Stat(dst); err == nil && info.Size() == obj.Size {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		src := filepath.Join(objectsDir, obj.RelPath())
		// A hard link costs nothing; copying is the fallback when the store
		// and the target sit on different filesystems.
		os.Remove(dst)
		if err := os.Link(src, dst); err == nil {
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("materialising asset %s: %w", name, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// Asset objects are small — sounds, language files, icons — so reading
	// one whole is fine here, unlike the instance-copy path.
	return os.WriteFile(dst, data, 0o644)
}
