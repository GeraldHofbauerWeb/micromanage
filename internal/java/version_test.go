package java

import (
	"strings"
	"testing"
)

// The strings below are verbatim `java -version` output from the runtimes on
// this machine and the common alternatives.
func TestParseVersion(t *testing.T) {
	cases := []struct {
		out   string
		major int
		full  string
	}{
		{`openjdk version "1.8.0_392"
OpenJDK Runtime Environment (build 1.8.0_392-b08)`, 8, "1.8.0_392"},
		{`openjdk version "17.0.15" 2025-04-15 LTS
OpenJDK Runtime Environment Microsoft-11369909 (build 17.0.15+6-LTS)`, 17, "17.0.15"},
		{`openjdk version "21.0.7" 2025-04-15 LTS`, 21, "21.0.7"},
		{`openjdk version "21.0.12.1" 2026-08-18`, 21, "21.0.12.1"},
		{`java version "1.7.0_80"`, 7, "1.7.0_80"},
		{`openjdk version "11.0.22" 2024-01-16`, 11, "11.0.22"},
		{`openjdk version "17.0.9+9" 2023-10-17`, 17, "17.0.9+9"},
	}

	for _, tc := range cases {
		major, full, err := ParseVersion(tc.out)
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", firstLine(tc.out), err)
			continue
		}
		if major != tc.major {
			t.Errorf("major of %q = %d, want %d", tc.full, major, tc.major)
		}
		if full != tc.full {
			t.Errorf("full = %q, want %q", full, tc.full)
		}
	}

	for _, bad := range []string{"", "not a version", `version "abc"`, `version ""`} {
		if _, _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) = nil error, want a failure", bad)
		}
	}
}

func TestParseVendor(t *testing.T) {
	cases := map[string]string{
		"OpenJDK Runtime Environment Microsoft-11369909 (build 17.0.15+6-LTS)": "Microsoft",
		"OpenJDK Runtime Environment Zulu17.48+15-CA":                          "Zulu",
		"OpenJDK Runtime Environment Temurin-21.0.2+13":                        "Temurin",
		"OpenJDK Runtime Environment GraalVM CE 21+35.1":                       "GraalVM",
		"Java(TM) SE Runtime Environment (build 1.8.0_392-b08)":                "Oracle",
		"something else entirely":                                              "",
	}
	for out, want := range cases {
		if got := ParseVendor(out); got != want {
			t.Errorf("ParseVendor(%q) = %q, want %q", out, got, want)
		}
	}
}

// TestSelectPrefersExactMajor covers the case that matters on this machine:
// 1.20.1 asks for Java 17 and both 17 and 21 are installed.
func TestSelectPrefersExactMajor(t *testing.T) {
	runtimes := []Runtime{
		{Path: "/j21", Major: 21, Component: "java-runtime-delta"},
		{Path: "/j17", Major: 17, Component: "java-runtime-gamma"},
	}

	sel, err := Select(runtimes, Requirement{Major: 17, Component: "java-runtime-gamma"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if sel.Runtime.Path != "/j17" {
		t.Errorf("chose %s, want the exact major match", sel.Runtime.Path)
	}
	if sel.Warning != "" {
		t.Errorf("an exact match produced a warning: %s", sel.Warning)
	}
}

func TestSelectPrefersMatchingComponent(t *testing.T) {
	runtimes := []Runtime{
		{Path: "/other17", Major: 17},
		{Path: "/gamma", Major: 17, Component: "java-runtime-gamma"},
	}
	sel, err := Select(runtimes, Requirement{Major: 17, Component: "java-runtime-gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Runtime.Path != "/gamma" {
		t.Errorf("chose %s, want the runtime whose component the version names", sel.Runtime.Path)
	}
}

// TestSelectStrictRefusesNewer is the Forge guard: 47.x on Java 21 is a known
// broken combination, so no silent upgrade.
func TestSelectStrictRefusesNewer(t *testing.T) {
	runtimes := []Runtime{{Path: "/j21", Major: 21}}

	if _, err := Select(runtimes, Requirement{Major: 17, Strict: true}); err == nil {
		t.Fatal("strict selection accepted a newer major release")
	}

	// Without strict it is allowed, but must say so.
	sel, err := Select(runtimes, Requirement{Major: 17})
	if err != nil {
		t.Fatalf("non-strict selection: %v", err)
	}
	if sel.Warning == "" {
		t.Error("upgrading 17 to 21 produced no warning")
	}
}

func TestSelectRefusesOlder(t *testing.T) {
	runtimes := []Runtime{{Path: "/j8", Major: 8}}
	if _, err := Select(runtimes, Requirement{Major: 17}); err == nil {
		t.Fatal("selection accepted an older major release")
	}
}

// TestSelectReportsRepairableRuntimes turns the silent failure this project
// started with into an actionable message.
func TestSelectReportsRepairableRuntimes(t *testing.T) {
	runtimes := []Runtime{
		{Path: "/broken/bin/java", Major: 17, Broken: true, Reason: "not executable"},
	}
	_, err := Select(runtimes, Requirement{Major: 17})
	if err == nil {
		t.Fatal("selection succeeded with only a broken runtime")
	}
	if want := "repair-perms"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not point at %q", err, want)
	}
}

func TestSelectNoRuntimes(t *testing.T) {
	if _, err := Select(nil, Requirement{Major: 17}); err == nil {
		t.Fatal("selection succeeded with no runtimes at all")
	}
}

func TestSelectWithoutRequirement(t *testing.T) {
	runtimes := []Runtime{{Path: "/j21", Major: 21}, {Path: "/j17", Major: 17}}
	sel, err := Select(runtimes, Requirement{})
	if err != nil {
		t.Fatal(err)
	}
	// Detect sorts newest first; with nothing to match, the first is taken.
	if sel.Runtime.Path != "/j21" {
		t.Errorf("chose %s", sel.Runtime.Path)
	}
}

func TestMajorFromComponent(t *testing.T) {
	cases := map[string]int{
		"jre-legacy": 8, "java-runtime-gamma": 17, "java-runtime-delta": 21, "nonsense": 0,
	}
	for component, want := range cases {
		if got := majorFromComponent(component); got != want {
			t.Errorf("majorFromComponent(%q) = %d, want %d", component, got, want)
		}
	}
}
