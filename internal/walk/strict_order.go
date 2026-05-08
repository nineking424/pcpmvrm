package walk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// StrictOrder emits one Job per directory. Workers process the directory
// content serially, in walk order, by re-walking from the directory root.
type StrictOrder struct {
	p       plan.Plan
	onError func(rel string, err error) // optional; called for skipped walk errors
}

// NewStrictOrder returns a StrictOrder walker.
func NewStrictOrder(p plan.Plan) *StrictOrder { return &StrictOrder{p: p} }

// OnError sets a callback that is invoked for each skipped walk error.
// Aligns StrictOrder with Default's best-effort default (brainstorm Q5).
func (w *StrictOrder) OnError(fn func(rel string, err error)) *StrictOrder {
	w.onError = fn
	return w
}

// Walk pushes one JobDirCopy per directory under src.
func (w *StrictOrder) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	if err := os.MkdirAll(w.p.Dst, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(w.p.Src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// best-effort: skip this entry and continue with siblings.
			rel, _ := filepath.Rel(w.p.Src, path)
			if w.onError != nil {
				w.onError(rel, err)
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}
		rel, _ := filepath.Rel(w.p.Src, path)
		// dst 디렉토리는 즉시 mkdir (워커가 자식 파일 처리할 때 부모가 존재해야)
		dst := filepath.Join(w.p.Dst, rel)
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		select {
		case jobs <- plan.Job{Kind: plan.JobDirCopy, Src: path, Dst: dst, RelPath: rel}:
		case <-ctx.Done():
			return filepath.SkipAll
		}
		return nil
	})
}
