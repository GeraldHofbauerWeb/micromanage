package proc

import (
	"os/exec"
	"syscall"
)

// createNoWindow runs a console program without giving it a console window.
const createNoWindow = 0x08000000

// Hide keeps cmd from opening a console window.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}
