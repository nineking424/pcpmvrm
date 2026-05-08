// Package walk implements the three walking strategies (default file-unit,
// strict-order directory-unit, strict-extension two-phase).
package walk

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Default is the standard streaming walker:
//   - DFS pre-order
//   - directories are mkdir'd eagerly (synchronously) by the walker
//   - files are pushed as JobCopy onto the queue
type Default struct {
	p       plan.Plan
	onError func(rel string, err error) // optional; called for skipped walk errors
}

// NewDefault returns a Default walker bound to p.
func NewDefault(p plan.Plan) *Default { return &Default{p: p} }

// OnError sets a callback that is invoked for each skipped walk error.
// This allows the caller to record walk-level errors (e.g. unreadable dirs).
func (w *Default) OnError(fn func(rel string, err error)) *Default {
	w.onError = fn
	return w
}

// Walk pushes JobCopy values onto jobs until the tree is exhausted or ctx done.
func (w *Default) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	srcInfo, err := os.Lstat(w.p.Src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		if !w.p.Recursive {
			return fmt.Errorf("%s is a directory (use -r)", w.p.Src)
		}
		return w.walkDir(ctx, jobs)
	}
	// 단일 파일
	return w.pushFile(ctx, jobs, w.p.Src, srcInfo, "")
}

func (w *Default) walkDir(ctx context.Context, jobs chan<- plan.Job) error {
	// dst 루트가 없으면 src의 모드로 mkdir
	if err := os.MkdirAll(w.p.Dst, 0755); err != nil {
		return err
	}

	skipExt := buildExtSet(w.p.StrictExtensions)

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
		if path == w.p.Src {
			return nil
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}

		rel, _ := filepath.Rel(w.p.Src, path)
		dst := filepath.Join(w.p.Dst, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}

		// strict-extension: 대상 확장자는 2-phase에서 처리하므로 여기선 skip
		if len(skipExt) > 0 {
			if _, hit := skipExt[strings.ToLower(filepath.Ext(path))]; hit {
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return w.pushJob(ctx, jobs, plan.Job{
			Kind:    plan.JobCopy,
			Src:     path,
			Dst:     dst,
			RelPath: rel,
			Info:    info,
		})
	})
}

func (w *Default) pushFile(ctx context.Context, jobs chan<- plan.Job, src string, info fs.FileInfo, rel string) error {
	dst := w.p.Dst
	if info.IsDir() {
		return fmt.Errorf("internal: pushFile called with directory")
	}
	if rel == "" {
		rel = filepath.Base(src)
	}
	return w.pushJob(ctx, jobs, plan.Job{
		Kind: plan.JobCopy, Src: src, Dst: dst, RelPath: rel, Info: info,
	})
}

func (w *Default) pushJob(ctx context.Context, jobs chan<- plan.Job, j plan.Job) error {
	select {
	case <-ctx.Done():
		return filepath.SkipAll
	case jobs <- j:
		return nil
	}
}

func buildExtSet(list []string) map[string]struct{} {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(list))
	for _, e := range list {
		out[strings.ToLower(e)] = struct{}{}
	}
	return out
}
