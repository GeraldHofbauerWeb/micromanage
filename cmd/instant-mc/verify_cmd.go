package main

import (
	"fmt"
	"os"
	"time"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/download"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launch"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(verifyCmd)

	verifyCmd.Flags().StringVar(&verifyVersion, "version", "", "verify a version id directly instead of an instance")
	verifyCmd.Flags().BoolVar(&verifyDeep, "deep", false, "re-hash files that are already present")
	verifyCmd.Flags().IntVar(&verifyWorkers, "workers", download.DefaultWorkers, "parallel downloads")
}

var (
	verifyVersion string
	verifyDeep    bool
	verifyWorkers int
)

var verifyCmd = &cobra.Command{
	Use:   "verify [instance-name]",
	Short: "Download and check everything a version needs",
	Long: `Resolve a version and make sure its client jar, libraries, natives, assets
and logging config are present in the shared store, downloading whatever is
missing.

Content is shared between instances, so verifying a version an instance
already covers costs nothing beyond the metadata request.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		versionID, err := resolveVersionID(manager, args, verifyVersion)
		if err != nil {
			return err
		}

		layout := launch.NewLayout(manager.AppDir)
		d := download.New()
		d.Workers = verifyWorkers
		d.Verify = verifyDeep

		prep := launch.NewPreparer(layout, d)
		prep.Observer = newPhasePrinter()

		started := time.Now()
		result, err := prep.Prepare(cmd.Context(), versionID)
		if err != nil {
			return err
		}

		fmt.Printf("\n%s is ready (%s)\n", versionID, time.Since(started).Round(time.Second))
		fmt.Printf("  main class:  %s\n", result.Version.MainClass)
		fmt.Printf("  classpath:   %d entries\n", len(result.Classpath))
		fmt.Printf("  asset index: %s\n", orDash(result.AssetIndexID))
		if result.Version.JavaVersion != nil {
			fmt.Printf("  java:        %s (major %d)\n",
				result.Version.JavaVersion.Component, result.Version.JavaVersion.MajorVersion)
		}
		fmt.Printf("  store:       %s\n", layout.Root)
		return nil
	},
}

// resolveVersionID works out which version to act on: an explicit flag, or the
// version an instance is configured to run.
func resolveVersionID(manager *instance.Manager, args []string, explicit string) (string, error) {
	if explicit != "" {
		if len(args) > 0 {
			return "", fmt.Errorf("pass either an instance name or --version, not both")
		}
		return explicit, nil
	}
	if len(args) == 0 {
		return "", fmt.Errorf("name an instance, or pass --version")
	}

	meta, err := manager.GetMeta(args[0])
	if err != nil {
		return "", err
	}
	if id := meta.ResolvedVersionID; id != "" {
		return id, nil
	}
	if meta.MinecraftVersion != "" {
		return meta.MinecraftVersion, nil
	}
	return "", fmt.Errorf(
		"instance %q is not configured yet; run 'instance detect %s --write' first", args[0], args[0])
}

// newPhasePrinter renders preparation progress as one line per phase, updated
// in place so a few thousand asset downloads do not scroll the terminal.
func newPhasePrinter() launch.Observer {
	var current launch.Phase
	var lastDraw time.Time

	return func(e launch.Event) {
		if e.Phase != current {
			if current != "" {
				fmt.Fprintln(os.Stderr)
			}
			current = e.Phase
		}

		switch {
		case e.Done:
			fmt.Fprintf(os.Stderr, "\r\033[K  %-10s done\n", e.Phase)
			current = ""
		case e.Progress.FilesTotal > 0:
			// Progress arrives sampled at 10 Hz; redraw at most that often.
			if time.Since(lastDraw) < 100*time.Millisecond {
				return
			}
			lastDraw = time.Now()
			fmt.Fprintf(os.Stderr, "\r\033[K  %-10s %5.1f%%  %d/%d files  %s",
				e.Phase, e.Progress.Percent(), e.Progress.FilesDone, e.Progress.FilesTotal,
				truncate(e.Progress.Current, 30))
		case e.Message != "":
			fmt.Fprintf(os.Stderr, "\r\033[K  %-10s %s", e.Phase, e.Message)
		}
	}
}
