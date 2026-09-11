package instance

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeMinecraft fills m.MinecraftPath with what the official launcher leaves
// behind: a world, a mod, a screenshot, the options, its profiles and a
// downloaded version.
func fakeMinecraft(t *testing.T, m *Manager) {
	t.Helper()
	files := map[string]string{
		"saves/World/level.dat":              "level",
		"mods/sodium.jar":                    "jar",
		"screenshots/2026-09-01.png":         "png",
		"config/sodium.json":                 "{}",
		OptionsFile:                          "fov:0.0\n",
		"versions/1.21.1/1.21.1.json":        "{}",
		"libraries/org/lwjgl/lwjgl.jar":      "lib",
		"assets/indexes/17.json":             "{}",
		"launcher_profiles.json":             `{"profiles":{"a":{"name":"Latest release","type":"custom","lastVersionId":"1.21.1","javaArgs":"-Xmx6G","lastUsed":"2026-09-01T10:00:00.000Z"}}}`,
		"logs/latest.log":                    "log",
		"launcher_log.txt":                   "cruft",
		"resourcepacks/Faithful/pack.mcmeta": "{}",
	}
	for rel, content := range files {
		writeFile(t, filepath.Join(m.MinecraftPath, filepath.FromSlash(rel)), content)
	}
}

