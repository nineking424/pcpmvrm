package walk_test

import (
	"context"
	"os"
	"path/filepath"
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
		"img/a.jpg":     "A",
		"img/b.jpg":     "B",
		"data/x.json":   "X",
		"data/y.json":   "Y",
		"plain.txt":     "P",
	})
	_ = os.MkdirAll(dst, 0755)

	w := walk.NewStrictExt(plan.Plan{
		Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true,
		StrictExtensions: []string{".json"},
	})

	var phaseSeen []string
	var mu sync.Mutex
	jobs := make(chan plan.Job, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for j := range jobs {
			mu.Lock()
			phaseSeen = append(phaseSeen, j.RelPath)
			mu.Unlock()
		}
	}()

	// Phase 1만 먼저 끝내고, Phase 2는 RunPhase2로 트리거
	if err := w.RunPhase1(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	phase1Count := len(phaseSeen)
	mu.Unlock()
	if phase1Count == 0 {
		t.Fatal("phase1 produced no jobs")
	}

	if err := w.RunPhase2(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	close(jobs)
	<-done

	mu.Lock()
	defer mu.Unlock()
	// Phase1 안에 .json이 없어야 한다
	for _, r := range phaseSeen[:phase1Count] {
		if filepath.Ext(r) == ".json" {
			t.Errorf("phase1 contained .json file: %s", r)
		}
	}
	// Phase2엔 .json만
	for _, r := range phaseSeen[phase1Count:] {
		if filepath.Ext(r) != ".json" {
			t.Errorf("phase2 contained non-.json: %s", r)
		}
	}
}
