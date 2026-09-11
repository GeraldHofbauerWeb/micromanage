package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version information
const (
	Version = "v2.0.0-dev"
	AppName = "Instant Launcher"
)

// guiBinaryName is the companion GUI executable. It is a separate binary
// because Gio requires cgo on Linux and macOS; keeping it out of this one lets
// the CLI go on cross-compiling from a single host with CGO_ENABLED=0.
var guiBinaryName = "instant-launcher"

// defaultMSAClientID is injected at build time with
// -ldflags "-X main.defaultMSAClientID=<uuid>".
var defaultMSAClientID = ""

var rootCmd = &cobra.Command{
	Use:   "instant-mc",
	Short: "A modern Minecraft instance manager and launcher",
	Long: `A lightweight and efficient Minecraft instance manager and launcher. Each
instance is a directory of its own that the game runs in directly, next to
the official launcher's .minecraft, which is only ever read to import it.

Run without a subcommand to start the graphical launcher.

Version: ` + Version,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGUI()
	},
}

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Start the graphical launcher",
	Long: `Start the graphical launcher.

The GUI ships as a separate executable; this command locates it next to the
CLI binary or on PATH and hands over to it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGUI()
	},
}

// runGUI hands control over to the GUI binary.
func runGUI() error {
	path, err := findGUIBinary()
	if err != nil {
		return err
	}
	return handOver(path, os.Args[1:])
}

func findGUIBinary() (string, error) {
	name := guiBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	// Prefer a GUI sitting next to this binary, so a release archive that was
	// unpacked anywhere keeps working without touching PATH.
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	return "", fmt.Errorf(
		"graphical launcher not found: no %q next to this binary or on PATH.\n"+
			"Install it from the releases page, or run a subcommand instead (see --help)", name)
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.instant-mc.yaml)")
	rootCmd.PersistentFlags().Bool("verbose", false, "verbose output")

	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))

	rootCmd.AddCommand(guiCmd)
}

var cfgFile string

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".instant-mc")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil && viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func main() {
	// Errors are reported here rather than by cobra so the usage block does not
	// drown out the message.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
