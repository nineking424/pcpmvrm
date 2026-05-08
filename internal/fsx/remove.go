package fsx

import "os"

// RemoveFile removes a regular file or symlink. Use a separate helper
// (introduced in Plan 3) for directories.
func RemoveFile(path string) error {
	return os.Remove(path)
}

// RemoveDir removes an empty directory (rmdir(2) semantics).
// On a non-empty directory, the underlying syscall returns ENOTEMPTY.
func RemoveDir(path string) error {
	return os.Remove(path)
}
