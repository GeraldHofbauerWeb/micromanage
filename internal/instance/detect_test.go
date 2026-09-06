package instance

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures below are trimmed copies of what the official launcher wrote
// into the real instances this was developed against.

// realProfiles reproduces sebsmodpack5/launcher_profiles.json: the two
// synthetic "latest-*" entries with their epoch timestamps, a NeoForge profile
// that is the most recently used, an older Forge profile, and a profile whose
// display name ("1.21.1") contradicts the version it actually points at.
const realProfiles = `{
  "profiles": {
    "2b21a55acfeec74be870b5b4289b38c8": {
      "name": "", "type": "latest-snapshot",
      "lastVersionId": "latest-snapshot", "lastUsed": "1970-01-01T00:00:00.000Z"
    },
    "62760ebdd48fc93f9ea0d320e41dcde9": {
      "name": "", "type": "latest-release",
      "lastVersionId": "latest-release", "lastUsed": "1970-01-02T00:00:00.000Z"
    },
    "8bcf8c06f34c3d58fac9c94384920d64": {
      "name": "1.21.1", "type": "custom",
      "lastVersionId": "1.20.1", "lastUsed": "2025-09-24T18:56:16.848Z",
      "javaArgs": "-Xmx8G -XX:+UseG1GC"
    },
    "NeoForge": {
      "name": "NeoForge", "type": "custom",
      "lastVersionId": "neoforge-21.1.248", "lastUsed": "2026-09-06T01:48:21.897Z",
      "javaArgs": "-Xmx8G -XX:+UnlockExperimentalVMOptions -XX:+UseG1GC -XX:G1HeapRegionSize=32M"
    },
    "forge": {
      "name": "forge", "type": "custom",
      "lastVersionId": "1.20.1-forge-47.4.0", "lastUsed": "2025-10-03T22:51:22.729Z"
    }
  },
  "settings": {},
  "version": 3
}`

func writeVersionJSON(t *testing.T, dir, id, body string) {
	t.Helper()
	vdir := filepath.Join(dir, "versions", id)
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, id+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// realInstanceDir builds a directory shaped like one of the real instances.
func realInstanceDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sebsmodpack5")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeVersionJSON(t, dir, "neoforge-21.1.248", `{
		"id": "neoforge-21.1.248",
		"inheritsFrom": "1.21.1",
		"mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
		"type": "release"
	}`)
	writeVersionJSON(t, dir, "1.20.1-forge-47.4.0", `{
		"id": "1.20.1-forge-47.4.0",
		"inheritsFrom": "1.20.1",
		"mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
		"type": "release"
	}`)
	writeVersionJSON(t, dir, "1.20.1", `{
		"id": "1.20.1",
		"mainClass": "net.minecraft.client.main.Main",
		"type": "release"
	}`)

	// The launcher drops these two files directly into versions/, alongside
	// the version directories. A naive ReadDir loop trips over them.
	for _, name := range []string{"jre_manifest.json", "version_manifest_v2.json"} {
		p := filepath.Join(dir, "versions", name)
		if err := os.WriteFile(p, []byte(`{"manifest":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectMetaFromLauncherProfiles(t *testing.T) {
	dir := realInstanceDir(t)
	if err := os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), []byte(realProfiles), 0o644); err != nil {
		t.Fatal(err)
	}

	det, err := DetectMeta(dir)
	if err != nil {
		t.Fatalf("DetectMeta: %v", err)
	}

	if det.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %v, want high", det.Confidence)
	}
	if det.Meta.MinecraftVersion != "1.21.1" {
		t.Errorf("minecraft version = %q, want 1.21.1", det.Meta.MinecraftVersion)
	}
	if det.Meta.Loader.Type != LoaderNeoForge || det.Meta.Loader.Version != "21.1.248" {
		t.Errorf("loader = %v, want NeoForge 21.1.248", det.Meta.Loader)
	}
	if det.Meta.Memory.MaxMB != 8192 {
		t.Errorf("max heap = %d MB, want 8192", det.Meta.Memory.MaxMB)
	}
	// -Xmx is consumed into Memory; the tuning flags are carried over as-is.
	if len(det.Meta.JVMArgs) != 3 {
		t.Errorf("jvm args = %v, want the three non-heap flags", det.Meta.JVMArgs)
	}
	for _, arg := range det.Meta.JVMArgs {
		if arg == "-Xmx8G" {
			t.Error("-Xmx leaked into JVMArgs instead of becoming Memory.MaxMB")
		}
	}
	// The other two real profiles remain available as alternatives.
	if len(det.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(det.Candidates))
	}
}

