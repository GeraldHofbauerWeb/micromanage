package instance

import (
	"sort"
	"time"
)

// DefaultStatsDays is how far the daily breakdown looks back by default:
// two weeks is long enough to show a pattern and short enough to read as
// a row of bars.
const DefaultStatsDays = 14

// PlayStats is what the player has done with the launcher, added up.
type PlayStats struct {
	// Total is the playtime across every instance, Sessions how many runs
	// are on record (only sessions logged since v2 are counted).
	Total    time.Duration
	Sessions int

	// First and Last bracket the record: the earliest session start and the
	// most recent time anything was played.
	First time.Time
	Last  time.Time

	// Instances are the instances that have been played, longest first.
	Instances []InstancePlay

	// Days is the daily breakdown, oldest first, one entry per day up to
	// today — including the days nothing was played, so a chart of it is
	// evenly spaced.
	Days []DayPlay

	// Longest is the single longest session on record, and LongestOn the
	// instance it was played on.
	Longest   time.Duration
	LongestOn string

	// Untracked counts instances whose totals predate the session log, so
	// the daily breakdown can honestly say it does not cover everything.
	Untracked int
}

// InstancePlay is one instance's share of the playtime.
type InstancePlay struct {
	Name     string
	Loader   LoaderSpec
	Total    time.Duration
	Sessions int
	Last     time.Time
	// Share is Total against the busiest instance's total, 0…1, which is
	// what a bar next to the name should be as long as.
	Share float64
}

// DayPlay is one day's playtime.
type DayPlay struct {
	Day   time.Time
	Total time.Duration
}

// PlayStats adds up what every instance records about playing.
//
// The per-instance totals come from the metadata, which has counted since
// the first launch; the daily breakdown comes from the session log, which
// only covers sessions played since it existed.
func (m *Manager) PlayStats(days int) (PlayStats, error) {
	if days <= 0 {
		days = DefaultStatsDays
	}
	instances, err := m.ListInstances()
	if err != nil {
		return PlayStats{}, err
	}

	stats := PlayStats{}
	daily := make(map[string]time.Duration, days)
	var busiest time.Duration

	for _, inst := range instances {
		meta, _, err := LoadMeta(inst.Path)
		if err != nil {
			continue
		}
		sessions := LoadPlayLog(inst.Path)
		play := InstancePlay{
			Name:     inst.Name,
			Loader:   meta.Loader,
			Total:    time.Duration(meta.TotalPlaySeconds) * time.Second,
			Sessions: len(sessions),
			Last:     meta.LastPlayed,
		}
		if play.Total == 0 && len(sessions) == 0 {
			continue
		}
		if len(sessions) == 0 && play.Total > 0 {
			stats.Untracked++
		}

		for _, s := range sessions {
			d := s.Duration()
			if d <= 0 {
				continue
			}
			if d > stats.Longest {
				stats.Longest, stats.LongestOn = d, inst.Name
			}
			if stats.First.IsZero() || s.Start.Before(stats.First) {
				stats.First = s.Start
			}
			spreadOverDays(daily, s)
		}

		stats.Total += play.Total
		stats.Sessions += play.Sessions
		if play.Total > busiest {
			busiest = play.Total
		}
		if play.Last.After(stats.Last) {
			stats.Last = play.Last
		}
		stats.Instances = append(stats.Instances, play)
	}

	sort.Slice(stats.Instances, func(i, j int) bool {
		a, b := stats.Instances[i], stats.Instances[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Name < b.Name
	})
	for i := range stats.Instances {
		if busiest > 0 {
			stats.Instances[i].Share = float64(stats.Instances[i].Total) / float64(busiest)
		}
	}

	// Days are local calendar days, so the breakdown lines up with what
	// the player remembers rather than with UTC.
	now := time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	stats.Days = make([]DayPlay, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		stats.Days = append(stats.Days, DayPlay{Day: day, Total: daily[dayKey(day)]})
	}
	return stats, nil
}

// spreadOverDays books a session against the days it covers, so an evening
// that runs past midnight counts towards both.
func spreadOverDays(daily map[string]time.Duration, s PlaySession) {
	start, end := s.Start.Local(), s.End.Local()
	for start.Before(end) {
		midnight := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location()).AddDate(0, 0, 1)
		stop := end
		if midnight.Before(stop) {
			stop = midnight
		}
		daily[dayKey(start)] += stop.Sub(start)
		start = stop
	}
}

// dayKey identifies a local calendar day.
func dayKey(t time.Time) string { return t.Local().Format("2006-01-02") }

// Played reports whether anything has been played at all.
func (p PlayStats) Played() bool { return p.Total > 0 }

// BusiestDay is the longest day in the breakdown.
func (p PlayStats) BusiestDay() DayPlay {
	var best DayPlay
	for _, d := range p.Days {
		if d.Total > best.Total {
			best = d
		}
	}
	return best
}

// InWindow is the playtime the daily breakdown covers.
func (p PlayStats) InWindow() time.Duration {
	var total time.Duration
	for _, d := range p.Days {
		total += d.Total
	}
	return total
}
