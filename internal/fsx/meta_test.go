package fsx_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestPreserve_ModeAndTimestamps(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(src, past, past); err != nil {
		t.Fatal(err)
	}

	srcInfo, _ := os.Stat(src)
	if err := fsx.PreserveMeta(srcInfo, dst, plan.Preserve{Mode: true, Timestamps: true}); err != nil {
		t.Fatalf("PreserveMeta: %v", err)
	}

	got, _ := os.Stat(dst)
	if got.Mode().Perm() != 0640 {
		t.Errorf("dst mode = %v, want 0640", got.Mode().Perm())
	}
	if !got.ModTime().Truncate(time.Second).Equal(past) {
		t.Errorf("dst mtime = %v, want %v", got.ModTime(), past)
	}
}
