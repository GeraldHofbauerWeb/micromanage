package mojang

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Version is a Minecraft version manifest. It covers both the modern schema
// (structured "arguments") and the legacy one ("minecraftArguments"), because
// loader profiles and older releases still use the latter.
type Version struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom,omitempty"`
	Type         string `json:"type,omitempty"`
	MainClass    string `json:"mainClass,omitempty"`

	Assets     string      `json:"assets,omitempty"`
	AssetIndex *AssetIndex `json:"assetIndex,omitempty"`

	Downloads map[string]Download `json:"downloads,omitempty"`
	Libraries []Library           `json:"libraries,omitempty"`

	JavaVersion *JavaVersion `json:"javaVersion,omitempty"`
	Logging     *Logging     `json:"logging,omitempty"`

	Arguments *Arguments `json:"arguments,omitempty"`
	// MinecraftArguments is the pre-1.13 form: a single space-separated string.
	MinecraftArguments string `json:"minecraftArguments,omitempty"`

	MinimumLauncherVersion int    `json:"minimumLauncherVersion,omitempty"`
	ReleaseTime            string `json:"releaseTime,omitempty"`
	Time                   string `json:"time,omitempty"`
	ComplianceLevel        int    `json:"complianceLevel,omitempty"`
}

// Download is a single downloadable artifact.
type Download struct {
	SHA1 string `json:"sha1,omitempty"`
	Size int64  `json:"size,omitempty"`
	URL  string `json:"url"`
	// Path is set on library artifacts and is relative to the libraries root.
	Path string `json:"path,omitempty"`
}

// AssetIndex describes the asset index for a version.
type AssetIndex struct {
	ID        string `json:"id"`
	SHA1      string `json:"sha1,omitempty"`
	Size      int64  `json:"size,omitempty"`
	TotalSize int64  `json:"totalSize,omitempty"`
	URL       string `json:"url"`
}

// JavaVersion is the runtime a version requires.
type JavaVersion struct {
	Component    string `json:"component"`
	MajorVersion int    `json:"majorVersion"`
}

// Logging configures the game's log4j setup.
type Logging struct {
	Client *LoggingClient `json:"client,omitempty"`
}

// LoggingClient carries the log4j configuration file and the JVM argument
// that points at it. For 1.7 to 1.18 this is also the Log4Shell mitigation.
type LoggingClient struct {
	Argument string   `json:"argument"`
	File     Download `json:"file"`
	Type     string   `json:"type"`
}

// Library is one classpath or native entry.
type Library struct {
	Name      string            `json:"name"`
	Downloads *LibraryDownloads `json:"downloads,omitempty"`
	Rules     []Rule            `json:"rules,omitempty"`
	Extract   *Extract          `json:"extract,omitempty"`
	// Natives maps an OS name to the classifier holding its native artifact.
	// Used up to 1.18; newer versions express natives as ordinary rule-gated
	// libraries instead.
	Natives map[string]string `json:"natives,omitempty"`
	// URL is a maven repository root. Loader libraries frequently omit
	// downloads entirely and expect the path to be derived from Name.
	URL string `json:"url,omitempty"`
}

// LibraryDownloads holds a library's artifacts.
type LibraryDownloads struct {
	Artifact    *Download           `json:"artifact,omitempty"`
	Classifiers map[string]Download `json:"classifiers,omitempty"`
}

// Extract controls how a native archive is unpacked.
type Extract struct {
	Exclude []string `json:"exclude,omitempty"`
}

// Arguments is the modern argument schema.
type Arguments struct {
	Game []Argument `json:"game,omitempty"`
	JVM  []Argument `json:"jvm,omitempty"`
}

// Argument is one entry of an arguments array. Entries are either a bare
// string or an object carrying rules and one-or-more values, so the JSON is
// heterogeneous and needs its own decoder.
type Argument struct {
	Value []string
	Rules []Rule
}

// UnmarshalJSON accepts both forms an arguments array may contain.
func (a *Argument) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))

	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		a.Value = []string{s}
		a.Rules = nil
		return nil
	}

	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("argument: %w", err)
	}
	a.Rules = obj.Rules

	// value is a string or an array of strings.
	if len(obj.Value) == 0 {
		a.Value = nil
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(string(obj.Value)), `[`) {
		return json.Unmarshal(obj.Value, &a.Value)
	}
	var s string
	if err := json.Unmarshal(obj.Value, &s); err != nil {
		return fmt.Errorf("argument value: %w", err)
	}
	a.Value = []string{s}
	return nil
}

