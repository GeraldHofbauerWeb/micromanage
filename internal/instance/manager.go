package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	InstancesDir  = ".minecraft-instances"
	MinecraftDir  = ".minecraft"
	AppFolderName = "instant-launcher" // folder inside OS config/app support dir
)

type Config struct {
	InstancesPath string `json:"instances_path"`
	MinecraftPath string `json:"minecraft_path"`
	// BackupPath is where versions before 2 parked the original .minecraft
	// while it was a link; it is only read to put that directory back.
	BackupPath string `json:"backup_path"`
	// MSAClientID is the Azure application id Microsoft sign-in runs against.
	// It lives here rather than only in the binary so a player can point the
	// launcher at their own registration without rebuilding it.
	MSAClientID string `json:"msa_client_id,omitempty"`
	// LastInstance is the instance the player picked last, which the start
	// screen offers to play.
	LastInstance string `json:"last_instance,omitempty"`
}

type Manager struct {
	HomeDir    string
	AppDir     string
	ConfigFile string
	// InstancesPath holds the instances, each a directory the game is started
	// in directly.
	InstancesPath string
	// MinecraftPath is the official launcher's directory. The launcher only
	// ever reads it, to import it as an instance.
	MinecraftPath string
	// BackupPath is where versions before 2 parked the original .minecraft;
	// see ReleaseLegacyLink.
	BackupPath  string
	MSAClientID string

	// LegacyReleased reports that NewManager found .minecraft still linked to
	// an instance, the way versions before 2 left it, and made it a plain
	// directory again. LegacyErr is set when that went wrong.
	LegacyReleased bool
	LegacyErr      error

	// cfgMu guards cfg: the window's workers save the last instance while
	// another may be changing a path.
	cfgMu sync.Mutex
	cfg   Config
}

type Instance struct {
	Name string
	Path string
	// ModCount counts the mods that will load; DisabledMods the ones
	// switched off and kept.
	ModCount     int
	DisabledMods int
	ConfigCount  int
	SaveCount    int

	// v2 metadata, read from instance.json. Instances created before it
	// existed report Configured false and leave the rest zero.
	Configured       bool
	MinecraftVersion string
	Loader           LoaderSpec
	LastPlayed       time.Time
}

func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user config directory: %w", err)
	}

	appDir := filepath.Join(userConfigDir, AppFolderName)
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create app config dir: %w", err)
	}

	configFile := filepath.Join(appDir, "config.json")

	// Get platform-specific default Minecraft path
	defaultMinecraftPath, err := getDefaultMinecraftPath()
	if err != nil {
		return nil, fmt.Errorf("failed to determine default Minecraft path: %w", err)
	}

	// Other defaults
	defaultInstancesPath := filepath.Join(appDir, "instances")
	defaultBackupPath := filepath.Join(appDir, "backup")

	m := &Manager{
		HomeDir:       homeDir,
		AppDir:        appDir,
		ConfigFile:    configFile,
		InstancesPath: defaultInstancesPath,
		MinecraftPath: defaultMinecraftPath,
		BackupPath:    defaultBackupPath,
		cfg: Config{
			InstancesPath: defaultInstancesPath,
			MinecraftPath: defaultMinecraftPath,
			BackupPath:    defaultBackupPath,
		},
	}

	// Load config if exists, otherwise create it with defaults
	if err := m.loadConfig(); err != nil {
		// if load failed because file doesn't exist, save defaults
		if os.IsNotExist(err) {
			if err := os.MkdirAll(m.InstancesPath, 0755); err != nil {
				return nil, fmt.Errorf("failed to create default instances dir: %w", err)
			}
			if err := m.saveConfig(); err != nil {
				return nil, fmt.Errorf("failed to write default config: %w", err)
			}
		} else {
			return nil, err
		}
	} else {
		// apply loaded config
		m.InstancesPath = m.cfg.InstancesPath
		m.MinecraftPath = m.cfg.MinecraftPath
		m.BackupPath = m.cfg.BackupPath
		m.MSAClientID = m.cfg.MSAClientID
		// ensure instances dir exists
		if err := os.MkdirAll(m.InstancesPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create instances dir from config: %w", err)
		}
	}

	m.LegacyReleased, m.LegacyErr = m.ReleaseLegacyLink()
	return m, nil
}

// ReleaseLegacyLink undoes what versions before 2 did to .minecraft: they
// replaced it with a link to the active instance and parked the original at
// BackupPath. The link is removed and the original put back, so the official
// launcher finds its own directory again. The instances are not touched.
//
// Only a link into InstancesPath is removed; a link the player made
// themselves is none of the launcher's business.
func (m *Manager) ReleaseLegacyLink() (bool, error) {
	if !isDirLink(m.MinecraftPath) {
		return false, nil
	}
	target, err := os.Readlink(m.MinecraftPath)
	if err != nil {
		return false, nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(m.MinecraftPath), target)
	}
	if m.InstancesPath == "" || !within(target, m.InstancesPath) {
		return false, nil
	}

	// Removing a symlink or a junction removes the link alone, never what it
	// points at.
	if err := os.Remove(m.MinecraftPath); err != nil {
		return false, fmt.Errorf("removing the link at %s: %w", m.MinecraftPath, err)
	}
	if m.BackupPath == "" {
		return true, nil
	}
	if info, err := os.Stat(m.BackupPath); err == nil && info.IsDir() {
		if err := os.Rename(m.BackupPath, m.MinecraftPath); err != nil {
			return true, fmt.Errorf("the link at %s is gone, but putting the original back from %s failed: %w",
				m.MinecraftPath, m.BackupPath, err)
		}
	}
	return true, nil
}

