package launch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/proc"
)

// Process is a running game.
type Process struct {
	Cmd     *exec.Cmd
	Started time.Time
	// LogPath is the file every line of output is also written to.
	LogPath string
	// Log holds the most recent output for display.
	Log *Ring

	waitErr error
	done    chan struct{}
}

// Spec is everything needed to start the game.
type Spec struct {
	JavaPath string
	Args     []string
	GameDir  string
	// LogDir receives one log file per launch.
	LogDir string
	// Name labels the log file, normally the instance name.
	Name string
	// Env overrides the environment; nil inherits this process's.
	Env []string
	// OnLine, if set, receives each line of output as it arrives.
	OnLine func(string)
}

// Start launches the game and returns once the process is running.
//
// Output is captured rather than inherited so the GUI can show it, and is
// written to a log file at the same time — a crash is far easier to diagnose
// from a file than from a scrollback the window already closed over.
func Start(ctx context.Context, spec Spec) (*Process, error) {
	if spec.JavaPath == "" {
		return nil, fmt.Errorf("no Java executable given")
	}
	if err := os.MkdirAll(spec.GameDir, 0o755); err != nil {
		return nil, fmt.Errorf("preparing the game directory: %w", err)
	}

	cmd := proc.Hide(exec.CommandContext(ctx, spec.JavaPath, spec.Args...))
	cmd.Dir = spec.GameDir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	logFile, logPath, err := openLogFile(spec.LogDir, spec.Name)
	if err != nil {
		// A log we cannot write is not a reason to refuse to play.
		logFile, logPath = nil, ""
	}

	p := &Process{
		Cmd:     cmd,
		Started: time.Now(),
		LogPath: logPath,
		Log:     NewRing(DefaultRingSize),
		done:    make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("starting %s: %w", filepath.Base(spec.JavaPath), err)
	}

	var pumps sync.WaitGroup
	pumps.Add(2)
	for _, r := range []io.Reader{stdout, stderr} {
		go func(r io.Reader) {
			defer pumps.Done()
			p.pump(r, logFile, spec.OnLine)
		}(r)
	}

	go func() {
		// Wait only after both pipes are drained, or Wait would close them
		// from under the readers.
		pumps.Wait()
		p.waitErr = cmd.Wait()
		if logFile != nil {
			logFile.Close()
		}
		close(p.done)
	}()

	return p, nil
}

// pump forwards one stream into the ring buffer, the log file and the callback.
func (p *Process) pump(r io.Reader, logFile *os.File, onLine func(string)) {
	scanner := bufio.NewScanner(r)
	// Some mods log enormous single lines; the default 64 KB limit would cut
	// the stream short and hide everything after it.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		p.Log.Add(line)
		if logFile != nil {
			fmt.Fprintln(logFile, line)
		}
		if onLine != nil {
			onLine(line)
		}
	}
}

// Wait blocks until the game exits and reports its exit status.
func (p *Process) Wait() error {
	<-p.done
	return p.waitErr
}

// Done is closed when the game exits.
func (p *Process) Done() <-chan struct{} { return p.done }

// Running reports whether the game is still alive.
func (p *Process) Running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Stop asks the game to close, escalating to a kill if it ignores the request.
func (p *Process) Stop(grace time.Duration) error {
	if p.Cmd.Process == nil || !p.Running() {
		return nil
	}

	if err := signalTerminate(p.Cmd.Process); err != nil {
		return p.Cmd.Process.Kill()
	}

	select {
	case <-p.done:
		return nil
	case <-time.After(grace):
		return p.Cmd.Process.Kill()
	}
}

// openLogFile creates a timestamped log for one launch.
func openLogFile(dir, name string) (*os.File, string, error) {
	if dir == "" {
		return nil, "", fmt.Errorf("no log directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	if name == "" {
		name = "launch"
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-%s.log", sanitiseName(name), time.Now().Format("20060102-150405")))
	f, err := os.Create(path)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

// sanitiseName keeps a log file name to safe characters.
func sanitiseName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, name)
}
