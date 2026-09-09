package instance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordPlaySessionCountsTheTime checks the two things a session
// changes: the instance's running total, and the log behind it.
func TestRecordPlaySessionCountsTheTime(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-2 * time.Hour)

	meta, err := RecordPlaySession(dir, start, start.Add(90*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if meta.TotalPlaySeconds != int64(90*time.Minute/time.Second) {
		t.Errorf("total = %d seconds", meta.TotalPlaySeconds)
	}
	if meta.PlaySessions != 1 {
		t.Errorf("sessions = %d", meta.PlaySessions)
	}
	// Last played is the end of the session, so "just now" is right after
	// a long evening rather than hours out of date.
	if want := start.Add(90 * time.Minute); !meta.LastPlayed.Equal(want.UTC()) {
		t.Errorf("last played = %s, want %s", meta.LastPlayed, want.UTC())
	}

	meta, err = RecordPlaySession(dir, start.Add(2*time.Hour), start.Add(150*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if meta.TotalPlaySeconds != int64(120*time.Minute/time.Second) || meta.PlaySessions != 2 {
		t.Errorf("after the second session: %d seconds, %d sessions", meta.TotalPlaySeconds, meta.PlaySessions)
	}

	sessions := LoadPlayLog(dir)
	if len(sessions) != 2 {
		t.Fatalf("log holds %d sessions", len(sessions))
	}
	if sessions[0].Duration() != 90*time.Minute {
		t.Errorf("first session lasted %s", sessions[0].Duration())
	}

	// A launch that dies in the first seconds is not play.
	meta, err = RecordPlaySession(dir, start, start.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if meta.PlaySessions != 2 || len(LoadPlayLog(dir)) != 2 {
		t.Errorf("a failed launch was counted: %d sessions", meta.PlaySessions)
	}
	if _, err := os.Stat(filepath.Join(dir, PlayLogFile)); err != nil {
		t.Errorf("no log written: %v", err)
	}
}

// TestPlayStatsAddsUpEveryInstance covers the aggregation the start screen
// and the stats command both read.
func TestPlayStatsAddsUpEveryInstance(t *testing.T) {
	m := newTestManager(t)

	for _, name := range []string{"pack", "vanilla", "untouched"} {
		if err := m.CreateInstance(name); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, now.Location())
	packDir := filepath.Join(m.InstancesPath, "pack")
	if _, err := RecordPlaySession(packDir, today.AddDate(0, 0, -1), today.AddDate(0, 0, -1).Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordPlaySession(packDir, today, today.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordPlaySession(filepath.Join(m.InstancesPath, "vanilla"), today, today.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}

	stats, err := m.PlayStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if want := 3*time.Hour + 30*time.Minute; stats.Total != want {
		t.Errorf("total = %s, want %s", stats.Total, want)
	}
	if stats.Sessions != 3 {
		t.Errorf("sessions = %d", stats.Sessions)
	}
	if stats.Longest != 2*time.Hour || stats.LongestOn != "pack" {
		t.Errorf("longest = %s on %s", stats.Longest, stats.LongestOn)
	}
	// An instance nobody has played is not in the list at all.
	if len(stats.Instances) != 2 || stats.Instances[0].Name != "pack" {
		t.Fatalf("instances = %+v", stats.Instances)
	}
	if stats.Instances[0].Share != 1 || stats.Instances[1].Share >= 1 {
		t.Errorf("shares = %v, %v", stats.Instances[0].Share, stats.Instances[1].Share)
	}

	if len(stats.Days) != 7 {
		t.Fatalf("days = %d", len(stats.Days))
	}
	last := stats.Days[len(stats.Days)-1]
	if want := 2*time.Hour + 30*time.Minute; last.Total != want {
		t.Errorf("today = %s, want %s", last.Total, want)
	}
	if busiest := stats.BusiestDay(); !busiest.Day.Equal(last.Day) {
		t.Errorf("busiest day = %s", busiest.Day)
	}
	if stats.InWindow() != stats.Total {
		t.Errorf("window holds %s of %s", stats.InWindow(), stats.Total)
	}
}

// TestPlayStatsSplitsSessionsAtMidnight keeps a night that runs over into
// the next day honest on both days.
func TestPlayStatsSplitsSessionsAtMidnight(t *testing.T) {
	m := newTestManager(t)
	if err := m.CreateInstance("pack"); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start := midnight.Add(-90 * time.Minute) // 22:30 yesterday
	if _, err := RecordPlaySession(filepath.Join(m.InstancesPath, "pack"), start, start.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	stats, err := m.PlayStats(3)
	if err != nil {
		t.Fatal(err)
	}
	yesterday, today := stats.Days[len(stats.Days)-2], stats.Days[len(stats.Days)-1]
	if yesterday.Total != 90*time.Minute || today.Total != 90*time.Minute {
		t.Errorf("split = %s yesterday, %s today", yesterday.Total, today.Total)
	}
}

// TestPlayStatsCountsInstancesWithoutALog covers the instances played
// before sessions were logged: their total still counts, and the panel is
// told the daily chart cannot cover them.
func TestPlayStatsCountsInstancesWithoutALog(t *testing.T) {
	m := newTestManager(t)
	if err := m.CreateInstance("old"); err != nil {
		t.Fatal(err)
	}
	meta := DefaultMeta("old")
	meta.TotalPlaySeconds = 3600
	meta.LastPlayed = time.Now().Add(-72 * time.Hour).UTC()
	if err := m.SetMeta("old", meta); err != nil {
		t.Fatal(err)
	}

	stats, err := m.PlayStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != time.Hour || stats.Untracked != 1 {
		t.Errorf("total = %s, untracked = %d", stats.Total, stats.Untracked)
	}
	if stats.InWindow() != 0 {
		t.Errorf("an untracked instance filled the chart: %s", stats.InWindow())
	}
}
