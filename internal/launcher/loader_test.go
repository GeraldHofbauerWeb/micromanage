package launcher

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/loader"
)

// stubLoaderServices answers like Fabric's meta service.
func stubLoaderServices(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/fabric/versions/loader/1.21.1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"loader":{"version":"0.16.9","stable":true}},{"loader":{"version":"0.16.8","stable":true}}]`))
	})
	mux.HandleFunc("/fabric/versions/loader/1.21.1/0.16.9/profile/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"fabric-loader-0.16.9-1.21.1","inheritsFrom":"1.21.1","mainClass":"knot","libraries":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestCreateInstallsItsLoader: making a Fabric instance ends with the
// profile in the store, and the workbench knows it.
func TestCreateInstallsItsLoader(t *testing.T) {
	ctrl := newTestController(t, nil)
	isolateInstances(t, ctrl)
	srv := stubLoaderServices(t)
	ctrl.Loaders.Endpoints = loader.Endpoints{FabricMeta: srv.URL + "/fabric"}

	ctrl.Dispatch(ActionCreate{
		Name: "fab", Version: "1.21.1",
		Loader: instance.LoaderSpec{Type: instance.LoaderFabric, Version: "0.16.9"},
	})
	waitFor(t, ctrl, "the instance selected with its loader installed", func(s Snapshot) bool {
		return s.Selected == "fab" && s.ProfileInstalled && s.Task.Done
	})
	if _, err := os.Stat(ctrl.Layout.VersionJSON("fabric-loader-0.16.9-1.21.1")); err != nil {
		t.Fatalf("profile not in the store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctrl.Manager.InstancesPath, "fab", "mods")); err != nil {
		t.Fatal("instance directory missing")
	}
	if snap := ctrl.Store().Snapshot(); snap.Task.Err != nil {
		t.Errorf("task failed: %v", snap.Task.Err)
	}
}

// TestListVersionsPublishesAndClearsPending: the picker's "loading" state
// must end whether the fetch works or not.
func TestListVersionsPublishesAndClearsPending(t *testing.T) {
	ctrl := newTestController(t, nil)
	srv := stubLoaderServices(t)
	ctrl.Loaders.Endpoints = loader.Endpoints{FabricMeta: srv.URL + "/fabric", QuiltMeta: srv.URL + "/quilt"}

	ctrl.Dispatch(ActionListVersions{Kind: instance.LoaderFabric, MC: "1.21.1"})
	key := VersionsKey(instance.LoaderFabric, "1.21.1")
	snap := waitFor(t, ctrl, "the fabric list", func(s Snapshot) bool {
		return len(s.LoaderVersions[key]) == 2
	})
	if snap.VersionsPending[key] {
		t.Error("pending flag still set after the list arrived")
	}
	if snap.LoaderVersions[key][0].Version != "0.16.9" {
		t.Errorf("list = %+v", snap.LoaderVersions[key])
	}

	// Quilt's list is not served: the flag must clear and an error surface.
	ctrl.Dispatch(ActionListVersions{Kind: instance.LoaderQuilt, MC: "1.21.1"})
	qkey := VersionsKey(instance.LoaderQuilt, "1.21.1")
	waitFor(t, ctrl, "the quilt failure", func(s Snapshot) bool {
		return s.Err != nil && !s.VersionsPending[qkey]
	})
}

// TestSelectReportsMissingProfile: an instance whose loader was never
// installed says so, so the workbench can offer to install it.
func TestSelectReportsMissingProfile(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	dir := filepath.Join(m.InstancesPath, "neo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := instance.DefaultMeta("neo")
	meta.MinecraftVersion = "1.21.1"
	meta.Loader = instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.248"}
	if err := instance.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}

	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "neo"})
	snap := waitFor(t, ctrl, "selection", func(s Snapshot) bool { return s.Selected == "neo" })
	if snap.ProfileInstalled {
		t.Error("a NeoForge profile that was never installed was reported installed")
	}

	// Drop the profile in by hand: now it is.
	if err := os.MkdirAll(filepath.Dir(ctrl.Layout.VersionJSON("neoforge-21.1.248")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrl.Layout.VersionJSON("neoforge-21.1.248"), []byte(`{"id":"neoforge-21.1.248"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl.Dispatch(ActionSelect{Name: "neo"})
	waitFor(t, ctrl, "installed flag", func(s Snapshot) bool { return s.ProfileInstalled })
}
