// Command pcp is the parallel cp tool. See docs/superpowers/specs for design.
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/fallback"
	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/report"
	"github.com/nineking424/pcpmvrm/internal/walk"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	p, err := cli.ParsePCP(args)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		if !endsWithNewline(err.Error()) {
			fmt.Fprintln(os.Stderr)
		}
		return 2
	}

	sig := report.NewSignal(context.Background())
	sig.Notify(syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig.HardExit()
		os.Exit(130)
	}()

	errLog, err := report.NewErrorLog(p.ErrorLogPath, "pcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pcp: cannot open error log: %v\n", err)
		return 2
	}
	defer errLog.Close()

	prog := report.NewProgress(os.Stderr, "pcp", isTTY(os.Stderr) && !p.NoProgress)
	verb := report.NewVerbose(os.Stdout, p.Verbose)

	// results는 모든 phase가 공유하는 단일 sink. jobs/pool은 phase 단위로 새로 만든다.
	results := make(chan plan.Result, maxInt(1, p.Workers*4))

	progressDone := make(chan struct{})
	go prog.Loop(progressDone, time.Second)

	var consumeWg sync.WaitGroup
	consumeWg.Add(1)
	exitOnError := false
	go func() {
		defer consumeWg.Done()
		for r := range results {
			if r.Err != nil {
				errLog.Record(opName(r.Job.Kind), r.Job.RelPath, r.Err)
				prog.IncErrors()
				verb.Logf("ERR  %s: %s", r.Job.RelPath, r.Err)
				if p.ExitOnError {
					exitOnError = true
					sig.Trigger(syscall.SIGUSR2) // graceful 트리거 (cancel만 일으킴)
				}
				continue
			}
			if r.Skipped {
				prog.IncSkipped()
				verb.Logf("skip %s", r.Job.RelPath)
				continue
			}
			prog.IncFiles()
			prog.AddBytes(r.Bytes)
			verb.Logf("ok   %s", r.Job.RelPath)
		}
	}()

	walkErrCb := func(rel string, e error) {
		errLog.Record("walk", rel, e)
		prog.IncErrors()
		verb.Logf("ERR  %s: %s", rel, e)
	}

	var handler worker.Handler
	if p.Fallback {
		handler = fallback.Build(p)
	} else {
		handler = worker.PCP(p)
	}

	// runPhase는 각 phase마다 jobs 채널 + worker pool을 새로 만든다.
	// pool이 jobs를 다 비우고 종료될 때까지 블록 — phase 간 순서를 강제한다.
	runPhase := func(workers int, walkFn func(context.Context, chan<- plan.Job) error) {
		jobs := make(chan plan.Job, maxInt(1, workers*4))
		pool := worker.NewPool(workers, handler)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Run(sig.Ctx(), jobs, results)
		}()
		_ = walkFn(sig.Ctx(), jobs)
		close(jobs)
		wg.Wait()
	}

	// 워커 모드 선택
	switch {
	case len(p.StrictExtensions) > 0:
		// 스펙 §5.2: phase 1은 N 워커, phase 2는 워커=1로 직렬화. 두 phase는
		// 서로 다른 pool을 사용해야 trigger 순서가 보장된다.
		w := walk.NewStrictExt(p).OnError(walkErrCb)
		runPhase(p.Workers, w.RunPhase1)
		if sig.Ctx().Err() == nil {
			runPhase(1, w.RunPhase2)
		}
	case p.StrictOrder:
		w := walk.NewStrictOrder(p).OnError(walkErrCb)
		runPhase(p.Workers, w.Walk)
	default:
		w := walk.NewDefault(p).OnError(walkErrCb)
		runPhase(p.Workers, w.Walk)
	}

	close(results)
	consumeWg.Wait()
	close(progressDone)

	if sig.Ctx().Err() != nil {
		fmt.Fprintf(os.Stderr, "\nInterrupted: %d files processed, %d skipped, %d errors\n",
			prog.Files(), prog.Skipped(), prog.Errors())
		return 130
	}
	if prog.Errors() > 0 || exitOnError {
		fmt.Fprintf(os.Stderr, "\nCompleted with %d errors. See %s\n", prog.Errors(), errLog.Path())
		return 1
	}
	if !p.NoProgress && isTTY(os.Stderr) {
		fmt.Fprintln(os.Stderr)
	}
	return 0
}

func opName(k plan.JobKind) string {
	switch k {
	case plan.JobCopy:
		return "copy"
	case plan.JobDirCopy:
		return "dir-copy"
	}
	return "?"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

func endsWithNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}
