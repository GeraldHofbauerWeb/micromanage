package mojang

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// The fixtures mirror the real 1.21.1 and neoforge-21.1.248 manifests, trimmed
// to the handful of libraries that make the merge rules observable.

const parentJSON = `{
  "id": "1.21.1",
  "type": "release",
  "mainClass": "net.minecraft.client.main.Main",
  "assets": "17",
  "assetIndex": {"id": "17", "sha1": "aaa", "url": "https://example.invalid/17.json", "totalSize": 620000000},
  "javaVersion": {"component": "java-runtime-delta", "majorVersion": 21},
  "downloads": {"client": {"sha1": "clientsha", "size": 26000000, "url": "https://example.invalid/client.jar"}},
  "logging": {"client": {"argument": "-Dlog4j.configurationFile=${path}", "type": "log4j2-xml",
    "file": {"id": "client-1.12.xml", "sha1": "logsha", "url": "https://example.invalid/log.xml"}}},
  "libraries": [
    {"name": "org.ow2.asm:asm:9.6"},
    {"name": "com.google.guava:guava:32.1.2-jre"},
    {"name": "org.lwjgl:lwjgl:3.3.3"}
  ],
  "arguments": {
    "game": ["--username", "${auth_player_name}"],
    "jvm": ["-Djava.library.path=${natives_directory}", "-cp", "${classpath}"]
  }
}`

const childJSON = `{
  "id": "neoforge-21.1.248",
  "inheritsFrom": "1.21.1",
  "type": "release",
  "mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
  "libraries": [
    {"name": "org.ow2.asm:asm:9.7.1"},
    {"name": "net.neoforged:neoforge:21.1.248"}
  ],
  "arguments": {
    "game": ["--launchTarget", "neoforgeclient"],
    "jvm": ["-DignoreList=${classpath_separator}"]
  }
}`

// fixtureFetcher serves the two fixtures by id.
func fixtureFetcher(t *testing.T) Fetcher {
	t.Helper()
	byID := map[string]string{"1.21.1": parentJSON, "neoforge-21.1.248": childJSON}
	return func(_ context.Context, id string) (*Version, error) {
		body, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("no fixture for %q", id)
		}
		return ParseVersion([]byte(body))
	}
}

func TestResolveMergesLoaderProfile(t *testing.T) {
	v, err := Resolve(context.Background(), "neoforge-21.1.248", fixtureFetcher(t))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if v.ID != "neoforge-21.1.248" {
		t.Errorf("id = %q, want the child's", v.ID)
	}
	if v.InheritsFrom != "" {
		t.Error("the resolved manifest still declares inheritsFrom")
	}
	if v.MainClass != "cpw.mods.bootstraplauncher.BootstrapLauncher" {
		t.Errorf("mainClass = %q, want the child's", v.MainClass)
	}

	// Inherited from the parent, which the loader profile never restates.
	if v.AssetIndex == nil || v.AssetIndex.ID != "17" {
		t.Errorf("assetIndex = %v, want the parent's", v.AssetIndex)
	}
	if v.Assets != "17" {
		t.Errorf("assets = %q, want 17", v.Assets)
	}
	if v.JavaVersion == nil || v.JavaVersion.MajorVersion != 21 {
		t.Errorf("javaVersion = %v, want the parent's", v.JavaVersion)
	}
	if v.Logging == nil || v.Logging.Client == nil {
		t.Error("logging config was not inherited")
	}
	if _, ok := v.Downloads["client"]; !ok {
		t.Error("client download was not inherited")
	}
}

// TestMergeLibraryPrecedence is the important one: a loader ships its own ASM
// and it must come first and win, or the game dies with NoSuchMethodError.
func TestMergeLibraryPrecedence(t *testing.T) {
	v, err := Resolve(context.Background(), "neoforge-21.1.248", fixtureFetcher(t))
	if err != nil {
		t.Fatal(err)
	}

	if len(v.Libraries) != 4 {
		t.Fatalf("got %d libraries, want 4 (2 child + 3 parent - 1 duplicate): %v",
			len(v.Libraries), libNames(v.Libraries))
	}

	// The child's libraries lead.
	if v.Libraries[0].Name != "org.ow2.asm:asm:9.7.1" {
		t.Errorf("first library = %q, want the child's asm", v.Libraries[0].Name)
	}
	if v.Libraries[1].Name != "net.neoforged:neoforge:21.1.248" {
		t.Errorf("second library = %q, want the child's neoforge", v.Libraries[1].Name)
	}

	// Deduplication keeps the child's version and drops the parent's.
	for _, lib := range v.Libraries {
		if lib.Name == "org.ow2.asm:asm:9.6" {
			t.Error("the parent's asm 9.6 survived alongside the child's 9.7.1")
		}
	}

	// Parent-only libraries are still present.
	for _, want := range []string{"com.google.guava:guava:32.1.2-jre", "org.lwjgl:lwjgl:3.3.3"} {
		if !hasLib(v.Libraries, want) {
			t.Errorf("parent library %q was lost", want)
		}
	}
}

