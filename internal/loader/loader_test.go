package loader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
)

// stubServices serves the shapes each loader project publishes.
func stubServices(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/fabric/versions/loader/1.21.1", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`[{"loader":{"version":"0.17.0","stable":true}},{"loader":{"version":"0.16.9","stable":true}},{"loader":{"version":"0.16.8-rc1","stable":false}}]`))
	})
	mux.HandleFunc("/fabric/versions/loader/1.21.1/0.16.9/profile/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"fabric-loader-0.16.9-1.21.1","inheritsFrom":"1.21.1","mainClass":"net.fabricmc.loader.impl.launch.knot.KnotClient","libraries":[]}`))
	})
	mux.HandleFunc("/neo/maven-metadata.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<metadata><versioning><versions>
			<version>21.1.9</version><version>21.1.248</version><version>21.1.100-beta</version>
			<version>21.0.167</version><version>26.2.0.83</version><version>21.1.250</version>
		</versions></versioning></metadata>`))
	})
	mux.HandleFunc("/forge/maven-metadata.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<metadata><versioning><versions>
			<version>1.20.1-47.4.0</version><version>1.21.1-52.0.31</version><version>1.21.1-52.1.16</version>
		</versions></versioning></metadata>`))
	})
	return httptest.NewServer(mux)
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	layout := launch.NewLayout(t.TempDir())
	c := NewClient(layout, nil)
	c.Endpoints = Endpoints{
		FabricMeta:    srv.URL + "/fabric",
		QuiltMeta:     srv.URL + "/quilt",
		NeoForgeMaven: srv.URL + "/neo",
		ForgeMaven:    srv.URL + "/forge",
	}
	return c
}

func versionStrings(vs []Version) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Version)
	}
	return out
}

func TestVersionsPerLoader(t *testing.T) {
	var hits atomic.Int64
	srv := stubServices(t, &hits)
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	fabric, err := c.Versions(ctx, instance.LoaderFabric, "1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := versionStrings(fabric); len(got) != 3 || got[0] != "0.17.0" || fabric[2].Stable {
		t.Errorf("fabric = %v (stable flags %v)", got, fabric)
	}

	// NeoForge's maven is unsorted and mixes every Minecraft version; only
	// 21.1.x belongs to 1.21.1, newest first, betas flagged.
	neo, err := c.Versions(ctx, instance.LoaderNeoForge, "1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"21.1.250", "21.1.248", "21.1.100-beta", "21.1.9"}
	if got := versionStrings(neo); len(got) != len(want) {
		t.Fatalf("neoforge = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("neoforge[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	}
	if neo[2].Stable {
		t.Error("a beta was marked stable")
	}

	forge, err := c.Versions(ctx, instance.LoaderForge, "1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := versionStrings(forge); len(got) != 2 || got[0] != "52.1.16" || got[1] != "52.0.31" {
		t.Errorf("forge = %v", got)
	}

	if _, err := c.Versions(ctx, instance.LoaderVanilla, "1.21.1"); err == nil {
		t.Error("vanilla has no loader versions")
	}
}

func TestVersionsAreCachedForAnHour(t *testing.T) {
	var hits atomic.Int64
	srv := stubServices(t, &hits)
	defer srv.Close()
	c := newTestClient(t, srv)
	now := time.Now()
	c.Now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := c.Versions(ctx, instance.LoaderFabric, "1.21.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Versions(ctx, instance.LoaderFabric, "1.21.1"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Errorf("service asked %d times within the hour, want 1", hits.Load())
	}

	now = now.Add(2 * time.Hour)
	if _, err := c.Versions(ctx, instance.LoaderFabric, "1.21.1"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Errorf("stale cache was not refreshed (%d hits)", hits.Load())
	}

	// Offline: the stale list is better than an error.
	srv.Close()
	now = now.Add(2 * time.Hour)
	got, err := c.Versions(ctx, instance.LoaderFabric, "1.21.1")
	if err != nil || len(got) != 3 {
		t.Errorf("offline fallback: %v, %v", got, err)
	}
}

func TestNeoForgePrefix(t *testing.T) {
	cases := map[string]string{
		"1.21.1": "21.1.", "1.21": "21.0.", "1.20.6": "20.6.", "26.2": "26.2.", "26.1.2": "26.1.2.",
	}
	for mc, want := range cases {
		if got := neoForgePrefix(mc); got != want {
			t.Errorf("neoForgePrefix(%q) = %q, want %q", mc, got, want)
		}
	}
}

func TestVersionID(t *testing.T) {
	cases := []struct {
		kind  instance.LoaderType
		mc, v string
		want  string
	}{
		{instance.LoaderFabric, "1.21.1", "0.16.9", "fabric-loader-0.16.9-1.21.1"},
		{instance.LoaderQuilt, "1.21.1", "0.27.1", "quilt-loader-0.27.1-1.21.1"},
		{instance.LoaderNeoForge, "1.21.1", "21.1.248", "neoforge-21.1.248"},
		{instance.LoaderForge, "1.20.1", "47.4.0", "1.20.1-forge-47.4.0"},
		{instance.LoaderVanilla, "1.21.1", "", "1.21.1"},
	}
	for _, tc := range cases {
		if got := VersionID(tc.kind, tc.mc, tc.v); got != tc.want {
			t.Errorf("VersionID(%s) = %q, want %q", tc.kind, got, tc.want)
		}
		// The launcher must recognise what it installed.
		if tc.kind != instance.LoaderVanilla {
			if spec := instance.ParseLoaderID(tc.want); spec.Type != tc.kind || spec.Version != tc.v {
				t.Errorf("ParseLoaderID(%q) = %+v, does not round-trip", tc.want, spec)
			}
		}
	}
}

func TestInstallFabricWritesTheProfile(t *testing.T) {
	var hits atomic.Int64
	srv := stubServices(t, &hits)
	defer srv.Close()
	c := newTestClient(t, srv)

	id, err := c.Install(context.Background(), instance.LoaderFabric, "1.21.1", "0.16.9", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "fabric-loader-0.16.9-1.21.1" {
		t.Errorf("id = %q", id)
	}
	if !c.Installed(id) {
		t.Fatal("profile not written")
	}
	data, _ := os.ReadFile(c.Layout.VersionJSON(id))
	if len(data) == 0 {
		t.Fatal("empty profile")
	}

	// A second install is a no-op even with the service gone.
	srv.Close()
	if _, err := c.Install(context.Background(), instance.LoaderFabric, "1.21.1", "0.16.9", "", nil); err != nil {
		t.Errorf("reinstall hit the network: %v", err)
	}
	// Vanilla needs nothing.
	if id, err := c.Install(context.Background(), instance.LoaderVanilla, "1.21.1", "", "", nil); err != nil || id != "1.21.1" {
		t.Errorf("vanilla install = %q, %v", id, err)
	}
}

func TestInstallerNeedsJava(t *testing.T) {
	var hits atomic.Int64
	srv := stubServices(t, &hits)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Install(context.Background(), instance.LoaderNeoForge, "1.21.1", "21.1.248", "", nil); err == nil {
		t.Error("NeoForge installed without Java")
	}
	if _, err := os.Stat(filepath.Join(c.Layout.Loaders())); err == nil {
		t.Error("nothing should have been downloaded without Java")
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("21.1.250", "21.1.9") <= 0 {
		t.Error("numeric compare failed")
	}
	if compareVersions("21.1.100-beta", "21.1.100") >= 0 {
		t.Error("beta should sort below the release")
	}
	if compareVersions("26.2.0.83", "26.2.0.9") <= 0 {
		t.Error("four-part compare failed")
	}
}
