package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PCP returns a Handler that performs the actual copy work for pcp.
func PCP(p plan.Plan) Handler {
	return func(ctx context.Context, j plan.Job) plan.Result {
		switch j.Kind {
		case plan.JobCopy:
			return pcpCopyOne(p, j)
		case plan.JobDirCopy:
			return pcpDirCopy(ctx, p, j)
		default:
			return plan.Result{Job: j, Err: errors.New("worker/pcp: unexpected job kind")}
		}
	}
}

func pcpCopyOne(p plan.Plan, j plan.Job) plan.Result {
	started := time.Now()

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

	opts := fsx.CopyOpts{NoClobber: p.NoClobber, Overwrite: p.Overwrite}
	n, err := fsx.CopyFile(j.Src, j.Dst, opts)
	if errors.Is(err, fsx.ErrSkipExisting) {
		return plan.Result{Job: j, Skipped: true, Elapsed: time.Since(started)}
	}
	if err != nil {
		return plan.Result{Job: j, Err: err, Elapsed: time.Since(started)}
	}
	if p.Preserve.Mode || p.Preserve.Ownership || p.Preserve.Timestamps {
		if metaErr := fsx.PreserveMeta(j.Info, j.Dst, p.Preserve); metaErr != nil {
			return plan.Result{Job: j, Err: metaErr, Bytes: n, Elapsed: time.Since(started)}
		}
	}
	return plan.Result{Job: j, Bytes: n, Elapsed: time.Since(started)}
}

func pcpDirCopy(ctx context.Context, p plan.Plan, j plan.Job) plan.Result {
	started := time.Now()
	var totalBytes int64
	err := filepath.WalkDir(j.Src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}
		rel, _ := filepath.Rel(j.Src, path)
		if rel == "." {
			return nil
		}
		dst := filepath.Join(j.Dst, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		info, ie := d.Info()
		if ie != nil {
			return ie
		}
		sub := plan.Job{Kind: plan.JobCopy, Src: path, Dst: dst, RelPath: rel, Info: info}
		r := pcpCopyOne(p, sub)
		if r.Err != nil {
			return r.Err
		}
		totalBytes += r.Bytes
		return nil
	})
	if err != nil {
		return plan.Result{Job: j, Err: err, Bytes: totalBytes, Elapsed: time.Since(started)}
	}
	return plan.Result{Job: j, Bytes: totalBytes, Elapsed: time.Since(started)}
}
