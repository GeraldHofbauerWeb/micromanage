// Package mods reads what a mod says about itself, and finds its page.
//
// Every loader makes a mod carry a manifest inside its jar: NeoForge and
// Forge a TOML file under META-INF, Fabric and Quilt a JSON file at the root.
// The display name in there is what a player knows the mod by — "Create",
// not "create-1.21.1-6.0.6.jar".
package mods

import (
	"archive/zip"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// Info is what a mod's own manifest says.
type Info struct {
	ID      string
	Name    string
	Version string
	// Loader is the family the manifest belongs to, as its file name says.
	Loader string
}

// Read opens a mod jar and returns its manifest. A jar with no manifest —
// a library, a jar-in-jar container, a plain resource — returns an empty
// Info and no error; only an unreadable file is an error.
func Read(jarPath string) (Info, error) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return Info{}, err
	}
	defer r.Close()

	// Order matters where a jar carries several: the loader-specific
	// manifest names the mod, the generic one may name a wrapper.
	candidates := []struct {
		name  string
		parse func([]byte) Info
	}{
		{"META-INF/neoforge.mods.toml", parseModsToml},
		{"META-INF/mods.toml", parseModsToml},
		{"fabric.mod.json", parseFabric},
		{"quilt.mod.json", parseQuilt},
	}
	byName := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		byName[f.Name] = f
	}
	for _, c := range candidates {
		f, ok := byName[c.name]
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			continue
		}
		info := c.parse(data)
		if info.ID != "" || info.Name != "" {
			info.Loader = loaderOf(c.name)
			return info, nil
		}
	}
	return Info{}, nil
}

func loaderOf(manifest string) string {
	switch manifest {
	case "META-INF/neoforge.mods.toml":
		return "neoforge"
	case "META-INF/mods.toml":
		return "forge"
	case "fabric.mod.json":
		return "fabric"
	case "quilt.mod.json":
		return "quilt"
	}
	return ""
}

var (
	modsBlock = regexp.MustCompile(`(?s)\[\[mods\]\](.*?)(\[\[|\z)`)
	tomlKey   = regexp.MustCompile(`(?m)^\s*(modId|displayName|version)\s*=\s*"([^"]*)"`)
)

// parseModsToml reads the first [[mods]] table of a Forge-style manifest.
// A full TOML parser is not needed for three quoted strings, and pulling
// one in for this would be the largest dependency in the launcher.
func parseModsToml(data []byte) Info {
	m := modsBlock.FindSubmatch(data)
	if m == nil {
		return Info{}
	}
	var info Info
	for _, kv := range tomlKey.FindAllSubmatch(m[1], -1) {
		switch string(kv[1]) {
		case "modId":
			if info.ID == "" {
				info.ID = string(kv[2])
			}
		case "displayName":
			if info.Name == "" {
				info.Name = string(kv[2])
			}
		case "version":
			if info.Version == "" {
				info.Version = string(kv[2])
			}
		}
	}
	// "${file.jarVersion}" is a build-time placeholder some jars ship
	// unexpanded; it is not a version.
	if strings.HasPrefix(info.Version, "${") {
		info.Version = ""
	}
	return info
}

func parseFabric(data []byte) Info {
	var m struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &m) != nil {
		return Info{}
	}
	return Info{ID: m.ID, Name: m.Name, Version: m.Version}
}

func parseQuilt(data []byte) Info {
	var m struct {
		Loader struct {
			ID       string `json:"id"`
			Version  string `json:"version"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"quilt_loader"`
	}
	if json.Unmarshal(data, &m) != nil {
		return Info{}
	}
	return Info{ID: m.Loader.ID, Name: m.Loader.Metadata.Name, Version: m.Loader.Version}
}
