package fallback

import (
	"context"
	"fmt"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

// Build는 Plan에 따른 worker.Handler를 만든다. 워커 풀이 이 핸들러를 N개 호출한다.
// 각 Job 처리는 자식 프로세스 1회 fork+exec로 처리된다.
func Build(p plan.Plan) worker.Handler {
	return func(ctx context.Context, j plan.Job) plan.Result {
		if p.DryRun {
			return plan.Result{Job: j, Skipped: true}
		}
		bin, args := Translate(p, j)
		if bin == "" {
			return plan.Result{Job: j, Err: fmt.Errorf("fallback: cannot translate job %v", j.Kind)}
		}
		out, err := RunCmd(ctx, bin, args)
		if err != nil {
			return plan.Result{Job: j, Err: err}
		}
		return plan.Result{Job: j, Stdout: out.Stdout}
	}
}
