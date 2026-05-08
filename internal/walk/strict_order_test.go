package walk_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/walk"
)

func TestStrictOrder_OneJobPerDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"d1/a": "A",
		"d1/b": "B",
		"d2/c": "C",
	})
	_ = os.MkdirAll(dst, 0755)

	w := walk.NewStrictOrder(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true})
	jobs := make(chan plan.Job, 8)
	if err := w.Walk(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	close(jobs)

	dirs := map[string]bool{}
	for j := range jobs {
		if j.Kind != plan.JobDirCopy {
			t.Fatalf("unexpected kind %v", j.Kind)
		}
		dirs[j.RelPath] = true
	}
	for _, d := range []string{"d1", "d2"} {
		if !dirs[d] {
			t.Errorf("missing dir job %s, got %v", d, dirs)
		}
	}
}

func TestStrictOrder_OnError_BestEffort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0 cannot block reads")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"good/a": "A",
		"good/b": "B",
		"bad/x":  "X",
		"more/c": "C",
	})
	// 'bad' 디렉토리 권한 박탈 → WalkDir이 자식을 읽을 때 에러
	badDir := filepath.Join(src, "bad")
	if err := os.Chmod(badDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badDir, 0755) })

	var (
		mu   sync.Mutex
		errs []struct {
			rel string
			err error
		}
	)
	w := walk.NewStrictOrder(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true}).
		OnError(func(rel string, err error) {
			mu.Lock()
			defer mu.Unlock()
			errs = append(errs, struct {
				rel string
				err error
			}{rel, err})
		})

	jobs := make(chan plan.Job, 16)
	var drainWg sync.WaitGroup
	var seen []string
	drainWg.Add(1)
	go func() {
		defer drainWg.Done()
		for j := range jobs {
			seen = append(seen, j.RelPath)
		}
	}()

	if err := w.Walk(context.Background(), jobs); err != nil {
		t.Fatalf("Walk returned non-nil error: %v", err)
	}
	close(jobs)
	drainWg.Wait()

	mu.Lock()
	if len(errs) == 0 {
		mu.Unlock()
		t.Fatal("expected OnError to be invoked at least once")
	}
	foundBad := false
	for _, e := range errs {
		if e.rel == "bad" {
			foundBad = true
			break
		}
	}
	mu.Unlock()
	if !foundBad {
		t.Errorf("expected OnError to record 'bad', got %+v", errs)
	}

	// 형제 디렉토리는 best-effort로 계속 처리되어야 한다.
	gotGood := false
	gotMore := false
	for _, r := range seen {
		if r == "good" {
			gotGood = true
		}
		if r == "more" {
			gotMore = true
		}
	}
	if !gotGood || !gotMore {
		t.Errorf("sibling dirs not emitted; seen=%v", seen)
	}
}
