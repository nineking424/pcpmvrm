package walk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// StrictExt is a two-phase walker. Phase 1 = non-target files in DFS order;
// Phase 2 = target-extension files in lexical order, serially.
type StrictExt struct {
	p   plan.Plan
	def *Default
}

// NewStrictExt returns a StrictExt walker bound to p.
func NewStrictExt(p plan.Plan) *StrictExt {
	return &StrictExt{p: p, def: NewDefault(p)}
}

// RunPhase1 emits non-target files. Internally reuses Default's logic, which
// already skips strict-extension matches.
func (w *StrictExt) RunPhase1(ctx context.Context, jobs chan<- plan.Job) error {
	return w.def.Walk(ctx, jobs)
}

// RunPhase2 emits target files in lexical order. Caller is responsible for
// ensuring Phase1's workers have drained before calling this (typically by
// closing/replacing the jobs channel and re-creating the worker pool with
// workers=1; see cmd/pcp/main.go).
func (w *StrictExt) RunPhase2(ctx context.Context, jobs chan<- plan.Job) error {
	exts := buildExtSet(w.p.StrictExtensions)
	if len(exts) == 0 {
		return nil
	}

	var matches []string
	err := filepath.WalkDir(w.p.Src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if _, hit := exts[strings.ToLower(filepath.Ext(path))]; hit {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(matches)

	for _, m := range matches {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		info, err := os.Stat(m)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(w.p.Src, m)
		dst := filepath.Join(w.p.Dst, rel)
		select {
		case jobs <- plan.Job{Kind: plan.JobCopy, Src: m, Dst: dst, RelPath: rel, Info: info}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
