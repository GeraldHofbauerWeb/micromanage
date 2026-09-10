package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/auth"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
)

// TestVersionIDForKeepsStoredIDForOwnLoader checks that the id detected from
// disk is used unchanged when the user has not overridden the loader. That id
// is the one we know exists in the store.
func TestVersionIDForKeepsStoredID(t *testing.T) {
	meta := instance.Meta{
		Name:              "sebsmodpack5",
		MinecraftVersion:  "1.21.1",
		Loader:            instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.248"},
		ResolvedVersionID: "neoforge-21.1.248",
	}

	got, err := versionIDFor(meta, meta.Loader)
	if err != nil {
		t.Fatalf("versionIDFor: %v", err)
	}
	if got != "neoforge-21.1.248" {
		t.Errorf("version id = %q, want the stored one", got)
	}
}

// TestVersionIDForRebuildsOnOverride is the other half of step 3: once the
// user picks a different system, the stored id belongs to the old choice and
// has to be rebuilt.
func TestVersionIDForRebuildsOnOverride(t *testing.T) {
	meta := instance.Meta{
		Name:              "pack",
		MinecraftVersion:  "1.20.1",
		Loader:            instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.248"},
		ResolvedVersionID: "neoforge-21.1.248",
	}

	cases := []struct {
		loader instance.LoaderSpec
		want   string
	}{
		{instance.LoaderSpec{Type: instance.LoaderVanilla}, "1.20.1"},
		{instance.LoaderSpec{Type: instance.LoaderForge, Version: "47.4.0"}, "1.20.1-forge-47.4.0"},
		{instance.LoaderSpec{Type: instance.LoaderFabric, Version: "0.15.7"}, "fabric-loader-0.15.7-1.20.1"},
		{instance.LoaderSpec{Type: instance.LoaderQuilt, Version: "0.20.0"}, "quilt-loader-0.20.0-1.20.1"},
		{instance.LoaderSpec{Type: instance.LoaderNeoForge, Version: "21.1.100"}, "neoforge-21.1.100"},
	}

	for _, tc := range cases {
		got, err := versionIDFor(meta, tc.loader)
		if err != nil {
			t.Errorf("versionIDFor(%v): %v", tc.loader, err)
			continue
		}
		if got != tc.want {
			t.Errorf("versionIDFor(%v) = %q, want %q", tc.loader, got, tc.want)
		}
	}
}

func TestVersionIDForRejectsIncomplete(t *testing.T) {
	// A loader with no version cannot name a profile.
	meta := instance.Meta{Name: "pack", MinecraftVersion: "1.20.1"}
	for _, lt := range []instance.LoaderType{
		instance.LoaderNeoForge, instance.LoaderForge, instance.LoaderFabric, instance.LoaderQuilt,
	} {
		if _, err := versionIDFor(meta, instance.LoaderSpec{Type: lt}); err == nil {
			t.Errorf("%s with no version was accepted", lt)
		}
	}

	// An unconfigured instance cannot launch at all.
	empty := instance.Meta{Name: "fresh"}
	if _, err := versionIDFor(empty, instance.LoaderSpec{Type: instance.LoaderVanilla}); err == nil {
		t.Error("an instance with no Minecraft version was accepted")
	}
}

// TestStoreSnapshotIsACopy guards the render loop: it reads a snapshot while
// the pump writes, so a snapshot sharing backing arrays would be a data race
// and a torn frame.
func TestStoreSnapshotIsACopy(t *testing.T) {
	s := NewStore()
	s.SetInstances([]instance.Instance{{Name: "a"}, {Name: "b"}})
	s.SetConfig(map[string]string{"minecraft-path": "/one"})
	s.SetTask(Task{ID: 1, Steps: []string{"metadata"}})

	snap := s.Snapshot()
	snap.Instances[0].Name = "mutated"
	snap.Config["minecraft-path"] = "/two"
	snap.Task.Steps[0] = "mutated"

	again := s.Snapshot()
	if again.Instances[0].Name != "a" {
		t.Error("mutating a snapshot changed the store's instances")
	}
	if again.Config["minecraft-path"] != "/one" {
		t.Error("mutating a snapshot changed the store's config")
	}
	if again.Task.Steps[0] != "metadata" {
		t.Error("mutating a snapshot changed the store's task steps")
	}
}

