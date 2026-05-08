// Command pmv is the parallel mv tool. See docs/superpowers/specs for design.
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
	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/report"
	"github.com/nineking424/pcpmvrm/internal/walk"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	p, err := cli.ParsePMV(args)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		if !endsWithNewline(err.Error()) {
			fmt.Fprintln(os.Stderr)
		}
		return 2
	}

	// Pre-checks before allocating signal/log/progress resources.
	if _, err := os.Lstat(p.Src); err != nil {
		fmt.Fprintf(os.Stderr, "pmv: %v\n", err)
		return 2
	}

	// SameDevice는 dst가 없으면 부모 디렉토리로 fallback해서 비교한다(fsx.SameDevice 내부 처리).
	same, err := fsx.SameDevice(p.Src, p.Dst)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pmv: device check failed: %v\n", err)
		return 2
	}
	p.SameDevice = same
	if same && p.Workers > 1 {
		fmt.Fprintln(os.Stderr,
			"pmv: same-device move detected — downgrading --parallel to 1 (rename(2) is atomic and benefits little from parallelism)")
		p.Workers = 1
	}

	sig := report.NewSignal(context.Background())
	sig.Notify(syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig.HardExit()
		os.Exit(130)
	}()

	errLog, err := report.NewErrorLog(p.ErrorLogPath, "pmv")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pmv: cannot open error log: %v\n", err)
		return 2
	}
	defer errLog.Close()

	prog := report.NewProgress(os.Stderr, "pmv", isTTY(os.Stderr) && !p.NoProgress)
	verb := report.NewVerbose(os.Stdout, p.Verbose)

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

	var handler worker.Handler
	if p.Fallback {
		handler = fallback.Build(p)
	} else {
		handler = worker.PMV(p)
	}

	jobs := make(chan plan.Job, maxInt(1, p.Workers*4))
	pool := worker.NewPool(p.Workers, handler)
	var poolWg sync.WaitGroup
	poolWg.Add(1)
	go func() {
		defer poolWg.Done()
		pool.Run(sig.Ctx(), jobs, results)
	}()

	w := walk.NewMove(p)
	_ = w.Walk(sig.Ctx(), jobs)
	close(jobs)
	poolWg.Wait()

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
	case plan.JobUnlink:
		return "unlink"
	case plan.JobRename:
		return "rename"
	case plan.JobDirRemove:
		return "dir-remove"
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
