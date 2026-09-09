//go:build gui_screenshot

package gui

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/loader"
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
		{Name: "sebi-1.20.1", Path: "/home/gerry/.minecraft-instances/sebi-1.20.1", ModCount: 112, ConfigCount: 95, SaveCount: 13,
			Configured: true, MinecraftVersion: "1.20.1",
			Loader: instance.LoaderSpec{Type: instance.LoaderForge, Version: "47.4.0"}},
		{Name: "sebsmodpack4", Path: "/home/gerry/.minecraft-instances/sebsmodpack4", ModCount: 136, ConfigCount: 136, SaveCount: 75,
			Configured: true, MinecraftVersion: "1.21.1",
			Loader: instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.227"}},
		{Name: "sebsmodpack5", Path: "/home/gerry/.minecraft-instances/sebsmodpack5", ModCount: 153, DisabledMods: 2, ConfigCount: 135, SaveCount: 1, IsActive: true,
			Configured: true, MinecraftVersion: "1.21.1",
			Loader:     instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.248"},
			LastPlayed: time.Now().Add(-3 * time.Hour)},
		{Name: "vanilla", Path: "/home/gerry/.minecraft-instances/vanilla", Configured: true, MinecraftVersion: "1.21.8",
			Loader: instance.LoaderSpec{Type: instance.LoaderVanilla}},
		{Name: "fabric-test", Path: "/home/gerry/.minecraft-instances/fabric-test", ModCount: 11, ConfigCount: 103, SaveCount: 10,
			Configured: true, MinecraftVersion: "1.21.1",
			Loader: instance.LoaderSpec{Type: instance.LoaderFabric, Version: "0.16.9"}},
		{Name: "vanillaplus-test", Path: "/home/gerry/.minecraft-instances/vanillaplus-test", ModCount: 11, ConfigCount: 103, SaveCount: 10},
	}

	now := time.Now()
	mod := func(name string, mb float64, days int, off bool) instance.Entry {
		full := name
		if off {
			full += instance.DisabledSuffix
		}
		return instance.Entry{Name: full, Path: "/home/gerry/.minecraft-instances/sebsmodpack5/mods/" + full,
			Size: int64(mb * 1024 * 1024), ModTime: now.Add(-time.Duration(days) * 24 * time.Hour), Disabled: off}
	}
	titled := func(e instance.Entry, title, version string) instance.Entry {
		e.Title, e.Version = title, version
		return e
	}
	content := map[instance.ContentKind][]instance.Entry{
		instance.ContentMods: {
			mod("AmbientSounds_NEOFORGE_v6.1.4_mc1.21.1.jar", 1.9, 12, false),
			mod("appleskin-neoforge-mc1.21.1-3.0.6.jar", 0.2, 30, false),
			mod("architectury-13.0.8-neoforge.jar", 0.6, 30, false),
			mod("balm-neoforge-1.21.1-21.0.47.jar", 0.4, 9, false),
			mod("BetterF3-11.0.3-NeoForge-1.21.1.jar", 0.3, 40, true),
			titled(mod("create-1.21.1-6.0.6.jar", 24.8, 3, false), "Create", "6.0.6"),
			titled(mod("CreativeCore_NEOFORGE_v2.12.30_mc1.21.1.jar", 1.2, 12, false), "CreativeCore", "2.12.30"),
			titled(mod("curios-neoforge-9.5.1+1.21.1.jar", 0.5, 30, false), "Curios API", "9.5.1+1.21.1"),
			mod("EnchantmentDescriptions-NeoForge-1.21.1-21.1.6.jar", 0.1, 60, false),
			titled(mod("jei-1.21.1-neoforge-19.22.1.318.jar", 1.4, 5, false), "Just Enough Items", "19.22.1.318"),
			mod("modernfix-neoforge-5.24.0+mc1.21.1.jar", 0.9, 5, false),
			mod("sodium-neoforge-0.6.13+mc1.21.1.jar", 1.1, 5, true),
			mod("supplementaries-1.21.1-3.1.28-beta.jar", 9.7, 20, false),
			mod("xaerominimap-25.2.10_NeoForge_1.21.jar", 2.6, 2, false),
		},
		instance.ContentConfig: {
			{Name: "create-client.toml", Size: 3100, ModTime: now.Add(-2 * time.Hour)},
			{Name: "create-common.toml", Size: 900, ModTime: now.Add(-48 * time.Hour)},
			{Name: "jei/jei-client.toml", Size: 4800, ModTime: now.Add(-3 * time.Hour)},
			{Name: "jei/jei-mod-id-format.toml", Size: 400, ModTime: now.Add(-3 * time.Hour)},
			{Name: "xaerominimap.txt", Size: 1200, ModTime: now.Add(-30 * time.Minute)},
		},
		instance.ContentSaves: {
			{Name: "Sebis Welt", IsDir: true, ModTime: now.Add(-3 * time.Hour)},
		},
		instance.ContentResourcePacks: {
			{Name: "FreshAnimations_v1.10.4.zip", Size: 1_800_000, ModTime: now.Add(-9 * 24 * time.Hour)},
			{Name: "Fancy Crops v1.3.zip", Size: 130_000, ModTime: now.Add(-9 * 24 * time.Hour)},
		},
		instance.ContentShaderPacks: {
			{Name: "ComplementaryUnbound_r5.5.1.zip", Size: 2_100_000, ModTime: now.Add(-20 * 24 * time.Hour)},
		},
		instance.ContentScreenshots: {},
		instance.ContentLogs: {
			{Name: "latest.log", Size: 812_000, ModTime: now.Add(-3 * time.Hour)},
			{Name: "2026-09-08-1.log.gz", Size: 61_000, ModTime: now.Add(-26 * time.Hour)},
		},
		instance.ContentCrashReports: {},
	}

	return launcher.Snapshot{
		Screen:           launcher.ScreenInstances,
		Accounts:         []auth.Account{account},
		Active:           account,
		HasAccount:       true,
		Instances:        instances,
		Selected:         "sebsmodpack5",
		Content:          content,
		ContentFor:       "sebsmodpack5",
		ProfileInstalled: true,
		Options: instance.OptionsInfo{Path: "/home/gerry/.minecraft-instances/sebsmodpack5/options.txt",
			Exists: true, Size: 9500, ModTime: now.Add(-3 * time.Hour)},
		OptionsSnapshots: []instance.OptionsSnapshot{
			{Name: "20260909-181104--before restore.txt", Label: "before restore", Time: now.Add(-40 * time.Minute), Size: 9500, Changes: 0},
			{Name: "20260902-142210--keybinds sorted out.txt", Label: "keybinds sorted out", Time: now.Add(-7 * 24 * time.Hour), Size: 9300, Changes: 3},
			{Name: "20260814-201500.txt", Time: now.Add(-26 * 24 * time.Hour), Size: 9100, Changes: 17},
		},
		OptionsFor: "sebsmodpack5",
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
		Stats:  demoStats(now),
	}
}

