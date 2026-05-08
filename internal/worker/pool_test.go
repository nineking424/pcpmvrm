package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func TestPool_RunsAllJobsConcurrently(t *testing.T) {
	var processed atomic.Int64
	handler := func(ctx context.Context, j plan.Job) plan.Result {
		processed.Add(1)
		return plan.Result{Job: j}
	}

	jobs := make(chan plan.Job, 16)
	for i := 0; i < 10; i++ {
		jobs <- plan.Job{Kind: plan.JobCopy, Src: "x"}
	}
	close(jobs)

	results := make(chan plan.Result, 16)
	pool := worker.NewPool(4, handler)
	pool.Run(context.Background(), jobs, results)
	close(results)

	got := 0
	for range results {
		got++
	}
	if got != 10 || processed.Load() != 10 {
		t.Fatalf("processed=%d results=%d, want 10/10", processed.Load(), got)
	}
}

func TestPool_StopsOnContextCancel(t *testing.T) {
	handler := func(ctx context.Context, j plan.Job) plan.Result {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return plan.Result{Job: j, Err: ctx.Err()}
		}
		return plan.Result{Job: j}
	}

	jobs := make(chan plan.Job, 100)
	for i := 0; i < 100; i++ {
		jobs <- plan.Job{}
	}
	close(jobs)

	results := make(chan plan.Result, 100)
	pool := worker.NewPool(2, handler)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	pool.Run(ctx, jobs, results)
	close(results)
}

func TestPool_RecoversPanicsAsErrors(t *testing.T) {
	handler := func(ctx context.Context, j plan.Job) plan.Result {
		panic("boom")
	}

	jobs := make(chan plan.Job, 1)
	jobs <- plan.Job{Kind: plan.JobCopy}
	close(jobs)

	results := make(chan plan.Result, 1)
	pool := worker.NewPool(1, handler)
	pool.Run(context.Background(), jobs, results)
	close(results)

	r := <-results
	if r.Err == nil || !errors.Is(r.Err, worker.ErrPanic) {
		t.Fatalf("expected ErrPanic, got %v", r.Err)
	}
}
