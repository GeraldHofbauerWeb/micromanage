package main

import (
	"fmt"
	"os"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version information
const (
	Version = "v1.3.0"
	AppName = "Minecraft Instance Manager"
)

var rootCmd = &cobra.Command{
	Use:   "minecraft-instance-manager",
	Short: "A modern Minecraft instance manager with TUI interface",
	Long: `A lightweight and efficient Minecraft instance manager that uses symlinks 
to instantly switch between different Minecraft setups without copying files.

Features a beautiful terminal interface for easy instance management.

Version: ` + Version,
	Run: func(cmd *cobra.Command, args []string) {
		// If no subcommand is specified, run the TUI
		tui.RunTUI()
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.minecraft-instance-manager.yaml)")
	rootCmd.PersistentFlags().Bool("verbose", false, "verbose output")

	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
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
		viper.SetConfigName(".minecraft-instance-manager")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil && viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
