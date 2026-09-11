//go:build !windows

package instance

import "os"

// isJunction is always false: junctions are a Windows thing.
func isJunction(string, os.FileInfo) bool {
	return false
}
