# pcpmvrm Plan 3 — `prm` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Plan 1의 foundation과 Plan 2에서 추가한 `JobUnlink`/`JobDirRemove`를 활용해 `prm` CLI를 완성한다. `prm`은 `rm`의 시맨틱을 따르며, 디렉토리 트리는 post-order로 자식부터 unlink하고 자식 처리가 모두 끝난 디렉토리만 rmdir한다. 자식 unlink와 부모 rmdir의 순서는 디렉토리당 `sync.WaitGroup` barrier로 보장한다.

**Architecture:** Walker가 DFS post-order로 트리를 순회하면서, 디렉토리마다 그 자식 파일 수만큼 `wg.Add(N)`을 미리 계산해 두고 `JobUnlink`를 큐에 push한다. 워커는 unlink 완료 시 해당 wg에 `Done()`을 호출한다. Walker는 그 디렉토리의 모든 자식이 끝나기를 `wg.Wait()`하고 `JobDirRemove`를 큐에 push한다. `--strict-order` 모드에서는 디렉토리 단위 Job 한 개로 처리되어 barrier가 자동으로 충족된다.

**Tech Stack:**
- Plan 1과 동일
- 추가: `sync.WaitGroup` barrier, Job 메타데이터 필드 (Job에 `Done func()` 추가)

**Dependencies:** Plan 1, Plan 2 (`JobUnlink`, `JobDirRemove`, `fsx.RemoveFile`, `fsx.RemoveDir`).

---

## File Structure

```
pcpmvrm/
├── cmd/
│   └── prm/main.go                       # prm 진입점 (신규)
├── internal/
│   ├── cli/
│   │   └── prm.go                        # prm 전용 플래그 (-r, -f, -v, -d) (신규)
│   ├── plan/
│   │   └── job.go                        # Job 구조체에 Done 콜백 추가 (수정)
│   ├── worker/
│   │   └── prm.go                        # prm handler — JobUnlink/JobDirRemove (신규)
│   └── walk/
│       └── remove.go                     # post-order walker + WaitGroup barrier (신규)
└── tests/
    └── integration/
        └── prm_test.go                   # end-to-end prm 시나리오 (신규)
```

수정되는 기존 파일:

| 파일 | 변경 내용 |
|---|---|
| `internal/plan/job.go` | `Job` 구조체에 `Done func()` 필드 추가 (nil-safe) |
| `internal/cli/unsupported.go` | prm용 거부 옵션(`-i`, `--one-file-system`, `--preserve-root` non-default 등) |
| `internal/worker/pool.go` | Job.Done이 nil이 아니면 처리 후(성공/실패 무관) 호출 |

각 신규 파일의 책임:

| 파일 | 책임 | 의존하는 것 |
|---|---|---|
| `cli/prm.go` | prm 플래그(`-r`/`-R`, `-f`, `-v`, `-d`) + Plan 변환 | `cli/common.go` |
| `worker/prm.go` | JobUnlink/JobDirRemove 처리 — `-f`(no error on missing), `-v` | `fsx`, `report` |
| `walk/remove.go` | DFS post-order, 자식 unlink Job 큐잉 + 디렉토리 barrier rmdir | `plan`, `sync` |
| `cmd/prm/main.go` | os.Args → cli/prm → walk/worker/report 조립 | 전부 |

---

## Conventions

Plan 1과 동일.

---

## Task 1: `plan/job` 확장 — `Done` 콜백 필드

**Files:**
- Modify: `internal/plan/job.go`
- Test: `internal/plan/job_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestJob_DoneNilSafe(t *testing.T) {
	j := plan.Job{Kind: plan.JobUnlink, Src: "/x"}
	// Done이 nil이어도 panic 없이 호출 가능해야 한다.
	j.Finish()
}

func TestJob_DoneCalled(t *testing.T) {
	called := false
	j := plan.Job{Kind: plan.JobUnlink, Src: "/x", Done: func() { called = true }}
	j.Finish()
	if !called {
		t.Error("Job.Finish should invoke Done")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/plan -run TestJob_Done`
Expected: FAIL — `Done` 필드 / `Finish` 메서드 미정의