// demoStats is a fortnight of playing, with one long weekend, so the panel
// is rendered against a chart that actually varies.
func demoStats(now time.Time) instance.PlayStats {
	play := func(name string, loader instance.LoaderType, hours float64, sessions int, days float64) instance.InstancePlay {
		return instance.InstancePlay{Name: name, Loader: instance.LoaderSpec{Type: loader},
			Total:    time.Duration(hours * float64(time.Hour)),
			Sessions: sessions, Last: now.Add(-time.Duration(days * float64(24*time.Hour)))}
	}
	stats := instance.PlayStats{
		Sessions:  61,
		Last:      now.Add(-4 * time.Minute),
		First:     now.Add(-90 * 24 * time.Hour),
		Longest:   5*time.Hour + 40*time.Minute,
		LongestOn: "sebsmodpack5",
		Instances: []instance.InstancePlay{
			play("sebsmodpack5", instance.LoaderNeoForge, 92.5, 38, 0),
			play("sebsmodpack4", instance.LoaderNeoForge, 41, 14, 12),
			play("sebi-1.20.1", instance.LoaderForge, 12.25, 6, 30),
			play("fabric-test", instance.LoaderFabric, 2, 2, 44),
			play("vanilla", instance.LoaderVanilla, 0.75, 1, 51),
		},
	}
	hours := []float64{0, 1.5, 0, 0, 3.25, 5.5, 4, 0, 0, 2, 1.25, 0, 3.75, 2.5}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i, h := range hours {
		day := today.AddDate(0, 0, -(len(hours) - 1 - i))
		stats.Days = append(stats.Days, instance.DayPlay{Day: day, Total: time.Duration(h * float64(time.Hour))})
	}
	var busiest time.Duration
	for _, p := range stats.Instances {
		stats.Total += p.Total
		if p.Total > busiest {
			busiest = p.Total
		}
	}
	for i := range stats.Instances {
		stats.Instances[i].Share = float64(stats.Instances[i].Total) / float64(busiest)
	}
	return stats
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

	empty := demoSnapshot()
	empty.Instances = nil
	empty.Selected = ""
	empty.Stats = instance.PlayStats{}

	unselected := demoSnapshot()
	unselected.Selected = ""

	adopting := empty
	adopting.Task = launcher.Task{ID: 4, Kind: launcher.TaskAdopt, Label: "Adopting your .minecraft as Default",
		Phase: "Sharing its game files", Message: "1204 files, 612 MB", Started: time.Now()}

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

	// Interface states that live in widgets rather than in the snapshot.
	overviewTab := func(u *ui) { u.bench.shownFor = "sebsmodpack5"; u.bench.tab = 0 }
	modsTab := func(u *ui) { u.bench.shownFor = "sebsmodpack5"; u.bench.tab = 1 }
	configTab := func(u *ui) { u.bench.shownFor = "sebsmodpack5"; u.bench.tab = 2 }
	worldsTab := func(u *ui) { u.bench.shownFor = "sebsmodpack5"; u.bench.tab = 3 }
	instanceSettings := func(u *ui) { u.bench.shownFor = "sebsmodpack5"; u.bench.tab = len(benchTabs()) - 1 }
	confirmDelete := func(u *ui) {
		modsTab(u)
		u.bench.content[instance.ContentMods].confirming = "create-1.21.1-6.0.6.jar"
	}
	createDialog := func(u *ui) { u.dialogs.openCreate(demoSnapshot(), "") }
	duplicateDialog := func(u *ui) { u.dialogs.openCreate(demoSnapshot(), "sebsmodpack5") }
	deleteDialog := func(u *ui) { u.dialogs.openDelete(demoSnapshot().Instances[1]) }
	filtered := func(u *ui) { modsTab(u); u.bench.content[instance.ContentMods].filter.SetText("neoforge 1.21") }
	pickerLoader := func(u *ui) {
		u.dialogs.openCreate(demoSnapshot(), "")
		u.dialogs.pickLoaderVersion(u, instance.LoaderNeoForge, "1.21.1", "21.1.248", func(string) {})
	}
	contextMenu := func(u *ui) {
		snap := demoSnapshot()
		u.pointer = image.Pt(150, 205)
		u.openInstanceMenu(snap, snap.Instances[1])
	}
	contextMenuActive := func(u *ui) {
		snap := demoSnapshot()
		u.pointer = image.Pt(150, 268)
		u.openInstanceMenu(snap, snap.Instances[2])
	}
	optionsCard := func(u *ui) {
		instanceSettings(u)
		u.bench.settings.list.Position.First = 2
	}
	pickerFetching := func(u *ui) {
		u.dialogs.openCreate(demoSnapshot(), "")
		u.dialogs.pickLoaderVersion(u, instance.LoaderForge, "1.20.1", "", func(string) {})
	}

	withVersions := demoSnapshot()
	withVersions.LoaderVersions = map[string][]loader.Version{
		launcher.VersionsKey(instance.LoaderNeoForge, "1.21.1"): {
			{Version: "21.1.250", Stable: true}, {Version: "21.1.249-beta"}, {Version: "21.1.248", Stable: true},
			{Version: "21.1.247", Stable: true}, {Version: "21.1.246", Stable: true}, {Version: "21.1.209", Stable: true},
		},
	}
	fetching := demoSnapshot()
	fetching.VersionsPending = map[string]bool{launcher.VersionsKey(instance.LoaderForge, "1.20.1"): true}

	notInstalled := demoSnapshot()
	notInstalled.ProfileInstalled = false
	installing := notInstalled
	installing.Task = launcher.Task{ID: 3, Kind: launcher.TaskInstall, Label: "Installing NeoForge 21.1.248",
		Phase: "Installing NeoForge 21.1.248", Message: "Processor: net.minecraftforge:binarypatcher", Started: time.Now()}

	cases := []struct {
		name  string
		snap  launcher.Snapshot
		setup func(*ui)
	}{
		{"instances", demoSnapshot(), overviewTab},
		{"mods", demoSnapshot(), modsTab},
		{"instances-filtered", demoSnapshot(), filtered},
		{"instances-confirm-delete", demoSnapshot(), confirmDelete},
		{"configs", demoSnapshot(), configTab},
		{"worlds", demoSnapshot(), worldsTab},
		{"instance-settings", demoSnapshot(), instanceSettings},
		{"instance-options", demoSnapshot(), optionsCard},
		{"context-menu", demoSnapshot(), contextMenu},
		{"context-menu-active", demoSnapshot(), contextMenuActive},
		{"adopting", adopting, nil},
		{"dialog-create", demoSnapshot(), createDialog},
		{"picker-loader", withVersions, pickerLoader},
		{"picker-fetching", fetching, pickerFetching},
		{"not-installed", notInstalled, instanceSettings},
		{"installing", installing, nil},
		{"dialog-duplicate", demoSnapshot(), duplicateDialog},
		{"dialog-delete", demoSnapshot(), deleteDialog},
		{"launching", launching, nil},
		{"running", running, nil},
		{"empty", empty, nil},
		{"unselected", unselected, nil},
		{"login", login, nil},
		{"login-first-run", firstRun, nil},
		{"login-device-code", deviceCode, nil},
		{"settings", settings, nil},
	}

	for _, tc := range cases {
		path := filepath.Join(dir, tc.name+".png")
		if err := Screenshot(path, 1180, 760, tc.snap, tc.setup); err != nil {
			t.Fatalf("rendering %s: %v", tc.name, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("%s produced no image", tc.name)
		}
		t.Logf("%-10s %s (%d bytes)", tc.name, path, info.Size())
	}

	// A short window drops the daily chart rather than pushing the panel
	// off the bottom; render it too, so that path is exercised.
	short := filepath.Join(dir, "unselected-short.png")
	if err := Screenshot(short, 1180, 700, unselected, nil); err != nil {
		t.Fatalf("rendering unselected-short: %v", err)
	}
}
