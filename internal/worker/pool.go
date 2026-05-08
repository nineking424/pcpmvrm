// Package worker hosts the worker-pool plumbing and per-tool job handlers.
package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Handler processes one Job and returns a Result.
type Handler func(ctx context.Context, j plan.Job) plan.Result

// ErrPanic wraps a recovered panic value so callers can detect it via errors.Is.
var ErrPanic = errors.New("worker panic")

// Pool fans out jobs to N goroutines, each calling the handler.
type Pool struct {
	n      int
	handle Handler
}

// NewPool builds a pool with n workers.
func NewPool(n int, h Handler) *Pool {
	if n < 1 {
		n = 1
	}
	return &Pool{n: n, handle: h}
}

// Run consumes jobs and writes results until either jobs is closed (and drained)
// or ctx is cancelled. Run does NOT close results — caller does.
func (p *Pool) Run(ctx context.Context, jobs <-chan plan.Job, results chan<- plan.Result) {
	var wg sync.WaitGroup
	for i := 0; i < p.n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.workerLoop(ctx, jobs, results)
		}()
	}
	wg.Wait()
}

func (p *Pool) workerLoop(ctx context.Context, jobs <-chan plan.Job, results chan<- plan.Result) {
	for {
		select {
		case <-ctx.Done():
			return
		case j, ok := <-jobs:
			if !ok {
				return
			}
			r := p.safeHandle(ctx, j)
			select {
			case results <- r:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (p *Pool) safeHandle(ctx context.Context, j plan.Job) (r plan.Result) {
	defer func() {
		if rec := recover(); rec != nil {
			r = plan.Result{Job: j, Err: fmt.Errorf("%w: %v", ErrPanic, rec)}
		}
	}()
	return p.handle(ctx, j)
}
