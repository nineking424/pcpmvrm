package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
)

func TestRenameOrCopy_SameDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{})
	if err != nil {
		t.Fatalf("RenameOrCopy: %v", err)
	}
	if n != 5 {
		t.Errorf("bytes=%d, want 5", n)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("src still exists: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("dst content=%q", got)
	}
}

func TestRenameOrCopy_EXDEVFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("xy"), 0600); err != nil {
		t.Fatal(err)
	}
	restore := fsx.SetRenameForTest(func(_, _ string) error {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	})
	defer restore()

	n, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if n != 2 {
		t.Errorf("bytes=%d, want 2", n)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("src must be unlinked after cp+unlink fallback")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "xy" {
		t.Errorf("dst content=%q", got)
	}
}

func TestRenameOrCopy_NoClobberSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("s"), 0644)
	os.WriteFile(dst, []byte("d"), 0644)

	_, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{NoClobber: true})
	if !errors.Is(err, fsx.ErrSkipExisting) {
		t.Errorf("err=%v, want ErrSkipExisting", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "d" {
		t.Errorf("dst overwritten: %q", got)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should remain untouched on no-clobber skip: %v", err)
	}
}

func TestRenameOrCopy_UpdateOnlySkipsOlderSrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("s"), 0644)
	os.WriteFile(dst, []byte("d"), 0644)
	// Make dst strictly newer than src.
	older := mustParseTime(t, "2024-01-01T00:00:00Z")
	newer := mustParseTime(t, "2026-01-01T00:00:00Z")
	if err := os.Chtimes(src, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dst, newer, newer); err != nil {
		t.Fatal(err)
	}

	_, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{UpdateOnly: true})
	if !errors.Is(err, fsx.ErrSkipExisting) {
		t.Errorf("err=%v, want ErrSkipExisting", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should remain on update-only skip: %v", err)
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
