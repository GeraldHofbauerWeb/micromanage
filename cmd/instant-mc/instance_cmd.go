package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(instanceCmd)
	instanceCmd.AddCommand(instanceDetectCmd, instanceShowCmd, instanceSetCmd)

	instanceDetectCmd.Flags().BoolVar(&detectAll, "all", false, "inspect every instance")
	instanceDetectCmd.Flags().BoolVar(&detectWrite, "write", false, "save the detected settings to instance.json")
}

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Inspect and configure individual instances",
}

var (
	detectAll   bool
	detectWrite bool
)

var instanceDetectCmd = &cobra.Command{
	Use:   "detect [instance-name]",
	Short: "Detect an instance's Minecraft version, loader and JVM settings",
	Long: `Detect what an instance is configured to run.

Instances created by the official launcher carry a launcher_profiles.json and
a versions/ directory; both are read to work out the Minecraft version, the mod
loader and the JVM settings the user had chosen. Nothing is written unless
--write is given.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		names, err := targetInstances(manager, args, detectAll)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "INSTANCE\tMINECRAFT\tLOADER\tRAM\tCONFIDENCE\tSOURCE")

		var written int
		for _, name := range names {
			dir, err := manager.InstancePath(name)
			if err != nil {
				return err
			}
			det, err := instance.DetectMeta(dir)
			if err != nil {
				return fmt.Errorf("detecting %s: %w", name, err)
			}

			mc := det.Meta.MinecraftVersion
			if mc == "" {
				mc = "-"
			}
			ram := "-"
			if det.Meta.Memory.MaxMB > 0 {
				ram = fmt.Sprintf("%d MB", det.Meta.Memory.MaxMB)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				name, mc, det.Meta.Loader, ram, det.Confidence, det.Source)

			if detectWrite && det.Confidence > instance.ConfidenceNone {
				meta := det.Meta
				// Preserve anything already configured by hand.
				if existing, found, err := instance.LoadMeta(dir); err == nil && found {
					meta.Created = existing.Created
					meta.LastPlayed = existing.LastPlayed
					meta.TotalPlaySeconds = existing.TotalPlaySeconds
					meta.Notes = existing.Notes
				}
				if err := instance.SaveMeta(dir, meta); err != nil {
					return fmt.Errorf("writing %s: %w", name, err)
				}
				written++
			}
		}
		w.Flush()

		if detectWrite {
			fmt.Printf("\nWrote %s for %d instance(s).\n", instance.MetaFileName, written)
		} else {
			fmt.Printf("\nNothing was written. Re-run with --write to save these settings.\n")
		}
		return nil
	},
}

var instanceShowCmd = &cobra.Command{
	Use:           "show <instance-name>",
	Short:         "Show an instance's stored configuration",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		dir, err := manager.InstancePath(args[0])
		if err != nil {
			return err
		}
		meta, found, err := instance.LoadMeta(dir)
		if err != nil {
			return err
		}
		if !found {
			fmt.Printf("%s has no %s yet. Run 'instance detect %s --write' to create one.\n",
				args[0], instance.MetaFileName, args[0])
			return nil
		}

		minMB, maxMB := meta.Memory.Resolved()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "name:\t%s\n", meta.Name)
		fmt.Fprintf(w, "minecraft:\t%s\n", orDash(meta.MinecraftVersion))
		fmt.Fprintf(w, "loader:\t%s\n", meta.Loader)
		fmt.Fprintf(w, "version id:\t%s\n", orDash(meta.ResolvedVersionID))
		fmt.Fprintf(w, "memory:\t%d MB - %d MB\n", minMB, maxMB)
		fmt.Fprintf(w, "java:\t%s\n", orDash(meta.Java.Path))
		fmt.Fprintf(w, "jvm args:\t%s\n", orDash(strings.Join(meta.JVMArgs, " ")))
		if !meta.LastPlayed.IsZero() {
			fmt.Fprintf(w, "last played:\t%s\n", meta.LastPlayed.Format("2006-01-02 15:04"))
		}
		return w.Flush()
	},
}

var instanceSetCmd = &cobra.Command{
	Use:   "set <instance-name> <key> <value>",
	Short: "Change one setting of an instance",
	Long: `Change one setting of an instance.

Keys: minecraft-version, loader, loader-version, java-path, min-ram, max-ram, notes`,
	Args:          cobra.ExactArgs(3),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		name, key, value := args[0], strings.ToLower(args[1]), args[2]

		meta, err := manager.GetMeta(name)
		if err != nil {
			return err
		}

		switch key {
		case "minecraft-version", "minecraft":
			meta.MinecraftVersion = value
		case "loader":
			lt := instance.LoaderType(strings.ToLower(value))
			if !lt.Valid() {
				return fmt.Errorf("unknown loader %q (want one of: %s)", value, loaderNames())
			}
			meta.Loader.Type = lt
		case "loader-version":
			meta.Loader.Version = value
		case "java-path", "java":
			meta.Java.Path = value
		case "min-ram", "max-ram":
			mb, err := parseMB(value)
			if err != nil {
				return err
			}
			if key == "min-ram" {
				meta.Memory.MinMB = mb
			} else {
				meta.Memory.MaxMB = mb
			}
		case "notes":
			meta.Notes = value
		default:
			return fmt.Errorf("unknown key %q", key)
		}

		if err := manager.SetMeta(name, meta); err != nil {
			return err
		}
		fmt.Printf("Updated %s: %s = %s\n", name, key, value)
		return nil
	},
}

// targetInstances resolves the instance names a command should act on.
func targetInstances(manager *instance.Manager, args []string, all bool) ([]string, error) {
	if all {
		if len(args) > 0 {
			return nil, fmt.Errorf("--all takes no instance name")
		}
		instances, err := manager.ListInstances()
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(instances))
		for _, inst := range instances {
			names = append(names, inst.Name)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no instances found")
		}
		return names, nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("name an instance, or pass --all")
	}
	return args, nil
}

func loaderNames() string {
	var names []string
	for _, l := range instance.LoaderTypes() {
		names = append(names, string(l))
	}
	return strings.Join(names, ", ")
}

// parseMB accepts a plain megabyte count or a JVM-style size such as "8G".
func parseMB(s string) (int, error) {
	mem, _ := instance.ParseJavaArgs("-Xmx" + s)
	if mem.MaxMB > 0 {
		return mem.MaxMB, nil
	}
	return 0, fmt.Errorf("cannot parse %q as a memory size (try 4096 or 8G)", s)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
