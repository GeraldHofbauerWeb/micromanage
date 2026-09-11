package instance

import "os"

// isJunction reports whether an irregular entry is a link. Readlink only
// succeeds on symlinks and mount points, which rules out the other reparse
// points Go also reports as irregular, such as cloud placeholders.
func isJunction(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeIrregular == 0 {
		return false
	}
	_, err := os.Readlink(path)
	return err == nil
}