- [ ] **Step 3: 구현**

`internal/plan/job.go`의 Job 구조체에 추가:

```go
type Job struct {
	Kind JobKind
	Src  string
	Dst  string
	// Done은 워커가 Job을 처리한 직후 (성공/실패 무관) 호출된다. nil이면 무시.
	// 주로 prm walker가 디렉토리 barrier WaitGroup을 Done() 시키는 데 사용한다.
	Done func()
}

// Finish는 Done이 nil이 아니면 호출한다. 워커 풀이 모든 Job 처리 종료 시점에 호출한다.
func (j Job) Finish() {
	if j.Done != nil {
		j.Done()
	}
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/plan -run TestJob_Done`
Expected: PASS

```bash
git add internal/plan/job.go internal/plan/job_test.go
git commit -m "feat(plan): Job.Done 콜백 + Finish() (prm barrier 용)"
```

---

## Task 2: `worker/pool` — `Job.Finish` 호출 보장

**Files:**
- Modify: `internal/worker/pool.go`
- Test: `internal/worker/pool_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestPool_CallsJobFinish(t *testing.T) {
	finishedCount := 0
	var mu sync.Mutex
	mark := func() { mu.Lock(); finishedCount++; mu.Unlock() }

	jobs := make(chan plan.Job, 4)
	results := make(chan plan.Result, 4)
	jobs <- plan.Job{Kind: plan.JobUnlink, Src: "/a", Done: mark}
	jobs <- plan.Job{Kind: plan.JobUnlink, Src: "/b", Done: mark}
	close(jobs)

	pool := worker.NewPool(2, func(j plan.Job) plan.Result { return plan.Result{Job: j} })
	pool.Run(context.Background(), jobs, results)
	close(results)

	mu.Lock()
	defer mu.Unlock()
	if finishedCount != 2 {
		t.Errorf("Done called %d times, want 2", finishedCount)
	}
}

func TestPool_CallsJobFinishOnPanic(t *testing.T) {
	finished := false
	jobs := make(chan plan.Job, 1)
	results := make(chan plan.Result, 1)
	jobs <- plan.Job{Kind: plan.JobUnlink, Src: "/x", Done: func() { finished = true }}
	close(jobs)

	pool := worker.NewPool(1, func(j plan.Job) plan.Result {
		panic("boom")
	})
	pool.Run(context.Background(), jobs, results)
	close(results)

	if !finished {
		t.Error("Done must be called even when handler panics")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker -run TestPool_CallsJobFinish`
Expected: FAIL — Pool이 Done 호출 안 함

- [ ] **Step 3: 구현**

Plan 1 Task 13에서 정의한 `safeHandle`을 수정해 `defer j.Finish()` 추가:

```go
func safeHandle(h Handler, j plan.Job) (r plan.Result) {
	defer j.Finish() // panic 경로에서도 호출되도록 defer
	defer func() {
		if rec := recover(); rec != nil {
			r = plan.Result{Job: j, Err: fmt.Errorf("%w: %v", ErrPanic, rec)}
		}
	}()
	return h(j)
}
```

defer 순서 주의: `j.Finish()`가 panic recover 다음(즉 더 안쪽)에 실행되어야 한다. 위처럼 `Finish`를 먼저 defer하면 LIFO로 recover가 먼저 실행되고 그 다음 Finish가 실행되어 panic 시점에도 Done이 호출된다.

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/worker -run TestPool_CallsJobFinish`
Expected: PASS

```bash
git add internal/worker/pool.go internal/worker/pool_test.go
git commit -m "feat(worker): pool이 Job.Finish 호출 보장 (panic 경로 포함)"
```

---

## Task 3: `cli/unsupported` — prm 거부 옵션

**Files:**
- Modify: `internal/cli/unsupported.go`
- Test: `internal/cli/unsupported_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestPRMUnsupported_Interactive(t *testing.T) {
	if err := cli.CheckPRMUnsupported([]string{"-i", "/tmp/x"}); err == nil {
		t.Fatal("prm -i should be unsupported")
	}
}