// treeState records every entry under root, and for files their size and
// modification time, so a test can tell whether anything in it changed. A
// directory's own time is left out: Windows updates it lazily after files
// are written inside, which would read as a change nobody made.
func treeState(t *testing.T, root string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			state[rel] = "dir"
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		state[rel] = fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestImportCopiesMinecraftAndLeavesItAlone(t *testing.T) {
	m := newTestManager(t)

	if m.CanImport() == nil {
		t.Fatal("nothing to import yet, but CanImport said yes")
	}
	if err := os.MkdirAll(m.MinecraftPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if m.CanImport() == nil {
		t.Fatal("an empty .minecraft is not worth importing")
	}
	fakeMinecraft(t, m)
	if err := m.CanImport(); err != nil {
		t.Fatalf("a populated .minecraft should be importable: %v", err)
	}
	before := treeState(t, m.MinecraftPath)

	res, err := m.ImportMinecraft(ImportOptions{IncludeSaves: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != DefaultInstanceName || res.Path != filepath.Join(m.InstancesPath, DefaultInstanceName) {
		t.Errorf("result = %+v", res)
	}

	for _, rel := range []string{"saves/World/level.dat", "mods/sodium.jar", "config/sodium.json", OptionsFile, "resourcepacks/Faithful/pack.mcmeta"} {
		if _, err := os.Stat(filepath.Join(res.Path, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s did not come along: %v", rel, err)
		}
	}
	// Screenshots were not asked for; the game files belong in the store and
	// the logs and launcher files to the official launcher.
	for _, rel := range []string{"screenshots", "versions", "libraries", "assets", "logs", "launcher_log.txt"} {
		if _, err := os.Stat(filepath.Join(res.Path, rel)); !os.IsNotExist(err) {
			t.Errorf("%s was copied: %v", rel, err)
		}
	}

	if !res.Detected || res.Meta.MinecraftVersion != "1.21.1" || res.Meta.Memory.MaxMB != 6144 {
		t.Errorf("detected meta = %+v", res.Meta)
	}
	if meta, found, err := LoadMeta(res.Path); err != nil || !found || meta.Name != DefaultInstanceName {
		t.Errorf("saved meta = %+v found=%v err=%v", meta, found, err)
	}

	after := treeState(t, m.MinecraftPath)
	if len(after) != len(before) {
		t.Errorf(".minecraft changed: %d entries before, %d after", len(before), len(after))
	}
	for rel, was := range before {
		if now := after[rel]; now != was {
			t.Errorf(".minecraft changed at %s: %q, now %q", rel, was, now)
		}
	}

	instances, err := m.ListInstances()
	if err != nil || len(instances) != 1 || instances[0].SaveCount != 1 {
		t.Errorf("instances = %+v (%v)", instances, err)
	}

	// The same name twice is refused; another name is a second copy.
	if _, err := m.ImportMinecraft(ImportOptions{}); err == nil {
		t.Error("importing onto an existing instance succeeded")
	}
	second, err := m.ImportMinecraft(ImportOptions{Name: "Second", IncludeScreenshots: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second.Path, "screenshots", "2026-09-01.png")); err != nil {
		t.Errorf("screenshots were asked for: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second.Path, "saves", "World")); !os.IsNotExist(err) {
		t.Errorf("worlds were not asked for: %v", err)
	}
}

func TestImportRefusesInstancesInsideMinecraft(t *testing.T) {
	m := newTestManager(t)
	m.InstancesPath = filepath.Join(m.MinecraftPath, "instances")
	fakeMinecraft(t, m)

	if m.CanImport() == nil {
		t.Error("instances inside .minecraft must not be importable")
	}
	if _, err := m.ImportMinecraft(ImportOptions{Name: "x"}); err == nil {
		t.Error("importing should have failed")
	}
}

func TestImportRefusesALink(t *testing.T) {
	m := newTestManager(t)
	target := mkInstance(t, m, "pack")
	if err := linkDir(target, m.MinecraftPath); err != nil {
		t.Skipf("links unavailable: %v", err)
	}
	if m.CanImport() == nil {
		t.Error("a .minecraft that is a link must not be importable")
	}
}

func TestCancelledImportLeavesNoInstance(t *testing.T) {
	m := newTestManager(t)
	fakeMinecraft(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.ImportMinecraft(ImportOptions{Ctx: ctx}); err == nil {
		t.Fatal("a cancelled import succeeded")
	}
	if _, err := os.Lstat(filepath.Join(m.InstancesPath, DefaultInstanceName)); !os.IsNotExist(err) {
		t.Errorf("a cancelled import left its instance behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.MinecraftPath, OptionsFile)); err != nil {
		t.Errorf(".minecraft lost a file: %v", err)
	}
}

func TestReleaseLegacyLinkPutsTheOriginalBack(t *testing.T) {
	m := newTestManager(t)
	target := mkInstance(t, m, "pack")
	writeFile(t, filepath.Join(m.BackupPath, OptionsFile), "fov:0.0\n")
	if err := linkDir(target, m.MinecraftPath); err != nil {
		t.Skipf("links unavailable: %v", err)
	}

	released, err := m.ReleaseLegacyLink()
	if err != nil || !released {
		t.Fatalf("released = %v, err = %v", released, err)
	}
	info, err := os.Lstat(m.MinecraftPath)
	if err != nil || !info.IsDir() || isLinkInfo(m.MinecraftPath, info) {
		t.Fatalf(".minecraft is not a plain directory again: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.MinecraftPath, OptionsFile)); err != nil {
		t.Errorf("the original did not come back: %v", err)
	}
	if _, err := os.Stat(m.BackupPath); !os.IsNotExist(err) {
		t.Errorf("the backup is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "mods")); err != nil {
		t.Errorf("the instance was harmed: %v", err)
	}

	// Nothing left to do the second time.
	if released, err := m.ReleaseLegacyLink(); released || err != nil {
		t.Errorf("second release = %v, %v", released, err)
	}
}

func TestReleaseLegacyLinkWithoutBackup(t *testing.T) {
	m := newTestManager(t)
	target := mkInstance(t, m, "pack")
	if err := linkDir(target, m.MinecraftPath); err != nil {
		t.Skipf("links unavailable: %v", err)
	}
	if released, err := m.ReleaseLegacyLink(); !released || err != nil {
		t.Fatalf("released = %v, err = %v", released, err)
	}
	if _, err := os.Lstat(m.MinecraftPath); !os.IsNotExist(err) {
		t.Errorf("the link is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "mods")); err != nil {
		t.Errorf("the instance was harmed: %v", err)
	}
}

func TestReleaseLegacyLinkLeavesForeignLinksAlone(t *testing.T) {
	m := newTestManager(t)
	elsewhere := filepath.Join(m.HomeDir, "my-own-minecraft")
	writeFile(t, filepath.Join(elsewhere, OptionsFile), "fov:0.0\n")
	if err := linkDir(elsewhere, m.MinecraftPath); err != nil {
		t.Skipf("links unavailable: %v", err)
	}

	if released, err := m.ReleaseLegacyLink(); released || err != nil {
		t.Fatalf("released = %v, err = %v", released, err)
	}
	if !isDirLink(m.MinecraftPath) {
		t.Error("a link the player made was removed")
	}
}

func TestLastInstanceIsRememberedAndForgotten(t *testing.T) {
	m := newTestManager(t)
	mkInstance(t, m, "pack")

	if got := m.LastInstance(); got != "" {
		t.Fatalf("last instance = %q before any was picked", got)
	}
	if err := m.SetLastInstance("pack"); err != nil {
		t.Fatal(err)
	}

	// A fresh manager reads it back from the file.
	again := newTestManagerAt(m)
	if err := again.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := again.LastInstance(); got != "pack" {
		t.Errorf("last instance after reload = %q, want pack", got)
	}

	if err := os.RemoveAll(filepath.Join(m.InstancesPath, "pack")); err != nil {
		t.Fatal(err)
	}
	if got := again.LastInstance(); got != "" {
		t.Errorf("a deleted instance is still offered: %q", got)
	}
}

// newTestManagerAt is a second manager over the same directories, the way a
// restart would see them.
func newTestManagerAt(m *Manager) *Manager {
	return &Manager{
		HomeDir:       m.HomeDir,
		AppDir:        m.AppDir,
		ConfigFile:    m.ConfigFile,
		InstancesPath: m.InstancesPath,
		MinecraftPath: m.MinecraftPath,
		BackupPath:    m.BackupPath,
	}
}
