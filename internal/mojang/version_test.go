package mojang

import "testing"

func TestMavenPath(t *testing.T) {
	cases := []struct {
		coordinate string
		want       string
	}{
		{"org.ow2.asm:asm:9.7.1", "org/ow2/asm/asm/9.7.1/asm-9.7.1.jar"},
		{"com.google.guava:guava:32.1.2-jre", "com/google/guava/guava/32.1.2-jre/guava-32.1.2-jre.jar"},
		{"org.lwjgl:lwjgl:3.3.3:natives-linux", "org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-linux.jar"},
		{"net.neoforged:neoforge:21.1.248", "net/neoforged/neoforge/21.1.248/neoforge-21.1.248.jar"},
		// Loader manifests use the @extension form for non-jar artifacts.
		{"net.minecraftforge:forge:1.20.1-47.4.0:universal@zip", "net/minecraftforge/forge/1.20.1-47.4.0/forge-1.20.1-47.4.0-universal.zip"},
	}

	for _, tc := range cases {
		got, err := MavenPath(tc.coordinate)
		if err != nil {
			t.Errorf("MavenPath(%q): %v", tc.coordinate, err)
			continue
		}
		if got != tc.want {
			t.Errorf("MavenPath(%q) = %q, want %q", tc.coordinate, got, tc.want)
		}
	}

	for _, bad := range []string{"", "group", "group:artifact"} {
		if _, err := MavenPath(bad); err == nil {
			t.Errorf("MavenPath(%q) = nil error, want a failure", bad)
		}
	}
}

// TestMavenKey covers classpath deduplication: the same group:artifact at a
// different version must collide, but a natives classifier must not.
func TestMavenKey(t *testing.T) {
	if MavenKey("org.ow2.asm:asm:9.6") != MavenKey("org.ow2.asm:asm:9.7.1") {
		t.Error("two versions of the same artifact produced different keys")
	}
	if MavenKey("org.lwjgl:lwjgl:3.3.3") == MavenKey("org.lwjgl:lwjgl:3.3.3:natives-linux") {
		t.Error("a natives classifier collided with the plain jar")
	}
	if got := MavenKey("org.ow2.asm:asm:9.6"); got != "org.ow2.asm:asm" {
		t.Errorf("MavenKey = %q, want group:artifact", got)
	}
}

// TestLibraryArtifactSynthesised covers loader libraries, which routinely omit
// a downloads block and expect the path to come from the coordinate.
func TestLibraryArtifactSynthesised(t *testing.T) {
	lib := Library{Name: "net.fabricmc:intermediary:1.20.1", URL: "https://maven.fabricmc.net/"}

	d, ok := lib.Artifact(linux64)
	if !ok {
		t.Fatal("no artifact derived from the coordinate")
	}
	if d.Path != "net/fabricmc/intermediary/1.20.1/intermediary-1.20.1.jar" {
		t.Errorf("path = %q", d.Path)
	}
	if d.URL != "https://maven.fabricmc.net/net/fabricmc/intermediary/1.20.1/intermediary-1.20.1.jar" {
		t.Errorf("url = %q", d.URL)
	}

	// With no repository named, Mojang's is the default.
	plain := Library{Name: "org.ow2.asm:asm:9.6"}
	d, _ = plain.Artifact(linux64)
	if want := MojangLibrariesURL + "org/ow2/asm/asm/9.6/asm-9.6.jar"; d.URL != want {
		t.Errorf("url = %q, want %q", d.URL, want)
	}
}

func TestLibraryArtifactPrefersDownloads(t *testing.T) {
	lib := Library{
		Name: "org.ow2.asm:asm:9.6",
		Downloads: &LibraryDownloads{Artifact: &Download{
			URL:  "https://libraries.minecraft.net/org/ow2/asm/asm/9.6/asm-9.6.jar",
			Path: "org/ow2/asm/asm/9.6/asm-9.6.jar",
			SHA1: "deadbeef",
			Size: 123,
		}},
	}
	d, ok := lib.Artifact(linux64)
	if !ok || d.SHA1 != "deadbeef" || d.Size != 123 {
		t.Errorf("declared download was not used: %+v", d)
	}
}

// TestLibraryNativesLegacy covers the pre-1.19 shape, where a natives map
// names a classifier and ${arch} needs substituting.
func TestLibraryNativesLegacy(t *testing.T) {
	lib := Library{
		Name:    "org.lwjgl.lwjgl:lwjgl-platform:2.9.4",
		Natives: map[string]string{"linux": "natives-linux", "windows": "natives-windows-${arch}"},
		Downloads: &LibraryDownloads{Classifiers: map[string]Download{
			"natives-linux":      {URL: "https://example.invalid/linux.jar", SHA1: "l"},
			"natives-windows-64": {URL: "https://example.invalid/win64.jar", SHA1: "w"},
		}},
	}

	if !lib.IsNative(linux64) {
		t.Error("legacy natives library not recognised on linux")
	}
	if d, _ := lib.Artifact(linux64); d.SHA1 != "l" {
		t.Errorf("linux classifier resolved to %+v", d)
	}

	if got := lib.NativeClassifier(windows64); got != "natives-windows-64" {
		t.Errorf("${arch} substitution gave %q, want natives-windows-64", got)
	}
	if got := lib.NativeClassifier(Platform{OS: "windows", Arch: "x86"}); got != "natives-windows-32" {
		t.Errorf("32-bit substitution gave %q", got)
	}

	// A platform the map does not cover has no natives.
	if lib.IsNative(osxArm) {
		t.Error("osx reported as having natives although the map omits it")
	}
}

// TestLibraryNativesModern covers 1.19+, where natives are ordinary libraries
// identified by their classifier.
func TestLibraryNativesModern(t *testing.T) {
	lib := Library{
		Name:  "org.lwjgl:lwjgl:3.3.3:natives-linux",
		Rules: []Rule{{Action: "allow", OS: &OSRule{Name: "linux"}}},
	}
	if !lib.IsNative(linux64) {
		t.Error("modern natives library not recognised by its classifier")
	}

	plain := Library{Name: "org.lwjgl:lwjgl:3.3.3"}
	if plain.IsNative(linux64) {
		t.Error("a plain library was treated as natives")
	}
}

func TestParseVersionRejectsIDLess(t *testing.T) {
	if _, err := ParseVersion([]byte(`{"type":"release"}`)); err == nil {
		t.Error("a manifest with no id was accepted")
	}
	if _, err := ParseVersion([]byte(`{`)); err == nil {
		t.Error("malformed JSON was accepted")
	}
}
