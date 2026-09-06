package launch

import "os"

// signalTerminate has no clean equivalent on Windows: the JVM does not handle
// a console control event sent from another process group, so Stop escalates
// to a kill immediately.
func signalTerminate(p *os.Process) error {
	return p.Kill()
}
