package fallback

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestBuild_PCPCopiesViaCp(t *testing.T) {
	// Create a source temp file with known content.
	src, err := os.CreateTemp(t.TempDir(), "src-")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello fallback")
	if _, err := src.Write(content); err != nil {
		t.Fatal(err)
	}
	src.Close()

	dst := filepath.Join(t.TempDir(), "dst")

	p := plan.Plan{Op: plan.OpCopy}
	h := Build(p)
	r := h(context.Background(), plan.Job{
		Kind: plan.JobCopy,
		Src:  src.Name(),
		Dst:  dst,
	})

	if r.Err != nil {
		t.Fatalf("expected no error, got: %v", r.Err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("dst not created: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q want %q", got, content)
	}
}

func TestBuild_DryRunNoSpawn(t *testing.T) {
	p := plan.Plan{Op: plan.OpCopy, DryRun: true}
	h := Build(p)
	r := h(context.Background(), plan.Job{
		Kind: plan.JobCopy,
		Src:  "/nonexistent/src",
		Dst:  "/nonexistent/dst",
	})

	if r.Err != nil {
		t.Fatalf("dry-run should not error, got: %v", r.Err)
	}
	if !r.Skipped {
		t.Fatal("dry-run result must have Skipped=true")
	}
}

func TestBuild_ChildExitsNonZero(t *testing.T) {
	// 존재하지 않는 src로 cp를 호출 → 자식이 비-0 종료. result.Err가 채워지는지 검증.
	dst := filepath.Join(t.TempDir(), "dst")
	p := plan.Plan{Op: plan.OpCopy}
	h := Build(p)
	r := h(context.Background(), plan.Job{
		Kind: plan.JobCopy,
		Src:  "/no/such/src/path",
		Dst:  dst,
	})

	if r.Err == nil {
		t.Fatal("expected error from non-existent source, got nil")
	}
}

func TestBuild_PRMRemovesViaRm(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "rm-")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	target := f.Name()

	p := plan.Plan{Op: plan.OpRemove}
	h := Build(p)
	r := h(context.Background(), plan.Job{
		Kind: plan.JobUnlink,
		Src:  target,
	})

	if r.Err != nil {
		t.Fatalf("expected no error, got: %v", r.Err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("file should have been removed")
	}
}
