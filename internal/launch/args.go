package launch

import (
	"os"
	"strconv"
	"strings"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/mojang"
)

// Session identifies the player a launch runs as.
type Session struct {
	PlayerName  string
	UUID        string // undashed
	AccessToken string
	// XUID comes from the XSTS token. Modern versions template it, and an
	// empty one misbehaves, so offline sessions use "0".
	XUID     string
	UserType string // "msa" for a Microsoft account, "legacy" offline
	ClientID string
}

// Options are the per-launch inputs that are not part of the version manifest.
type Options struct {
	Session       Session
	GameDir       string
	LauncherName  string
	LauncherVer   string
	MinMB, MaxMB  int
	ExtraJVMArgs  []string
	ExtraGameArgs []string
	Width, Height int
	// QuickPlayServer, when set, joins a server directly.
	QuickPlayServer string
}

// DefaultJVMArgs are the garbage-collector settings the official launcher
// gives every profile. An instance detected from one carries them already;
// one created here would otherwise run on the JVM's own defaults, whose
// longer pauses show up in-game as stutter.
var DefaultJVMArgs = []string{
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:+UseG1GC",
	"-XX:G1NewSizePercent=20",
	"-XX:G1ReservePercent=20",
	"-XX:MaxGCPauseMillis=50",
	"-XX:G1HeapRegionSize=32M",
}

// JVMArgsFor returns an instance's own JVM flags, or the defaults when it has
// none. A configured empty list is indistinguishable from none, which is
// fine: nobody wants a stock JVM for Minecraft.
func JVMArgsFor(own []string) []string {
	if len(own) > 0 {
		return own
	}
	return append([]string(nil), DefaultJVMArgs...)
}

// placeholders builds the substitution table applied to both argument lists.
func placeholders(p *Prepared, o Options) map[string]string {
	assetsRoot := p.AssetsDir
	gameAssets := p.GameAssetsDir
	if gameAssets == "" {
		// Modern versions never read ${game_assets}, but leaving it unset
		// would strand the token in the command line.
		gameAssets = assetsRoot
	}

	userType := o.Session.UserType
	if userType == "" {
		userType = "msa"
	}
	xuid := o.Session.XUID
	if xuid == "" {
		xuid = "0"
	}
	// An empty ${clientid} would put a blank argument on the command line,
	// which some versions parse as the next flag's value.
	clientID := o.Session.ClientID
	if clientID == "" {
		clientID = "0"
	}
	versionType := p.Version.Type
	if versionType == "" {
		versionType = "release"
	}

	return map[string]string{
		"auth_player_name":  o.Session.PlayerName,
		"auth_uuid":         o.Session.UUID,
		"auth_access_token": o.Session.AccessToken,
		"auth_xuid":         xuid,
		"auth_session":      "token:" + o.Session.AccessToken + ":" + o.Session.UUID,
		"user_type":         userType,
		"user_properties":   "{}",
		"clientid":          clientID,

		"version_name": p.Version.ID,
		"version_type": versionType,

		"game_directory":    o.GameDir,
		"assets_root":       assetsRoot,
		"game_assets":       gameAssets,
		"assets_index_name": p.AssetIndexID,

		"natives_directory": p.NativesDir,
		"classpath":         strings.Join(p.Classpath, string(os.PathListSeparator)),
		// Forge and NeoForge template these two into their module arguments;
		// without them the game fails with an unreadable module error.
		"library_directory":   p.LibrariesDir,
		"classpath_separator": string(os.PathListSeparator),

		"launcher_name":    o.LauncherName,
		"launcher_version": o.LauncherVer,

		"resolution_width":  strconv.Itoa(o.Width),
		"resolution_height": strconv.Itoa(o.Height),
	}
}

// BuildArgs assembles the full command line after the java executable.
func BuildArgs(p *Prepared, o Options, platform mojang.Platform) []string {
	features := featuresFor(o)
	subs := placeholders(p, o)

	var args []string

	// Heap sizing first, so a user-supplied -Xmx later can still override it.
	if o.MinMB > 0 {
		args = append(args, "-Xms"+strconv.Itoa(o.MinMB)+"M")
	}
	if o.MaxMB > 0 {
		args = append(args, "-Xmx"+strconv.Itoa(o.MaxMB)+"M")
	}
	args = append(args, o.ExtraJVMArgs...)

	if p.Version.Arguments != nil && len(p.Version.Arguments.JVM) > 0 {
		args = append(args, expand(p.Version.Arguments.JVM, platform, features, subs)...)
	} else {
		// Pre-1.13 manifests describe no JVM arguments at all, so the
		// launcher has to supply the two that matter.
		args = append(args,
			"-Djava.library.path="+p.NativesDir,
			"-cp", subs["classpath"],
		)
	}

	if p.LoggingArgument != "" {
		args = append(args, p.LoggingArgument)
	}

	args = append(args, p.Version.MainClass)

	switch {
	case p.Version.Arguments != nil && len(p.Version.Arguments.Game) > 0:
		args = append(args, expand(p.Version.Arguments.Game, platform, features, subs)...)
	case p.Version.MinecraftArguments != "":
		for _, a := range strings.Fields(p.Version.MinecraftArguments) {
			args = append(args, substitute(a, subs))
		}
	}

	args = append(args, o.ExtraGameArgs...)

	if o.QuickPlayServer != "" {
		args = append(args, "--quickPlayMultiplayer", o.QuickPlayServer)
	}

	return args
}

// featuresFor derives the rule features a launch enables.
func featuresFor(o Options) map[string]bool {
	return map[string]bool{
		mojang.FeatureDemoUser:             false,
		mojang.FeatureCustomResolution:     o.Width > 0 && o.Height > 0,
		mojang.FeatureQuickPlaySupport:     o.QuickPlayServer != "",
		mojang.FeatureQuickPlayMultiplayer: o.QuickPlayServer != "",
	}
}

// expand filters an argument list by its rules and substitutes placeholders.
func expand(args []mojang.Argument, platform mojang.Platform, features map[string]bool, subs map[string]string) []string {
	var out []string
	for _, a := range args {
		if !mojang.Allowed(a.Rules, platform, features) {
			continue
		}
		for _, v := range a.Value {
			out = append(out, substitute(v, subs))
		}
	}
	return out
}

// substitute replaces ${name} tokens.
//
// An unknown token is left verbatim rather than blanked: a loader that
// templates something we do not know about produces a readable error if its
// argument survives, and an inscrutable crash if it silently becomes empty.
func substitute(s string, subs map[string]string) string {
	if !strings.Contains(s, "${") {
		return s
	}

	var b strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := strings.Index(s[start:], "}")
		if end < 0 {
			// Unterminated token; nothing sensible to do but keep it.
			b.WriteString(s)
			return b.String()
		}
		end += start

		b.WriteString(s[:start])
		name := s[start+2 : end]
		if value, ok := subs[name]; ok {
			b.WriteString(value)
		} else {
			b.WriteString(s[start : end+1])
		}
		s = s[end+1:]
	}
}

// replacePlaceholder substitutes a single named token.
func replacePlaceholder(s, name, value string) string {
	return strings.ReplaceAll(s, "${"+name+"}", value)
}
