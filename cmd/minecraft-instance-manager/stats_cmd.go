package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().IntVar(&statsDays, "days", instance.DefaultStatsDays,
		"how many days the daily breakdown covers")
}

var statsDays int

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show how much has been played, across every instance",
	Long: `Show how much has been played.

Every instance keeps its own total, counted since it was first launched from
here, and a log of the sessions themselves. This adds them up: the totals per
instance, the last two weeks day by day, and the longest single session.

Instances played before the session log existed still count towards the
totals, but cannot appear in the daily breakdown.`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		stats, err := manager.PlayStats(statsDays)
		if err != nil {
			return err
		}
		if !stats.Played() {
			fmt.Println("Nothing played yet. Start a game with 'launch <instance-name>'.")
			return nil
		}

		fmt.Printf("Played %s across %s", formatPlaytime(stats.Total), count(len(stats.Instances), "instance"))
		if stats.Sessions > 0 {
			fmt.Printf(" in %s", count(stats.Sessions, "session"))
		}
		fmt.Println(".")
		if !stats.Last.IsZero() {
			fmt.Printf("Last played %s.\n", stats.Last.Local().Format("2 Jan 2006, 15:04"))
		}
		if stats.Longest > 0 {
			fmt.Printf("Longest session: %s on %s.\n", formatPlaytime(stats.Longest), stats.LongestOn)
		}

		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "INSTANCE\tPLAYED\tSHARE\tSESSIONS\tLAST")
		for _, p := range stats.Instances {
			last := "-"
			if !p.Last.IsZero() {
				last = p.Last.Local().Format("2006-01-02 15:04")
			}
			sessions := "-"
			if p.Sessions > 0 {
				sessions = fmt.Sprint(p.Sessions)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, formatPlaytime(p.Total),
				bar(p.Share, 12), sessions, last)
		}
		if err := w.Flush(); err != nil {
			return err
		}

		if window := stats.InWindow(); window > 0 {
			fmt.Printf("\nLast %d days — %s\n", len(stats.Days), formatPlaytime(window))
			busiest := stats.BusiestDay().Total
			for _, d := range stats.Days {
				share := 0.0
				if busiest > 0 {
					share = float64(d.Total) / float64(busiest)
				}
				played := ""
				if d.Total > 0 {
					played = formatPlaytime(d.Total)
				}
				fmt.Printf("  %s  %s  %s\n", d.Day.Format("Mon 02 Jan"), bar(share, 20), played)
			}
		}
		if stats.Untracked > 0 {
			was := "were"
			if stats.Untracked == 1 {
				was = "was"
			}
			fmt.Printf("\n%s %s played before sessions were logged; "+
				"the time counts in the totals only.\n", count(stats.Untracked, "instance"), was)
		}
		return nil
	},
}

// count says how many, with the word in the right number.
func count(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// bar draws a share as a run of blocks, so the table reads as a chart.
func bar(share float64, width int) string {
	if share <= 0 {
		return ""
	}
	n := int(share*float64(width) + 0.5)
	if n < 1 {
		n = 1
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n)
}

// formatPlaytime renders a duration the way a launcher talks about it.
func formatPlaytime(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}