func TestSetInstancesClearsStaleSelection(t *testing.T) {
	s := NewStore()
	s.SetInstances([]instance.Instance{{Name: "a"}, {Name: "b"}})
	s.SetSelected("b")

	s.SetInstances([]instance.Instance{{Name: "a"}})
	if got := s.Snapshot().Selected; got != "" {
		t.Errorf("selection = %q after the instance vanished, want empty", got)
	}

	s.SetSelected("a")
	s.SetInstances([]instance.Instance{{Name: "a"}, {Name: "c"}})
	if got := s.Snapshot().Selected; got != "a" {
		t.Errorf("selection = %q, want it kept across a refresh", got)
	}
}

// TestUpdateTaskIgnoresStaleIDs stops a cancelled task's late progress from
// overwriting the task that replaced it.
func TestUpdateTaskIgnoresStaleIDs(t *testing.T) {
	s := NewStore()
	s.SetTask(Task{ID: 2, Label: "current"})

	s.UpdateTask(1, func(t *Task) { t.Label = "stale" })
	if got := s.Snapshot().Task.Label; got != "current" {
		t.Errorf("label = %q, want the current task untouched", got)
	}

	s.UpdateTask(2, func(t *Task) { t.Label = "updated" })
	if got := s.Snapshot().Task.Label; got != "updated" {
		t.Errorf("label = %q, want the update applied", got)
	}
}

func TestTaskRunning(t *testing.T) {
	if !(Task{ID: 1}).Running() {
		t.Error("a fresh task is not running")
	}
	if (Task{ID: 1, Done: true}).Running() {
		t.Error("a finished task reports running")
	}
	if (Task{ID: 1, Err: errTest}).Running() {
		t.Error("a failed task reports running")
	}
}

var errTest = errStr("boom")

type errStr string

func (e errStr) Error() string { return string(e) }

// --- accounts ---

func TestAccountStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()

	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore: %v", err)
	}
	if _, ok := store.Active(); ok {
		t.Error("a fresh store reported an active account")
	}

	account, err := auth.NewOfflineAccount("Gerry")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(account); err != nil {
		t.Fatalf("Add: %v", err)
	}

	active, ok := store.Active()
	if !ok || active.Name != "Gerry" {
		t.Fatalf("active = %+v, %v", active, ok)
	}

	// A second store over the same directory must see it.
	reloaded, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	accounts, activeUUID := reloaded.List()
	if len(accounts) != 1 || activeUUID != account.UUID {
		t.Errorf("reloaded %d account(s), active %q", len(accounts), activeUUID)
	}
}

// TestAccountStorePermissions matters because a Microsoft refresh token ends
// up in this file.
func TestAccountStorePermissions(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := auth.NewOfflineAccount("Gerry")
	if err := store.Add(account); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, accountsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("accounts file mode = %v, want 0600", perm)
	}
}

func TestAccountStoreReplaceAndRemove(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewAccountStore(dir)

	first, _ := auth.NewOfflineAccount("Gerry")
	second, _ := auth.NewOfflineAccount("Sebi")
	_ = store.Add(first)
	_ = store.Add(second)

	accounts, active := store.List()
	if len(accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(accounts))
	}
	if active != second.UUID {
		t.Error("the newly added account did not become active")
	}

	// Adding the same identity again replaces rather than duplicates.
	_ = store.Add(first)
	if accounts, _ := store.List(); len(accounts) != 2 {
		t.Errorf("re-adding an account produced %d entries", len(accounts))
	}

	if err := store.SetActive(second.UUID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := store.SetActive("nonsense"); err == nil {
		t.Error("SetActive accepted an unknown id")
	}

	if err := store.Remove(second.UUID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	active2, ok := store.Active()
	if !ok || active2.UUID != first.UUID {
		t.Error("removing the active account did not fall back to the other one")
	}
}

// TestAccountStoreRefusesNewerSchema mirrors the instance metadata guard.
func TestAccountStoreRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, accountsFileName)
	if err := os.WriteFile(path, []byte(`{"schema_version":99,"accounts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewAccountStore(dir); err == nil {
		t.Fatal("a newer schema was accepted")
	}
}
