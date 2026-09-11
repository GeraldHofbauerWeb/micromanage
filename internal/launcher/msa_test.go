package launcher

import (
	"path/filepath"
	"testing"
)

// TestMSAClientIDTakesEffectWhenSaved sets the application id in Settings and
// expects Microsoft sign-in to be offered at once rather than after a restart.
func TestMSAClientIDTakesEffectWhenSaved(t *testing.T) {
	t.Setenv("INSTANT_LAUNCHER_MSA_CLIENT_ID", "")
	t.Setenv("MIM_MSA_CLIENT_ID", "")
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)
	m.ConfigFile = filepath.Join(m.AppDir, "config.json")
	ctrl.MSAClientID = ""

	const id = "00000000-0000-0000-0000-000000000001"
	ctrl.Dispatch(ActionSetConfig{Key: "msa-client-id", Value: " " + id + " "})
	snap := waitFor(t, ctrl, "sign-in offered", func(s Snapshot) bool { return s.MSAConfigured })
	if snap.Err != nil {
		t.Fatal(snap.Err)
	}
	if got := ctrl.msaClientID(); got != id {
		t.Errorf("sign-in runs against %q, want %q", got, id)
	}
}
