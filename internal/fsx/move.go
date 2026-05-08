package fsx

import (
	"fmt"
	"os"
)

// MoveOpts controls RenameOrCopy beyond a plain rename.
type MoveOpts struct {
	NoClobber  bool // -n: dst가 이미 있으면 ErrSkipExisting (src는 그대로)
	Overwrite  bool // -f: dst가 있어도 진행 (rename은 자체 덮어쓰기)
	UpdateOnly bool // -u: src.mtime > dst.mtime일 때만 진행, 아니면 ErrSkipExisting
}

// renameFn은 테스트에서 EXDEV 강제 주입용으로 교체할 수 있는 후크.
var renameFn = os.Rename

// SetRenameForTest는 renameFn을 일시 교체하고 복구 함수를 반환한다.
// 테스트 종료 시 반드시 반환된 함수를 호출해야 한다.
func SetRenameForTest(f func(string, string) error) func() {
	prev := renameFn
	renameFn = f
	return func() { renameFn = prev }
}

// RenameOrCopy moves src → dst. First it tries os.Rename. On EXDEV (cross-device)
// it falls back to CopyFile + os.Remove(src) ("cp+unlink" semantics matching mv).
//
// Behavior under each MoveOpts flag:
//   - NoClobber: if dst exists, returns (0, ErrSkipExisting); src untouched.
//   - UpdateOnly: if dst exists and src is not strictly newer, returns
//     (0, ErrSkipExisting); src untouched.
//   - Overwrite: rename overwrites by default; CopyFile fallback uses CopyOpts{Overwrite: true}.
//
// Returns the byte count of the moved file (best-effort via Lstat after rename).
func RenameOrCopy(src, dst string, opt MoveOpts) (int64, error) {
	if opt.NoClobber {
		if _, err := os.Lstat(dst); err == nil {
			return 0, ErrSkipExisting
		}
	}
	if opt.UpdateOnly {
		srcInfo, err := os.Lstat(src)
		if err != nil {
			return 0, err
		}
		newer, err := IsNewer(srcInfo, dst)
		if err != nil {
			return 0, err
		}
		if !newer {
			return 0, ErrSkipExisting
		}
	}

	if err := renameFn(src, dst); err == nil {
		fi, statErr := os.Lstat(dst)
		if statErr != nil || fi.IsDir() {
			return 0, nil
		}
		return fi.Size(), nil
	} else if !IsEXDEV(err) {
		return 0, fmt.Errorf("rename: %w", err)
	}

	// EXDEV: copy + unlink fallback.
	n, err := CopyFile(src, dst, CopyOpts{NoClobber: opt.NoClobber, Overwrite: opt.Overwrite})
	if err != nil {
		return 0, err
	}
	if rmErr := os.Remove(src); rmErr != nil {
		return n, fmt.Errorf("unlink src after copy: %w", rmErr)
	}
	return n, nil
}
