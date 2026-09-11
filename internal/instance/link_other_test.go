//go:build !windows

package instance

import "os"

// linkDir makes the kind of link older versions put in place of .minecraft.
func linkDir(target, link string) error {
	return os.Symlink(target, link)
}
