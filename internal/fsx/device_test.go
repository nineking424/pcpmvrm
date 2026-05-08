package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
)

func TestSameDevice_SamePath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	same, err := fsx.SameDevice(a, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !same {
		t.Error("expected same device for files under same tmpdir")
	}
}

func TestIsEXDEV(t *testing.T) {
	if !fsx.IsEXDEV(syscall.EXDEV) {
		t.Error("EXDEV should match")
	}
	if !fsx.IsEXDEV(&os.LinkError{Err: syscall.EXDEV}) {
		t.Error("wrapped EXDEV should match")
	}
	if fsx.IsEXDEV(errors.New("other")) {
		t.Error("non-EXDEV should not match")
	}
}