func TestPRMUnsupported_OneFileSystem(t *testing.T) {
	if err := cli.CheckPRMUnsupported([]string{"--one-file-system", "-r", "/tmp/x"}); err == nil {
		t.Fatal("prm --one-file-system should be unsupported")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestPRMUnsupported`
Expected: FAIL

- [ ] **Step 3: 구현**

`internal/cli/unsupported.go`에 추가:

```go
var prmUnsupported = map[string]string{
	"-i":                   "interactive prompt — 병렬과 본질적으로 충돌",
	"-I":                   "interactive once-prompt — 병렬과 본질적으로 충돌",
	"--interactive":        "interactive prompt — 병렬과 본질적으로 충돌",
	"--one-file-system":    "one-file-system 가드",
	"--no-preserve-root":   "preserve-root 끄기 (안전상 거부)",
	"--preserve-root=all":  "preserve-root all 모드",
}

// CheckPRMUnsupported는 prm이 native에서 거부하는 옵션을 검사한다.
func CheckPRMUnsupported(args []string) error {
	return checkUnsupported("prm", args, prmUnsupported)
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/cli -run TestPRMUnsupported`
Expected: PASS

```bash
git add internal/cli/unsupported.go internal/cli/unsupported_test.go
git commit -m "feat(cli): prm 거부 옵션 (-i, --one-file-system, --no-preserve-root)"
```

---

## Task 4: `cli/prm` — prm 전용 플래그 파서

**Files:**
- Create: `internal/cli/prm.go`
- Test: `internal/cli/prm_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package cli_test

import (
	"reflect"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestParsePRM_Minimum(t *testing.T) {
	p, err := cli.ParsePRM([]string{"/tmp/x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := plan.Plan{Op: plan.OpRemove, Src: "/tmp/x", Workers: 1}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan=%+v want %+v", p, want)
	}
}

func TestParsePRM_AllSupportedFlags(t *testing.T) {
	args := []string{
		"-rf", "-v", "-d",
		"--parallel=4",
		"--exit-on-error",
		"--dry-run",
		"/tmp/x",
	}
	p, err := cli.ParsePRM(args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := plan.Plan{
		Op: plan.OpRemove, Src: "/tmp/x", Workers: 4,
		Recursive: true, ForceMissing: true, Verbose: true, RemoveEmptyDir: true,
		ExitOnError: true, DryRun: true,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan=%+v\nwant %+v", p, want)
	}
}

func TestParsePRM_RejectsExtraArgs(t *testing.T) {
	if _, err := cli.ParsePRM([]string{"/a", "/b"}); err == nil {
		t.Fatal("prm should accept exactly one PATH")
	}
}

func TestParsePRM_RejectsRecursiveOnFile(t *testing.T) {
	// 이는 사전 검증 단계 — parser는 통과시키고 main.go가 lstat 후 거부.
	// 여기서는 단순히 -r과 PATH 둘 다 받아서 통과해야 함.
	if _, err := cli.ParsePRM([]string{"-r", "/tmp/x"}); err != nil {
		t.Errorf("parser should accept -r + PATH: %v", err)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestParsePRM`
Expected: FAIL — `ParsePRM` 미정의 / `Plan.ForceMissing`, `Plan.RemoveEmptyDir` 미정의

- [ ] **Step 3: `Plan`에 두 필드 추가**

`internal/plan/plan.go`에 추가:

```go
ForceMissing   bool // prm -f: 존재하지 않는 파일에 대해 에러 안 냄
RemoveEmptyDir bool // prm -d: 빈 디렉토리도 삭제 (regular file 외에 dir도 unlink-equivalent)
```

- [ ] **Step 4: `cli/prm.go` 구현**

```go
package cli

import (
	"fmt"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/spf13/pflag"
)

// ParsePRM은 prm args를 Plan으로 변환한다.
func ParsePRM(args []string) (plan.Plan, error) {
	if err := CheckPRMUnsupported(args); err != nil {
		return plan.Plan{}, err
	}

	fs := pflag.NewFlagSet("prm", pflag.ContinueOnError)
	fs.SortFlags = false

	var (
		c          Common
		recursive  bool
		recursive2 bool
		forceMiss  bool
		verbose    bool
		emptyDir   bool
	)
	BindCommon(fs, &c)
	fs.BoolVarP(&recursive, "recursive", "r", false, "recursively remove directories")
	fs.BoolVarP(&recursive2, "RECURSIVE", "R", false, "alias for -r")
	fs.BoolVarP(&forceMiss, "force", "f", false, "no error on missing files")
	fs.BoolVarP(&verbose, "verbose", "v", false, "print each removal")
	fs.BoolVarP(&emptyDir, "dir", "d", false, "remove empty directories")

	if err := fs.Parse(args); err != nil {
		return plan.Plan{}, fmt.Errorf("prm: %w", err)
	}
	if err := c.Normalize(); err != nil {
		return plan.Plan{}, err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return plan.Plan{}, fmt.Errorf("prm: exactly one PATH required (got %d args)", len(rest))
	}

	return plan.Plan{
		Op:               plan.OpRemove,
		Src:              rest[0],
		Workers:          c.Workers,
		Recursive:        recursive || recursive2,
		ForceMissing:     forceMiss,
		Verbose:          verbose,
		RemoveEmptyDir:   emptyDir,
		DryRun:           c.DryRun,
		ExitOnError:      c.ExitOnError,
		ErrorLogPath:     c.ErrorLogPath,
		NoProgress:       c.NoProgress,
		StrictOrder:      c.StrictOrder,
		StrictExtensions: c.StrictExtensions,
	}, nil
}
```

- [ ] **Step 5: 통과 + 커밋**

Run: `go test ./internal/cli -run TestParsePRM`
Expected: PASS

```bash
git add internal/cli/prm.go internal/cli/prm_test.go internal/plan/plan.go
git commit -m "feat(cli): prm 플래그 파서 (-r/-R, -f, -v, -d) + Plan 필드"
```

---

## Task 5: `worker/prm` — prm 핸들러

**Files:**
- Create: `internal/worker/prm.go`
- Test: `internal/worker/prm_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package worker_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func TestPRMHandler_UnlinkExisting(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, []byte("z"), 0644)

	h := worker.PRM(plan.Plan{Op: plan.OpRemove})
	r := h(plan.Job{Kind: plan.JobUnlink, Src: f})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should be gone: %v", err)
	}
}

func TestPRMHandler_MissingFileWithoutForce(t *testing.T) {
	h := worker.PRM(plan.Plan{Op: plan.OpRemove})
	r := h(plan.Job{Kind: plan.JobUnlink, Src: "/no/such/file"})
	if r.Err == nil {
		t.Fatal("missing file without -f should error")
	}
}

func TestPRMHandler_MissingFileWithForce(t *testing.T) {
	h := worker.PRM(plan.Plan{Op: plan.OpRemove, ForceMissing: true})
	r := h(plan.Job{Kind: plan.JobUnlink, Src: "/no/such/file"})
	if r.Err != nil {
		t.Errorf("missing file with -f should be silent, got: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("missing file with -f should be Skipped")
	}
}

func TestPRMHandler_DryRunNoIO(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, []byte("z"), 0644)

	h := worker.PRM(plan.Plan{Op: plan.OpRemove, DryRun: true})
	r := h(plan.Job{Kind: plan.JobUnlink, Src: f})
	if r.Err != nil || !r.Skipped {
		t.Fatalf("dry-run: err=%v skipped=%v", r.Err, r.Skipped)
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("dry-run must not unlink: %v", err)
	}
}

func TestPRMHandler_DirRemove(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "empty")
	os.MkdirAll(d, 0755)
	h := worker.PRM(plan.Plan{Op: plan.OpRemove})
	r := h(plan.Job{Kind: plan.JobDirRemove, Src: d})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir should be gone")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker -run TestPRMHandler`
Expected: FAIL — `worker.PRM` 미정의

- [ ] **Step 3: 구현**

Create `internal/worker/prm.go`:

```go
package worker

import (
	"errors"
	"fmt"
	"os"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PRM은 prm용 Job 핸들러를 만든다.
func PRM(p plan.Plan) Handler {
	return func(j plan.Job) plan.Result {
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
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/worker -run TestPRMHandler`
Expected: PASS

```bash
git add internal/worker/prm.go internal/worker/prm_test.go
git commit -m "feat(worker): prm 핸들러 (JobUnlink/JobDirRemove + ForceMissing)"
```

---

## Task 6: `walk/remove` — post-order walker + WaitGroup barrier

**Files:**
- Create: `internal/walk/remove.go`
- Test: `internal/walk/remove_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package walk_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/walk"
)

func TestRemoveWalker_FileOnly(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	os.WriteFile(f, []byte("x"), 0644)

	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: f})
	jobs := make(chan plan.Job, 4)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()
	got := drainJobs(jobs)

	if len(got) != 1 || got[0].Kind != plan.JobUnlink {
		t.Fatalf("single file should emit one JobUnlink, got: %+v", got)
	}
}

func TestRemoveWalker_DirRequiresRecursive(t *testing.T) {
	dir := t.TempDir()
	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: dir, Recursive: false})
	jobs := make(chan plan.Job, 1)
	close(jobs) // walk가 즉시 에러 반환해야 함
	err := w.Walk(context.Background(), jobs)
	if err == nil {
		t.Fatal("removing directory without -r should fail")
	}
}

func TestRemoveWalker_DirEmpty_DOption(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "empty")
	os.MkdirAll(d, 0755)

	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: d, RemoveEmptyDir: true})
	jobs := make(chan plan.Job, 4)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()
	got := drainJobs(jobs)
	if len(got) != 1 || got[0].Kind != plan.JobDirRemove {
		t.Fatalf("empty dir + -d should emit one JobDirRemove, got: %+v", got)
	}
}

func TestRemoveWalker_RecursiveBarrier(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	os.MkdirAll(filepath.Join(root, "a", "b"), 0755)
	os.WriteFile(filepath.Join(root, "a", "f1"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(root, "a", "b", "f2"), []byte("x"), 0644)

	w := walk.NewRemove(plan.Plan{Op: plan.OpRemove, Src: root, Recursive: true})

	jobs := make(chan plan.Job, 32)
	done := make(chan struct{})
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs); close(done) }()

	// 병렬 워커 흉내: jobs 받아서 즉시 Finish 호출 (실제 unlink는 안 함)
	go func() {
		for j := range jobs {
			j.Finish()
		}
	}()

	<-done

	// 주된 검증은 walker가 deadlock 없이 완료되는 것 — barrier가 제대로 풀려야 함.
}
```

`drainJobs`는 Plan 1 walk 테스트 헬퍼.

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/walk -run TestRemoveWalker`
Expected: FAIL — `walk.NewRemove` 미정의

- [ ] **Step 3: 구현**

Create `internal/walk/remove.go`:

```go
package walk

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Remove는 prm용 walker. 디렉토리는 post-order로 처리하며,
// 각 디렉토리의 자식 unlink가 모두 끝난 뒤에만 JobDirRemove를 emit한다.
type Remove struct{ p plan.Plan }

// NewRemove는 Remove walker를 만든다.
func NewRemove(p plan.Plan) *Remove { return &Remove{p: p} }

// Walk는 src 트리를 순회하며 Job을 emit한다.
func (w *Remove) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	fi, err := os.Lstat(w.p.Src)
	if err != nil {
		if os.IsNotExist(err) && w.p.ForceMissing {
			return nil
		}
		return err
	}

	if !fi.IsDir() {
		return pushJob(ctx, jobs, plan.Job{Kind: plan.JobUnlink, Src: w.p.Src})
	}

	if !w.p.Recursive {
		if w.p.RemoveEmptyDir {
			// -d: 빈 디렉토리만 허용. 비어있는지 확인은 worker에 맡김 (rmdir이 ENOTEMPTY 반환).
			return pushJob(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: w.p.Src})
		}
		return fmt.Errorf("prm: %s is a directory (use -r or -d)", w.p.Src)
	}

	return w.walkDir(ctx, jobs, w.p.Src)
}

// walkDir은 한 디렉토리에 대해:
//   1. 자식 entry 모두 열거
//   2. 자식 디렉토리는 재귀 호출 (post-order)
//   3. 자식 파일은 JobUnlink emit
//   4. 자식 수만큼 wg.Add — Job.Done에 wg.Done 콜백 부여
//   5. 모든 자식 emit이 끝나면 wg.Wait — 그 다음 본인의 JobDirRemove emit
func (w *Remove) walkDir(ctx context.Context, jobs chan<- plan.Job, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := w.walkDir(ctx, jobs, path); err != nil {
				return err
			}
			continue
		}
		wg.Add(1)
		j := plan.Job{
			Kind: plan.JobUnlink,
			Src:  path,
			Done: wg.Done,
		}
		if err := pushJob(ctx, jobs, j); err != nil {
			wg.Done() // pushJob 실패 시 Add 보상
			return err
		}
	}

	// 자식 처리 완료를 기다림 — 그 사이 워커들이 unlink 진행
	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh:
	}

	return pushJob(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: dir})
}

