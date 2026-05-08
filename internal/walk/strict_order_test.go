package walk_test

import (
	"context"
	"os"
	"path/filepath"
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
