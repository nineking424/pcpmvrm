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

func TestPRMHandler_UnlinkExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	h := worker.PRM(plan.Plan{Op: plan.OpRemove})
	ctx := context.Background()
	r := h(ctx, plan.Job{Kind: plan.JobUnlink, Src: src})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should be removed")
	}
}

func TestPRMHandler_MissingFileWithoutForce(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nonexistent.txt")

	h := worker.PRM(plan.Plan{Op: plan.OpRemove, ForceMissing: false})
	ctx := context.Background()
	r := h(ctx, plan.Job{Kind: plan.JobUnlink, Src: src})
	if r.Err == nil {
		t.Fatal("expected error for missing file without ForceMissing")
	}
	if !errors.Is(r.Err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", r.Err)
	}
}

func TestPRMHandler_MissingFileWithForce(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nonexistent.txt")

	h := worker.PRM(plan.Plan{Op: plan.OpRemove, ForceMissing: true})
	ctx := context.Background()
	r := h(ctx, plan.Job{Kind: plan.JobUnlink, Src: src})
	if r.Err != nil {
		t.Fatalf("unexpected error with ForceMissing: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped=true with ForceMissing and missing file")
	}
}

func TestPRMHandler_DryRunNoIO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	h := worker.PRM(plan.Plan{Op: plan.OpRemove, DryRun: true})
	ctx := context.Background()
	r := h(ctx, plan.Job{Kind: plan.JobUnlink, Src: src})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped=true under DryRun")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("file should still exist under DryRun: %v", err)
	}
}

func TestPRMHandler_DirRemove(t *testing.T) {
	dir := t.TempDir()
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}

	h := worker.PRM(plan.Plan{Op: plan.OpRemove})
	ctx := context.Background()
	r := h(ctx, plan.Job{Kind: plan.JobDirRemove, Src: emptyDir})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if _, err := os.Stat(emptyDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir should be removed: err=%v", err)
	}
}
