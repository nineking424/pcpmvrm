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

	jobs := make(chan plan.Job, maxInt(1, p.Workers*4))
	results := make(chan plan.Result, maxInt(1, p.Workers*4))
	pool := worker.NewPool(p.Workers, worker.PCP(p))

	var workerWg sync.WaitGroup
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		pool.Run(sig.Ctx(), jobs, results)
		close(results)
	}()

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

	// 워커 모드 선택
	switch {
	case len(p.StrictExtensions) > 0:
		runStrictExt(sig.Ctx(), p, jobs)
	case p.StrictOrder:
		_ = walk.NewStrictOrder(p).Walk(sig.Ctx(), jobs)
	default:
		_ = walk.NewDefault(p).Walk(sig.Ctx(), jobs)
	}
	close(jobs)

	workerWg.Wait()
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

func runStrictExt(ctx context.Context, p plan.Plan, jobs chan<- plan.Job) {
	w := walk.NewStrictExt(p)
	_ = w.RunPhase1(ctx, jobs)
	// Phase1 모든 워커가 drain되도록 채널을 비워주는 책임은 호출부 worker pool에 있음.
	// 우리는 Phase2를 단순히 같은 채널에 추가로 push한다.
	_ = w.RunPhase2(ctx, jobs)
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
