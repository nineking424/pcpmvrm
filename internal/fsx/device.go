// Package fsx provides filesystem helpers used by the worker pool.
//
// All helpers are written for Linux/macOS (POSIX). Windows is out of scope.
package fsx

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// SameDevice returns true if a and b live on the same filesystem.
//
// b can be a not-yet-existing path; in that case the parent directory is
// stat'd. This matches the behavior of mv: dst's parent decides device
// membership for the rename target.
func SameDevice(a, b string) (bool, error) {
	da, err := devID(a)
	if err != nil {
		return false, err
	}
	db, err := devID(b)
	if err != nil {
		// b 없으면 부모로 다시 시도
		if errors.Is(err, fs.ErrNotExist) {
			db, err = devID(parentOf(b))
			if err != nil {
				return false, err
			}
		} else {
			return false, err
		}
	}
	return da == db, nil
}

func devID(path string) (uint64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("stat: unsupported platform")
	}
	return uint64(sys.Dev), nil
}

func parentOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return "."
}

// IsEXDEV reports whether err is (or wraps) syscall.EXDEV.
func IsEXDEV(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, syscall.EXDEV)
	}
	return false
}
