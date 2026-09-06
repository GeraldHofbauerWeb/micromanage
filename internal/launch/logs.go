package launch

import "sync"

// DefaultRingSize is how many output lines are kept for display. Enough to
// hold a full crash report and the mod list that preceded it.
const DefaultRingSize = 2000

// Ring is a fixed-size buffer of the most recent log lines.
//
// It is written from the output pumps and read from the UI, so every method
// takes the lock; Lines returns a copy rather than a view.
type Ring struct {
	mu    sync.RWMutex
	lines []string
	next  int
	full  bool
}

// NewRing returns a ring holding at most size lines.
func NewRing(size int) *Ring {
	if size <= 0 {
		size = DefaultRingSize
	}
	return &Ring{lines: make([]string, size)}
}

// Add appends a line, discarding the oldest when full.
func (r *Ring) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines[r.next] = line
	r.next = (r.next + 1) % len(r.lines)
	if r.next == 0 {
		r.full = true
	}
}

// Len reports how many lines are held.
func (r *Ring) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.full {
		return len(r.lines)
	}
	return r.next
}

// Lines returns the buffered lines, oldest first.
func (r *Ring) Lines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.full {
		return append([]string(nil), r.lines[:r.next]...)
	}
	out := make([]string, 0, len(r.lines))
	out = append(out, r.lines[r.next:]...)
	return append(out, r.lines[:r.next]...)
}

// Tail returns the last n lines, oldest first.
func (r *Ring) Tail(n int) []string {
	lines := r.Lines()
	if n <= 0 || n >= len(lines) {
		return lines
	}
	return lines[len(lines)-n:]
}
