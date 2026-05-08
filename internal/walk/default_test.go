package walk_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/walk"
)

func mkTree(t *testing.T, root string, paths map[string]string) {
	t.Helper()
	for rel, body := range paths {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefaultWalk_QueuesAllFiles_AndCreatesDirsEager(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"a.txt":       "A",
		"sub/b.txt":   "B",
		"sub/c/d.bin": "D",
	})
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	jobs := make(chan plan.Job, 16)
	w := walk.NewDefault(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true})
	if err := w.Walk(context.Background(), jobs); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	close(jobs)

	got := map[string]bool{}
	for j := range jobs {
		got[j.RelPath] = true
	}
	want := []string{"a.txt", "sub/b.txt", "sub/c/d.bin"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing job for %s, got %v", w, got)
		}
	}

	// dst 디렉토리들이 즉시 만들어졌는지
	for _, d := range []string{"sub", "sub/c"} {
		if _, err := os.Stat(filepath.Join(dst, d)); err != nil {
			t.Errorf("dst dir %s not created: %v", d, err)
		}
	}
}

func TestDefaultWalk_NonRecursive_RejectsDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	jobs := make(chan plan.Job, 4)
	w := walk.NewDefault(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: false})
	err := w.Walk(context.Background(), jobs)
	if err == nil {
		t.Fatal("expected error when src is a directory and -r is unset")
	}
}
