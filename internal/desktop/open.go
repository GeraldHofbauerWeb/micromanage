// Package desktop hands files, folders and links to the programs the player
// already uses for them: the file manager, the text editor, the browser.
//
// Nothing here waits for a result. Whether the editor opened is the desktop's
// business; the launcher only has to ask.
package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Open hands a path or URL to the desktop, which picks the program: a folder
// opens in the file manager, a .toml in whatever edits text, a link in the
// browser.
func Open(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		// rundll32 is the one entry point that needs no shell quoting rules.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return start(cmd)
}

// Reveal opens the file manager on the folder containing a path, with the
// item selected where the platform can do that. It falls back to opening the
// folder plainly, which is never wrong, just less precise.
func Reveal(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("%s does not exist", filepath.Base(abs))
	}

	switch runtime.GOOS {
	case "darwin":
		return start(exec.Command("open", "-R", abs))
	case "windows":
		return start(exec.Command("explorer", "/select,", abs))
	default:
		// The freedesktop file manager interface is what "show in folder"
		// in browsers uses; every mainstream file manager implements it.
		if dbus, err := exec.LookPath("dbus-send"); err == nil {
			cmd := exec.Command(dbus, "--session", "--print-reply",
				"--dest=org.freedesktop.FileManager1",
				"/org/freedesktop/FileManager1",
				"org.freedesktop.FileManager1.ShowItems",
				"array:string:file://"+abs, "string:")
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		return Open(filepath.Dir(abs))
	}
}

// start launches a command and reaps it in the background, so a child that
// lingers for the whole session cannot become a zombie.
func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