// 빈 dir 체크는 worker에서 ENOTEMPTY 반환되므로 walker가 굳이 strictness 강제 안 함.
// 단, 자식 디렉토리들도 walkDir로 들어가면서 그 안의 JobDirRemove가 wg에 묶이지 않으므로
// 부모는 자식 디렉토리의 rmdir 결과까지 기다리지는 못한다 — 그러나 부모 emit은 자식 walk가
// 완료된 뒤에야 일어나므로(go-call 직렬), 큐에 들어간 순서가 post-order로 보장됨. 워커는
// 다수지만 큐 push 순서가 부모 < 자식이 아니라 자식 < 부모이며, 부모 JobDirRemove는 자식
// JobUnlink가 wg를 통해 다 끝난 다음에야 push되므로 race-free.
//
// 자식 디렉토리의 JobDirRemove는 부모 wg에 잡히지 않는다. 따라서 자식 디렉토리 rmdir이
// 부모 디렉토리 rmdir보다 먼저 push되지만 워커에서는 동시에 처리될 수 있다. 그래도 큐 내
// FIFO 순서가 자식 < 부모이고, 부모 rmdir이 호출되는 시점에 자식 디렉토리는 이미 rmdir
// 큐잉되었거나 처리 중이다. 부모가 rmdir을 시도할 때 자식 디렉토리가 아직 살아 있을 수
// 있으므로 ENOTEMPTY 가능. 이를 막으려면 자식 dir의 JobDirRemove도 부모 wg에 잡아야 함.

