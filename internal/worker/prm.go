package worker

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PRM은 prm용 Job 핸들러를 만든다.
func PRM(p plan.Plan) Handler {
	return func(ctx context.Context, j plan.Job) plan.Result {
		switch j.Kind {
		case plan.JobUnlink:
			return rmUnlink(p, j)
		case plan.JobDirRemove:
			return rmDir(p, j)
		}
		return plan.Result{Job: j, Err: fmt.Errorf("prm: unsupported job kind %s", j.Kind)}
	}
}

func rmUnlink(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	err := fsx.RemoveFile(j.Src)
	if err == nil {
		return plan.Result{Job: j}
	}
	if errors.Is(err, os.ErrNotExist) && p.ForceMissing {
		return plan.Result{Job: j, Skipped: true}
	}
	return plan.Result{Job: j, Err: err}
}

func rmDir(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	err := fsx.RemoveDir(j.Src)
	if err == nil {
		return plan.Result{Job: j}
	}
	if errors.Is(err, os.ErrNotExist) && p.ForceMissing {
		return plan.Result{Job: j, Skipped: true}
	}
	return plan.Result{Job: j, Err: err}
}
