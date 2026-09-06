package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileKind identifies which of an instance's content directories a file lives in.
type FileKind string

const (
	KindMod    FileKind = "mod"
	KindConfig FileKind = "config"
	KindSave   FileKind = "save"
)

// dir maps a kind to its directory name inside an instance.
func (k FileKind) dir() (string, error) {
	switch k {
	case KindMod:
		return "mods", nil
	case KindConfig:
		return "config", nil
	case KindSave:
		return "saves", nil
	default:
		return "", fmt.Errorf("unknown file type: %s", k)
	}
}

// validateName rejects anything that is not a single, literal path component.
// Comparing against filepath.Base in one shot rules out "..", "a/b", absolute
// paths and trailing separators alike; label names the value for the error.
func validateName(label, name string) error {
	if name == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid %s: %q", label, name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid %s: contains a null byte", label)
	}
	if name != filepath.Base(name) || strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("invalid %s: %q must be a plain name, not a path", label, name)
	}
	return nil
}

// ValidateInstanceName reports whether name is usable as an instance directory name.
func ValidateInstanceName(name string) error {
	return validateName("instance name", name)
}

// InstancePath returns the absolute directory of an instance, rejecting names
// that would escape InstancesPath.
func (m *Manager) InstancePath(name string) (string, error) {
	if err := ValidateInstanceName(name); err != nil {
		return "", err
	}
	return filepath.Join(m.InstancesPath, name), nil
}

// CanDelete reports whether an instance may be removed. It is the single
// source of truth for the "cannot delete the active instance" rule.
func (m *Manager) CanDelete(name string) error {
	instancePath, err := m.InstancePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(instancePath); os.IsNotExist(err) {
		return fmt.Errorf("instance '%s' does not exist", name)
	}
	if m.GetActiveInstance() == name {
		return fmt.Errorf("cannot delete active instance '%s'. Switch to another instance first", name)
	}
	return nil
}

// DeleteInstanceFile removes a single mod, config file or save from an
// instance. Both the instance name and the file name are validated, and the
// resolved path is re-checked against the instance directory, so a crafted
// name cannot reach outside it.
//
// Note that a save is a directory: deleting one removes the whole world.
func (m *Manager) DeleteInstanceFile(instanceName string, kind FileKind, name string) error {
	instancePath, err := m.InstancePath(instanceName)
	if err != nil {
		return err
	}
	subdir, err := kind.dir()
	if err != nil {
		return err
	}
	if err := validateName("file name", name); err != nil {
		return err
	}

	target := filepath.Join(instancePath, subdir, name)

	// Belt and braces: the name checks above already rule this out, but a
	// containment check costs nothing and survives future refactoring.
	rel, err := filepath.Rel(instancePath, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid file name: %q escapes the instance directory", name)
	}

	// Lstat, not Stat: a dangling symlink still exists and should be removable.
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", name)
	}

	return os.RemoveAll(target)
}
