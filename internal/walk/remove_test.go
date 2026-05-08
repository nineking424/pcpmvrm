package walk_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/walk"
)

func drainJobs(ch <-chan plan.Job) []plan.Job {
	var out []plan.Job
	for j := range ch {
		out = append(out, j)
	}
	return out
}

// TestRemoveWalker_FileOnly: single file → exactly 1 JobUnlink.
func TestRemoveWalker_FileOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: src})
	jobs := make(chan plan.Job, 4)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()

	got := drainJobs(jobs)
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d: %+v", len(got), got)
	}
	if got[0].Kind != plan.JobUnlink {
		t.Errorf("expected JobUnlink, got %v", got[0].Kind)
	}
	if got[0].Src != src {
		t.Errorf("expected Src=%s, got %s", src, got[0].Src)
	}
}

// TestRemoveWalker_DirRequiresRecursive: directory without -r → error.
func TestRemoveWalker_DirRequiresRecursive(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mydir")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: src, Recursive: false, RemoveEmptyDir: false})
	jobs := make(chan plan.Job, 4)
	err := w.Walk(context.Background(), jobs)
	close(jobs)

	if err == nil {
		t.Fatal("expected error for directory without -r or -d, got nil")
	}
}

// TestRemoveWalker_DirEmpty_DOption: empty dir + RemoveEmptyDir=true → 1 JobDirRemove.
func TestRemoveWalker_DirEmpty_DOption(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "emptydir")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: src, Recursive: false, RemoveEmptyDir: true})
	jobs := make(chan plan.Job, 4)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()

	got := drainJobs(jobs)
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d: %+v", len(got), got)
	}
	if got[0].Kind != plan.JobDirRemove {
		t.Errorf("expected JobDirRemove, got %v", got[0].Kind)
	}
	if got[0].Src != src {
		t.Errorf("expected Src=%s, got %s", src, got[0].Src)
	}
}

// TestRemoveWalker_RecursiveBarrier: root/a/{f1, b/f2}
// Simulates workers that call j.Finish() to release WaitGroup barriers.
// Asserts post-order: file unlinks arrive before their parent's JobDirRemove,
// and the root JobDirRemove is the very last job.
func TestRemoveWalker_RecursiveBarrier(t *testing.T) {
	root := t.TempDir()
	// tree: root/a/f1, root/a/b/f2
	aDir := filepath.Join(root, "a")
	bDir := filepath.Join(aDir, "b")
	if err := os.MkdirAll(bDir, 0755); err != nil {
		t.Fatal(err)
	}
	f1 := filepath.Join(aDir, "f1")
	f2 := filepath.Join(bDir, "f2")
	if err := os.WriteFile(f1, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("2"), 0644); err != nil {
		t.Fatal(err)
	}

	jobs := make(chan plan.Job, 16)
	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: root, Recursive: true})

	// Simulate worker pool: drain jobs and call Finish() on each.
	type indexedJob struct {
		job plan.Job
		idx int
	}
	collected := make(chan indexedJob, 16)

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		idx := 0
		for j := range jobs {
			collected <- indexedJob{job: j, idx: idx}
			idx++
			j.Finish()
		}
		close(collected)
	}()

	if err := w.Walk(context.Background(), jobs); err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}
	close(jobs)
	<-workerDone

	// Gather all collected jobs.
	var all []indexedJob
	for ij := range collected {
		all = append(all, ij)
	}

	// Must have: 2 JobUnlink (f1, f2), 2 JobDirRemove (a/b, a or root... actually root/a and root itself)
	// tree: root/a/b/f2 → unlink f2, rmdir b, unlink f1, rmdir a, rmdir root
	// But root is w.p.Src; walkDir is called on root, so we get rmdir root too.
	// Count kinds.
	var unlinks, rmdirs int
	for _, ij := range all {
		switch ij.job.Kind {
		case plan.JobUnlink:
			unlinks++
		case plan.JobDirRemove:
			rmdirs++
		}
	}
	if unlinks != 2 {
		t.Errorf("expected 2 JobUnlink, got %d", unlinks)
	}
	// root/a/b + root/a + root = 3 dirs
	if rmdirs != 3 {
		t.Errorf("expected 3 JobDirRemove, got %d", rmdirs)
	}

	// Build index map: job Src → index for easy lookup.
	indexOf := func(src string, kind plan.JobKind) int {
		for _, ij := range all {
			if ij.job.Src == src && ij.job.Kind == kind {
				return ij.idx
			}
		}
		return -1
	}

	// Ordering invariants (post-order):
	// 1. f2 unlink < b rmdir
	// 2. b rmdir < a rmdir
	// 3. f1 unlink < a rmdir
	// 4. a rmdir < root rmdir
	type check struct {
		beforeSrc  string
		beforeKind plan.JobKind
		afterSrc   string
		afterKind  plan.JobKind
		desc       string
	}
	checks := []check{
		{f2, plan.JobUnlink, bDir, plan.JobDirRemove, "f2 unlink before b rmdir"},
		{bDir, plan.JobDirRemove, aDir, plan.JobDirRemove, "b rmdir before a rmdir"},
		{f1, plan.JobUnlink, aDir, plan.JobDirRemove, "f1 unlink before a rmdir"},
		{aDir, plan.JobDirRemove, root, plan.JobDirRemove, "a rmdir before root rmdir"},
	}
	for _, c := range checks {
		before := indexOf(c.beforeSrc, c.beforeKind)
		after := indexOf(c.afterSrc, c.afterKind)
		if before == -1 {
			t.Errorf("missing job: kind=%v src=%s", c.beforeKind, c.beforeSrc)
			continue
		}
		if after == -1 {
			t.Errorf("missing job: kind=%v src=%s", c.afterKind, c.afterSrc)
			continue
		}
		if before >= after {
			t.Errorf("ordering violated: %s (idx %d >= %d)", c.desc, before, after)
		}
	}

	// Root rmdir must be last.
	rootIdx := indexOf(root, plan.JobDirRemove)
	if rootIdx != len(all)-1 {
		t.Errorf("root JobDirRemove must be last, got idx %d of %d total", rootIdx, len(all))
	}

	// Sanity: all job Srcs are under or equal to root.
	for _, ij := range all {
		if !strings.HasPrefix(ij.job.Src, root) {
			t.Errorf("unexpected Src outside root: %s", ij.job.Src)
		}
	}
}
