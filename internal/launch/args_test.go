package launch

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/mojang"
)

var testPlatform = mojang.Platform{OS: "linux", Arch: "x86_64", Version: "6.9.3"}

func testSession() Session {
	return Session{
		PlayerName:  "Gerry",
		UUID:        "0123456789abcdef0123456789abcdef",
		AccessToken: "tok",
		XUID:        "2535000000000000",
		UserType:    "msa",
		ClientID:    "cid",
	}
}

func testOptions() Options {
	return Options{
		Session:      testSession(),
		GameDir:      "/home/gerry/.minecraft",
		LauncherName: "instant-mc",
		LauncherVer:  "2.0.0",
		MinMB:        1024,
		MaxMB:        8192,
	}
}

func testPrepared(v *mojang.Version) *Prepared {
	return &Prepared{
		Version:      v,
		Classpath:    []string{"/store/libraries/a.jar", "/store/versions/1.20.1/1.20.1.jar"},
		NativesDir:   "/store/natives/1.20.1",
		AssetsDir:    "/store/assets",
		AssetIndexID: "5",
		LibrariesDir: "/store/libraries",
	}
}

func mustVersion(t *testing.T, body string) *mojang.Version {
	t.Helper()
	v, err := mojang.ParseVersion([]byte(body))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return v
}