// pushJob은 ctx 취소를 존중하며 단일 Job을 push.
func pushJob(ctx context.Context, jobs chan<- plan.Job, j plan.Job) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case jobs <- j:
		return nil
	}
}
```

위 주석에서 식별한 race(자식 dir rmdir과 부모 dir rmdir 순서)를 해결하려면 자식 dir의 JobDirRemove도 부모 wg에 묶어야 한다. walkDir 시그니처를 수정:

```go
// walkDir은 부모의 wg(parent *sync.WaitGroup)에 자기 자신의 rmdir 완료까지 잡힌다.
// 호출자가 wg.Add(1)을 미리 해 두고, walkDir 내부에서 자기 JobDirRemove의 Done에 parent.Done을 wrap한다.
func (w *Remove) walkDir(ctx context.Context, jobs chan<- plan.Job, dir string, parent *sync.WaitGroup) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if parent != nil {
			parent.Done()
		}
		return err
	}

	var local sync.WaitGroup
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			local.Add(1)
			if err := w.walkDir(ctx, jobs, path, &local); err != nil {
				return err
			}
			continue
		}
		local.Add(1)
		if err := pushJob(ctx, jobs, plan.Job{Kind: plan.JobUnlink, Src: path, Done: local.Done}); err != nil {
			local.Done()
			return err
		}
	}

	// 자식 unlink + 자식 디렉토리 rmdir이 모두 끝나길 기다린다.
	waitCh := make(chan struct{})
	go func() { local.Wait(); close(waitCh) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh:
	}

	// 자기 자신의 rmdir Job. parent wg가 있다면 그것의 Done까지 호출해야 한다.
	doneFn := func() {}
	if parent != nil {
		doneFn = parent.Done
	}
	return pushJob(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: dir, Done: doneFn})
}
```

`Walk`의 호출부도 변경:

```go
return w.walkDir(ctx, jobs, w.p.Src, nil)
```

이로써 자식 dir rmdir이 끝나야 부모 dir rmdir이 큐에 push되어 ENOTEMPTY가 발생하지 않는다.

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/walk -run TestRemoveWalker`
Expected: PASS

