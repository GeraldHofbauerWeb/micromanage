package mods

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
)

func writeJar(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for n, content := range files {
		e, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		e.Write([]byte(content))
	}
	w.Close()
	f.Close()
	return path
}

func TestReadManifests(t *testing.T) {
	dir := t.TempDir()
	neo := writeJar(t, dir, "create.jar", map[string]string{
		"META-INF/neoforge.mods.toml": `modLoader="javafml"
loaderVersion="[4,)"
[[mods]]
modId="create"
version="6.0.6"
displayName="Create"
[[dependencies.create]]
modId="neoforge"
`,
	})
	fabric := writeJar(t, dir, "sodium.jar", map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"sodium","name":"Sodium","version":"0.6.13"}`,
	})
	quilt := writeJar(t, dir, "q.jar", map[string]string{
		"quilt.mod.json": `{"quilt_loader":{"id":"qmod","version":"1.0","metadata":{"name":"Quilt Mod"}}}`,
	})
	forgeUnexpanded := writeJar(t, dir, "old.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId=\"jei\"\nversion=\"${file.jarVersion}\"\ndisplayName=\"Just Enough Items\"\n",
	})
	library := writeJar(t, dir, "lib.jar", map[string]string{"com/x/Y.class": ""})

	cases := []struct {
		path string
		want Info
	}{
		{neo, Info{ID: "create", Name: "Create", Version: "6.0.6", Loader: "neoforge"}},
		{fabric, Info{ID: "sodium", Name: "Sodium", Version: "0.6.13", Loader: "fabric"}},
		{quilt, Info{ID: "qmod", Name: "Quilt Mod", Version: "1.0", Loader: "quilt"}},
		{forgeUnexpanded, Info{ID: "jei", Name: "Just Enough Items", Loader: "forge"}},
		{library, Info{}},
	}
	for _, tc := range cases {
		got, err := Read(tc.path)
		if err != nil {
			t.Errorf("Read(%s): %v", filepath.Base(tc.path), err)
			continue
		}
		if got != tc.want {
			t.Errorf("Read(%s) = %+v, want %+v", filepath.Base(tc.path), got, tc.want)
		}
	}
	if _, err := Read(filepath.Join(dir, "missing.jar")); err == nil {
		t.Error("a missing jar was read")
	}
}

func TestGuessName(t *testing.T) {
	cases := map[string]string{
		"/m/create-1.21.1-6.0.6.jar":                 "create",
		"AmbientSounds_NEOFORGE_v6.1.4_mc1.21.1.jar": "AmbientSounds NEOFORGE v6.1.4 mc1.21.1",
		"jei-1.21.1-neoforge-19.22.1.318.jar":        "jei",
		"sodium.jar.disabled":                        "sodium",
	}
	for in, want := range cases {
		if got := guessName(in); got != want {
			t.Errorf("guessName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindPage(t *testing.T) {
	dir := t.TempDir()
	known := writeJar(t, dir, "known.jar", map[string]string{"a": "known bytes"})
	byName := writeJar(t, dir, "byname.jar", map[string]string{"b": "other bytes"})
	unknown := writeJar(t, dir, "unknown.jar", map[string]string{"c": "nothing"})
	knownSHA := download.FileSHA1(known)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/version_file/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version_file/"+knownSHA {
			w.Write([]byte(`{"project_id":"LNytGWDc"}`))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/project/LNytGWDc", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"slug":"create","title":"Create"}`))
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case "All The Leaks":
			w.Write([]byte(`{"hits":[{"slug":"all-the-leaks","title":"All The Leaks"}]}`))
		default:
			w.Write([]byte(`{"hits":[]}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := NewPageFinder(nil)
	f.Endpoints = Endpoints{
		ModrinthAPI: srv.URL + "/api", ModrinthWeb: "https://modrinth.com/mod/",
		CurseForgeSearch: "https://cf/search?q=",
	}
	ctx := context.Background()

	page, err := f.Find(ctx, known, Info{})
	if err != nil || page.URL != "https://modrinth.com/mod/create" || !page.Exact {
		t.Errorf("by hash: %+v, %v", page, err)
	}
	page, err = f.Find(ctx, byName, Info{ID: "alltheleaks", Name: "All The Leaks"})
	if err != nil || page.URL != "https://modrinth.com/mod/all-the-leaks" || page.Exact {
		t.Errorf("by name: %+v, %v", page, err)
	}
	page, err = f.Find(ctx, unknown, Info{Name: "Armor Damage Scale"})
	if err != nil || page.Site != "CurseForge" || page.URL != "https://cf/search?q=Armor+Damage+Scale" {
		t.Errorf("fallback: %+v, %v", page, err)
	}
}
