package instance

import "os"

// isDirLink reports whether path is a directory link: a symlink, or on
// Windows a junction. Versions before 2 replaced .minecraft with one; the
// launcher now only looks for it, to undo that.
func isDirLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return isLinkInfo(path, info)
}

// isLinkInfo is isDirLink for a path already Lstat'ed. Go reports a junction
// as irregular rather than as a symlink, so the mode alone does not say.
func isLinkInfo(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	return isJunction(path, info)
}
