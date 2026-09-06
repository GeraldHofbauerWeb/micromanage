//go:build !windows

package launch

import (
	"os"
	"syscall"
)

// signalTerminate asks the game to shut down cleanly, giving it the chance to
// save worlds before exiting.
func signalTerminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
