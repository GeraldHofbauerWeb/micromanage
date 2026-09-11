package instance

// ConfigKey describes one entry of the manager configuration. It is the single
// source of truth for both the display order and which keys may be written —
// GetConfig returns a map, so without this the CLI and GUI would each invent
// their own (and non-deterministic) ordering.
type ConfigKey struct {
	Key         string
	Label       string
	Description string
	Editable    bool
}

// ConfigKeys returns the configuration keys in canonical display order.
// app-dir and config-file are derived at startup and rejected by UpdateConfig,
// so they are marked read-only rather than offered for editing.
func ConfigKeys() []ConfigKey {
	return []ConfigKey{
		{
			Key:         "minecraft-path",
			Label:       "Minecraft directory",
			Description: "The official launcher's directory. It is only read, to import it as an instance.",
			Editable:    true,
		},
		{
			Key:         "instances-path",
			Label:       "Instances directory",
			Description: "Where instance directories are stored. Each is the game directory of its instance.",
			Editable:    true,
		},
		{
			Key:         "msa-client-id",
			Label:       "Microsoft application id",
			Description: "The Azure application id Microsoft sign-in runs against. Empty leaves local accounts as the only option.",
			Editable:    true,
		},
		{
			Key:         "app-dir",
			Label:       "Application directory",
			Description: "Derived from the OS config directory.",
			Editable:    false,
		},
		{
			Key:         "config-file",
			Label:       "Configuration file",
			Description: "Derived from the application directory.",
			Editable:    false,
		},
	}
}

// IsEditableConfigKey reports whether a key may be passed to UpdateConfig.
func IsEditableConfigKey(key string) bool {
	for _, k := range ConfigKeys() {
		if k.Key == key {
			return k.Editable
		}
	}
	return false
}
