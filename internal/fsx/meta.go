package fsx

import (
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PreserveMeta copies the requested attributes from srcInfo onto dst.
// Ownership preservation requires CAP_CHOWN; on failure we silently skip
// to match GNU cp's behavior when running as a non-root user.
func PreserveMeta(srcInfo fs.FileInfo, dst string, p plan.Preserve) error {
	if p.Mode {
		if err := os.Chmod(dst, srcInfo.Mode().Perm()); err != nil {
			return err
		}
	}
	if p.Ownership {
		if sys, ok := srcInfo.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(dst, int(sys.Uid), int(sys.Gid)) // best-effort
		}
	}
	if p.Timestamps {
		mt := srcInfo.ModTime()
		if err := os.Chtimes(dst, mt, mt); err != nil {
			return err
		}
	}
	return nil
}

// IsNewer compares mtimes for -u handling. Returns true when src has a strictly
// newer modification time than dst, or dst does not exist.
func IsNewer(srcInfo fs.FileInfo, dst string) (bool, error) {
	dstInfo, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return srcInfo.ModTime().After(dstInfo.ModTime()), nil
}

// ApproxSecond used by tests to compare timestamps tolerantly.
func ApproxSecond(a, b time.Time) bool {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d < time.Second
}