// MarshalJSON writes back the compact form when a value carries no rules, so a
// resolved manifest round-trips without growing.
func (a Argument) MarshalJSON() ([]byte, error) {
	if len(a.Rules) == 0 && len(a.Value) == 1 {
		return json.Marshal(a.Value[0])
	}
	return json.Marshal(struct {
		Rules []Rule   `json:"rules,omitempty"`
		Value []string `json:"value"`
	}{Rules: a.Rules, Value: a.Value})
}

// ParseVersion decodes a version manifest.
func ParseVersion(data []byte) (*Version, error) {
	var v Version
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse version json: %w", err)
	}
	if v.ID == "" {
		return nil, fmt.Errorf("version json has no id")
	}
	return &v, nil
}

// IsNative reports whether a library contributes native code rather than a
// classpath entry.
//
// Two eras must both be recognised: up to 1.18 a library carries a "natives"
// map naming a classifier, and from 1.19 natives are ordinary libraries whose
// maven coordinate ends in a natives-* classifier.
func (l Library) IsNative(p Platform) bool {
	if len(l.Natives) > 0 {
		_, ok := l.Natives[p.OS]
		return ok
	}
	return strings.Contains(mavenClassifier(l.Name), "natives")
}

// NativeClassifier returns the classifier holding this platform's natives for
// a legacy-style library, with ${arch} substituted.
func (l Library) NativeClassifier(p Platform) string {
	classifier, ok := l.Natives[p.OS]
	if !ok {
		return ""
	}
	arch := "64"
	if p.Arch == "x86" || p.Arch == "arm32" {
		arch = "32"
	}
	return strings.ReplaceAll(classifier, "${arch}", arch)
}

// Artifact returns the download describing this library's file for the given
// platform, synthesising one from the maven coordinate when the manifest omits
// downloads — which loader manifests routinely do.
func (l Library) Artifact(p Platform) (Download, bool) {
	if classifier := l.NativeClassifier(p); classifier != "" {
		if l.Downloads != nil {
			if d, ok := l.Downloads.Classifiers[classifier]; ok {
				return d, true
			}
		}
		return l.syntheticDownload(classifier)
	}

	if l.Downloads != nil && l.Downloads.Artifact != nil {
		d := *l.Downloads.Artifact
		if d.Path == "" {
			if p, err := MavenPath(l.Name); err == nil {
				d.Path = p
			}
		}
		return d, d.URL != "" || d.Path != ""
	}

	return l.syntheticDownload("")
}

// syntheticDownload builds an artifact from the maven coordinate plus the
// library's repository root.
func (l Library) syntheticDownload(classifier string) (Download, bool) {
	name := l.Name
	if classifier != "" && mavenClassifier(name) == "" {
		name += ":" + classifier
	}

	rel, err := MavenPath(name)
	if err != nil {
		return Download{}, false
	}

	base := l.URL
	if base == "" {
		base = MojangLibrariesURL
	}
	return Download{
		URL:  strings.TrimSuffix(base, "/") + "/" + rel,
		Path: rel,
	}, true
}

// MojangLibrariesURL is the default maven root for libraries that name no
// repository of their own.
const MojangLibrariesURL = "https://libraries.minecraft.net/"

// MavenPath converts a maven coordinate — group:artifact:version[:classifier]
// with an optional @extension — into its repository-relative path.
func MavenPath(coordinate string) (string, error) {
	spec := coordinate
	ext := "jar"
	if at := strings.LastIndex(spec, "@"); at >= 0 {
		ext = spec[at+1:]
		spec = spec[:at]
	}

	parts := strings.Split(spec, ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("malformed maven coordinate %q", coordinate)
	}

	group, artifact, version := parts[0], parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) > 3 && parts[3] != "" {
		file += "-" + parts[3]
	}
	file += "." + ext

	return path.Join(strings.ReplaceAll(group, ".", "/"), artifact, version, file), nil
}

// MavenKey returns the group:artifact identity of a coordinate, which is what
// classpath deduplication keys on: a loader overriding a library ships the
// same group and artifact with a different version.
func MavenKey(coordinate string) string {
	spec := coordinate
	if at := strings.LastIndex(spec, "@"); at >= 0 {
		spec = spec[:at]
	}
	parts := strings.Split(spec, ":")
	if len(parts) < 2 {
		return spec
	}
	key := parts[0] + ":" + parts[1]
	// A classifier makes it a different file, so natives-linux and the plain
	// jar must not collide.
	if len(parts) > 3 && parts[3] != "" {
		key += ":" + parts[3]
	}
	return key
}

// mavenClassifier returns a coordinate's classifier, or "".
func mavenClassifier(coordinate string) string {
	spec := coordinate
	if at := strings.LastIndex(spec, "@"); at >= 0 {
		spec = spec[:at]
	}
	parts := strings.Split(spec, ":")
	if len(parts) > 3 {
		return parts[3]
	}
	return ""
}
