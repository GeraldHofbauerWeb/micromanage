// Package mojang reads Minecraft's version metadata: the version manifest,
// version JSONs, their inheritance, and the rules that gate libraries and
// launch arguments per platform.
package mojang

import (
	"regexp"
	"runtime"
)

// Platform is the target a version's rules are evaluated against. It is a
// value rather than a read of runtime.GOOS so rule evaluation stays pure and
// every platform can be exercised from a test on any host.
type Platform struct {
	OS      string // "linux", "osx" or "windows" — Mojang's names, not Go's
	Arch    string // "x86", "x86_64" or "arm64"
	Version string // OS version, matched against os.version as a regexp
}

// CurrentPlatform describes the host, translated into Mojang's vocabulary.
func CurrentPlatform() Platform {
	p := Platform{OS: osName(runtime.GOOS), Arch: archName(runtime.GOARCH)}
	p.Version = osVersion()
	return p
}

func osName(goos string) string {
	switch goos {
	case "darwin":
		return "osx"
	case "windows":
		return "windows"
	default:
		// Mojang only ever names linux, osx and windows; the BSDs are closest
		// to linux and its natives are the only ones that could work.
		return "linux"
	}
}

func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm32"
	}
	return goarch
}

// Rule gates a library or argument on the platform and on launcher features.
type Rule struct {
	Action   string          `json:"action"` // "allow" or "disallow"
	OS       *OSRule         `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

// OSRule constrains a rule to an operating system, version or architecture.
type OSRule struct {
	Name string `json:"name,omitempty"`
	// Version is a regular expression, e.g. "^10\\." for Windows 10.
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

// Feature keys that appear in official version manifests. Anything not set is
// treated as false.
const (
	FeatureDemoUser             = "is_demo_user"
	FeatureCustomResolution     = "has_custom_resolution"
	FeatureQuickPlaySupport     = "has_quick_plays_support"
	FeatureQuickPlaySingle      = "is_quick_play_singleplayer"
	FeatureQuickPlayMultiplayer = "is_quick_play_multiplayer"
	FeatureQuickPlayRealms      = "is_quick_play_realms"
)

// Allowed reports whether a rule set permits its subject on this platform.
//
// With no rules the subject is allowed. Otherwise evaluation starts at
// disallowed and every matching rule sets the outcome to its own action, so a
// later rule overrides an earlier one — that is what lets a manifest say
// "allow everywhere, except disallow on osx".
func Allowed(rules []Rule, p Platform, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}

	allowed := false
	for _, r := range rules {
		if !r.matches(p, features) {
			continue
		}
		allowed = r.Action == "allow"
	}
	return allowed
}

// matches reports whether a rule's conditions hold. A rule with no conditions
// matches unconditionally.
func (r Rule) matches(p Platform, features map[string]bool) bool {
	if r.OS != nil {
		if r.OS.Name != "" && r.OS.Name != p.OS {
			return false
		}
		if r.OS.Arch != "" && r.OS.Arch != p.Arch {
			return false
		}
		if r.OS.Version != "" && !matchOSVersion(r.OS.Version, p.Version) {
			return false
		}
	}

	for key, want := range r.Features {
		if features[key] != want {
			return false
		}
	}
	return true
}

// matchOSVersion applies a manifest's os.version regexp. A pattern that does
// not compile is treated as non-matching rather than panicking on data we do
// not control.
func matchOSVersion(pattern, version string) bool {
	if version == "" {
		return false
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(version)
}
