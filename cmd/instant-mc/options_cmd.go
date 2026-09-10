package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/desktop"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launch"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(optionsCmd)
	optionsCmd.AddCommand(optionsListCmd, optionsSaveCmd, optionsRestoreCmd, optionsDiffCmd,
		optionsDeleteCmd, optionsPathCmd, optionsEditCmd)
}

var optionsCmd = &cobra.Command{
	Use:   "options",
	Short: "Save and restore an instance's game options (options.txt)",
	Long: `Save and restore an instance's game options.

The game keeps keybinds, video settings, the resource pack order and every
other in-game setting in options.txt, and rewrites it whenever something
changes. A snapshot is a copy of that file kept in the instance's
options-snapshots folder; any snapshot can be put back later. Restoring keeps
the current file as a snapshot of its own, labelled "before restore", so a
restore can be undone.

Snapshots are named by the time they were taken; "latest" always means the
newest one.`,
}

var optionsListCmd = &cobra.Command{
	Use:           "list <instance-name>",
	Short:         "List an instance's saved options snapshots",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		name := args[0]
		info, err := manager.OptionsInfo(name)
		if err != nil {
			return err
		}
		if info.Exists {
			fmt.Printf("%s: %s, changed %s\n\n", instance.OptionsFile, launch.FormatBytes(info.Size),
				info.ModTime.Format("2006-01-02 15:04"))
		} else {
			fmt.Printf("%s has no %s yet; the game writes it on the first run.\n\n", name, instance.OptionsFile)
		}

		snapshots, err := manager.ListOptionsSnapshots(name)
		if err != nil {
			return err
		}
		if len(snapshots) == 0 {
			fmt.Printf("No snapshots. Save one with 'options save %s [label]'.\n", name)
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SAVED\tLABEL\tSIZE\tDIFFERS\tSNAPSHOT")
		for _, s := range snapshots {
			differs := "-"
			switch {
			case s.Changes == 0:
				differs = "no"
			case s.Changes > 0:
				differs = fmt.Sprintf("%d settings", s.Changes)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Time.Format("2006-01-02 15:04:05"),
				orDash(s.Label), launch.FormatBytes(s.Size), differs, s.Name)
		}
		return w.Flush()
	},
}

var optionsSaveCmd = &cobra.Command{
	Use:           "save <instance-name> [label...]",
	Short:         "Keep a copy of the instance's current options.txt",
	Args:          cobra.MinimumNArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		snap, err := manager.SaveOptions(args[0], strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		fmt.Printf("Saved the game options of %s as %q\n  %s\n", args[0], snap.Display(), snap.Path)
		return nil
	},
}

var optionsRestoreCmd = &cobra.Command{
	Use:   "restore <instance-name> [snapshot]",
	Short: "Put a snapshot back as the instance's options.txt",
	Long: `Put a snapshot back as the instance's options.txt.

Without a snapshot name the newest one is used. The current file is kept as a
snapshot labelled "before restore" whenever it differs, so nothing is lost.`,
	Args:          cobra.RangeArgs(1, 2),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		name, snapshot := args[0], instance.LatestSnapshot
		if len(args) == 2 {
			snapshot = args[1]
		}
		changes, err := manager.OptionsDiff(name, snapshot)
		if err != nil {
			return err
		}
		kept, ok, err := manager.RestoreOptions(name, snapshot)
		if err != nil {
			return err
		}
		switch {
		case len(changes) == 0:
			fmt.Printf("The game options of %s already matched the snapshot.\n", name)
		case len(changes) == 1:
			fmt.Printf("Restored the game options of %s: 1 setting changed.\n", name)
		default:
			fmt.Printf("Restored the game options of %s: %d settings changed.\n", name, len(changes))
		}
		if ok {
			fmt.Printf("The previous options are kept as %q (%s).\n", kept.Display(), kept.Name)
		}
		return nil
	},
}

var optionsDiffCmd = &cobra.Command{
	Use:           "diff <instance-name> [snapshot]",
	Short:         "Show which settings a restore would change",
	Args:          cobra.RangeArgs(1, 2),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		snapshot := instance.LatestSnapshot
		if len(args) == 2 {
			snapshot = args[1]
		}
		changes, err := manager.OptionsDiff(args[0], snapshot)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Println("No differences.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SETTING\tNOW\tSNAPSHOT")
		for _, c := range changes {
			fmt.Fprintf(w, "%s\t%s\t%s\n", c.Key, orDash(c.Old), orDash(c.New))
		}
		return w.Flush()
	},
}

var optionsDeleteCmd = &cobra.Command{
	Use:           "delete <instance-name> <snapshot>",
	Short:         "Remove one snapshot",
	Args:          cobra.ExactArgs(2),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		if err := manager.DeleteOptionsSnapshot(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("Removed snapshot %s of %s\n", args[1], args[0])
		return nil
	},
}

var optionsPathCmd = &cobra.Command{
	Use:           "path <instance-name>",
	Short:         "Print where the instance's options.txt is",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		path, err := manager.OptionsPath(args[0])
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	},
}

var optionsEditCmd = &cobra.Command{
	Use:   "edit <instance-name>",
	Short: "Open the instance's options.txt in your editor",
	Long: `Open the instance's options.txt in your editor.

$VISUAL or $EDITOR is used when set; otherwise the file is handed to the
desktop, which opens it in whatever is registered for text files.`,
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		path, err := manager.OptionsPath(args[0])
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s has no %s yet; the game writes it on the first run", args[0], instance.OptionsFile)
		}
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			return desktop.Open(path)
		}
		parts := strings.Fields(editor)
		c := exec.CommandContext(cmd.Context(), parts[0], append(parts[1:], path)...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	},
}
