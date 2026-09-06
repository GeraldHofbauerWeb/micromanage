package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Confidence expresses how much to trust a Detection.
type Confidence int

const (
	// ConfidenceNone means nothing usable was found.
	ConfidenceNone Confidence = iota
	// ConfidenceLow comes from a guess with no corroboration.
	ConfidenceLow
	// ConfidenceMedium comes from scanning versions/ without a profile file,
	// or from sources that disagree.
	ConfidenceMedium
	// ConfidenceHigh comes from the launcher's own profile list.
	ConfidenceHigh
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceHigh:
		return "high"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceLow:
		return "low"
	}
	return "none"
}

// Detection is the result of inspecting an existing instance directory.
type Detection struct {
	Meta       Meta
	Confidence Confidence
	Source     string
	// Candidates holds the other plausible profiles, most recent first, so a
	// UI can offer them instead of the winner.
	Candidates []Meta
}

// launcherProfiles mirrors the parts of launcher_profiles.json we read.
type launcherProfiles struct {
	Profiles map[string]launcherProfile `json:"profiles"`
}

type launcherProfile struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	LastVersionID string `json:"lastVersionId"`
	LastUsed      string `json:"lastUsed"`
	JavaArgs      string `json:"javaArgs"`
	JavaDir       string `json:"javaDir"`
}

// versionJSON mirrors the parts of a version manifest we need for detection.
type versionJSON struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	MainClass    string `json:"mainClass"`
	Type         string `json:"type"`
}

// DetectMeta infers an instance's Minecraft version, loader and JVM settings
// from what the official launcher left in the directory.
//
// It is read-only: nothing is written, so callers can show the result for
// confirmation before committing it.
func DetectMeta(instanceDir string) (Detection, error) {
	name := filepath.Base(instanceDir)

	if metas, ok := detectFromProfiles(instanceDir, name); ok && len(metas) > 0 {
		return Detection{
			Meta:       metas[0],
			Confidence: ConfidenceHigh,
			Source:     "launcher_profiles.json",
			Candidates: metas[1:],
		}, nil
	}

	if metas := detectFromVersionsDir(instanceDir, name); len(metas) > 0 {
		return Detection{
			Meta:       metas[0],
			Confidence: ConfidenceMedium,
			Source:     "versions/ scan",
			Candidates: metas[1:],
		}, nil
	}

	return Detection{
		Meta:       DefaultMeta(name),
		Confidence: ConfidenceNone,
		Source:     "none",
	}, nil
}

// detectFromProfiles reads launcher_profiles.json and returns one Meta per
// real profile, most recently used first.
func detectFromProfiles(instanceDir, name string) ([]Meta, bool) {
	data, err := os.ReadFile(filepath.Join(instanceDir, "launcher_profiles.json"))
	if err != nil {
		return nil, false
	}

	var lp launcherProfiles
	if err := json.Unmarshal(data, &lp); err != nil {
		return nil, false
	}

	type scored struct {
		profile  launcherProfile
		lastUsed time.Time
	}
	var real []scored

	for _, p := range lp.Profiles {
		if p.LastVersionID == "" {
			continue
		}
		// The launcher keeps two synthetic entries that track whatever the
		// newest release and snapshot happen to be. They carry an epoch
		// timestamp, which is the reliable way to tell them apart from a
		// profile the user actually played.
		if p.Type == "latest-release" || p.Type == "latest-snapshot" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, p.LastUsed)
		if err != nil || ts.Year() <= 1970 {
			// Keep it, but sort it last.
			ts = time.Time{}
		}
		real = append(real, scored{profile: p, lastUsed: ts})
	}
	if len(real) == 0 {
		return nil, false
	}

	sort.SliceStable(real, func(i, j int) bool {
		return real[i].lastUsed.After(real[j].lastUsed)
	})

	metas := make([]Meta, 0, len(real))
	for _, s := range real {
		metas = append(metas, metaFromProfile(instanceDir, name, s.profile))
	}
	return metas, true
}

