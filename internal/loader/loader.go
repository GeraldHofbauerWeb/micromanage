// Package loader lists and installs mod loaders.
//
// Every supported loader publishes its versions and hands out what a launcher
// needs to run it: Fabric and Quilt serve a finished launcher profile from
// their meta services, NeoForge and Forge ship an installer that writes the
// profile and processes the game jar. Both routes end in the same place —
// versions/<id>/<id>.json in the shared store — which is all the launcher
// asks for.
package loader

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GeraldHofbauerWeb/micromanage/internal/download"
	"github.com/GeraldHofbauerWeb/micromanage/internal/instance"
	"github.com/GeraldHofbauerWeb/micromanage/internal/launch"
)

// Endpoints are the services the loaders publish through. They are a struct
// so a test can point every one of them at a local server.
type Endpoints struct {
	FabricMeta    string // lists loaders and serves profiles
	QuiltMeta     string
	NeoForgeMaven string // maven root holding maven-metadata.xml and installers
	ForgeMaven    string
}

// DefaultEndpoints are the projects' own services.
var DefaultEndpoints = Endpoints{
	FabricMeta:    "https://meta.fabricmc.net/v2",
	QuiltMeta:     "https://meta.quiltmc.org/v3",
	NeoForgeMaven: "https://maven.neoforged.net/releases/net/neoforged/neoforge",
	ForgeMaven:    "https://maven.minecraftforge.net/net/minecraftforge/forge",
}

// Version is one release of a loader for one Minecraft version.
type Version struct {
	Version string `json:"version"`
	// Stable is false for betas and release candidates. Every loader marks
	// them differently; here they are all just "not stable".
	Stable bool `json:"stable"`
}

// listTTL is how long a cached version list is trusted before the service
// is asked again. Lists change a few times a week; a launcher opened twice
// in an hour should not fetch twice.
const listTTL = time.Hour

// Client talks to the loader services and caches their answers.
type Client struct {
	Endpoints  Endpoints
	Downloader *download.Downloader
	Layout     *launch.Layout
	// Now is overridable for tests of the cache's expiry.
	Now func() time.Time
}

// NewClient wires a client against the shared store.
func NewClient(layout *launch.Layout, d *download.Downloader) *Client {
	if d == nil {
		d = download.New()
	}
	return &Client{Endpoints: DefaultEndpoints, Downloader: d, Layout: layout, Now: time.Now}
}

// VersionID is the version id a loader installation is known by, which is
// also the directory it lives in under versions/.
func VersionID(kind instance.LoaderType, mc, version string) string {
	switch kind {
	case instance.LoaderFabric:
		return "fabric-loader-" + version + "-" + mc
	case instance.LoaderQuilt:
		return "quilt-loader-" + version + "-" + mc
	case instance.LoaderNeoForge:
		return "neoforge-" + version
	case instance.LoaderForge:
		return mc + "-forge-" + version
	}
	return mc
}

// Installed reports whether a version's profile is in the store.
func (c *Client) Installed(id string) bool {
	_, err := os.Stat(c.Layout.VersionJSON(id))
	return err == nil
}

// Versions lists the loader releases available for a Minecraft version,
// newest first. Results are cached for an hour, and a cached list is used
// past its age when the service cannot be reached: an old list beats none.
func (c *Client) Versions(ctx context.Context, kind instance.LoaderType, mc string) ([]Version, error) {
	if !kind.Valid() || kind == instance.LoaderVanilla {
		return nil, fmt.Errorf("%s has no loader versions", kind.Display())
	}
	if mc == "" {
		return nil, fmt.Errorf("choose a Minecraft version first")
	}

	cachePath := filepath.Join(c.Layout.Cache(), "loaders", string(kind)+"-"+mc+".json")
	if cached, fresh := c.readCache(cachePath); fresh {
		return cached, nil
	}

	var versions []Version
	var err error
	switch kind {
	case instance.LoaderFabric:
		versions, err = c.metaVersions(ctx, c.Endpoints.FabricMeta+"/versions/loader/"+mc)
	case instance.LoaderQuilt:
		versions, err = c.metaVersions(ctx, c.Endpoints.QuiltMeta+"/versions/loader/"+mc)
	case instance.LoaderNeoForge:
		versions, err = c.mavenVersions(ctx, c.Endpoints.NeoForgeMaven, neoForgePrefix(mc), "")
	case instance.LoaderForge:
		versions, err = c.mavenVersions(ctx, c.Endpoints.ForgeMaven, mc+"-", mc+"-")
	}
	if err != nil {
		if cached, _ := c.readCache(cachePath); cached != nil {
			return cached, nil
		}
		return nil, err
	}

	c.writeCache(cachePath, versions)
	return versions, nil
}