// TestDetectMetaIgnoresProfileName guards the case where a profile's display
// name contradicts its lastVersionId. Only lastVersionId is trustworthy.
func TestDetectMetaIgnoresProfileName(t *testing.T) {
	dir := realInstanceDir(t)
	profiles := `{"profiles": {"p": {
		"name": "1.21.1", "type": "custom",
		"lastVersionId": "1.20.1", "lastUsed": "2025-09-24T18:56:16.848Z"
	}}}`
	if err := os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), []byte(profiles), 0o644); err != nil {
		t.Fatal(err)
	}

	det, err := DetectMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if det.Meta.MinecraftVersion != "1.20.1" {
		t.Errorf("minecraft version = %q, want 1.20.1 (the profile name says 1.21.1 and is wrong)",
			det.Meta.MinecraftVersion)
	}
	if det.Meta.Loader.Type != LoaderVanilla {
		t.Errorf("loader = %v, want vanilla", det.Meta.Loader)
	}
}

// TestDetectMetaSkipsSyntheticProfiles ensures the launcher's own
// latest-release / latest-snapshot entries never win.
func TestDetectMetaSkipsSyntheticProfiles(t *testing.T) {
	dir := realInstanceDir(t)
	// The synthetic entries carry epoch timestamps but would otherwise be
	// indistinguishable; here they are the only ones with a recent date.
	profiles := `{"profiles": {
		"a": {"name": "", "type": "latest-release", "lastVersionId": "latest-release", "lastUsed": "2030-01-01T00:00:00.000Z"},
		"b": {"name": "NeoForge", "type": "custom", "lastVersionId": "neoforge-21.1.248", "lastUsed": "2026-09-06T01:48:21.897Z"}
	}}`
	if err := os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), []byte(profiles), 0o644); err != nil {
		t.Fatal(err)
	}

	det, err := DetectMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if det.Meta.ResolvedVersionID != "neoforge-21.1.248" {
		t.Errorf("version id = %q, want neoforge-21.1.248", det.Meta.ResolvedVersionID)
	}
}

// TestDetectMetaFallsBackToVersionsScan covers an instance with no profile
// file, and asserts the non-directory entries in versions/ are filtered.
func TestDetectMetaFallsBackToVersionsScan(t *testing.T) {
	dir := realInstanceDir(t)

	det, err := DetectMeta(dir)
	if err != nil {
		t.Fatalf("DetectMeta: %v", err)
	}
	if det.Confidence != ConfidenceMedium {
		t.Errorf("confidence = %v, want medium", det.Confidence)
	}
	// A loader profile is preferred over the plain vanilla manifest.
	if det.Meta.Loader.Type == LoaderVanilla {
		t.Errorf("loader = vanilla, want a loader profile to win the scan")
	}
	for _, m := range append([]Meta{det.Meta}, det.Candidates...) {
		if m.ResolvedVersionID == "jre_manifest.json" || m.ResolvedVersionID == "version_manifest_v2.json" {
			t.Errorf("non-directory entry %q treated as a version", m.ResolvedVersionID)
		}
	}
}

func TestDetectMetaEmptyInstance(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	det, err := DetectMeta(dir)
	if err != nil {
		t.Fatalf("DetectMeta: %v", err)
	}
	if det.Confidence != ConfidenceNone {
		t.Errorf("confidence = %v, want none", det.Confidence)
	}
	if det.Meta.Configured() {
		t.Error("empty instance reported as configured")
	}
}