func TestMergeArgumentOrder(t *testing.T) {
	v, err := Resolve(context.Background(), "neoforge-21.1.248", fixtureFetcher(t))
	if err != nil {
		t.Fatal(err)
	}

	game := flatten(v.Arguments.Game)
	want := []string{"--username", "${auth_player_name}", "--launchTarget", "neoforgeclient"}
	if len(game) != len(want) {
		t.Fatalf("game args = %v, want %v", game, want)
	}
	for i := range want {
		if game[i] != want[i] {
			t.Errorf("game arg %d = %q, want %q (parent's must come first)", i, game[i], want[i])
		}
	}

	jvm := flatten(v.Arguments.JVM)
	if jvm[len(jvm)-1] != "-DignoreList=${classpath_separator}" {
		t.Errorf("last jvm arg = %q, want the child's", jvm[len(jvm)-1])
	}
}

func TestResolveDetectsCycle(t *testing.T) {
	loop := func(_ context.Context, id string) (*Version, error) {
		other := map[string]string{"a": "b", "b": "a"}[id]
		return &Version{ID: id, InheritsFrom: other}, nil
	}
	if _, err := Resolve(context.Background(), "a", loop); err == nil {
		t.Fatal("Resolve on a cycle = nil, want an error")
	}
}

func TestResolveBoundsDepth(t *testing.T) {
	// Each id inherits from a fresh one, so the chain never repeats and only
	// the depth limit can stop it.
	deep := func(_ context.Context, id string) (*Version, error) {
		return &Version{ID: id, InheritsFrom: id + "x"}, nil
	}
	if _, err := Resolve(context.Background(), "a", deep); err == nil {
		t.Fatal("Resolve on an endless chain = nil, want an error")
	}
}

func TestResolveWithoutParent(t *testing.T) {
	v, err := Resolve(context.Background(), "1.21.1", fixtureFetcher(t))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if v.MainClass != "net.minecraft.client.main.Main" {
		t.Errorf("mainClass = %q", v.MainClass)
	}
	if len(v.Libraries) != 3 {
		t.Errorf("libraries = %d, want 3", len(v.Libraries))
	}
}

// TestMergeLegacyArguments checks that the pre-1.13 argument string is
// replaced wholesale rather than concatenated.
func TestMergeLegacyArguments(t *testing.T) {
	parent := &Version{ID: "1.12.2", MinecraftArguments: "--username ${auth_player_name} --version ${version_name}"}
	child := &Version{ID: "1.12.2-forge", InheritsFrom: "1.12.2", MinecraftArguments: "--username ${auth_player_name} --tweakClass forge"}

	merged := Merge(child, parent)
	if merged.MinecraftArguments != child.MinecraftArguments {
		t.Errorf("legacy arguments = %q, want the child's verbatim", merged.MinecraftArguments)
	}

	// A child that says nothing inherits the parent's.
	silent := &Version{ID: "x", InheritsFrom: "1.12.2"}
	if got := Merge(silent, parent).MinecraftArguments; got != parent.MinecraftArguments {
		t.Errorf("legacy arguments = %q, want the parent's", got)
	}
}

func TestArgumentUnmarshal(t *testing.T) {
	var args []Argument
	body := `["--plain", {"rules":[{"action":"allow","os":{"name":"osx"}}],"value":"-XstartOnFirstThread"},
	          {"rules":[{"action":"allow"}],"value":["--width","${resolution_width}"]}]`
	if err := json.Unmarshal([]byte(body), &args); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(args) != 3 {
		t.Fatalf("got %d arguments, want 3", len(args))
	}
	if len(args[0].Value) != 1 || args[0].Value[0] != "--plain" || len(args[0].Rules) != 0 {
		t.Errorf("bare string decoded as %+v", args[0])
	}
	if len(args[1].Rules) != 1 || args[1].Value[0] != "-XstartOnFirstThread" {
		t.Errorf("single-value object decoded as %+v", args[1])
	}
	if len(args[2].Value) != 2 {
		t.Errorf("array-value object decoded as %+v", args[2])
	}
}

// --- helpers ---

func flatten(args []Argument) []string {
	var out []string
	for _, a := range args {
		out = append(out, a.Value...)
	}
	return out
}

func libNames(libs []Library) []string {
	out := make([]string, len(libs))
	for i, l := range libs {
		out[i] = l.Name
	}
	return out
}

func hasLib(libs []Library, name string) bool {
	for _, l := range libs {
		if l.Name == name {
			return true
		}
	}
	return false
}
