package mojang

import (
	"context"
	"fmt"
)

// MaxInheritDepth bounds an inheritsFrom chain. Real chains are one or two
// links; anything longer is a malformed or hostile manifest.
const MaxInheritDepth = 8

// Fetcher loads a version manifest by id.
type Fetcher func(ctx context.Context, id string) (*Version, error)

// Resolve follows a version's inheritsFrom chain and merges it into a single
// self-contained manifest.
//
// Modded profiles are expressed as a thin manifest inheriting from the vanilla
// version: neoforge-21.1.248 inherits from 1.21.1 and supplies only its own
// mainClass, libraries and arguments.
func Resolve(ctx context.Context, id string, fetch Fetcher) (*Version, error) {
	seen := map[string]bool{}

	var walk func(id string, depth int) (*Version, error)
	walk = func(id string, depth int) (*Version, error) {
		if depth > MaxInheritDepth {
			return nil, fmt.Errorf("inheritsFrom chain deeper than %d starting at %q", MaxInheritDepth, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("inheritsFrom cycle at %q", id)
		}
		seen[id] = true

		v, err := fetch(ctx, id)
		if err != nil {
			return nil, err
		}
		if v.InheritsFrom == "" {
			return v, nil
		}

		parent, err := walk(v.InheritsFrom, depth+1)
		if err != nil {
			return nil, fmt.Errorf("resolving parent of %q: %w", id, err)
		}
		return Merge(v, parent), nil
	}

	return walk(id, 0)
}

// Merge combines a child manifest with its parent, child taking precedence.
//
// The library ordering is the part that matters most: child libraries come
// first and duplicates are dropped keeping the first occurrence, so a loader
// that ships its own ASM or Guava wins over the vanilla one. Getting this
// backwards produces NoSuchMethodError at runtime rather than a clear failure.
func Merge(child, parent *Version) *Version {
	out := *child

	// Identity stays the child's; the resolved manifest no longer inherits.
	out.ID = child.ID
	out.InheritsFrom = ""

	if out.Type == "" {
		out.Type = parent.Type
	}
	if out.MainClass == "" {
		out.MainClass = parent.MainClass
	}
	if out.Assets == "" {
		out.Assets = parent.Assets
	}
	if out.AssetIndex == nil {
		out.AssetIndex = parent.AssetIndex
	}
	if out.JavaVersion == nil {
		out.JavaVersion = parent.JavaVersion
	}
	if out.Logging == nil {
		out.Logging = parent.Logging
	}
	if out.MinimumLauncherVersion == 0 {
		out.MinimumLauncherVersion = parent.MinimumLauncherVersion
	}
	if out.ComplianceLevel == 0 {
		out.ComplianceLevel = parent.ComplianceLevel
	}
	if out.ReleaseTime == "" {
		out.ReleaseTime = parent.ReleaseTime
	}
	if out.Time == "" {
		out.Time = parent.Time
	}

	// The client jar comes from the parent unless the child overrides it.
	out.Downloads = mergeDownloads(child.Downloads, parent.Downloads)

	out.Libraries = mergeLibraries(child.Libraries, parent.Libraries)

	// A legacy argument string is replaced wholesale; the two forms describe
	// the same command line and interleaving them would produce nonsense.
	if child.MinecraftArguments == "" {
		out.MinecraftArguments = parent.MinecraftArguments
	}

	out.Arguments = mergeArguments(child.Arguments, parent.Arguments)

	return &out
}

func mergeDownloads(child, parent map[string]Download) map[string]Download {
	if child == nil && parent == nil {
		return nil
	}
	out := make(map[string]Download, len(child)+len(parent))
	for k, v := range parent {
		out[k] = v
	}
	for k, v := range child {
		out[k] = v
	}
	return out
}

// mergeLibraries concatenates child libraries ahead of the parent's and drops
// later duplicates of the same group:artifact.
func mergeLibraries(child, parent []Library) []Library {
	if len(child) == 0 {
		return parent
	}
	if len(parent) == 0 {
		return child
	}

	out := make([]Library, 0, len(child)+len(parent))
	seen := make(map[string]bool, len(child)+len(parent))

	for _, lib := range append(append([]Library{}, child...), parent...) {
		key := MavenKey(lib.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, lib)
	}
	return out
}

// mergeArguments appends the child's arguments after the parent's, so loader
// flags land after the vanilla ones they extend.
func mergeArguments(child, parent *Arguments) *Arguments {
	if child == nil {
		return parent
	}
	if parent == nil {
		return child
	}
	return &Arguments{
		Game: append(append([]Argument{}, parent.Game...), child.Game...),
		JVM:  append(append([]Argument{}, parent.JVM...), child.JVM...),
	}
}