func TestParseLoaderID(t *testing.T) {
	cases := []struct {
		id      string
		loader  LoaderType
		version string
		mc      string
	}{
		{"neoforge-21.1.248", LoaderNeoForge, "21.1.248", ""},
		{"neoforge-21.0.167", LoaderNeoForge, "21.0.167", ""},
		{"1.20.1-forge-47.4.0", LoaderForge, "47.4.0", "1.20.1"},
		{"1.16.5-forge-36.2.39", LoaderForge, "36.2.39", "1.16.5"},
		{"fabric-loader-0.15.7-1.20.1", LoaderFabric, "0.15.7", "1.20.1"},
		{"quilt-loader-0.20.0-beta.9-1.20.1", LoaderQuilt, "0.20.0-beta.9", "1.20.1"},
		{"1.20.1", LoaderVanilla, "", ""},
		{"25w44a", LoaderVanilla, "", ""},
		{"26.3-snapshot-9", LoaderVanilla, "", ""},
	}

	for _, tc := range cases {
		got := ParseLoaderID(tc.id)
		if got.Type != tc.loader || got.Version != tc.version {
			t.Errorf("ParseLoaderID(%q) = %v %q, want %v %q",
				tc.id, got.Type, got.Version, tc.loader, tc.version)
		}
		if tc.mc != "" {
			if mc := mcVersionFromLoaderID(tc.id, got.Type); mc != tc.mc {
				t.Errorf("mcVersionFromLoaderID(%q) = %q, want %q", tc.id, mc, tc.mc)
			}
		}
	}
}

func TestParseJavaArgs(t *testing.T) {
	mem, rest := ParseJavaArgs("-Xmx8G -Xms512M -XX:+UseG1GC -XX:G1HeapRegionSize=32M")

	if mem.MaxMB != 8192 {
		t.Errorf("MaxMB = %d, want 8192", mem.MaxMB)
	}
	if mem.MinMB != 512 {
		t.Errorf("MinMB = %d, want 512", mem.MinMB)
	}
	if len(rest) != 2 {
		t.Fatalf("remaining args = %v, want 2", rest)
	}

	// A size we cannot parse must survive as a flag rather than be dropped.
	_, rest = ParseJavaArgs("-Xmx -XX:+UseG1GC")
	if len(rest) != 2 {
		t.Errorf("unparseable -Xmx was dropped: %v", rest)
	}

	if mem, _ := ParseJavaArgs(""); mem.MaxMB != 0 || mem.MinMB != 0 {
		t.Error("empty javaArgs produced a heap size")
	}
}

func TestParseHeapSize(t *testing.T) {
	cases := map[string]int{
		"8G": 8192, "8g": 8192, "4096M": 4096, "2048m": 2048, "1048576k": 1024,
	}
	for in, want := range cases {
		got, ok := parseHeapSize(in)
		if !ok || got != want {
			t.Errorf("parseHeapSize(%q) = %d, %v; want %d, true", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "G", "abc", "-1G", "0"} {
		if _, ok := parseHeapSize(in); ok {
			t.Errorf("parseHeapSize(%q) unexpectedly succeeded", in)
		}
	}
}

func TestMemSpecResolved(t *testing.T) {
	if minMB, maxMB := (MemSpec{}).Resolved(); minMB != DefaultMinMB || maxMB != DefaultMaxMB {
		t.Errorf("zero MemSpec = %d/%d, want defaults %d/%d", minMB, maxMB, DefaultMinMB, DefaultMaxMB)
	}
	// A min above max would make the JVM refuse to start.
	if minMB, maxMB := (MemSpec{MinMB: 8192, MaxMB: 2048}).Resolved(); minMB > maxMB {
		t.Errorf("min %d exceeds max %d", minMB, maxMB)
	}
}
