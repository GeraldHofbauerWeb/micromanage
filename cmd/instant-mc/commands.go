package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(repairPermsCmd)
	rootCmd.AddCommand(versionCmd)

	createCmd.Flags().StringVar(&createClone, "clone", "", "copy content from an existing instance")
	createCmd.Flags().BoolVar(&createFromMinecraft, "from-minecraft", false, "copy content from the current .minecraft directory")
	createCmd.Flags().BoolVar(&createWithSaves, "with-saves", false, "include saves/ when cloning (can be very large)")
	createCmd.Flags().BoolVar(&createWithScreenshots, "with-screenshots", false, "include screenshots/ when cloning")

	repairPermsCmd.Flags().BoolVar(&repairDryRun, "dry-run", false, "report what would change without modifying anything")
}

// newManager builds the instance manager, reporting failures uniformly.
func newManager() (*instance.Manager, error) {
	manager, err := instance.NewManager()
	if err != nil {
		return nil, fmt.Errorf("initializing manager: %w", err)
	}
	switch {
	case manager.LegacyErr != nil:
		fmt.Fprintln(os.Stderr, "Warning:", manager.LegacyErr)
	case manager.LegacyReleased:
		fmt.Fprintf(os.Stderr, "%s was still linked to an instance, the way earlier versions left it; it is a plain directory again.\n",
			manager.MinecraftPath)
	}
	return manager, nil
}

var (
	createClone           string
	createFromMinecraft   bool
	createWithSaves       bool
	createWithScreenshots bool
)

var createCmd = &cobra.Command{
	Use:   "create <instance-name>",
	Short: "Create a new Minecraft instance",
	Long: `Create a new Minecraft instance.

By default the instance is created empty: just the mods, config, saves,
resourcepacks and shaderpacks directories. Game content (versions, libraries,
assets, runtimes) is shared rather than copied, so an empty instance costs
almost nothing.

Use --clone to copy an existing instance's content, or --from-minecraft to copy
the official launcher's .minecraft (see also 'import', which brings its game
files and settings along too). Cloning excludes saves/ and screenshots/ unless
you ask for them.`,
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		if createClone != "" && createFromMinecraft {
			return fmt.Errorf("--clone and --from-minecraft are mutually exclusive")
		}

		instanceName := args[0]
		opts := instance.CreateOptions{
			CloneFrom:             createClone,
			CloneFromMinecraftDir: createFromMinecraft,
			IncludeSaves:          createWithSaves,
			IncludeScreenshots:    createWithScreenshots,
			Ctx:                   cmd.Context(),
		}
		if createClone != "" || createFromMinecraft {
			opts.Progress = newProgressPrinter()
		}

		if err := manager.CreateInstanceWithOptions(instanceName, opts); err != nil {
			return fmt.Errorf("creating instance: %w", err)
		}

		fmt.Printf("Created instance: %s\n", instanceName)
		fmt.Printf("Add mods to: %s\n", manager.InstancesPath+"/"+instanceName+"/mods/")
		return nil
	},
}

// newProgressPrinter returns a progress callback that redraws a single line at
// most a few times a second, so a multi-gigabyte clone does not scroll the
// terminal off its own output.
func newProgressPrinter() func(copied, total int64, current string) {
	var last time.Time
	return func(copied, total int64, current string) {
		now := time.Now()
		done := copied == total
		if !done && now.Sub(last) < 200*time.Millisecond {
			return
		}
		last = now

		pct := 100.0
		if total > 0 {
			pct = float64(copied) / float64(total) * 100
		}
		fmt.Fprintf(os.Stderr, "\r\033[Kcopying %5.1f%%  %s / %s  %s",
			pct, humanBytes(copied), humanBytes(total), truncate(current, 40))
		if done {
			fmt.Fprintln(os.Stderr)
		}
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max+1:]
}

