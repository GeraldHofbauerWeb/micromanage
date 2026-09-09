package main

import (
	"fmt"

	"github.com/GeraldHofbauerWeb/micromanage/internal/instance"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(adoptCmd)
	adoptCmd.Flags().BoolVarP(&adoptYes, "yes", "y", false, "do not ask before moving the directory")
}

var adoptYes bool

var adoptCmd = &cobra.Command{
	Use:   "adopt [instance-name]",
	Short: "Turn the current .minecraft directory into an instance",
	Long: `Turn the current .minecraft directory into an instance.

The directory is moved into the instances folder under the given name
("Default" if none) and .minecraft becomes a link to it, so the worlds, mods
and settings already there are the first instance rather than something to
copy. Nothing is deleted. What the official launcher last ran is detected
and stored, so the instance is ready to play.

The graphical launcher does this on its own the first time it starts with no
instances. Afterwards, 'migrate' shares the instance's game files into the
store.`,
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

		if !adoptYes {
			fmt.Printf("Move %s to the instance '%s' and link it back? (y/N): ", manager.MinecraftPath, name)
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Nothing changed")
				return nil
			}
		}

		res, err := manager.AdoptMinecraft(name)
		if err != nil {
			return err
		}
		if res.Copied {
			fmt.Printf("Copied %s to %s (another filesystem); the original is kept at %s\n",
				manager.MinecraftPath, res.Path, manager.BackupPath)
		} else {
			fmt.Printf("Moved %s to %s\n", manager.MinecraftPath, res.Path)
		}
		fmt.Printf("%s now links to it.\n", manager.MinecraftPath)
		if res.Detected {
			fmt.Printf("It runs %s %s.\n", res.Meta.MinecraftVersion, res.Meta.Loader)
		} else {
			fmt.Printf("Could not tell what it runs; set it with 'instance set %s minecraft <version>'.\n", name)
		}
		fmt.Println("Run 'migrate' to share its game files into the store.")
		return nil
	},
}
