//go:build gui_screenshot

package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
)

// outDir is where the rendered frames are written.
func outDir(t *testing.T) string {
	dir := os.Getenv("GUI_SHOT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	return dir
}

// demoSnapshot mirrors the real machine this was developed against, so the
// rendered frames show the layout under realistic text lengths rather than
// convenient short ones.
func demoSnapshot() launcher.Snapshot {
	account := auth.Account{Kind: auth.KindOffline, Name: "Gerry", UUID: auth.OfflineUUID("Gerry")}

	instances := []instance.Instance{
		{Name: "sebsmodpack5", ModCount: 155, ConfigCount: 135, SaveCount: 1, IsActive: true,
			Configured: true, MinecraftVersion: "1.21.1",
			Loader:     instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.248"},
			LastPlayed: time.Now().Add(-3 * time.Hour)},
		{Name: "sebsmodpack4", ModCount: 136, ConfigCount: 136, SaveCount: 75,
			Configured: true, MinecraftVersion: "1.21.1",
			Loader: instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.227"}},
		{Name: "sebi-1.20.1", ModCount: 112, ConfigCount: 95, SaveCount: 13,
			Configured: true, MinecraftVersion: "1.20.1",
			Loader: instance.LoaderSpec{Type: instance.LoaderForge, Version: "47.4.0"}},
		{Name: "vanillaplus-test", ModCount: 11, ConfigCount: 103, SaveCount: 10},
	}

	return launcher.Snapshot{
		Screen:     launcher.ScreenInstances,
		Accounts:   []auth.Account{account},
		Active:     account,
		HasAccount: true,
		Instances:  instances,
		Selected:   "sebsmodpack5",
		Editing: instance.Meta{
			Name:             "sebsmodpack5",
			MinecraftVersion: "1.21.1",
			Loader:           instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.248"},
			Memory:           instance.MemSpec{MaxMB: 8192},
		},
		EditingOK: true,
		Runtimes: []java.Runtime{
			{Path: "/home/gerry/.config/minecraft-instance/shared/runtimes/java-runtime-delta/linux/java-runtime-delta/bin/java",
				Major: 21, FullVersion: "21.0.7", Vendor: "Microsoft", Component: "java-runtime-delta", Source: "shared store"},
			{Path: "/usr/bin/java", Major: 21, FullVersion: "21.0.12.1", Vendor: "OpenJDK", Source: "PATH"},
		},
		Config: map[string]string{
			"minecraft-path": "/home/gerry/.minecraft",
			"instances-path": "/home/gerry/.minecraft-instances",
			"backup-path":    "/home/gerry/.config/minecraft-instance/backup",
		},
		Status: "6 instances",
	}
}

// TestRenderScreens draws each screen offscreen. It is a smoke test as much as
// a screenshot tool: a layout that panics or produces an impossible constraint
// fails here instead of in front of a user.
func TestRenderScreens(t *testing.T) {
	dir := outDir(t)

	launching := demoSnapshot()
	launching.Task = launcher.Task{
		ID: 1, Kind: launcher.TaskLaunch, Label: "Launching sebsmodpack5",
		Phase: "assets", Steps: []string{"metadata", "client", "libraries", "natives"},
		Progress: download.Progress{FilesDone: 2143, FilesTotal: 3888,
			BytesDone: 412 << 20, BytesTotal: 690 << 20},
		Started: time.Now(),
	}

	running := demoSnapshot()
	running.Game = launcher.GameState{
		Instance: "sebsmodpack5", PID: 48211, Running: true, Started: time.Now(),
		Tail: []string{"[Render thread/INFO]: Setting user: Gerry"},
	}

	login := demoSnapshot()
	login.Screen = launcher.ScreenLogin
	login.MSAConfigured = true

	// The two states a first sign-in passes through: the choice, and the code
	// the player has to carry to a browser.
	firstRun := login
	firstRun.Accounts = nil
	firstRun.Active = auth.Account{}
	firstRun.HasAccount = false

	deviceCode := firstRun
	deviceCode.Login = launcher.LoginState{
		Active:          true,
		Task:            2,
		UserCode:        "K7QM-HZ4T",
		VerificationURI: "https://www.microsoft.com/link",
		ExpiresAt:       time.Now().Add(13*time.Minute + 20*time.Second),
		Step:            "Waiting for you to enter the code",
	}

	settings := demoSnapshot()
	settings.Screen = launcher.ScreenSettings
	const gb = int64(1) << 30
	settings.Reclaimable = map[string]int64{
		"sebsmodpack5": 4*gb + gb/3, "sebsmodpack4": 4*gb + gb/10,
		"sebi-1.20.1": 3 * gb, "vanillaplus-test": 2*gb + gb/2,
	}

	cases := []struct {
		name string
		snap launcher.Snapshot
	}{
		{"instances", demoSnapshot()},
		{"launching", launching},
		{"running", running},
		{"login", login},
		{"login-first-run", firstRun},
		{"login-device-code", deviceCode},
		{"settings", settings},
	}

	for _, tc := range cases {
		path := filepath.Join(dir, tc.name+".png")
		if err := Screenshot(path, 1100, 720, tc.snap); err != nil {
			t.Fatalf("rendering %s: %v", tc.name, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("%s produced no image", tc.name)
		}
		t.Logf("%-10s %s (%d bytes)", tc.name, path, info.Size())
	}
}
