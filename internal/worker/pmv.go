package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PMV returns a Handler that performs the move/rename work for pmv.
func PMV(p plan.Plan) Handler {
	return func(ctx context.Context, j plan.Job) plan.Result {
		switch j.Kind {
		case plan.JobRename:
			return handleRename(p, j)
		case plan.JobCopy:
			return PCP(p)(ctx, j)
		case plan.JobUnlink:
			return handleUnlink(p, j)
		case plan.JobDirRemove:
			return handleDirRemove(p, j)
		}
		return plan.Result{Job: j, Err: fmt.Errorf("worker/pmv: unsupported job kind %s", j.Kind)}
	}
}

func handleRename(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	opts := fsx.MoveOpts{
		NoClobber:  p.NoClobber,
		Overwrite:  p.Overwrite,
		UpdateOnly: p.UpdateOnly,
	}
	n, err := fsx.RenameOrCopy(j.Src, j.Dst, opts)
	if errors.Is(err, fsx.ErrSkipExisting) {
		return plan.Result{Job: j, Skipped: true}
	}
	if err != nil {
		return plan.Result{Job: j, Err: err}
	}
	return plan.Result{Job: j, Bytes: n}
}

func handleUnlink(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	if err := fsx.RemoveFile(j.Src); err != nil {
		return plan.Result{Job: j, Err: err}
	}
	return plan.Result{Job: j}
}

func handleDirRemove(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	if err := fsx.RemoveDir(j.Src); err != nil {
		return plan.Result{Job: j, Err: err}
	}
	return plan.Result{Job: j}
}
