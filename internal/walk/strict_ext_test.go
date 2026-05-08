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
