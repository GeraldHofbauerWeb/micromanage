package mojang

import "testing"

var (
	linux64   = Platform{OS: "linux", Arch: "x86_64", Version: "6.9.3"}
	windows64 = Platform{OS: "windows", Arch: "x86_64", Version: "10.0.19045"}
	osxArm    = Platform{OS: "osx", Arch: "arm64", Version: "14.5"}
	linux32   = Platform{OS: "linux", Arch: "x86", Version: "6.9.3"}
)

func TestAllowedNoRules(t *testing.T) {
	if !Allowed(nil, linux64, nil) {
		t.Error("a subject with no rules must be allowed")
	}
	if !Allowed([]Rule{}, linux64, nil) {
		t.Error("a subject with an empty rule list must be allowed")
	}
}

func TestAllowedOSGating(t *testing.T) {
	onlyOSX := []Rule{{Action: "allow", OS: &OSRule{Name: "osx"}}}

	if !Allowed(onlyOSX, osxArm, nil) {
		t.Error("osx-only rule rejected osx")
	}
	for _, p := range []Platform{linux64, windows64} {
		if Allowed(onlyOSX, p, nil) {
			t.Errorf("osx-only rule allowed %s", p.OS)
		}
	}
}

// TestAllowedLaterRuleWins covers the common "allow everywhere, except here"
// shape: evaluation is ordered and the last matching rule decides.
func TestAllowedLaterRuleWins(t *testing.T) {
	rules := []Rule{
		{Action: "allow"},
		{Action: "disallow", OS: &OSRule{Name: "osx"}},
	}

	if !Allowed(rules, linux64, nil) {
		t.Error("linux was disallowed by an osx-only exclusion")
	}
	if !Allowed(rules, windows64, nil) {
		t.Error("windows was disallowed by an osx-only exclusion")
	}
	if Allowed(rules, osxArm, nil) {
		t.Error("osx was allowed despite the exclusion")
	}
}

func TestAllowedArchGating(t *testing.T) {
	only32 := []Rule{{Action: "allow", OS: &OSRule{Name: "linux", Arch: "x86"}}}

	if !Allowed(only32, linux32, nil) {
		t.Error("x86 rule rejected an x86 platform")
	}
	if Allowed(only32, linux64, nil) {
		t.Error("x86 rule allowed x86_64")
	}
}

// TestAllowedOSVersionRegexp checks that os.version is treated as a regular
// expression, which is how manifests express "Windows 10 or newer".
func TestAllowedOSVersionRegexp(t *testing.T) {
	win10 := []Rule{{Action: "allow", OS: &OSRule{Name: "windows", Version: `^10\.`}}}

	if !Allowed(win10, windows64, nil) {
		t.Errorf("version %q did not match ^10\\.", windows64.Version)
	}

	win7 := Platform{OS: "windows", Arch: "x86_64", Version: "6.1.7601"}
	if Allowed(win10, win7, nil) {
		t.Error("Windows 7 matched a ^10\\. rule")
	}

	// A platform with no known version cannot satisfy a version constraint.
	noVersion := Platform{OS: "windows", Arch: "x86_64"}
	if Allowed(win10, noVersion, nil) {
		t.Error("a rule with os.version matched a platform with no version")
	}

	// Data we do not control must not panic the launcher.
	broken := []Rule{{Action: "allow", OS: &OSRule{Name: "windows", Version: "(unclosed"}}}
	if Allowed(broken, windows64, nil) {
		t.Error("an uncompilable os.version pattern was treated as matching")
	}
}

func TestAllowedFeatureGating(t *testing.T) {
	demoOnly := []Rule{{Action: "allow", Features: map[string]bool{FeatureDemoUser: true}}}

	if !Allowed(demoOnly, linux64, map[string]bool{FeatureDemoUser: true}) {
		t.Error("demo rule rejected a demo session")
	}
	if Allowed(demoOnly, linux64, map[string]bool{FeatureDemoUser: false}) {
		t.Error("demo rule allowed a non-demo session")
	}
	// An absent feature is false, not unknown.
	if Allowed(demoOnly, linux64, nil) {
		t.Error("demo rule allowed a session with no features set")
	}

	// The inverse form, used to exclude the demo arguments from normal runs.
	notDemo := []Rule{{Action: "allow", Features: map[string]bool{FeatureDemoUser: false}}}
	if !Allowed(notDemo, linux64, nil) {
		t.Error("is_demo_user:false rule rejected a normal session")
	}
}

func TestAllowedCombinedConditions(t *testing.T) {
	rule := []Rule{{
		Action:   "allow",
		OS:       &OSRule{Name: "windows"},
		Features: map[string]bool{FeatureCustomResolution: true},
	}}

	withRes := map[string]bool{FeatureCustomResolution: true}
	if !Allowed(rule, windows64, withRes) {
		t.Error("both conditions held but the rule did not match")
	}
	if Allowed(rule, linux64, withRes) {
		t.Error("matched despite the wrong OS")
	}
	if Allowed(rule, windows64, nil) {
		t.Error("matched despite the feature being off")
	}
}

func TestPlatformNaming(t *testing.T) {
	cases := map[string]string{"darwin": "osx", "windows": "windows", "linux": "linux", "freebsd": "linux"}
	for goos, want := range cases {
		if got := osName(goos); got != want {
			t.Errorf("osName(%q) = %q, want %q", goos, got, want)
		}
	}

	archCases := map[string]string{"amd64": "x86_64", "386": "x86", "arm64": "arm64"}
	for goarch, want := range archCases {
		if got := archName(goarch); got != want {
			t.Errorf("archName(%q) = %q, want %q", goarch, got, want)
		}
	}
}