// ReloadConfig reads the configuration file again, the way a restart would.
func (m *Manager) ReloadConfig() error {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	return m.loadConfig()
}

func (m *Manager) loadConfig() error {
	data, err := os.ReadFile(m.ConfigFile)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// expand ~ in paths
	cfg.InstancesPath = expandPath(cfg.InstancesPath)
	cfg.MinecraftPath = expandPath(cfg.MinecraftPath)
	cfg.BackupPath = expandPath(cfg.BackupPath)

	// if any are empty, set defaults relative to app dir / platform-specific paths
	if cfg.InstancesPath == "" {
		cfg.InstancesPath = filepath.Join(m.AppDir, "instances")
	}
	if cfg.MinecraftPath == "" {
		if defaultPath, err := getDefaultMinecraftPath(); err == nil {
			cfg.MinecraftPath = defaultPath
		} else {
			// Fallback to the old behavior if detection fails
			cfg.MinecraftPath = filepath.Join(m.HomeDir, MinecraftDir)
		}
	}
	if cfg.BackupPath == "" {
		cfg.BackupPath = filepath.Join(m.AppDir, "backup")
	}

	m.cfg = cfg
	return nil
}

func (m *Manager) saveConfig() error {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	return m.writeConfig()
}

// writeConfig persists the configuration; the caller holds cfgMu.
func (m *Manager) writeConfig() error {
	m.cfg.InstancesPath = m.InstancesPath
	m.cfg.MinecraftPath = m.MinecraftPath
	m.cfg.BackupPath = m.BackupPath
	m.cfg.MSAClientID = m.MSAClientID

	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.ConfigFile), 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	if err := os.WriteFile(m.ConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

func expandPath(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// getDefaultMinecraftPath returns the platform-specific default Minecraft directory path
// This function automatically detects the operating system and returns the appropriate
// default path where Minecraft is typically installed on each platform.
func getDefaultMinecraftPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		// Windows: %APPDATA%\.minecraft (e.g., C:\Users\Username\AppData\Roaming\.minecraft)
		appDataDir := os.Getenv("APPDATA")
		if appDataDir != "" {
			return filepath.Join(appDataDir, ".minecraft"), nil
		}
		// Fallback to user profile if APPDATA environment variable is not set
		userProfile := os.Getenv("USERPROFILE")
		if userProfile != "" {
			return filepath.Join(userProfile, "AppData", "Roaming", ".minecraft"), nil
		}
		// Ultimate fallback - construct path from home directory
		return filepath.Join(homeDir, "AppData", "Roaming", ".minecraft"), nil

	case "darwin":
		// macOS: ~/Library/Application Support/minecraft
		// Note: Minecraft on macOS uses "minecraft" (lowercase) not ".minecraft"
		return filepath.Join(homeDir, "Library", "Application Support", "minecraft"), nil

	default:
		// Linux and other Unix-like systems: ~/.minecraft
		// This includes most Linux distributions, BSD variants, etc.
		return filepath.Join(homeDir, ".minecraft"), nil
	}
}

// LastInstance names the instance the player picked last, or "" when there is
// none or it has since gone.
func (m *Manager) LastInstance() string {
	m.cfgMu.Lock()
	name := m.cfg.LastInstance
	m.cfgMu.Unlock()
	if name == "" {
		return ""
	}
	path, err := m.InstancePath(name)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return ""
	}
	return name
}

// SetLastInstance remembers the instance the player picked last; "" forgets
// it. A manager without a configuration file keeps it in memory only.
func (m *Manager) SetLastInstance(name string) error {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	if m.cfg.LastInstance == name {
		return nil
	}
	m.cfg.LastInstance = name
	if m.ConfigFile == "" {
		return nil
	}
	return m.writeConfig()
}