// metaFromProfile turns one launcher profile into instance metadata.
func metaFromProfile(instanceDir, name string, p launcherProfile) Meta {
	meta := DefaultMeta(name)

	// The profile's display name is user-editable and routinely wrong — one
	// real instance has a profile called "1.21.1" pointing at 1.20.1. Only
	// lastVersionId is trustworthy.
	meta.ResolvedVersionID = p.LastVersionID
	meta.Loader = ParseLoaderID(p.LastVersionID)
	meta.MinecraftVersion = minecraftVersionFor(instanceDir, p.LastVersionID, meta.Loader)

	// Cross-check the loader against the profile's mainClass; disagreement is
	// surfaced by lowering confidence at the call site.
	if v, err := readVersionJSON(instanceDir, p.LastVersionID); err == nil {
		if fromMain := loaderFromMainClass(v.MainClass); fromMain != "" && fromMain != meta.Loader.Type {
			// Trust the id for the version string, the mainClass for the family.
			meta.Loader.Type = fromMain
		}
	}

	meta.Memory, meta.JVMArgs = ParseJavaArgs(p.JavaArgs)
	if p.JavaDir != "" {
		meta.Java.Path = p.JavaDir
	}
	return meta
}

// detectFromVersionsDir falls back to whatever version manifests are present.
func detectFromVersionsDir(instanceDir, name string) []Meta {
	ids := listVersionIDs(instanceDir)
	if len(ids) == 0 {
		return nil
	}

	// Prefer a loader profile over plain vanilla, then the most recently
	// modified manifest.
	type scored struct {
		id     string
		loader LoaderSpec
		mtime  time.Time
	}
	var all []scored
	for _, id := range ids {
		s := scored{id: id, loader: ParseLoaderID(id)}
		if info, err := os.Stat(versionJSONPath(instanceDir, id)); err == nil {
			s.mtime = info.ModTime()
		}
		all = append(all, s)
	}
	sort.SliceStable(all, func(i, j int) bool {
		iMod := all[i].loader.Type != LoaderVanilla
		jMod := all[j].loader.Type != LoaderVanilla
		if iMod != jMod {
			return iMod
		}
		return all[i].mtime.After(all[j].mtime)
	})

	metas := make([]Meta, 0, len(all))
	for _, s := range all {
		meta := DefaultMeta(name)
		meta.ResolvedVersionID = s.id
		meta.Loader = s.loader
		meta.MinecraftVersion = minecraftVersionFor(instanceDir, s.id, s.loader)
		metas = append(metas, meta)
	}
	return metas
}

