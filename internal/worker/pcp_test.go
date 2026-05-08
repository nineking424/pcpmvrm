package worker_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func TestPCPHandler_CopiesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(src)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("dst = %q, want hello", got)
	}
}

func TestPCPHandler_NoClobberSkips(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	_ = os.WriteFile(src, []byte("new"), 0644)
	_ = os.WriteFile(dst, []byte("old"), 0644)
	info, _ := os.Stat(src)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, NoClobber: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped=true")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst overwritten: %q", got)
	}
}

func TestPCPHandler_UpdateOnlyOlderDstAllows(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	_ = os.WriteFile(src, []byte("new"), 0644)
	_ = os.WriteFile(dst, []byte("old"), 0644)
	info, _ := os.Stat(src)

	// dst를 더 오래된 mtime으로 강제
	older := info.ModTime().Add(-1 * 1)
	_ = os.Chtimes(dst, older, older)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, UpdateOnly: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Errorf("dst = %q, want new", got)
	}
}

func TestPCPHandler_DryRunNoIO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	_ = os.WriteFile(src, []byte("hi"), 0644)
	info, _ := os.Stat(src)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, DryRun: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run wrote dst: stat err=%v", err)
	}
	if !r.Skipped {
		t.Error("dry-run should report Skipped=true")
	}
	_ = fsx.ErrSkipExisting // keep import
}
