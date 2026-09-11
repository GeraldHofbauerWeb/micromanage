//go:build !windows

package proc

import "os/exec"

// Hide does nothing: only Windows opens a window for a console child.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	return cmd
}
