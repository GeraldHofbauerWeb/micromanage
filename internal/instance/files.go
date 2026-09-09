package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
