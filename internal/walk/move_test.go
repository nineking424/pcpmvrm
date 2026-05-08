package walk_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/walk"
)

func drainMoveJobs(ch <-chan plan.Job) []plan.Job {
	var out []plan.Job
	for j := range ch {
		out = append(out, j)
	}
	return out
}

func TestMoveWalker_SameDevice_SingleRenameJob(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w := walk.NewMove(plan.Plan{Op: plan.OpMove, Src: src, Dst: dst, SameDevice: true})
	jobs := make(chan plan.Job, 4)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()

	got := drainMoveJobs(jobs)
	if len(got) != 1 || got[0].Kind != plan.JobRename {
		t.Fatalf("same-device walker should emit exactly one JobRename, got: %+v", got)
	}
	if got[0].Src != src || got[0].Dst != dst {
		t.Errorf("rename job paths mismatch: %+v", got[0])
	}
}

func TestMoveWalker_CrossDevice_PreOrderMkdirThenFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "a", "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "b", "f.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w := walk.NewMove(plan.Plan{Op: plan.OpMove, Src: src, Dst: dst, SameDevice: false})
	jobs := make(chan plan.Job, 16)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()
	got := drainMoveJobs(jobs)

	if _, err := os.Stat(filepath.Join(dst, "a", "b")); err != nil {
		t.Errorf("walker should mkdir dst tree: %v", err)
	}

	var copies, unlinks, rmdirs int
	for _, j := range got {
		switch j.Kind {
		case plan.JobCopy:
			copies++
		case plan.JobUnlink:
			unlinks++
		case plan.JobDirRemove:
			rmdirs++
		}
	}
	if copies != 1 || unlinks != 1 || rmdirs < 1 {
		t.Errorf("counts: copies=%d unlinks=%d rmdirs=%d", copies, unlinks, rmdirs)
	}
}
