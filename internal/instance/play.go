package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// PlayLogFile records every session of one instance, so playtime can be
	// shown per day rather than as one ever-growing number.
	PlayLogFile = "play-sessions.json"

	// maxLoggedSessions caps the log. A player who starts the game twice a
	// day fills this in three years; older sessions still count towards the
	// instance's total, which lives in the metadata.
	maxLoggedSessions = 2000

	// minSession is the shortest session worth recording. A launch that
	// fails in the first seconds is a failed launch, not play.
	minSession = 10 * time.Second
)

// PlaySession is one run of the game, from launch to exit.
type PlaySession struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Duration is how long that session lasted.
func (s PlaySession) Duration() time.Duration {
	if s.End.Before(s.Start) {
		return 0
	}
	return s.End.Sub(s.Start)
}

// LoadPlayLog reads an instance's sessions, oldest first.
//
// A missing or unreadable log is not an error: sessions have only been
// written since v2, and the totals in the metadata stand on their own.
func LoadPlayLog(instanceDir string) []PlaySession {
	data, err := os.ReadFile(filepath.Join(instanceDir, PlayLogFile))
	if err != nil {
		return nil
	}
	var sessions []PlaySession
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Start.Before(sessions[j].Start) })
	return sessions
}

// RecordPlaySession books one finished session against an instance: the
// metadata gets the running total and the time the player last stopped, the
// log gets the session itself.
//
// Sessions shorter than a few seconds are dropped, and so is the whole call
// when the instance directory has gone; play statistics are never worth an
// error in front of the player.
func RecordPlaySession(instanceDir string, start, end time.Time) (Meta, error) {
	session := PlaySession{Start: start.UTC(), End: end.UTC()}
	played := session.Duration()
	if played < minSession {
		meta, _, err := LoadMeta(instanceDir)
		return meta, err
	}

	meta, _, err := LoadMeta(instanceDir)
	if err != nil {
		return meta, err
	}
	// Last played is when the player stopped, not when they started: after
	// a long evening "just now" is the honest answer.
	meta.LastPlayed = session.End
	meta.TotalPlaySeconds += int64(played.Seconds())
	meta.PlaySessions++
	if err := SaveMeta(instanceDir, meta); err != nil {
		return meta, err
	}

	sessions := append(LoadPlayLog(instanceDir), session)
	if len(sessions) > maxLoggedSessions {
		sessions = sessions[len(sessions)-maxLoggedSessions:]
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return meta, err
	}
	if err := writeFileAtomic(filepath.Join(instanceDir, PlayLogFile), append(data, '\n'), 0o644); err != nil {
		return meta, fmt.Errorf("write %s: %w", PlayLogFile, err)
	}
	return meta, nil
}

// RecordPlaySession books a session against a named instance.
func (m *Manager) RecordPlaySession(name string, start, end time.Time) (Meta, error) {
	dir, err := m.InstancePath(name)
	if err != nil {
		return Meta{}, err
	}
	return RecordPlaySession(dir, start, end)
}
