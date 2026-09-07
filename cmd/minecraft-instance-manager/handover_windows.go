package main

import (
	"os"
	"os/exec"
)

// handOver runs the GUI as a child process. Windows has no exec(2), so the
// parent stays alive and mirrors the child's exit code.
func handOver(path string, args []string) error {
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