// TestBuildArgsModern is the golden test for the post-1.13 argument schema.
func TestBuildArgsModern(t *testing.T) {
	v := mustVersion(t, `{
		"id": "1.20.1",
		"type": "release",
		"mainClass": "net.minecraft.client.main.Main",
		"arguments": {
			"jvm": ["-Djava.library.path=${natives_directory}", "-cp", "${classpath}"],
			"game": ["--username", "${auth_player_name}", "--version", "${version_name}",
			         "--gameDir", "${game_directory}", "--assetsDir", "${assets_root}",
			         "--assetIndex", "${assets_index_name}", "--uuid", "${auth_uuid}",
			         "--accessToken", "${auth_access_token}", "--userType", "${user_type}",
			         "--versionType", "${version_type}"]
		}
	}`)

	got := BuildArgs(testPrepared(v), testOptions(), testPlatform)
	want := []string{
		"-Xms1024M", "-Xmx8192M",
		"-Djava.library.path=/store/natives/1.20.1",
		"-cp", "/store/libraries/a.jar:/store/versions/1.20.1/1.20.1.jar",
		"net.minecraft.client.main.Main",
		"--username", "Gerry",
		"--version", "1.20.1",
		"--gameDir", "/home/gerry/.minecraft",
		"--assetsDir", "/store/assets",
		"--assetIndex", "5",
		"--uuid", "0123456789abcdef0123456789abcdef",
		"--accessToken", "tok",
		"--userType", "msa",
		"--versionType", "release",
	}

	if len(got) != len(want) {
		t.Fatalf("got %d args, want %d:\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}

	assertNoUnsubstituted(t, got)
}

// TestBuildArgsLegacy covers pre-1.13 versions, where the manifest carries a
// single argument string and no JVM arguments at all.
func TestBuildArgsLegacy(t *testing.T) {
	v := mustVersion(t, `{
		"id": "1.8.9",
		"type": "release",
		"mainClass": "net.minecraft.client.main.Main",
		"minecraftArguments": "--username ${auth_player_name} --version ${version_name} --gameDir ${game_directory} --assetsDir ${assets_root} --assetIndex ${assets_index_name} --uuid ${auth_uuid} --accessToken ${auth_access_token} --userProperties ${user_properties}"
	}`)

	got := BuildArgs(testPrepared(v), testOptions(), testPlatform)
	joined := strings.Join(got, " ")

	// The launcher must supply the JVM arguments the manifest omits.
	if !strings.Contains(joined, "-Djava.library.path=/store/natives/1.20.1") {
		t.Error("legacy launch did not get a java.library.path")
	}
	if !strings.Contains(joined, "-cp /store/libraries/a.jar:/store/versions/1.20.1/1.20.1.jar") {
		t.Error("legacy launch did not get a classpath")
	}
	if !strings.Contains(joined, "--username Gerry") {
		t.Error("legacy game arguments were not substituted")
	}
	if !strings.Contains(joined, "--userProperties {}") {
		t.Error("${user_properties} was not substituted")
	}
	assertNoUnsubstituted(t, got)
}

// TestBuildArgsRespectsRules checks that rule-gated arguments are filtered by
// platform and by feature.
func TestBuildArgsRespectsRules(t *testing.T) {
	v := mustVersion(t, `{
		"id": "1.20.1",
		"mainClass": "Main",
		"arguments": {
			"jvm": [{"rules":[{"action":"allow","os":{"name":"osx"}}],"value":"-XstartOnFirstThread"},
			        {"rules":[{"action":"allow","os":{"name":"linux"}}],"value":"-Dlinux=1"}],
			"game": [{"rules":[{"action":"allow","features":{"has_custom_resolution":true}}],
			          "value":["--width","${resolution_width}","--height","${resolution_height}"]}]
		}
	}`)

	// Without a custom resolution the width/height block is dropped.
	got := BuildArgs(testPrepared(v), testOptions(), testPlatform)
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "--width") {
		t.Error("resolution arguments appeared although the feature is off")
	}
	if strings.Contains(joined, "-XstartOnFirstThread") {
		t.Error("an osx-only argument appeared on linux")
	}
	if !strings.Contains(joined, "-Dlinux=1") {
		t.Error("a linux-only argument was dropped on linux")
	}

	// With one, it appears and is substituted.
	o := testOptions()
	o.Width, o.Height = 1920, 1080
	got = BuildArgs(testPrepared(v), o, testPlatform)
	joined = strings.Join(got, " ")
	if !strings.Contains(joined, "--width 1920 --height 1080") {
		t.Errorf("resolution arguments missing or unsubstituted: %v", got)
	}
}

// TestBuildArgsForgePlaceholders covers the two tokens Forge and NeoForge need
// and that nothing else supplies.
func TestBuildArgsForgePlaceholders(t *testing.T) {
	v := mustVersion(t, `{
		"id": "neoforge-21.1.248",
		"mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
		"arguments": {
			"jvm": ["-DlibraryDirectory=${library_directory}",
			        "-DignoreList=${classpath_separator}",
			        "--add-modules", "ALL-MODULE-PATH"],
			"game": ["--launchTarget", "neoforgeclient"]
		}
	}`)

	got := BuildArgs(testPrepared(v), testOptions(), testPlatform)
	joined := strings.Join(got, " ")

	if !strings.Contains(joined, "-DlibraryDirectory=/store/libraries") {
		t.Error("${library_directory} was not substituted")
	}
	if !strings.Contains(joined, "-DignoreList="+string(os.PathListSeparator)) {
		t.Error("${classpath_separator} was not substituted")
	}
	assertNoUnsubstituted(t, got)
}

// TestSubstituteKeepsUnknownTokens is deliberate: blanking a token a loader
// relies on turns a clear failure into an unreadable crash.
func TestSubstituteKeepsUnknownTokens(t *testing.T) {
	subs := map[string]string{"known": "value"}

	cases := map[string]string{
		"${known}":             "value",
		"${unknown}":           "${unknown}",
		"a=${known},b=${nope}": "a=value,b=${nope}",
		"no tokens here":       "no tokens here",
		"${unterminated":       "${unterminated",
		"":                     "",
		"${}":                  "${}",
	}
	for in, want := range cases {
		if got := substitute(in, subs); got != want {
			t.Errorf("substitute(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlaceholdersOfflineDefaults(t *testing.T) {
	v := mustVersion(t, `{"id":"1.20.1","mainClass":"Main"}`)

	o := testOptions()
	o.Session = Session{PlayerName: "Dev", UUID: "abc", AccessToken: "0", UserType: "legacy"}

	subs := placeholders(testPrepared(v), o)
	// An empty XUID makes modern versions misbehave, so it defaults to "0".
	if subs["auth_xuid"] != "0" {
		t.Errorf("auth_xuid = %q, want 0", subs["auth_xuid"])
	}
	if subs["user_type"] != "legacy" {
		t.Errorf("user_type = %q, want legacy", subs["user_type"])
	}
	if subs["auth_session"] != "token:0:abc" {
		t.Errorf("auth_session = %q", subs["auth_session"])
	}
	// A version with no type still needs one templated.
	if subs["version_type"] != "release" {
		t.Errorf("version_type = %q, want release", subs["version_type"])
	}
	// Modern versions never read ${game_assets}, but it must not be left empty.
	if subs["game_assets"] != "/store/assets" {
		t.Errorf("game_assets = %q", subs["game_assets"])
	}
}

func TestBuildArgsIncludesLoggingAndExtras(t *testing.T) {
	v := mustVersion(t, `{"id":"1.20.1","mainClass":"Main","arguments":{"jvm":["-cp","${classpath}"],"game":["--x"]}}`)
	p := testPrepared(v)
	p.LoggingArgument = "-Dlog4j.configurationFile=/store/assets/log_configs/client-1.12.xml"

	o := testOptions()
	o.ExtraJVMArgs = []string{"-XX:+UseG1GC"}
	o.ExtraGameArgs = []string{"--demo"}
	o.QuickPlayServer = "mc.example.com"

	got := BuildArgs(p, o, testPlatform)
	joined := strings.Join(got, " ")

	if !strings.Contains(joined, p.LoggingArgument) {
		t.Error("the logging argument was not included")
	}
	if !strings.Contains(joined, "-XX:+UseG1GC") {
		t.Error("extra JVM arguments were dropped")
	}
	if !strings.Contains(joined, "--demo") {
		t.Error("extra game arguments were dropped")
	}
	if !strings.Contains(joined, "--quickPlayMultiplayer mc.example.com") {
		t.Error("quick play server was not passed")
	}

	// The main class separates JVM arguments from game arguments; everything
	// before it must be a JVM flag.
	mainAt := indexOf(got, "Main")
	if mainAt < 0 {
		t.Fatal("main class missing from the command line")
	}
	if indexOf(got, "-XX:+UseG1GC") > mainAt {
		t.Error("a JVM argument landed after the main class")
	}
	if indexOf(got, "--demo") < mainAt {
		t.Error("a game argument landed before the main class")
	}
}

// assertNoUnsubstituted fails if any ${...} survived in a fixture built from
// tokens we claim to support.
func assertNoUnsubstituted(t *testing.T, args []string) {
	t.Helper()
	for _, a := range args {
		if strings.Contains(a, "${") {
			t.Errorf("argument %q still contains an unsubstituted placeholder", a)
		}
	}
}

func indexOf(items []string, want string) int {
	for i, s := range items {
		if s == want {
			return i
		}
	}
	return -1
}

// TestArgumentRoundTrip guards the custom marshaller used when a resolved
// manifest is written back to disk.
func TestArgumentRoundTrip(t *testing.T) {
	original := `["--plain",{"rules":[{"action":"allow","os":{"name":"osx"}}],"value":["-a","-b"]}]`

	var args []mojang.Argument
	if err := json.Unmarshal([]byte(original), &args); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}

	var again []mojang.Argument
	if err := json.Unmarshal(encoded, &again); err != nil {
		t.Fatalf("re-decoding what we encoded failed: %v (%s)", err, encoded)
	}
	if len(again) != 2 || again[0].Value[0] != "--plain" || len(again[1].Value) != 2 {
		t.Errorf("round trip changed the arguments: %s", encoded)
	}
}