type cachedList struct {
	FetchedAt time.Time `json:"fetched_at"`
	Versions  []Version `json:"versions"`
}

func (c *Client) readCache(path string) (versions []Version, fresh bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var l cachedList
	if json.Unmarshal(data, &l) != nil || l.Versions == nil {
		return nil, false
	}
	return l.Versions, c.Now().Sub(l.FetchedAt) < listTTL
}

func (c *Client) writeCache(path string, versions []Version) {
	data, err := json.Marshal(cachedList{FetchedAt: c.Now(), Versions: versions})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// metaVersions reads the Fabric-style meta listing, which both Fabric and
// Quilt serve: an array of {loader:{version,stable}} newest first.
func (c *Client) metaVersions(ctx context.Context, url string) ([]Version, error) {
	data, err := c.Downloader.GetJSON(ctx, url, "")
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  *bool  `json:"stable"`
		} `json:"loader"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing the loader list: %w", err)
	}
	out := make([]Version, 0, len(entries))
	for _, e := range entries {
		if e.Loader.Version == "" {
			continue
		}
		v := Version{Version: e.Loader.Version, Stable: true}
		if e.Loader.Stable != nil {
			v.Stable = *e.Loader.Stable
		} else {
			// Quilt does not flag stability; a pre-release says so in its name.
			v.Stable = !strings.ContainsAny(e.Loader.Version, "-+") ||
				!strings.Contains(strings.ToLower(e.Loader.Version), "beta")
		}
		out = append(out, v)
	}
	return out, nil
}

// mavenVersions reads a maven-metadata.xml and keeps the versions with a
// prefix, stripping strip from what is shown. Maven lists are unordered, so
// they are sorted numerically, newest first.
func (c *Client) mavenVersions(ctx context.Context, root, prefix, strip string) ([]Version, error) {
	data, err := c.Downloader.GetJSON(ctx, strings.TrimSuffix(root, "/")+"/maven-metadata.xml", "")
	if err != nil {
		return nil, err
	}
	var meta struct {
		Versioning struct {
			Versions []string `xml:"versions>version"`
		} `xml:"versioning"`
	}
	if err := xml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing maven-metadata.xml: %w", err)
	}

	var out []Version
	for _, v := range meta.Versioning.Versions {
		if !strings.HasPrefix(v, prefix) {
			continue
		}
		shown := strings.TrimPrefix(v, strip)
		out = append(out, Version{Version: shown, Stable: !strings.ContainsAny(shown, "-")})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compareVersions(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// neoForgePrefix maps a Minecraft version onto NeoForge's numbering, which
// drops the leading "1.": 1.21.1 is 21.1.x, 1.21 is 21.0.x, and the
// year-based 26.2 is 26.2.x.
func neoForgePrefix(mc string) string {
	v := strings.TrimPrefix(mc, "1.")
	if !strings.Contains(v, ".") {
		v += ".0"
	}
	return v + "."
}

// compareVersions orders dotted versions numerically, with a suffix such as
// -beta sorting below the plain release of the same number.
func compareVersions(a, b string) int {
	aNum, aSuffix := splitSuffix(a)
	bNum, bSuffix := splitSuffix(b)
	as, bs := strings.Split(aNum, "."), strings.Split(bNum, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x - y
		}
	}
	switch {
	case aSuffix == bSuffix:
		return 0
	case aSuffix == "":
		return 1
	case bSuffix == "":
		return -1
	}
	return strings.Compare(aSuffix, bSuffix)
}

func splitSuffix(v string) (num, suffix string) {
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i], v[i:]
	}
	return v, ""
}

// Install puts a loader version into the store and returns its version id.
// It is idempotent: an installed version is not installed twice.
//
// javaPath is only needed for NeoForge and Forge, whose installers run as a
// Java program; progress receives the lines they print.
func (c *Client) Install(ctx context.Context, kind instance.LoaderType, mc, version, javaPath string, progress func(string)) (string, error) {
	if mc == "" {
		return "", fmt.Errorf("no Minecraft version given")
	}
	if kind == instance.LoaderVanilla || kind == "" {
		return mc, nil
	}
	if version == "" {
		return "", fmt.Errorf("no %s version chosen", kind.Display())
	}
	if progress == nil {
		progress = func(string) {}
	}

	id := VersionID(kind, mc, version)
	if c.Installed(id) {
		return id, nil
	}

	switch kind {
	case instance.LoaderFabric:
		return id, c.installProfile(ctx, id, fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", c.Endpoints.FabricMeta, mc, version))
	case instance.LoaderQuilt:
		return id, c.installProfile(ctx, id, fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", c.Endpoints.QuiltMeta, mc, version))
	case instance.LoaderNeoForge:
		url := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", c.Endpoints.NeoForgeMaven, version, version)
		return id, c.runInstaller(ctx, id, url, javaPath, progress)
	case instance.LoaderForge:
		full := mc + "-" + version
		url := fmt.Sprintf("%s/%s/forge-%s-installer.jar", c.Endpoints.ForgeMaven, full, full)
		return id, c.runInstaller(ctx, id, url, javaPath, progress)
	}
	return "", fmt.Errorf("unknown loader %q", kind)
}

// installProfile fetches a ready-made profile and drops it into the store.
func (c *Client) installProfile(ctx context.Context, id, url string) error {
	data, err := c.Downloader.GetJSON(ctx, url, "")
	if err != nil {
		return fmt.Errorf("fetching the %s profile: %w", id, err)
	}
	// The service names the profile; trust our id for the path but make
	// sure the two agree, or the launcher would never find it.
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &probe); err != nil || probe.ID == "" {
		return fmt.Errorf("the %s profile is not a version manifest", id)
	}
	if probe.ID != id {
		data = []byte(strings.Replace(string(data), `"id":"`+probe.ID+`"`, `"id":"`+id+`"`, 1))
	}
	return writeFileAtomic(c.Layout.VersionJSON(id), data)
}

// runInstaller downloads a Forge-style installer and runs it against the
// shared store, which has exactly the layout the installer expects of a
// .minecraft directory: versions/ and libraries/, plus a profile list it
// insists on finding.
func (c *Client) runInstaller(ctx context.Context, id, url, javaPath string, progress func(string)) error {
	if javaPath == "" {
		return fmt.Errorf("installing %s needs a Java runtime, and none was found", id)
	}

	jar := filepath.Join(c.Layout.Loaders(), filepath.Base(url))
	progress("Downloading the installer")
	if err := c.Downloader.Fetch(ctx, download.Item{URL: url, Path: jar}); err != nil {
		return fmt.Errorf("downloading the installer: %w", err)
	}

	// The installer refuses a directory without launcher_profiles.json; an
	// empty one satisfies it and is never read by anything else.
	profiles := filepath.Join(c.Layout.Root, "launcher_profiles.json")
	if _, err := os.Stat(profiles); err != nil {
		if err := os.MkdirAll(c.Layout.Root, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(profiles, []byte("{\"profiles\":{}}\n"), 0o644); err != nil {
			return err
		}
	}

	logPath := filepath.Join(c.Layout.Loaders(), id+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, javaPath, "-jar", jar, "--installClient", c.Layout.Root)
	cmd.Dir = c.Layout.Loaders()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	progress("Running the installer")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the installer: %w", err)
	}
	scanner := bufio.NewScanner(io.TeeReader(stdout, logFile))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lastLines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lastLines = append(lastLines, line)
		if len(lastLines) > 5 {
			lastLines = lastLines[1:]
		}
		progress(line)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("the installer failed (%v): %s — full output in %s",
			err, strings.Join(lastLines, " | "), logPath)
	}

	if !c.Installed(id) {
		return fmt.Errorf("the installer finished but wrote no %s profile; see %s", id, logPath)
	}
	return nil
}

// writeFileAtomic writes data via a temporary file and a rename.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
