package instance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The game keeps every setting a player changes in-game — keybinds, video
// settings, the resource pack order, the render distance — in one file,
// options.txt, and rewrites it whenever something changes. A pack that ships
// its own, a mod that resets it, or a session of experimenting can all lose
// a setup that took an evening to get right. Snapshots keep copies of that
// file beside the instance so any of them can be put back.
const (
	// OptionsFile is the game's settings file inside an instance.
	OptionsFile = "options.txt"
	// OptionsSnapshotsDir holds an instance's saved copies of OptionsFile.
	// It lives inside the instance so a duplicate carries them along.
	OptionsSnapshotsDir = "options-snapshots"
	// LatestSnapshot names the newest snapshot wherever a name is accepted.
	LatestSnapshot = "latest"

	snapshotStamp     = "20060102-150405"
	snapshotSeparator = "--"
	snapshotExt       = ".txt"
)

// OptionsInfo describes an instance's current options.txt.
type OptionsInfo struct {
	Path    string
	Exists  bool
	Size    int64
	ModTime time.Time
}

// OptionsSnapshot is one saved copy of options.txt.
type OptionsSnapshot struct {
	// Name is the file name, which is what the mutating methods accept.
	Name string
	Path string
	// Label is what the snapshot was saved as; empty when it was saved
	// without one.
	Label string
	Time  time.Time
	Size  int64
	// Changes counts the settings that differ from the instance's current
	// options.txt — what restoring this snapshot would change. It is -1
	// when there is no current file to compare with.
	Changes int
	// written is the file's own time, finer than the second in the name,
	// so two snapshots from one second still list in the order they were
	// made.
	written time.Time
}

// Display is the label, or the time when there is none.
func (s OptionsSnapshot) Display() string {
	if s.Label != "" {
		return s.Label
	}
	return s.Time.Format("2 Jan 2006 15:04")
}

// OptionChange is one setting that differs between two options files. An
// empty Old means the key is new; an empty New means it went away.
type OptionChange struct {
	Key, Old, New string
}

// OptionsPath returns where an instance's options.txt lives.
func (m *Manager) OptionsPath(instanceName string) (string, error) {
	dir, err := m.InstancePath(instanceName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, OptionsFile), nil
}

// OptionsInfo reports whether an instance has an options.txt and how big
// and how old it is.
func (m *Manager) OptionsInfo(instanceName string) (OptionsInfo, error) {
	path, err := m.OptionsPath(instanceName)
	if err != nil {
		return OptionsInfo{}, err
	}
	info := OptionsInfo{Path: path}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return info, err
	}
	info.Exists, info.Size, info.ModTime = true, st.Size(), st.ModTime()
	return info, nil
}

// ListOptionsSnapshots returns an instance's snapshots, newest first, each
// with how many settings it differs from the current file in.
func (m *Manager) ListOptionsSnapshots(instanceName string) ([]OptionsSnapshot, error) {
	dir, err := m.snapshotsDir(instanceName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	optionsPath, err := m.OptionsPath(instanceName)
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(optionsPath)
	hasCurrent := err == nil

	var out []OptionsSnapshot
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), snapshotExt) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		s := snapshotFromFile(filepath.Join(dir, e.Name()), info.Size(), info.ModTime())
		s.Changes = -1
		if hasCurrent {
			if data, err := os.ReadFile(s.Path); err == nil {
				s.Changes = len(DiffOptions(current, data))
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].written.Equal(out[j].written) {
			return out[i].written.After(out[j].written)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// SaveOptions copies the instance's options.txt into a new snapshot. The
// label is optional and becomes part of the file name, so it survives
// without any index to keep in step.
func (m *Manager) SaveOptions(instanceName, label string) (OptionsSnapshot, error) {
	src, err := m.OptionsPath(instanceName)
	if err != nil {
		return OptionsSnapshot{}, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return OptionsSnapshot{}, fmt.Errorf("%s has no %s yet; the game writes it on the first run", instanceName, OptionsFile)
		}
		return OptionsSnapshot{}, err
	}

	dir, err := m.snapshotsDir(instanceName)
	if err != nil {
		return OptionsSnapshot{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return OptionsSnapshot{}, err
	}

	now := time.Now()
	base := now.Format(snapshotStamp)
	if clean := cleanLabel(label); clean != "" {
		base += snapshotSeparator + clean
	}
	path := filepath.Join(dir, base+snapshotExt)
	// Two saves within a second get distinct names rather than one file.
	for n := 2; ; n++ {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, n, snapshotExt))
	}

	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return OptionsSnapshot{}, err
	}
	return snapshotFromFile(path, int64(len(data)), now), nil
}

