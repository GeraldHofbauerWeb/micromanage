package java

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Runtime is one Java installation found on the machine.
type Runtime struct {
	Path        string // absolute path to the java executable
	Major       int
	FullVersion string
	Vendor      string
	Source      string // where it was found, for display
	// Component is the Mojang runtime component name when known, e.g.
	// "java-runtime-gamma".
	Component string
	// Managed marks runtimes we downloaded into the shared store.
	Managed bool
	// Broken marks a runtime that exists but cannot be used as-is, with
	// Reason saying why. The common case is a missing executable bit after an
	// instance was copied.
	Broken bool
	Reason string
}

// String renders a runtime for a listing.
func (r Runtime) String() string {
	label := r.FullVersion
	if label == "" {
		label = "unknown"
	}
	if r.Vendor != "" {
		label = r.Vendor + " " + label
	}
	return label
}

// probeTimeout bounds a single `java -version` call so a broken or networked
// installation cannot stall startup.
const probeTimeout = 3 * time.Second

// probeParallelism bounds how many JVMs are asked for their version at once.
// The query is short-lived, so a handful in flight finishes a cold scan in
// about the time one used to take, without turning startup into a fork storm.
const probeParallelism = 6

// Detector finds Java runtimes.
type Detector struct {
	// SharedRuntimes is the managed runtime directory in the shared store.
	SharedRuntimes string
	// ExtraRoots are additional directories to scan for Mojang-style runtime
	// trees — in practice the instances, each of which carries its own.
	ExtraRoots []string
	// CachePath is where probe results persist between runs. Empty means
	// every runtime is asked for its version on every Detect.
	CachePath string
	// Rescan ignores the cache for one detection, for when a runtime is
	// suspected of having changed without its file changing.
	Rescan bool
}

// NewDetector wires a detector against the shared store: its managed
// runtimes, its cache directory, and the instance directories to look inside.
func NewDetector(sharedRuntimes, cacheDir string, instanceRoots []string) *Detector {
	d := &Detector{SharedRuntimes: sharedRuntimes, ExtraRoots: instanceRoots}
	if cacheDir != "" {
		d.CachePath = filepath.Join(cacheDir, CacheFileName)
	}
	return d
}

// Detect returns every usable runtime, best first, plus any that are present
// but broken so the caller can offer to repair them.
//
// Order matters: harvested Mojang runtimes come first because they are the
// ones a version's javaVersion actually names, and on a machine with only a
// newer system JDK they may be the only correct choice.
//
// Runtimes already known to the cache are not executed again; the rest are
// probed concurrently.
func (d *Detector) Detect(ctx context.Context) []Runtime {
	var candidates []probe

	candidates = append(candidates, d.mojangRuntimes(d.SharedRuntimes, "shared store", true)...)
	for _, root := range d.ExtraRoots {
		candidates = append(candidates, d.mojangRuntimes(filepath.Join(root, "runtime"), "instance "+filepath.Base(root), false)...)
	}
	candidates = append(candidates, environmentCandidates()...)
	candidates = append(candidates, platformCandidates()...)

	cache := loadProbeCache(d.CachePath)
	if d.Rescan {
		cache.entries = map[string]cacheEntry{}
	}

	// Resolve and deduplicate first, so the same JDK reached through two
	// paths is probed once, then split into what the cache answers and what
	// has to be asked.
	type slot struct {
		probe probe
		key   string
		id    identity
		rt    Runtime
		known bool
	}
	seen := make(map[string]bool)
	var slots []*slot

	for _, c := range candidates {
		real := resolvePath(c.path)
		if real == "" || seen[real] {
			continue
		}
		seen[real] = true

		info, err := os.Stat(c.path)
		if err != nil || info.IsDir() {
			continue
		}
		sl := &slot{probe: c, key: real, id: identityOf(info)}
		if e, ok := cache.lookup(real, sl.id); ok {
			sl.rt = runtimeFromEntry(c, e)
			sl.known = true
		}
		slots = append(slots, sl)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, probeParallelism)
	for _, sl := range slots {
		if sl.known {
			continue
		}
		wg.Add(1)
		go func(sl *slot) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sl.rt = probeRuntime(ctx, sl.probe)
			if sl.rt.Path != "" {
				cache.remember(sl.key, sl.id, sl.rt)
			}
		}(sl)
	}
	wg.Wait()
	cache.save(seen)

	var out []Runtime
	for _, sl := range slots {
		if sl.rt.Path == "" {
			continue
		}
		out = append(out, sl.rt)
	}

	// Usable runtimes first, then by descending major version.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Broken != out[j].Broken {
			return !out[i].Broken
		}
		return out[i].Major > out[j].Major
	})
	return out
}

// runtimeFromEntry rebuilds a Runtime from a cached probe. Where it was found
// is not cached: the same file is reported under whichever source asked.
func runtimeFromEntry(c probe, e cacheEntry) Runtime {
	return Runtime{
		Path:        c.path,
		Major:       e.Major,
		FullVersion: e.FullVersion,
		Vendor:      e.Vendor,
		Source:      c.source,
		Component:   c.component,
		Managed:     c.managed,
		Broken:      e.Broken,
		Reason:      e.Reason,
	}
}

// probe is a candidate before it has been interrogated.
type probe struct {
	path      string
	source    string
	component string
	managed   bool
}