```bash
git add internal/walk/remove.go internal/walk/remove_test.go
git commit -m "feat(walk): prm post-order walker (자식 → 부모 barrier via WaitGroup)"
```

---

## Task 7: `cmd/prm/main.go` — prm 진입점

**Files:**
- Create: `cmd/prm/main.go`

- [ ] **Step 1: 진입점 작성**

Create `cmd/prm/main.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"

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
	p, err := cli.ParsePRM(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// 사전 검증: src 존재 (ForceMissing이면 무시).
	if _, err := os.Lstat(p.Src); err != nil {
		if os.IsNotExist(err) && p.ForceMissing {
			return 0
		}
		fmt.Fprintf(os.Stderr, "prm: %v\n", err)
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := report.NewSignal(cancel)
	sig.Notify(syscall.SIGINT, syscall.SIGTERM)

	errLog, err := report.NewErrorLog(p.ErrorLogPath, "prm")
	if err != nil {
		fmt.Fprintf(os.Stderr, "prm: error log: %v\n", err)
		return 2
	}
	defer errLog.Close()

	prog := report.NewProgress("prm", os.Stderr, p.NoProgress)
	defer prog.Stop()
	verb := report.NewVerbose(os.Stdout, p.Verbose)

	jobs := make(chan plan.Job, p.Workers*4)
	results := make(chan plan.Result, p.Workers*4)

	pool := worker.NewPool(p.Workers, worker.PRM(p))
	var poolWG sync.WaitGroup
	poolWG.Add(1)
	go func() {
		defer poolWG.Done()
		pool.Run(ctx, jobs, results)
	}()

	walker := walk.NewRemove(p)
	var walkWG sync.WaitGroup
	walkWG.Add(1)
	var walkErr error
	go func() {
		defer walkWG.Done()
		walkErr = walker.Walk(ctx, jobs)
		close(jobs)
	}()

	consumed := make(chan struct{})
	var nFail int
	go func() {
		defer close(consumed)
		for r := range results {
			if r.Skipped {
				continue
			}
			if r.Err != nil {
				nFail++
				errLog.Log(r.Job, r.Err)
				if p.ExitOnError {
					sig.Trigger(syscall.SIGUSR2)
				}
				continue
			}
			prog.Tick(1, 0)
			verb.Print(r.Job)
		}
	}()

	walkWG.Wait()
	poolWG.Wait()
	close(results)
	<-consumed

	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		fmt.Fprintf(os.Stderr, "prm: walk: %v\n", walkErr)
		nFail++
	}

	if sig.Triggered() || sig.Forced() {
		return 130
	}
	if nFail > 0 {
		return 1
	}
	return 0
}
```

