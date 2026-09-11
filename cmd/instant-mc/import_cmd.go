package main

import (
	"fmt"
	"os"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launch"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().BoolVar(&importNoSaves, "no-saves", false, "leave the worlds out")
	importCmd.Flags().BoolVar(&importNoScreenshots, "no-screenshots", false, "leave the screenshots out")
}

var (
	importNoSaves       bool
	importNoScreenshots bool
)

var importCmd = &cobra.Command{
	Use:   "import [instance-name]",
	Short: "Copy the official launcher's .minecraft into a new instance",
	Long: `Copy the official launcher's .minecraft into a new instance.

The worlds, mods, configs, packs, screenshots and game options are copied
into a new instance ("Default" if no name is given), and the game files the
official launcher downloaded go into the shared store, copied as well. What
the official launcher last ran is detected and stored, so the instance is
ready to play.

.minecraft is only read. It stays exactly as it was, and the official
launcher and the new instance go their own ways from here.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		name := instance.DefaultInstanceName
		if len(args) == 1 {
			name = args[0]
		}

		res, err := manager.ImportMinecraft(instance.ImportOptions{
			Name:               name,
			IncludeSaves:       !importNoSaves,
			IncludeScreenshots: !importNoScreenshots,
			Ctx:                cmd.Context(),
			Progress:           newProgressPrinter(),
		})
		if err != nil {
			return err
		}
		fmt.Printf("Copied %s to %s\n", manager.MinecraftPath, res.Path)
		_ = manager.SetLastInstance(res.Name)

		fmt.Fprintln(os.Stderr, "Copying the game files into the shared store…")
		layout := launch.NewLayout(manager.AppDir)
		stats, err := launch.HarvestWith(cmd.Context(), layout, manager.MinecraftPath, launch.HarvestOptions{Copy: true}, nil)
		if err != nil {
			return fmt.Errorf("%s is imported, but copying the game files failed: %w", name, err)
		}
		fmt.Printf("Copied %d game files (%s) into the shared store.\n", stats.FilesCopied, launch.FormatBytes(stats.BytesShared))

		if res.Detected {
			fmt.Printf("It runs %s %s.\n", res.Meta.MinecraftVersion, res.Meta.Loader)
		} else {
			fmt.Printf("Could not tell what it runs; set it with 'instance set %s minecraft <version>'.\n", name)
		}
		fmt.Printf("%s was only read and is unchanged.\n", manager.MinecraftPath)
		return nil
	},
}