// UpdateConfig updates one of the supported config keys and persists the file.
// Supported keys: "minecraft-path", "instances-path", "msa-client-id"
func (m *Manager) UpdateConfig(key, value string) error {
	// An application id is not a path, so it is set before the expansion the
	// path keys need.
	if key == "msa-client-id" {
		m.MSAClientID = strings.TrimSpace(value)
		return m.saveConfig()
	}

	value = expandPath(value)
	switch key {
	case "minecraft-path", "minecraft-dir", "minecraft":
		m.MinecraftPath = value
	case "instances-path", "instances-dir", "instances":
		m.InstancesPath = value
		// ensure instances dir exists
		if err := os.MkdirAll(m.InstancesPath, 0755); err != nil {
			return fmt.Errorf("failed to create instances dir: %w", err)
		}
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	if err := m.saveConfig(); err != nil {
		return err
	}
	return nil
}

// GetConfig returns current configuration as a map
func (m *Manager) GetConfig() map[string]string {
	return map[string]string{
		"minecraft-path": m.MinecraftPath,
		"instances-path": m.InstancesPath,
		"app-dir":        m.AppDir,
		"config-file":    m.ConfigFile,
		"msa-client-id":  m.MSAClientID,
	}
}

// essentialDirs are created in every instance regardless of how it was made.
var essentialDirs = []string{"mods", "config", "saves", "resourcepacks", "shaderpacks"}

// CreateOptions controls how a new instance is populated.
type CreateOptions struct {
	// CloneFrom names an existing instance to copy content from. Empty means
	// an empty skeleton, which is the default: cloning here costs gigabytes,
	// and the shared store supplies assets, libraries and versions anyway.
	CloneFrom string

	// CloneFromMinecraftDir copies the current .minecraft directory instead of
	// a named instance. This is what create did unconditionally before v2.
	CloneFromMinecraftDir bool

	// IncludeSaves and IncludeScreenshots opt into the two directories that
	// dominate a clone's size. Both default to off.
	IncludeSaves       bool
	IncludeScreenshots bool

	Ctx      context.Context
	Progress func(copied, total int64, current string)
}

// CreateInstance creates a new, empty instance.
func (m *Manager) CreateInstance(name string) error {
	return m.CreateInstanceWithOptions(name, CreateOptions{})
}

// CreateInstanceWithOptions creates an instance, optionally cloning content
// from an existing instance or from the current .minecraft directory.
func (m *Manager) CreateInstanceWithOptions(name string, o CreateOptions) error {
	instancePath, err := m.InstancePath(name)
	if err != nil {
		return err
	}

	// Check if instance already exists
	if _, err := os.Stat(instancePath); err == nil {
		return fmt.Errorf("instance '%s' already exists", name)
	}

	// Create instances directory if it doesn't exist
	if err := os.MkdirAll(m.InstancesPath, 0755); err != nil {
		return fmt.Errorf("failed to create instances directory: %w", err)
	}

	// Create instance directory
	if err := os.MkdirAll(instancePath, 0755); err != nil {
		return fmt.Errorf("failed to create instance directory: %w", err)
	}

	if src, err := m.cloneSource(o); err != nil {
		return err
	} else if src != "" {
		opts := CopyOptions{
			Ctx:      o.Ctx,
			Skip:     cloneSkip(o),
			Progress: o.Progress,
		}
		if err := CopyTree(src, instancePath, opts); err != nil {
			return fmt.Errorf("failed to copy instance content: %w", err)
		}
	}

	// Create essential directories
	for _, dir := range essentialDirs {
		dirPath := filepath.Join(instancePath, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

func (m *Manager) ListInstances() ([]Instance, error) {
	var instances []Instance

	// Check if instances directory exists
	if _, err := os.Stat(m.InstancesPath); os.IsNotExist(err) {
		return instances, nil
	}

	// Read instances directory
	entries, err := os.ReadDir(m.InstancesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read instances directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		instancePath := filepath.Join(m.InstancesPath, name)

		instance := Instance{
			Name: name,
			Path: instancePath,
		}

		// Count mods
		modsPath := filepath.Join(instancePath, "mods")
		instance.ModCount, instance.DisabledMods = countMods(modsPath)

		// Count configs
		configPath := filepath.Join(instancePath, "config")
		instance.ConfigCount = countFiles(configPath)

		// Count saves
		savesPath := filepath.Join(instancePath, "saves")
		instance.SaveCount = countDirectories(savesPath)

		// Metadata is optional; a missing or unreadable file simply leaves
		// the instance reported as unconfigured.
		if meta, found, err := LoadMeta(instancePath); err == nil && found {
			instance.Configured = meta.Configured()
			instance.MinecraftVersion = meta.MinecraftVersion
			instance.Loader = meta.Loader
			instance.LastPlayed = meta.LastPlayed
		}

		instances = append(instances, instance)
	}

	// Sort instances alphabetically
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Name < instances[j].Name
	})

	return instances, nil
}

func (m *Manager) DeleteInstance(name string) error {
	if err := m.CanDelete(name); err != nil {
		return err
	}

	instancePath, err := m.InstancePath(name)
	if err != nil {
		return err
	}

	// Remove the instance directory
	return os.RemoveAll(instancePath)
}

// Helper functions

func countMods(dir string) (enabled, disabled int) {
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			switch {
			case strings.HasSuffix(entry.Name(), ".jar"):
				enabled++
			case strings.HasSuffix(entry.Name(), ".jar"+DisabledSuffix):
				disabled++
			}
		}
	}
	return enabled, disabled
}

func countFiles(dir string) int {
	count := 0
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				count++
			}
		}
	}
	return count
}

func countDirectories(dir string) int {
	count := 0
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				count++
			}
		}
	}
	return count
}