var listCmd = &cobra.Command{
	Use:           "list",
	Short:         "List all Minecraft instances",
	Long:          `List all available Minecraft instances with their mod counts, marking the one picked last.`,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		instances, err := manager.ListInstances()
		if err != nil {
			return fmt.Errorf("listing instances: %w", err)
		}

		fmt.Println("Available instances:")
		if len(instances) == 0 {
			fmt.Println("  No instances found. Run 'import' to copy your .minecraft, or 'create <name>'.")
			return nil
		}
		last := manager.LastInstance()
		for _, inst := range instances {
			mark := ""
			if inst.Name == last {
				mark = "  [last used]"
			}
			fmt.Printf("  - %-20s (%d mods, %d configs, %d saves)%s\n",
				inst.Name, inst.ModCount, inst.ConfigCount, inst.SaveCount, mark)
		}
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete <instance-name>",
	Short: "Delete a Minecraft instance",
	Long: `Delete the specified Minecraft instance permanently.
The official launcher's .minecraft is never touched.`,
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		instanceName := args[0]

		// Fail before prompting when the instance cannot be deleted anyway.
		if err := manager.CanDelete(instanceName); err != nil {
			return err
		}

		fmt.Printf("Are you sure you want to delete instance '%s'? This cannot be undone. (y/N): ", instanceName)
		var response string
		fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			fmt.Println("Deletion cancelled")
			return nil
		}

		if err := manager.DeleteInstance(instanceName); err != nil {
			return fmt.Errorf("deleting instance: %w", err)
		}

		fmt.Printf("Deleted instance: %s\n", instanceName)
		return nil
	},
}

var repairDryRun bool

var repairPermsCmd = &cobra.Command{
	Use:   "repair-perms",
	Short: "Restore the executable bit on bundled Java runtimes",
	Long: `Restore the executable bit on Java runtimes bundled inside instances.

Instances created by earlier versions were copied with a hardcoded file mode,
which stripped the executable bit from their bundled java binaries. The symptom
is an instance that appears to have no usable Java even though one is present.`,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		fixes, err := manager.RepairRuntimePermissions(repairDryRun)
		if err != nil {
			return fmt.Errorf("scanning runtimes: %w", err)
		}

		if len(fixes) == 0 {
			fmt.Println("All bundled Java runtimes are executable; nothing to repair.")
			return nil
		}

		var failed int
		for _, f := range fixes {
			switch {
			case f.Err != nil:
				failed++
				fmt.Fprintf(os.Stderr, "  FAILED %s: %v\n", f.Path, f.Err)
			case repairDryRun:
				fmt.Printf("  would fix %s (%s)\n", f.Path, f.OldMode)
			default:
				fmt.Printf("  fixed %s (%s -> %s)\n", f.Path, f.OldMode, f.NewMode)
			}
		}

		verb := "Repaired"
		if repairDryRun {
			verb = "Would repair"
		}
		fmt.Printf("\n%s %d file(s) across the instances.\n", verb, len(fixes)-failed)
		if failed > 0 {
			return fmt.Errorf("%d file(s) could not be repaired", failed)
		}
		return nil
	},
}

/*
config command usage:

	config show
	config <key>
	config <key> <path>

Supported keys: minecraft-path, instances-path, msa-client-id
*/
var configCmd = &cobra.Command{
	Use:   "config <key|show> [path]",
	Short: "Get or set configuration values",
	Long: `Get or set configuration values.
Examples:
  config show
  config minecraft-path
  config minecraft-path /home/user/.minecraft
  config instances-path /path/to/instances
  config msa-client-id 00000000-0000-0000-0000-000000000000
`,
	Args:          cobra.RangeArgs(1, 2),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		key := strings.ToLower(args[0])

		if key == "show" || key == "list" {
			cfg := manager.GetConfig()
			fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Println("Configuration:")
			// Iterate the canonical key list rather than the map, so the
			// output has a stable order.
			for _, ck := range instance.ConfigKeys() {
				suffix := ""
				if !ck.Editable {
					suffix = "  (read-only)"
				}
				fmt.Printf("  %-15s %s%s\n", ck.Key+":", cfg[ck.Key], suffix)
			}
			return nil
		}

		key = normalizeConfigKey(key)

		if len(args) == 1 {
			cfg := manager.GetConfig()
			val, ok := cfg[key]
			if !ok {
				return fmt.Errorf("unknown config key: %s", key)
			}
			fmt.Printf("%s: %s\n", key, val)
			return nil
		}

		if !instance.IsEditableConfigKey(key) {
			return fmt.Errorf("config key %q is read-only", key)
		}

		newPath := args[1]
		if err := manager.UpdateConfig(key, newPath); err != nil {
			return fmt.Errorf("updating config: %w", err)
		}
		fmt.Printf("Updated %s -> %s\n", key, newPath)
		return nil
	},
}

// normalizeConfigKey maps the accepted aliases onto canonical key names.
func normalizeConfigKey(key string) string {
	switch key {
	case "minecraft", "minecraft-dir":
		return "minecraft-path"
	case "instances", "instances-dir":
		return "instances-path"
	case "msa", "client-id", "msa-client":
		return "msa-client-id"
	}
	return key
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the current version of Instant Launcher.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s %s\n", AppName, Version)
		fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}