// RestoreOptions puts a snapshot back as the instance's options.txt. When
// the current file differs from the snapshot it is saved first, labelled
// "before restore", so a restore is itself undoable. The returned snapshot
// is that safety copy; ok is false when none was needed.
func (m *Manager) RestoreOptions(instanceName, snapshot string) (kept OptionsSnapshot, ok bool, err error) {
	src, err := m.snapshotPath(instanceName, snapshot)
	if err != nil {
		return OptionsSnapshot{}, false, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return OptionsSnapshot{}, false, err
	}
	dst, err := m.OptionsPath(instanceName)
	if err != nil {
		return OptionsSnapshot{}, false, err
	}

	if current, err := os.ReadFile(dst); err == nil && !bytes.Equal(current, data) {
		kept, err = m.SaveOptions(instanceName, "before restore")
		if err != nil {
			return OptionsSnapshot{}, false, err
		}
		ok = true
	}
	if err := writeFileAtomic(dst, data, 0o644); err != nil {
		return OptionsSnapshot{}, false, err
	}
	return kept, ok, nil
}

// DeleteOptionsSnapshot removes one snapshot.
func (m *Manager) DeleteOptionsSnapshot(instanceName, snapshot string) error {
	path, err := m.snapshotPath(instanceName, snapshot)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// OptionsDiff lists the settings that differ between the instance's current
// options.txt and a snapshot: what a restore would change.
func (m *Manager) OptionsDiff(instanceName, snapshot string) ([]OptionChange, error) {
	path, err := m.snapshotPath(instanceName, snapshot)
	if err != nil {
		return nil, err
	}
	want, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	optionsPath, err := m.OptionsPath(instanceName)
	if err != nil {
		return nil, err
	}
	have, err := os.ReadFile(optionsPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return DiffOptions(have, want), nil
}

// ParseOptions reads options.txt's key:value lines. The order is kept so a
// rewrite can preserve it; unknown lines are returned under their whole
// text so nothing is dropped.
func ParseOptions(data []byte) (keys []string, values map[string]string) {
	values = map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			key, value = line, ""
		}
		if _, dup := values[key]; !dup {
			keys = append(keys, key)
		}
		values[key] = value
	}
	return keys, values
}

// DiffOptions compares two options files setting by setting. The result is
// what applying "to" over "from" changes, sorted by key.
func DiffOptions(from, to []byte) []OptionChange {
	_, old := ParseOptions(from)
	_, new := ParseOptions(to)

	var changes []OptionChange
	for key, value := range new {
		if before, ok := old[key]; !ok || before != value {
			changes = append(changes, OptionChange{Key: key, Old: before, New: value})
		}
	}
	for key, value := range old {
		if _, ok := new[key]; !ok {
			changes = append(changes, OptionChange{Key: key, Old: value})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Key < changes[j].Key })
	return changes
}

func (m *Manager) snapshotsDir(instanceName string) (string, error) {
	dir, err := m.InstancePath(instanceName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, OptionsSnapshotsDir), nil
}

// snapshotPath resolves a snapshot name, or LatestSnapshot, to its file.
// Only plain file names are accepted, so a name can never reach outside
// the snapshots directory.
func (m *Manager) snapshotPath(instanceName, snapshot string) (string, error) {
	if snapshot == LatestSnapshot {
		list, err := m.ListOptionsSnapshots(instanceName)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "", fmt.Errorf("%s has no options snapshots", instanceName)
		}
		return list[0].Path, nil
	}
	if snapshot == "" || filepath.Base(snapshot) != snapshot || snapshot == "." || snapshot == ".." {
		return "", fmt.Errorf("invalid snapshot name %q", snapshot)
	}
	dir, err := m.snapshotsDir(instanceName)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, snapshot)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s has no snapshot %q", instanceName, snapshot)
		}
		return "", err
	}
	return path, nil
}

// snapshotFromFile reads the stamp and label back out of a file name.
func snapshotFromFile(path string, size int64, modTime time.Time) OptionsSnapshot {
	name := filepath.Base(path)
	s := OptionsSnapshot{Name: name, Path: path, Size: size, Time: modTime, written: modTime}

	base := strings.TrimSuffix(name, snapshotExt)
	stamp, label, _ := strings.Cut(base, snapshotSeparator)
	// A duplicate-in-the-same-second suffix belongs to neither.
	if label == "" {
		if i := strings.LastIndex(stamp, "-"); i > len(snapshotStamp)-1 {
			stamp = stamp[:i]
		}
	}
	if t, err := time.ParseInLocation(snapshotStamp, stamp, time.Local); err == nil {
		s.Time = t
	}
	s.Label = label
	return s
}

// cleanLabel makes a label safe as part of a file name.
func cleanLabel(label string) string {
	label = strings.TrimSpace(label)
	var b strings.Builder
	for _, r := range label {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('-')
		case r < ' ':
			// Control characters have no place in a file name.
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 80 {
		out = strings.TrimSpace(out[:80])
	}
	return out
}

// writeFileAtomic writes data beside the target and renames it into place,
// so a crash mid-write never leaves a truncated options.txt.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
