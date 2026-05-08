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

func TestStrictExt_TriggerOrdering(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"img/a.jpg":   "A",
		"img/b.jpg":   "B",
		"data/x.json": "X",
		"data/y.json": "Y",
		"plain.txt":   "P",
	})
	_ = os.MkdirAll(dst, 0755)

	w := walk.NewStrictExt(plan.Plan{
		Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true,
		StrictExtensions: []string{".json"},
	})

	drain := func(ch <-chan plan.Job) []string {
		var out []string
		for j := range ch {
			out = append(out, j.RelPath)
		}
		return out
	}

	// Phase 1: 별도 채널로 받아서 완전히 드레인
	p1 := make(chan plan.Job, 16)
	var phase1 []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		phase1 = drain(p1)
	}()
	if err := w.RunPhase1(context.Background(), p1); err != nil {
		t.Fatal(err)
	}
	close(p1)
	wg.Wait()

	if len(phase1) == 0 {
		t.Fatal("phase1 produced no jobs")
	}
	for _, r := range phase1 {
		if filepath.Ext(r) == ".json" {
			t.Errorf("phase1 contained .json file: %s", r)
		}
	}

	// Phase 2: 별도 채널로 받아서 완전히 드레인
	p2 := make(chan plan.Job, 16)
	var phase2 []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		phase2 = drain(p2)
	}()
	if err := w.RunPhase2(context.Background(), p2); err != nil {
		t.Fatal(err)
	}
	close(p2)
	wg.Wait()

	if len(phase2) == 0 {
		t.Fatal("phase2 produced no jobs")
	}
	for _, r := range phase2 {
		if filepath.Ext(r) != ".json" {
			t.Errorf("phase2 contained non-.json: %s", r)
		}
	}
}

func TestStrictExt_OnError_BothPhases(t *testing.T) {
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
		"good/a.txt":  "A",
		"good/b.json": "B",
		"bad/x.txt":   "X",
		"bad/y.json":  "Y",
		"more/c.txt":  "C",
		"more/d.json": "D",
	})
	_ = os.MkdirAll(dst, 0755)

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
	w := walk.NewStrictExt(plan.Plan{
		Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true,
		StrictExtensions: []string{".json"},
	}).OnError(func(rel string, err error) {
		mu.Lock()
		defer mu.Unlock()
		errs = append(errs, struct {
			rel string
			err error
		}{rel, err})
	})

	drain := func(ch <-chan plan.Job) []string {
		var out []string
		for j := range ch {
			out = append(out, j.RelPath)
		}
		return out
	}

	// Phase 1
	p1 := make(chan plan.Job, 16)
	var phase1 []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); phase1 = drain(p1) }()
	if err := w.RunPhase1(context.Background(), p1); err != nil {
		t.Fatalf("RunPhase1 returned non-nil: %v", err)
	}
	close(p1)
	wg.Wait()

	mu.Lock()
	phase1Errs := len(errs)
	mu.Unlock()
	if phase1Errs == 0 {
		t.Fatal("Phase 1: expected OnError to fire for unreadable subtree")
	}
	// 형제 디렉토리의 non-target 파일은 phase 1에서 처리되어야 한다.
	hasGood := false
	hasMore := false
	for _, r := range phase1 {
		if r == "good/a.txt" {
			hasGood = true
		}
		if r == "more/c.txt" {
			hasMore = true
		}
	}
	if !hasGood || !hasMore {
		t.Errorf("Phase 1 dropped sibling files: %v", phase1)
	}

	// Phase 2
	p2 := make(chan plan.Job, 16)
	var phase2 []string
	wg.Add(1)
	go func() { defer wg.Done(); phase2 = drain(p2) }()
	if err := w.RunPhase2(context.Background(), p2); err != nil {
		t.Fatalf("RunPhase2 returned non-nil: %v", err)
	}
	close(p2)
	wg.Wait()

	mu.Lock()
	totalErrs := len(errs)
	mu.Unlock()
	if totalErrs <= phase1Errs {
		t.Errorf("Phase 2: expected additional OnError from unreadable subtree (phase1=%d, total=%d)", phase1Errs, totalErrs)
	}
	hasGoodJSON := false
	hasMoreJSON := false
	for _, r := range phase2 {
		if r == "good/b.json" {
			hasGoodJSON = true
		}
		if r == "more/d.json" {
			hasMoreJSON = true
		}
	}
	if !hasGoodJSON || !hasMoreJSON {
		t.Errorf("Phase 2 dropped sibling .json files: %v", phase2)
	}
}
