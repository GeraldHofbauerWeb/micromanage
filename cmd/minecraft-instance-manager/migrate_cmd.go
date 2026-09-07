package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "only report what could be shared")
}

var migrateDryRun bool

var migrateCmd = &cobra.Command{
	Use:   "migrate [instance-name]",
	Short: "Move an instance's shareable content into the shared store",
	Long: `Move the content instances duplicate — libraries, versions, assets and Java
runtimes — into the shared store.

Files are hard-linked where the filesystem allows it, so this costs no extra
space, and the instance is only read. Nothing is deleted: freeing the redundant
copies is a separate, explicit step.

A side effect worth knowing: harvesting an instance brings its mod loader
profile and processed libraries into the store, which is what makes a modded
instance launchable without running the loader's installer.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		names, err := targetInstances(manager, args, len(args) == 0)
		if err != nil {
			return err
		}

		layout := launch.NewLayout(manager.AppDir)

		if migrateDryRun {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "INSTANCE\tSHAREABLE")
			var total int64
			for _, name := range names {
				dir, err := manager.InstancePath(name)
				if err != nil {
					return err
				}
				size, err := launch.Reclaimable(dir)
				if err != nil {
					return err
				}
				total += size
				fmt.Fprintf(w, "%s\t%s\n", name, launch.FormatBytes(size))
			}
			fmt.Fprintf(w, "\t\n%s\t%s\n", "TOTAL", launch.FormatBytes(total))
			w.Flush()
			fmt.Println("\nNothing was changed. Re-run without --dry-run to build the shared store.")
			return nil
		}

		started := time.Now()
		var shared, reclaimable int64

		for _, name := range names {
			dir, err := manager.InstancePath(name)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "  %s\n", name)
			stats, err := launch.Harvest(cmd.Context(), layout, dir, func(p launch.HarvestProgress) {
				fmt.Fprintf(os.Stderr, "\r\033[K    %d files, %s  %s",
					p.Files, launch.FormatBytes(p.Bytes), truncate(p.Current, 40))
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "\r\033[K    %d linked, %d copied, %d already shared\n",
				stats.FilesLinked, stats.FilesCopied, stats.FilesSkipped)

			for _, e := range stats.Errors {
				fmt.Fprintf(os.Stderr, "    warning: %s\n", e)
			}
			shared += stats.BytesShared
			reclaimable += stats.BytesReclaimable
		}

		fmt.Printf("\nShared store built in %s\n", time.Since(started).Round(time.Second))
		fmt.Printf("  added:       %s\n", launch.FormatBytes(shared))
		fmt.Printf("  now redundant in the instances: %s\n", launch.FormatBytes(reclaimable))

		var loaders []string
		for _, id := range launch.HarvestedVersions(layout) {
			if launch.IsLoaderProfile(id) {
				loaders = append(loaders, id)
			}
		}
		if len(loaders) > 0 {
			fmt.Printf("\nLoader profiles now available without an installer:\n")
			for _, id := range loaders {
				fmt.Printf("  %s\n", id)
			}
		}
		return nil
	},
}
