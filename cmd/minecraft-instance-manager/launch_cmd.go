package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/download"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launcher"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(launchCmd)

	launchCmd.Flags().StringVar(&launchOffline, "offline", "", "play as a local account with this name")
	launchCmd.Flags().StringVar(&launchJava, "java", "", "use a specific java executable")
	launchCmd.Flags().IntVar(&launchMaxMB, "max-ram", 0, "override the maximum heap in MB")
	launchCmd.Flags().BoolVar(&launchDryRun, "dry-run", false, "print the command line instead of starting the game")
	launchCmd.Flags().BoolVar(&launchWait, "wait", false, "stay attached and mirror the game's output")
	launchCmd.Flags().StringVar(&launchServer, "server", "", "join a server directly")
}

var (
	launchOffline string
	launchJava    string
	launchMaxMB   int
	launchDryRun  bool
	launchWait    bool
	launchServer  string
)

var launchCmd = &cobra.Command{
	Use:   "launch <instance-name>",
	Short: "Launch an instance",
	Long: `Launch an instance.

The instance is activated first, so ~/.minecraft points at it and the game
writes its saves, screenshots and logs there. Game content is taken from the
shared store and downloaded if missing.`,
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		manager, err := newManager()
		if err != nil {
			return err
		}

		meta, err := manager.GetMeta(name)
		if err != nil {
			return err
		}
		versionID := meta.ResolvedVersionID
		if versionID == "" {
			versionID = meta.MinecraftVersion
		}
		if versionID == "" {
			return fmt.Errorf(
				"instance %q is not configured yet; run 'instance detect %s --write' first", name, name)
		}

		account, err := resolveAccount(cmd, manager, launchOffline)
		if err != nil {
			return err
		}

		// The game's gameDir is the ~/.minecraft symlink, so the instance has
		// to be the active one before anything starts.
		if manager.GetActiveInstance() != name {
			fmt.Fprintf(os.Stderr, "  activating  %s\n", name)
			if !launchDryRun {
				if err := manager.SwitchInstance(name); err != nil {
					return fmt.Errorf("activating %s: %w", name, err)
				}
			}
		}

		layout := launch.NewLayout(manager.AppDir)
		d := download.New()
		prep := launch.NewPreparer(layout, d)
		prep.Observer = newPhasePrinter()

		prepared, err := prep.Prepare(ctx, versionID)
		if err != nil {
			return err
		}

		selection, err := selectJava(cmd, manager, layout, prepared, meta)
		if err != nil {
			return err
		}
		if selection.Warning != "" {
			fmt.Fprintf(os.Stderr, "  warning     %s\n", selection.Warning)
		}

		minMB, maxMB := meta.Memory.Resolved()
		if launchMaxMB > 0 {
			maxMB = launchMaxMB
			if minMB > maxMB {
				minMB = maxMB
			}
		}

		opts := launch.Options{
			Session: launch.Session{
				PlayerName:  account.Name,
				UUID:        account.UUID,
				AccessToken: account.AccessToken(),
				XUID:        account.EffectiveXUID(),
				UserType:    account.UserType(),
				ClientID:    msaClientID(manager),
			},
			GameDir:         manager.MinecraftPath,
			LauncherName:    "minecraft-instance-manager",
			LauncherVer:     Version,
			MinMB:           minMB,
			MaxMB:           maxMB,
			ExtraJVMArgs:    launch.JVMArgsFor(meta.JVMArgs),
			ExtraGameArgs:   meta.GameArgs,
			QuickPlayServer: launchServer,
		}
		gameArgs := launch.BuildArgs(prepared, opts, prep.Platform)

		if launchDryRun {
			fmt.Printf("\n%s \\\n", selection.Runtime.Path)
			for i, a := range gameArgs {
				suffix := " \\"
				if i == len(gameArgs)-1 {
					suffix = ""
				}
				fmt.Printf("  %s%s\n", quoteIfNeeded(a), suffix)
			}
			return nil
		}

		fmt.Fprintf(os.Stderr, "  java        %s (%s)\n", selection.Runtime.String(), selection.Runtime.Path)
		fmt.Fprintf(os.Stderr, "  account     %s (%s)\n", account.Name, account.Kind)

		proc, err := launch.Start(ctx, launch.Spec{
			JavaPath: selection.Runtime.Path,
			Args:     gameArgs,
			GameDir:  manager.MinecraftPath,
			LogDir:   filepath.Join(manager.AppDir, "logs"),
			Name:     name,
			OnLine:   lineEcho(launchWait),
		})
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "\nMinecraft is running (pid %d)\n", proc.Cmd.Process.Pid)
		if proc.LogPath != "" {
			fmt.Fprintf(os.Stderr, "Log: %s\n", proc.LogPath)
		}
		if !launchWait {
			// Detaching here would kill the game, because the context is
			// cancelled when this command returns; wait for the exit but stay
			// quiet about the output.
			fmt.Fprintln(os.Stderr, "Waiting for the game to exit (Ctrl-C stops it).")
		}

		if err := proc.Wait(); err != nil {
			for _, line := range proc.Log.Tail(30) {
				fmt.Fprintln(os.Stderr, line)
			}
			return fmt.Errorf("minecraft exited with an error: %w", err)
		}

		played := time.Since(proc.Started).Round(time.Second)
		fmt.Fprintf(os.Stderr, "\nMinecraft exited cleanly after %s\n", played)
		recordPlaytime(manager, name, proc.Started)
		return nil
	},
}