// listVersionIDs returns the version ids that have a manifest on disk.
func listVersionIDs(instanceDir string) []string {
	entries, err := os.ReadDir(filepath.Join(instanceDir, "versions"))
	if err != nil {
		return nil
	}

	var ids []string
	for _, e := range entries {
		// versions/ is not purely directories: the launcher drops
		// jre_manifest.json and version_manifest_v2.json in there too.
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if _, err := os.Stat(versionJSONPath(instanceDir, id)); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func versionJSONPath(instanceDir, id string) string {
	return filepath.Join(instanceDir, "versions", id, id+".json")
}

func readVersionJSON(instanceDir, id string) (versionJSON, error) {
	var v versionJSON
	data, err := os.ReadFile(versionJSONPath(instanceDir, id))
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(data, &v)
	return v, err
}

// minecraftVersionFor resolves the base game version behind a version id. For
// a loader profile that is its inheritsFrom; for vanilla the id itself.
func minecraftVersionFor(instanceDir, id string, loader LoaderSpec) string {
	if v, err := readVersionJSON(instanceDir, id); err == nil && v.InheritsFrom != "" {
		return v.InheritsFrom
	}
	// Some loader ids embed the game version, which is enough when the
	// manifest is missing.
	if mc := mcVersionFromLoaderID(id, loader.Type); mc != "" {
		return mc
	}
	if loader.Type == LoaderVanilla {
		return id
	}
	return ""
}

// ParseLoaderID derives the loader and its version from a version id such as
// "neoforge-21.1.248", "1.20.1-forge-47.4.0" or "fabric-loader-0.15.7-1.20.1".
func ParseLoaderID(id string) LoaderSpec {
	switch {
	case strings.HasPrefix(id, "neoforge-"):
		return LoaderSpec{Type: LoaderNeoForge, Version: strings.TrimPrefix(id, "neoforge-")}

	case strings.HasPrefix(id, "fabric-loader-"):
		// fabric-loader-<loader>-<mc>
		rest := strings.TrimPrefix(id, "fabric-loader-")
		if loader, _, ok := splitLoaderAndMC(rest); ok {
			return LoaderSpec{Type: LoaderFabric, Version: loader}
		}
		return LoaderSpec{Type: LoaderFabric}

	case strings.HasPrefix(id, "quilt-loader-"):
		rest := strings.TrimPrefix(id, "quilt-loader-")
		if loader, _, ok := splitLoaderAndMC(rest); ok {
			return LoaderSpec{Type: LoaderQuilt, Version: loader}
		}
		return LoaderSpec{Type: LoaderQuilt}

	case strings.Contains(id, "-forge-"):
		// <mc>-forge-<forge version>
		if i := strings.Index(id, "-forge-"); i >= 0 {
			return LoaderSpec{Type: LoaderForge, Version: id[i+len("-forge-"):]}
		}
	}
	return LoaderSpec{Type: LoaderVanilla}
}

// mcVersionFromLoaderID extracts the game version embedded in a loader id,
// where the naming scheme carries one.
func mcVersionFromLoaderID(id string, loader LoaderType) string {
	switch loader {
	case LoaderForge:
		if i := strings.Index(id, "-forge-"); i > 0 {
			return id[:i]
		}
	case LoaderFabric, LoaderQuilt:
		prefix := "fabric-loader-"
		if loader == LoaderQuilt {
			prefix = "quilt-loader-"
		}
		if _, mc, ok := splitLoaderAndMC(strings.TrimPrefix(id, prefix)); ok {
			return mc
		}
	}
	return ""
}

// splitLoaderAndMC splits "<loader version>-<mc version>" as used by the
// Fabric and Quilt profile ids. The game version is the trailing component
// that starts with a digit and contains a dot, which is what distinguishes
// "1.20.1" from the loader's own dotted version.
func splitLoaderAndMC(s string) (loader, mc string, ok bool) {
	// Walk separators from the right; the last one that leaves a plausible
	// game version on the right wins.
	for i := len(s) - 1; i > 0; i-- {
		if s[i] != '-' {
			continue
		}
		candidate := s[i+1:]
		if looksLikeMCVersion(candidate) {
			return s[:i], candidate, true
		}
	}
	return "", "", false
}

func looksLikeMCVersion(s string) bool {
	if s == "" {
		return false
	}
	if s[0] < '0' || s[0] > '9' {
		// Snapshots such as "25w44a" also start with a digit, so this only
		// rejects genuinely non-version text.
		return false
	}
	return strings.ContainsRune(s, '.') || strings.ContainsRune(s, 'w')
}

// loaderFromMainClass identifies the loader family from a version manifest's
// mainClass, which is harder to fake than a directory name.
func loaderFromMainClass(mainClass string) LoaderType {
	switch {
	case mainClass == "":
		return ""
	case strings.HasPrefix(mainClass, "net.fabricmc."):
		return LoaderFabric
	case strings.HasPrefix(mainClass, "org.quiltmc."):
		return LoaderQuilt
	case strings.HasPrefix(mainClass, "net.minecraft.client."):
		return LoaderVanilla
	}
	// Forge and NeoForge share bootstraplauncher and the old launchwrapper,
	// so the mainClass cannot tell them apart; leave that to the id.
	return ""
}

// ParseJavaArgs splits a launcher javaArgs string into heap sizing and the
// remaining JVM flags, which are carried over verbatim.
func ParseJavaArgs(args string) (MemSpec, []string) {
	var mem MemSpec
	var rest []string

	for _, arg := range strings.Fields(args) {
		switch {
		case strings.HasPrefix(arg, "-Xmx"):
			if mb, ok := parseHeapSize(strings.TrimPrefix(arg, "-Xmx")); ok {
				mem.MaxMB = mb
				continue
			}
		case strings.HasPrefix(arg, "-Xms"):
			if mb, ok := parseHeapSize(strings.TrimPrefix(arg, "-Xms")); ok {
				mem.MinMB = mb
				continue
			}
		}
		rest = append(rest, arg)
	}
	return mem, rest
}

// parseHeapSize converts a JVM size such as "8G", "4096M" or "2048k" to MiB.
func parseHeapSize(s string) (int, bool) {
	if s == "" {
		return 0, false
	}

	mult := 1
	switch s[len(s)-1] {
	case 'g', 'G':
		mult = 1024
		s = s[:len(s)-1]
	case 'm', 'M':
		mult = 1
		s = s[:len(s)-1]
	case 'k', 'K':
		// Sub-megabyte heaps are not meaningful here; round down.
		mult = 0
		s = s[:len(s)-1]
	}

	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	if mult == 0 {
		return n / 1024, true
	}
	return n * mult, true
}