// mojangRuntimes finds runtimes in the layout the official launcher uses:
//
//	<root>/<component>/<platform>/<component>/bin/java
//
// The platform segment varies (linux, windows-x64, mac-os-arm64), so it is
// globbed rather than hardcoded.
func (d *Detector) mojangRuntimes(root, source string, managed bool) []probe {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	exe := "java"
	if runtime.GOOS == "windows" {
		exe = "java.exe"
	}

	var out []probe
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		component := e.Name()

		// Both the nested launcher layout and a flat <component>/bin/java.
		patterns := []string{
			filepath.Join(root, component, "*", component, "bin", exe),
			filepath.Join(root, component, "bin", exe),
		}
		for _, pattern := range patterns {
			matches, _ := filepath.Glob(pattern)
			for _, m := range matches {
				out = append(out, probe{path: m, source: source, component: component, managed: managed})
			}
		}
	}
	return out
}

// environmentCandidates covers JAVA_HOME and PATH.
func environmentCandidates() []probe {
	exe := "java"
	if runtime.GOOS == "windows" {
		exe = "java.exe"
	}

	var out []probe
	if home := os.Getenv("JAVA_HOME"); home != "" {
		out = append(out, probe{path: filepath.Join(home, "bin", exe), source: "JAVA_HOME"})
	}
	if path, err := exec.LookPath(exe); err == nil {
		out = append(out, probe{path: path, source: "PATH"})
	}
	return out
}

// platformCandidates scans the usual installation locations.
func platformCandidates() []probe {
	var patterns []string

	switch runtime.GOOS {
	case "darwin":
		patterns = []string{
			"/Library/Java/JavaVirtualMachines/*/Contents/Home/bin/java",
			os.ExpandEnv("$HOME/Library/Java/JavaVirtualMachines/*/Contents/Home/bin/java"),
		}
	case "windows":
		patterns = []string{
			`C:\Program Files\Java\*\bin\java.exe`,
			`C:\Program Files\Eclipse Adoptium\*\bin\java.exe`,
			`C:\Program Files\Microsoft\*\bin\java.exe`,
			`C:\Program Files\Zulu\*\bin\java.exe`,
		}
	default:
		patterns = []string{
			"/usr/lib/jvm/*/bin/java",
			"/usr/lib64/jvm/*/bin/java",
			"/opt/java/*/bin/java",
			os.ExpandEnv("$HOME/.sdkman/candidates/java/*/bin/java"),
			// Other launchers keep runtimes we can happily reuse.
			os.ExpandEnv("$HOME/.local/share/PrismLauncher/java/*/bin/java"),
			os.ExpandEnv("$HOME/.local/share/multimc/java/*/bin/java"),
		}
	}

	var out []probe
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			out = append(out, probe{path: m, source: "system"})
		}
	}
	return out
}

// probeRuntime interrogates one candidate.
func probeRuntime(ctx context.Context, c probe) Runtime {
	info, err := os.Stat(c.path)
	if err != nil || info.IsDir() {
		return Runtime{}
	}

	rt := Runtime{
		Path:      c.path,
		Source:    c.source,
		Component: c.component,
		Managed:   c.managed,
	}

	// A runtime copied by an older version of this tool lost its executable
	// bit. Reporting it as broken beats skipping it silently, which is what
	// makes an instance look like it has no Java at all.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		rt.Broken = true
		rt.Reason = "not executable"
		rt.Major = majorFromComponent(c.component)
		return rt
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// `java -version` writes to stderr.
	out, err := exec.CommandContext(probeCtx, c.path, "-version").CombinedOutput()
	if err != nil {
		rt.Broken = true
		rt.Reason = strings.TrimSpace(firstLine(string(out)))
		if rt.Reason == "" {
			rt.Reason = err.Error()
		}
		return rt
	}

	major, full, err := ParseVersion(string(out))
	if err != nil {
		rt.Broken = true
		rt.Reason = err.Error()
		return rt
	}

	rt.Major = major
	rt.FullVersion = full
	rt.Vendor = ParseVendor(string(out))
	return rt
}

// majorFromComponent maps Mojang's runtime component names to major releases,
// so a runtime we cannot execute can still be reported usefully.
func majorFromComponent(component string) int {
	switch component {
	case "jre-legacy":
		return 8
	case "java-runtime-alpha", "java-runtime-beta", "java-runtime-gamma", "java-runtime-gamma-snapshot":
		return 17
	case "java-runtime-delta":
		return 21
	}
	return 0
}

// resolvePath returns the canonical path of an executable, following symlinks
// so the same JDK reached two ways is only listed once.
func resolvePath(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return real
	}
	return abs
}

// Repair restores the executable bit on a runtime's binaries.
func Repair(javaPath string) error {
	// The whole bin/ directory plus the spawn helper need it, not just java.
	binDir := filepath.Dir(javaPath)
	targets, _ := filepath.Glob(filepath.Join(binDir, "*"))

	root := filepath.Dir(binDir)
	if helper := filepath.Join(root, "lib", "jspawnhelper"); fileExists(helper) {
		targets = append(targets, helper)
	}

	for _, t := range targets {
		info, err := os.Stat(t)
		if err != nil || info.IsDir() {
			continue
		}
		mode := info.Mode().Perm()
		if mode&0o111 != 0 {
			continue
		}
		if err := os.Chmod(t, mode|((mode&0o444)>>2)); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