// resolveAccount produces the identity to play as.
//
// --offline wins, because it is an explicit instruction for this one launch.
// Otherwise the account signed in with 'account login' or in the graphical
// launcher is used, and its Microsoft session renewed if it has lapsed.
func resolveAccount(cmd *cobra.Command, manager *instance.Manager, offlineName string) (auth.Account, error) {
	if offlineName != "" {
		return auth.NewOfflineAccount(offlineName)
	}

	store, err := launcher.NewAccountStore(manager.AppDir)
	if err != nil {
		return auth.Account{}, err
	}
	account, ok := store.Active()
	if !ok {
		return auth.Account{}, fmt.Errorf(
			"no account selected; run 'account login' for a Microsoft account, " +
				"'account offline <name>' for a local one, or pass --offline <name>")
	}
	return refreshIfExpired(cmd, manager, store, account)
}

// selectJava resolves the runtime for a prepared version.
func selectJava(cmd *cobra.Command, manager *instance.Manager, layout *launch.Layout,
	prepared *launch.Prepared, meta instance.Meta) (java.Selection, error) {

	instances, err := manager.ListInstances()
	if err != nil {
		return java.Selection{}, err
	}
	roots := make([]string, 0, len(instances))
	for _, inst := range instances {
		roots = append(roots, inst.Path)
	}

	detector := java.NewDetector(layout.Runtimes(), layout.Cache(), roots)

	var req java.Requirement
	if jv := prepared.Version.JavaVersion; jv != nil {
		req = java.Requirement{Major: jv.MajorVersion, Component: jv.Component}
	}
	// Forge pins itself to the JVM it was built against; letting the launcher
	// quietly move it to a newer major is a known way to break it.
	if meta.Loader.Type == instance.LoaderForge {
		req.Strict = true
	}

	override := launchJava
	if override == "" {
		override = meta.Java.Path
	}
	return detector.Resolve(cmd.Context(), req, override)
}

// lineEcho mirrors game output to stderr when asked.
func lineEcho(enabled bool) func(string) {
	if !enabled {
		return nil
	}
	return func(line string) { fmt.Fprintln(os.Stderr, line) }
}

// recordPlaytime books the finished session, best-effort.
func recordPlaytime(manager *instance.Manager, name string, started time.Time) {
	_, _ = manager.RecordPlaySession(name, started, time.Now())
}

// quoteIfNeeded makes a dry-run command line copy-pasteable.
func quoteIfNeeded(arg string) string {
	if strings.ContainsAny(arg, " \t\"'$") {
		return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return arg
}
