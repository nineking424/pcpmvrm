package fsx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ErrSkipExisting indicates -n (no-clobber) skipped a file because dst exists.
var ErrSkipExisting = errors.New("skip: destination exists")

// CopyOpts controls CopyFile behavior beyond a plain copy.
type CopyOpts struct {
	NoClobber bool // -n: dst가 이미 있으면 skip (race-free via O_EXCL)
	Overwrite bool // -f: dst가 있어도 덮어쓰기 (no-clobber와 상호 배타)
}

// CopyFile copies src → dst atomically by writing into a temp file
// (`<dst>.pcp-tmp-XXXXXX`) and renaming on success.
//
// Returns the number of bytes written. On error the temp file is removed.
func CopyFile(src, dst string, opt CopyOpts) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	if opt.NoClobber {
		// O_EXCL로 직접 dst를 잡아본다. 존재하면 EEXIST.
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return 0, ErrSkipExisting
			}
			return 0, err
		}
		n, copyErr := io.Copy(f, in)
		closeErr := f.Close()
		if copyErr != nil {
			_ = os.Remove(dst)
			return 0, copyErr
		}
		if closeErr != nil {
			_ = os.Remove(dst)
			return 0, closeErr
		}
		return n, nil
	}

	tmp, err := openTempBeside(dst)
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	n, copyErr := io.Copy(tmp, in)
	if copyErr != nil {
		_ = tmp.Close()
		cleanup()
		return 0, copyErr
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return 0, err
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		cleanup()
		return 0, err
	}
	return n, nil
}

func openTempBeside(dst string) (*os.File, error) {
	dir := filepath.Dir(dst)
	for try := 0; try < 8; try++ {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		name := filepath.Base(dst) + ".pcp-tmp-" + hex.EncodeToString(b[:])
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	return nil, errors.New("failed to allocate temp file after 8 tries")
}
