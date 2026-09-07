package gui

import (
	"os/exec"
	"runtime"
)

// openURL hands a link to the desktop's browser.
//
// It is fire-and-forget on purpose: the sign-in continues whether or not the
// browser opened, because the code on screen can always be typed by hand.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// rundll32 is the one entry point that needs no shell quoting rules.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Reaping the child keeps a zombie from lingering for the session; the
	// exit status says nothing useful about whether the page opened.
	go func() { _ = cmd.Wait() }()
	return nil
}
