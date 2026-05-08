package fsx

import "os"

// RemoveFile removes a regular file or symlink. Use a separate helper
// (introduced in Plan 3) for directories.
func RemoveFile(path string) error {
	return os.Remove(path)
}
