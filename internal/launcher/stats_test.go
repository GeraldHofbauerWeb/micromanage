package launcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPlaytimeReachesTheScreenWhenTheGameExits is the bug this guards: the
// numbers were written to disk when the game ended and only read again on
// the next start, so the overview kept showing yesterday's playtime after
// an evening of playing.
func TestPlaytimeReachesTheScreenWhenTheGameExits(t *testing.T) {
	ctrl := newTestController(t, nil)
	m := isolateInstances(t, ctrl)

	if err := os.MkdirAll(filepath.Join(m.InstancesPath, "pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctrl.Dispatch(ActionRefresh{})
	ctrl.Dispatch(ActionSelect{Name: "pack"})
	waitFor(t, ctrl, "the selection", func(s Snapshot) bool { return s.Selected == "pack" })

	end := time.Now()
	ctrl.recordPlaytime("pack", end.Add(-90*time.Minute), end)

	snap := waitFor(t, ctrl, "the playtime", func(s Snapshot) bool {
		return s.Editing.TotalPlaySeconds > 0
	})
	if want := int64(90 * 60); snap.Editing.TotalPlaySeconds != want {
		t.Errorf("time played = %d seconds, want %d", snap.Editing.TotalPlaySeconds, want)
	}
	if snap.Editing.PlaySessions != 1 {
		t.Errorf("sessions = %d", snap.Editing.PlaySessions)
	}

	stats := waitFor(t, ctrl, "the totals", func(s Snapshot) bool { return s.Stats.Played() }).Stats
	if stats.Total != 90*time.Minute || stats.Sessions != 1 {
		t.Errorf("stats = %s in %d sessions", stats.Total, stats.Sessions)
	}
	if len(stats.Instances) != 1 || stats.Instances[0].Name != "pack" {
		t.Errorf("stats.Instances = %+v", stats.Instances)
	}
	if stats.Days[len(stats.Days)-1].Total == 0 {
		t.Error("today's column is empty")
	}
}
