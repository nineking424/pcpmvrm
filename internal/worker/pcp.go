package worker

import (
	"context"
	"errors"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PCP returns a Handler that performs the actual copy work for pcp.
func PCP(p plan.Plan) Handler {
	return func(ctx context.Context, j plan.Job) plan.Result {
		if j.Kind != plan.JobCopy {
			return plan.Result{Job: j, Err: errors.New("worker/pcp: unexpected job kind")}
		}
		started := time.Now()

		// -u: dst가 src보다 같거나 새로우면 skip
		if p.UpdateOnly {
			newer, err := fsx.IsNewer(j.Info, j.Dst)
			if err != nil {
				return plan.Result{Job: j, Err: err, Elapsed: time.Since(started)}
			}
			if !newer {
				return plan.Result{Job: j, Skipped: true, Elapsed: time.Since(started)}
			}
		}

		if p.DryRun {
			return plan.Result{Job: j, Skipped: true, Elapsed: time.Since(started)}
		}

		// 실제 copy
		opts := fsx.CopyOpts{
			NoClobber: p.NoClobber,
			Overwrite: p.Overwrite,
		}
		n, err := fsx.CopyFile(j.Src, j.Dst, opts)
		if errors.Is(err, fsx.ErrSkipExisting) {
			return plan.Result{Job: j, Skipped: true, Elapsed: time.Since(started)}
		}
		if err != nil {
			return plan.Result{Job: j, Err: err, Elapsed: time.Since(started)}
		}

		// 메타데이터 보존
		if p.Preserve.Mode || p.Preserve.Ownership || p.Preserve.Timestamps {
			if metaErr := fsx.PreserveMeta(j.Info, j.Dst, p.Preserve); metaErr != nil {
				return plan.Result{Job: j, Err: metaErr, Bytes: n, Elapsed: time.Since(started)}
			}
		}

		return plan.Result{Job: j, Bytes: n, Elapsed: time.Since(started)}
	}
}
