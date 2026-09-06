package instance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadMetaMissingFileWritesNothing is the guard for the lazy-migration
// rule: reading an instance that predates instance.json must not create one,
// so `list` never touches the disk.
func TestLoadMetaMissingFileWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	meta, found, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if found {
		t.Error("found = true for an instance without metadata")
	}
	if meta.Name != "legacy" {
		t.Errorf("name = %q, want legacy", meta.Name)
	}
	if meta.Configured() {
		t.Error("instance without metadata reported as configured")
	}
	if meta.Loader.Type != LoaderVanilla {
		t.Errorf("default loader = %v, want vanilla", meta.Loader.Type)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("LoadMeta created %d file(s); it must be read-only", len(entries))
	}
}

func TestSaveLoadMetaRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	want := DefaultMeta("pack")
	want.MinecraftVersion = "1.21.1"
	want.Loader = LoaderSpec{Type: LoaderNeoForge, Version: "21.1.248"}
	want.Memory = MemSpec{MinMB: 1024, MaxMB: 8192}
	want.JVMArgs = []string{"-XX:+UseG1GC"}
	want.ResolvedVersionID = "neoforge-21.1.248"
	want.LastPlayed = time.Date(2026, 9, 6, 1, 48, 21, 0, time.UTC)

	if err := SaveMeta(dir, want); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	got, found, err := LoadMeta(dir)
	if err != nil || !found {
		t.Fatalf("LoadMeta after save: found=%v err=%v", found, err)
	}
	if got.MinecraftVersion != want.MinecraftVersion {
		t.Errorf("minecraft = %q, want %q", got.MinecraftVersion, want.MinecraftVersion)
	}
	if got.Loader != want.Loader {
		t.Errorf("loader = %v, want %v", got.Loader, want.Loader)
	}
	if got.Memory != want.Memory {
		t.Errorf("memory = %v, want %v", got.Memory, want.Memory)
	}
	if !got.LastPlayed.Equal(want.LastPlayed) {
		t.Errorf("last played = %v, want %v", got.LastPlayed, want.LastPlayed)
	}
	if got.SchemaVersion != MetaSchemaVersion {
		t.Errorf("schema = %d, want %d", got.SchemaVersion, MetaSchemaVersion)
	}
	if got.Created.IsZero() {
		t.Error("Created was not stamped on save")
	}

	// No temp file should survive the atomic write.
	if _, err := os.Stat(filepath.Join(dir, MetaFileName+".tmp")); !os.IsNotExist(err) {
		t.Error("temporary file left behind")
	}
}

// TestSaveMetaRefusesNewerSchema stops an older build from truncating settings
// a newer one wrote.
func TestSaveMetaRefusesNewerSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	future := `{"schema_version": 99, "name": "pack", "minecraft_version": "1.30", "something_new": true}`
	path := filepath.Join(dir, MetaFileName)
	if err := os.WriteFile(path, []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveMeta(dir, DefaultMeta("pack")); err == nil {
		t.Fatal("SaveMeta over a newer schema = nil, want an error")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != future {
		t.Error("the newer file was modified despite the refusal")
	}
}

// TestLoadMetaPartialFile checks that a file carrying only some fields leaves
// the rest at their defaults rather than failing.
func TestLoadMetaPartialFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := `{"schema_version": 1, "minecraft_version": "1.20.1"}`
	if err := os.WriteFile(filepath.Join(dir, MetaFileName), []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, found, err := LoadMeta(dir)
	if err != nil || !found {
		t.Fatalf("LoadMeta: found=%v err=%v", found, err)
	}
	if meta.MinecraftVersion != "1.20.1" {
		t.Errorf("minecraft = %q, want 1.20.1", meta.MinecraftVersion)
	}
	if meta.Loader.Type != LoaderVanilla {
		t.Errorf("loader = %q, want vanilla by default", meta.Loader.Type)
	}
	if meta.Name != "pack" {
		t.Errorf("name = %q, want the directory name", meta.Name)
	}
}

// TestLoadMetaNameFollowsDirectory covers a renamed instance: the directory
// name wins over a stale name in the file.
func TestLoadMetaNameFollowsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "renamed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MetaFileName),
		[]byte(`{"schema_version":1,"name":"old-name"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, _, err := LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "renamed" {
		t.Errorf("name = %q, want renamed", meta.Name)
	}
}

func TestLoadMetaCorruptFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MetaFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := LoadMeta(dir); err == nil {
		t.Fatal("LoadMeta on corrupt JSON = nil, want an error")
	}
}

func TestLoaderSpecString(t *testing.T) {
	cases := []struct {
		spec LoaderSpec
		want string
	}{
		{LoaderSpec{Type: LoaderNeoForge, Version: "21.1.248"}, "NeoForge 21.1.248"},
		{LoaderSpec{Type: LoaderForge, Version: "47.4.0"}, "Forge 47.4.0"},
		{LoaderSpec{Type: LoaderVanilla}, "Vanilla"},
		{LoaderSpec{}, "Vanilla"},
	}
	for _, tc := range cases {
		if got := tc.spec.String(); got != tc.want {
			t.Errorf("LoaderSpec%v.String() = %q, want %q", tc.spec, got, tc.want)
		}
	}
}

func TestManagerMetaRoundTrip(t *testing.T) {
	m := newTestManager(t)
	mkInstance(t, m, "pack")

	meta, err := m.GetMeta("pack")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	meta.MinecraftVersion = "1.21.1"
	meta.Loader = LoaderSpec{Type: LoaderFabric, Version: "0.15.7"}

	if err := m.SetMeta("pack", meta); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	got, err := m.GetMeta("pack")
	if err != nil {
		t.Fatal(err)
	}
	if got.MinecraftVersion != "1.21.1" || got.Loader.Type != LoaderFabric {
		t.Errorf("round trip lost data: %+v", got)
	}

	// Metadata now shows up in the listing.
	instances, err := m.ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(instances))
	}
	if !instances[0].Configured || instances[0].MinecraftVersion != "1.21.1" {
		t.Errorf("ListInstances did not surface the metadata: %+v", instances[0])
	}

	if err := m.SetMeta("missing", meta); err == nil {
		t.Error("SetMeta on a missing instance = nil, want an error")
	}
	if err := m.SetMeta("../escape", meta); err == nil {
		t.Error("SetMeta with a traversing name = nil, want an error")
	}
}
