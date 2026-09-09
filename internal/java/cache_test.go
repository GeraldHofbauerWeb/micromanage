package java

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeJava writes a shell script that answers `-version` like a JDK and
// counts every invocation, so a test can tell a cache hit from a probe.
func fakeJava(t *testing.T, root, component, version string) (javaPath, counter string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	dir := filepath.Join(root, "runtime", component, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	javaPath = filepath.Join(dir, "java")
	counter = filepath.Join(root, component+".count")
	script := "#!/bin/sh\n" +
		"echo x >> " + counter + "\n" +
		"echo 'openjdk version \"" + version + "\" 2025-04-15' >&2\n" +
		"echo 'OpenJDK Runtime Environment Temurin-" + version + "' >&2\n"
	if err := os.WriteFile(javaPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return javaPath, counter
}

func invocations(t *testing.T, counter string) int {
	t.Helper()
	data, err := os.ReadFile(counter)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "x")
}

// find returns the runtime detected at a path.
func find(runtimes []Runtime, path string) (Runtime, bool) {
	for _, r := range runtimes {
		if r.Path == path {
			return r, true
		}
	}
	return Runtime{}, false
}

// TestDetectCachesProbes is the point of the cache: the second detection
// must not fork a JVM for a runtime that has not changed.
func TestDetectCachesProbes(t *testing.T) {
	root := t.TempDir()
	javaPath, counter := fakeJava(t, root, "java-runtime-delta", "21.0.7")

	d := NewDetector("", filepath.Join(root, "cache"), []string{root})
	first := d.Detect(context.Background())
	rt, ok := find(first, javaPath)
	if !ok {
		t.Fatalf("the fake runtime was not detected in %v", first)
	}
	if rt.Major != 21 || rt.Component != "java-runtime-delta" {
		t.Errorf("first probe = %+v", rt)
	}
	if n := invocations(t, counter); n != 1 {
		t.Fatalf("first detection ran java %d times, want 1", n)
	}

	second := NewDetector("", filepath.Join(root, "cache"), []string{root}).Detect(context.Background())
	again, ok := find(second, javaPath)
	if !ok {
		t.Fatal("the runtime vanished from the second detection")
	}
	if n := invocations(t, counter); n != 1 {
		t.Errorf("second detection ran java again (%d invocations); the cache was not used", n)
	}
	if again.Major != rt.Major || again.FullVersion != rt.FullVersion || again.Vendor != rt.Vendor {
		t.Errorf("cached runtime %+v differs from the probed one %+v", again, rt)
	}
	if again.Source != "instance "+filepath.Base(root) {
		t.Errorf("source %q was not rebuilt from the candidate", again.Source)
	}
}

// TestDetectReprobesAChangedRuntime covers the other side: a runtime that
// was upgraded in place must not keep reporting its old version.
func TestDetectReprobesAChangedRuntime(t *testing.T) {
	root := t.TempDir()
	javaPath, counter := fakeJava(t, root, "java-runtime-delta", "21.0.7")
	cache := filepath.Join(root, "cache")

	NewDetector("", cache, []string{root}).Detect(context.Background())

	// Same path, new contents: a different size and mtime.
	fakeJava(t, root, "java-runtime-delta", "21.0.8")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(javaPath, future, future); err != nil {
		t.Fatal(err)
	}

	got := NewDetector("", cache, []string{root}).Detect(context.Background())
	rt, ok := find(got, javaPath)
	if !ok {
		t.Fatal("runtime not detected after the change")
	}
	if rt.FullVersion != "21.0.8" {
		t.Errorf("version = %q after upgrade, want 21.0.8 (stale cache)", rt.FullVersion)
	}
	if n := invocations(t, counter); n != 2 {
		t.Errorf("java ran %d times, want 2 (once per distinct file)", n)
	}
}

// TestDetectRescanIgnoresTheCache gives the CLI a way to force a probe.
func TestDetectRescanIgnoresTheCache(t *testing.T) {
	root := t.TempDir()
	_, counter := fakeJava(t, root, "java-runtime-gamma", "17.0.15")
	cache := filepath.Join(root, "cache")

	NewDetector("", cache, []string{root}).Detect(context.Background())
	d := NewDetector("", cache, []string{root})
	d.Rescan = true
	d.Detect(context.Background())

	if n := invocations(t, counter); n != 2 {
		t.Errorf("java ran %d times, want 2 with Rescan", n)
	}
}

// TestDetectWithoutCachePathProbesEveryTime documents the opt-in.
func TestDetectWithoutCachePathProbesEveryTime(t *testing.T) {
	root := t.TempDir()
	_, counter := fakeJava(t, root, "jre-legacy", "1.8.0_392")

	d := &Detector{ExtraRoots: []string{root}}
	d.Detect(context.Background())
	d.Detect(context.Background())
	if n := invocations(t, counter); n != 2 {
		t.Errorf("java ran %d times, want 2 without a cache", n)
	}
}

// TestDetectProbesBrokenRuntimesOnce: an unusable runtime is remembered as
// such, since it costs the full probe timeout to rediscover.
func TestDetectRemembersBrokenRuntimes(t *testing.T) {
	root := t.TempDir()
	javaPath, _ := fakeJava(t, root, "java-runtime-delta", "21.0.7")
	// Strip the executable bit: the classic broken copy.
	if err := os.Chmod(javaPath, 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, "cache")

	first := NewDetector("", cache, []string{root}).Detect(context.Background())
	rt, ok := find(first, javaPath)
	if !ok || !rt.Broken {
		t.Fatalf("expected a broken runtime, got %+v (found=%v)", rt, ok)
	}

	// Repairing changes the mode, which must invalidate the entry.
	if err := os.Chmod(javaPath, 0o755); err != nil {
		t.Fatal(err)
	}
	second := NewDetector("", cache, []string{root}).Detect(context.Background())
	rt, ok = find(second, javaPath)
	if !ok || rt.Broken {
		t.Fatalf("repaired runtime still reported broken: %+v", rt)
	}
	if rt.Major != 21 {
		t.Errorf("repaired runtime major = %d, want 21", rt.Major)
	}
}
