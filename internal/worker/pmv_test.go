package worker_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func TestPMVHandler_RenameSameDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(src)

	h := worker.PMV(plan.Plan{Op: plan.OpMove, SameDevice: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobRename, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.Bytes != 2 {
		t.Errorf("Bytes = %d, want 2", r.Bytes)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src still exists after rename")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hi" {
		t.Errorf("dst = %q, want hi", got)
	}
}

func TestPMVHandler_DryRunNoIO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(src)

	h := worker.PMV(plan.Plan{Op: plan.OpMove, DryRun: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobRename, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped=true under DryRun")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src must still exist under DryRun: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst must not exist under DryRun")
	}
}

func TestPMVHandler_NoClobberSkips(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(src)

	h := worker.PMV(plan.Plan{Op: plan.OpMove, NoClobber: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobRename, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped=true with NoClobber and existing dst")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst overwritten: %q", got)
	}
}

func TestPMVHandler_DirRemove(t *testing.T) {
	dir := t.TempDir()
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}

	h := worker.PMV(plan.Plan{Op: plan.OpMove})
	r := h(context.Background(), plan.Job{Kind: plan.JobDirRemove, Src: emptyDir})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if _, err := os.Stat(emptyDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir should be removed: err=%v", err)
	}
}

func TestPMVHandler_DirRemoveNonEmpty(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "x")
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	h := worker.PMV(plan.Plan{Op: plan.OpMove})
	r := h(context.Background(), plan.Job{Kind: plan.JobDirRemove, Src: d})
	if r.Err == nil {
		t.Fatal("rmdir on non-empty dir should fail")
	}
}

func TestPMVHandler_DirRemoveDryRun(t *testing.T) {
	dir := t.TempDir()
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}

	h := worker.PMV(plan.Plan{Op: plan.OpMove, DryRun: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobDirRemove, Src: emptyDir})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("dry-run should report Skipped")
	}
	if _, err := os.Stat(emptyDir); err != nil {
		t.Errorf("dry-run must not remove dir: %v", err)
	}
}
