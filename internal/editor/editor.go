// Package editor locates a terminal text editor for opening instance files.
package editor

import "os/exec"
import "os"

// candidates are tried in order when neither EDITOR nor VISUAL is set.
var candidates = []string{"nano", "vim", "vi", "emacs", "code", "gedit"}

// Find returns an absolute path to a usable editor. Callers get a path rather
// than a bare name so the result can be handed straight to exec.Cmd.Path.
func Find() string {
	for _, env := range []string{"EDITOR", "VISUAL"} {
		if name := os.Getenv(env); name != "" {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	// Nothing found; "vi" is the most likely thing to exist on a PATH we
	// could not search successfully.
	return "vi"
}
