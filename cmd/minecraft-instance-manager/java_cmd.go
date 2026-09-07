package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/java"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/launch"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(javaCmd)
	javaCmd.AddCommand(javaListCmd)
}

var javaCmd = &cobra.Command{
	Use:   "java",
	Short: "Inspect the Java runtimes available to the launcher",
}

var javaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every Java runtime found",
	Long: `List every Java runtime the launcher can use.

Runtimes bundled inside instances by the official launcher are included, and
are often the only ones matching what a version actually asks for.`,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}

		instances, err := manager.ListInstances()
		if err != nil {
			return err
		}
		roots := make([]string, 0, len(instances))
		for _, inst := range instances {
			roots = append(roots, inst.Path)
		}

		layout := launch.NewLayout(manager.AppDir)
		d := &java.Detector{SharedRuntimes: layout.Runtimes(), ExtraRoots: roots}
		runtimes := d.Detect(cmd.Context())

		if len(runtimes) == 0 {
			fmt.Println("No Java runtime found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MAJOR\tVERSION\tCOMPONENT\tSOURCE\tPATH")
		var broken int
		for _, r := range runtimes {
			version := r.String()
			if r.Broken {
				broken++
				version = "UNUSABLE: " + r.Reason
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				majorLabel(r.Major), version, orDash(r.Component), r.Source, r.Path)
		}
		w.Flush()

		if broken > 0 {
			fmt.Fprintf(os.Stderr, "\n%d runtime(s) are unusable; try 'repair-perms'.\n", broken)
		}
		return nil
	},
}

func majorLabel(major int) string {
	if major == 0 {
		return "?"
	}
	return fmt.Sprint(major)
}