- [ ] **Step 2: 빌드 검증**

```bash
go build ./cmd/prm
```
Expected: 컴파일 에러 없음

- [ ] **Step 3: smoke test**

```bash
mkdir -p /tmp/prm-smoke/a/b
echo x > /tmp/prm-smoke/a/f1
echo y > /tmp/prm-smoke/a/b/f2
./prm -r --parallel=4 /tmp/prm-smoke/
test ! -d /tmp/prm-smoke
```
Expected: 트리 전체 사라짐, exit 0

- [ ] **Step 4: 커밋**

```bash
git add cmd/prm/main.go
git commit -m "feat(prm): 진입점 — walk/pool/report 조립"
```

---

## Task 8: 통합 테스트 — prm 시나리오

**Files:**
- Create: `tests/integration/prm_test.go`

- [ ] **Step 1: 테스트**

```go
//go:build integration

package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPRM_RecursiveTree(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	mustWrite(t, filepath.Join(target, "a/f1"), "x")
	mustWrite(t, filepath.Join(target, "a/b/f2"), "y")
	mustWrite(t, filepath.Join(target, "c/f3"), "z")

	bin := buildPRM(t)
	out, err := exec.Command(bin, "-r", "--parallel=4", target).CombinedOutput()
	if err != nil {
		t.Fatalf("prm: %v\n%s", err, out)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target should be gone: %v", err)
	}
}

func TestPRM_MissingFileWithForce(t *testing.T) {
	bin := buildPRM(t)
	out, err := exec.Command(bin, "-f", "/no/such/file").CombinedOutput()
	if err != nil {
		t.Fatalf("prm -f on missing should exit 0, got: %v\n%s", err, out)
	}
}

func TestPRM_DirWithoutRecursive(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "x")
	mustWrite(t, filepath.Join(target, "f"), "x")

	bin := buildPRM(t)
	cmd := exec.Command(bin, target)
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() == 0 {
		t.Fatal("prm without -r on a directory must fail")
	}
	if !bytes.Contains(out, []byte("is a directory")) {
		t.Errorf("expected 'is a directory' in stderr, got: %s", out)
	}
}

func buildPRM(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "prm")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/prm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build prm: %v\n%s", err, out)
	}
	return bin
}
```

