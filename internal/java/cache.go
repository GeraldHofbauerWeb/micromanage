package java

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// cacheSchema is bumped whenever the shape of a cache entry changes, so an
// older file is simply ignored rather than misread.
const cacheSchema = 1

// CacheFileName is the file probe results persist in, under the cache
// directory a Detector is given.
const CacheFileName = "java-runtimes.json"

// cacheEntry is one remembered probe. The identity fields say which file the
// result belongs to: a runtime that has been replaced, upgraded or repaired
// has a different size, modification time or mode and is probed again.
type cacheEntry struct {
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
	Mode    uint32 `json:"mode"`

	Major       int    `json:"major"`
	FullVersion string `json:"version,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Broken      bool   `json:"broken,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type cacheFile struct {
	Schema   int                   `json:"schema"`
	Runtimes map[string]cacheEntry `json:"runtimes"`
}

// probeCache remembers what `java -version` said about each executable.
//
// Every probe forks a JVM, which costs tens of milliseconds even for the
// trivial version query; a machine with a handful of instances, each carrying
// the runtimes the official launcher bundled, ends up with fifteen of them.
// Probing all of those on every start and again on every launch was most of
// the time the launcher spent before showing anything. The cache turns that
// into fifteen stat calls.
type probeCache struct {
	path string

	mu      sync.Mutex
	entries map[string]cacheEntry
	dirty   bool
}

// loadProbeCache reads the cache, treating anything unreadable as empty: a
// cache is only ever an optimisation.
func loadProbeCache(path string) *probeCache {
	c := &probeCache{path: path, entries: map[string]cacheEntry{}}
	if path == "" {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var f cacheFile
	if json.Unmarshal(data, &f) != nil || f.Schema != cacheSchema || f.Runtimes == nil {
		return c
	}
	c.entries = f.Runtimes
	return c
}

// identity is what a cache entry is compared against.
type identity struct {
	Size    int64
	ModTime int64
	Mode    uint32
}

func identityOf(info os.FileInfo) identity {
	return identity{
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
		Mode:    uint32(info.Mode()),
	}
}

// lookup returns the remembered probe for a file, if it is still the same file.
func (c *probeCache) lookup(key string, id identity) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.Size != id.Size || e.ModTime != id.ModTime || e.Mode != id.Mode {
		return cacheEntry{}, false
	}
	return e, true
}

// remember records a probe result.
func (c *probeCache) remember(key string, id identity, rt Runtime) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		Size: id.Size, ModTime: id.ModTime, Mode: id.Mode,
		Major: rt.Major, FullVersion: rt.FullVersion, Vendor: rt.Vendor,
		Broken: rt.Broken, Reason: rt.Reason,
	}
	c.dirty = true
}

// save writes the cache when something changed, keeping only the entries
// that were seen this time so runtimes that have disappeared do not
// accumulate. Failure is silent by design.
func (c *probeCache) save(seen map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		return
	}
	pruned := false
	for key := range c.entries {
		if !seen[key] {
			delete(c.entries, key)
			pruned = true
		}
	}
	if !c.dirty && !pruned {
		return
	}

	data, err := json.MarshalIndent(cacheFile{Schema: cacheSchema, Runtimes: c.entries}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, c.path); err != nil {
		os.Remove(tmp)
		return
	}
	c.dirty = false
}
