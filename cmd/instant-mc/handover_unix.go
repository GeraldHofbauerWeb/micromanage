//go:build !windows

package main

import (
	"os"
	"syscall"
)

// handOver replaces this process with the GUI. Using exec rather than spawning
// keeps a single process in the tree, so a desktop launcher or shell job
// control tracks the window instead of an idle parent.
func handOver(path string, args []string) error {
	argv := append([]string{path}, args...)
	return syscall.Exec(path, argv, os.Environ())
}