- [ ] **Step 2: 실행**

```bash
go test -tags integration ./tests/integration -run TestPRM
```
Expected: PASS

- [ ] **Step 3: 커밋**

```bash
git add tests/integration/prm_test.go
git commit -m "test(integration): prm 재귀 / -f 누락 / -r 없이 디렉토리 거부"
```

---

## Task 9: README + Plan 상태 업데이트

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 상태 갱신**

```markdown
## 상태 (2026-05-08)

- ✅ Plan 1: Foundation + `pcp`
- ✅ Plan 2: `pmv`
- ✅ Plan 3: `prm` (현재)
- ⏳ Plan 4: `--fallback` 모드
```

사용 예시에 추가:

```markdown
# 대량 삭제, 첫 에러에서 중단
prm -rf --parallel=16 --exit-on-error /var/cache/old/

# 빈 디렉토리만 (-d)
prm -d /tmp/maybe-empty/
```

- [ ] **Step 2: 커밋**

```bash
git add README.md
git commit -m "docs: prm 추가 (Plan 3 완료)"
```

---

## 마무리 검증

- [ ] **전체 테스트**

```bash
go test -race ./...
go test -tags integration ./tests/integration
```
Expected: PASS

- [ ] **빌드**

```bash
go build ./cmd/...
```
Expected: pcp, pmv, prm 세 바이너리 생성

- [ ] **prm 트리 smoke**

```bash
mkdir -p /tmp/prm-smoke/{a/b,c/d}
touch /tmp/prm-smoke/a/f1 /tmp/prm-smoke/a/b/f2 /tmp/prm-smoke/c/d/f3
./prm -rv --parallel=4 /tmp/prm-smoke
test ! -d /tmp/prm-smoke
```
Expected: 트리 사라짐 + verbose 출력

---

## Plan 3 완료 시 산출물

- 동작하는 `prm` 바이너리
- post-order DFS + WaitGroup barrier (자식 unlink 완료 후 부모 rmdir, 자식 dir rmdir 완료 후 부모 dir rmdir)
- 핵심 옵션: `-r/-R`, `-f`(no-error-on-missing), `-v`, `-d`, `--parallel`, `--exit-on-error`, `--dry-run`, `--error-log`
- 거부 옵션: `-i`, `--one-file-system`, `--no-preserve-root`
- pcp/pmv와 동일한 streaming + 진행 표시 + 에러 로그 + graceful 시그널

## 다음 plan에서 추가될 것

- **Plan 4 (--fallback)**: 자식 프로세스 wrapper. T4/T5 미지원 옵션을 `/bin/cp`, `/bin/mv`, `/bin/rm`에 위임. 셋 다 동일한 fallback 패키지를 공유
