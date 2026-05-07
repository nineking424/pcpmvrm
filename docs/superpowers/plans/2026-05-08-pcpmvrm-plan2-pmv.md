# pcpmvrm Plan 2 — `pmv` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Plan 1의 foundation(`plan`, `cli`, `fsx`, `report`, `worker`, `walk`) 위에 `pmv` CLI를 구축한다. `pmv`는 `mv`의 시맨틱을 따르며, same-device일 때는 `rename(2)` 한 번으로 처리하고 cross-device일 때는 copy + unlink 흐름으로 폴백한다. 워크 도중 `EXDEV`가 나오는 per-job cross-device도 동일하게 폴백한다.

**Architecture:** pcp의 Walker/Worker/Reporter 골격을 그대로 재사용한다. 차이점은 (1) 사전 검증 단계에서 `stat.Dev`를 비교해 same-device면 워커=1로 강제 다운그레이드 + 안내 메시지, (2) Job kind에 `Rename`을 추가해 same-device면 워커가 `os.Rename`을 호출하고, cross-device면 기존 pcp copy 흐름 + 추가 `os.Remove(src)` 후처리, (3) cross-device 디렉토리 이동의 경우 Walker가 DFS 두 번(pre-order로 mkdir + 자식 큐잉, post-order로 빈 src 디렉토리 rmdir) 수행한다.

**Tech Stack:**
- Plan 1과 동일 (Go 1.22+, `golang.org/x/sys/unix`, `github.com/spf13/pflag`, 표준 `testing`)
- 추가: `syscall.EXDEV` 감지 (`errors.Is`)

**Dependencies:** Plan 1이 완료되어 있어야 한다. 이 plan은 Plan 1의 패키지를 import하고 새 파일을 추가하며, 일부 기존 파일에 작은 분기를 더한다.

---

## File Structure

```
pcpmvrm/
├── cmd/
│   └── pmv/main.go                       # pmv 진입점 (신규)
├── internal/
│   ├── cli/
│   │   └── pmv.go                        # pmv 전용 플래그 + Plan 변환 (신규)
│   ├── fsx/
│   │   └── move.go                       # same-device rename + EXDEV 폴백 (신규)
│   ├── worker/
│   │   └── pmv.go                        # pmv handler (신규)
│   └── walk/
│       └── move.go                       # cross-device 이동용 walker (pre+post-order) (신규)
└── tests/
    └── integration/
        └── pmv_test.go                   # end-to-end pmv 시나리오 (신규)
```

수정되는 기존 파일:

| 파일 | 변경 내용 |
|---|---|
| `internal/plan/plan.go` | `Op` 상수에 `OpMove` 추가, `Plan.SameDevice bool` 필드 추가 |
| `internal/plan/job.go` | `JobKind` 에 `JobRename` 추가 |
| `internal/cli/unsupported.go` | pmv용 거부 옵션 매핑 추가 (`--backup`, `-b`, `--no-target-directory` 등) |

각 신규 파일의 책임:

| 파일 | 책임 | 의존하는 것 |
|---|---|---|
| `cli/pmv.go` | pmv 플래그(`-f`, `-v`, `-n`, `-u`) + Plan 변환 | `cli/common.go`, `pflag` |
| `fsx/move.go` | same-device 판정, `os.Rename` wrapper, `EXDEV` 폴백 | `fsx/device.go`, `fsx/copy.go`, `unix` |
| `worker/pmv.go` | Rename Job 처리 — same면 rename, cross면 copy+unlink | `fsx`, `report` |
| `walk/move.go` | cross-device 시 pre-order mkdir + 파일 Job, post-order src rmdir | `plan`, `fsx` |
| `cmd/pmv/main.go` | os.Args → cli/pmv → 사전 검증 → walk/worker → report | 전부 |

---

## Conventions

Plan 1과 동일. TDD, commit prefix(`feat:`/`test:`/`fix:`/`docs:`), 같은 디렉토리 `*_test.go`, 한 줄 영문 doc comment.

---

## Task 1: `plan` 패키지 확장 — `OpMove`, `JobRename`

**Files:**
- Modify: `internal/plan/plan.go`
- Modify: `internal/plan/job.go`
- Test: `internal/plan/plan_test.go`, `internal/plan/job_test.go`

- [ ] **Step 1: 실패하는 테스트 추가 — `OpMove` Validate 통과**

`internal/plan/plan_test.go`에 케이스 추가:

```go
func TestPlan_Validate_OpMove(t *testing.T) {
	p := plan.Plan{Op: plan.OpMove, Src: "/a", Dst: "/b", Workers: 1}
	if err := p.Validate(); err != nil {
		t.Fatalf("OpMove plan should validate, got: %v", err)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/plan -run TestPlan_Validate_OpMove`
Expected: FAIL — `OpMove` undefined

- [ ] **Step 3: 최소 구현 — `OpMove` 상수 + `SameDevice` 필드 추가**

`internal/plan/plan.go`에 `OpCopy` 옆에 추가:

```go
const (
	OpCopy   Op = "copy"
	OpMove   Op = "move"
	OpRemove Op = "remove"
)
```

`Plan` 구조체에 추가:

```go
// SameDevice는 pmv에서 사전 stat 결과 same-device일 때 true.
// true이면 워커=1로 다운그레이드된 상태이며, JobRename이 사용된다.
SameDevice bool
```

`Validate()`의 op 화이트리스트에 `OpMove`/`OpRemove` 포함되도록 수정.

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/plan`
Expected: PASS

- [ ] **Step 5: `JobRename` 추가 — 실패 테스트**

`internal/plan/job_test.go`에 추가:

```go
func TestJobKind_RenameString(t *testing.T) {
	if got := plan.JobRename.String(); got != "rename" {
		t.Errorf("JobRename.String()=%q, want %q", got, "rename")
	}
}
```

Run: `go test ./internal/plan -run TestJobKind_RenameString`
Expected: FAIL

- [ ] **Step 6: `JobRename` 상수 + String 매핑 추가**

`internal/plan/job.go`의 `JobKind` 상수 블록에 추가:

```go
const (
	JobCopy      JobKind = iota // 파일 복사
	JobDirCopy                  // strict-order 모드의 디렉토리 단위 복사
	JobRename                   // pmv same-device: os.Rename 한 번
	JobUnlink                   // prm/pmv cross-device 후처리: 파일 삭제
	JobDirRemove                // prm: 자식 처리 완료 후 빈 디렉토리 rmdir
)
```

`String()` switch에도 매핑 추가.

- [ ] **Step 7: 통과 확인 + 커밋**

Run: `go test ./internal/plan`
Expected: PASS

```bash
git add internal/plan/
git commit -m "feat(plan): OpMove, JobRename/JobUnlink/JobDirRemove, SameDevice 플래그"
```

---

## Task 2: `cli/unsupported` — pmv 거부 옵션 추가

**Files:**
- Modify: `internal/cli/unsupported.go`
- Test: `internal/cli/unsupported_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestPMVUnsupported_BackupFlag(t *testing.T) {
	args := []string{"-b", "src", "dst"}
	err := cli.CheckPMVUnsupported(args)
	if err == nil {
		t.Fatal("pmv -b should be unsupported")
	}
	if !strings.Contains(err.Error(), "--fallback") {
		t.Errorf("error should mention --fallback, got: %v", err)
	}
}

func TestPMVUnsupported_TargetDirectory(t *testing.T) {
	args := []string{"--no-target-directory", "src", "dst"}
	if err := cli.CheckPMVUnsupported(args); err == nil {
		t.Fatal("pmv --no-target-directory should be unsupported")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestPMVUnsupported`
Expected: FAIL — `CheckPMVUnsupported` 미정의

- [ ] **Step 3: 구현 — pmv 거부 목록**

`internal/cli/unsupported.go`에 추가:

```go
// pmvUnsupported는 pmv가 native 모드에서 거부하는 옵션 → 안내 키워드 매핑.
var pmvUnsupported = map[string]string{
	"-b":                     "백업 파일 생성",
	"--backup":               "백업 파일 생성",
	"--no-target-directory":  "대상 디렉토리 강제 해제",
	"-T":                     "대상 디렉토리 강제 해제",
	"--strip-trailing-slashes": "trailing slash 제거",
	"-Z":                     "SELinux 컨텍스트 설정",
	"--context":              "SELinux 컨텍스트 설정",
	"-i":                     "interactive prompt — 병렬과 본질적으로 충돌",
	"--interactive":          "interactive prompt — 병렬과 본질적으로 충돌",
}

// CheckPMVUnsupported는 args에 pmv가 거부하는 옵션이 있는지 확인하고,
// 발견 시 안내 메시지를 담은 에러를 반환한다.
func CheckPMVUnsupported(args []string) error {
	return checkUnsupported("pmv", args, pmvUnsupported)
}
```

기존 `checkUnsupported` 헬퍼는 Plan 1 Task 3에서 정의되었으므로 재사용.

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/cli`
Expected: PASS

```bash
git add internal/cli/unsupported.go internal/cli/unsupported_test.go
git commit -m "feat(cli): pmv 거부 옵션 검증 (--backup, -T, -i 등)"
```

---

## Task 3: `cli/pmv` — pmv 전용 플래그 파서

**Files:**
- Create: `internal/cli/pmv.go`
- Test: `internal/cli/pmv_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package cli_test

import (
	"reflect"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestParsePMV_Minimum(t *testing.T) {
	args := []string{"src", "dst"}
	p, err := cli.ParsePMV(args)
	if err != nil {
		t.Fatalf("ParsePMV err: %v", err)
	}
	want := plan.Plan{Op: plan.OpMove, Src: "src", Dst: "dst", Workers: 1}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan=%+v, want %+v", p, want)
	}
}

func TestParsePMV_AllSupportedFlags(t *testing.T) {
	args := []string{
		"-f", "-v", "-n", "-u",
		"--parallel=4",
		"--exit-on-error",
		"--dry-run",
		"src", "dst",
	}
	p, err := cli.ParsePMV(args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := plan.Plan{
		Op: plan.OpMove, Src: "src", Dst: "dst", Workers: 4,
		Overwrite: true, Verbose: true, NoClobber: true, UpdateOnly: true,
		ExitOnError: true, DryRun: true,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan=%+v\nwant %+v", p, want)
	}
}

func TestParsePMV_NoClobberAndOverwriteConflict(t *testing.T) {
	args := []string{"-f", "-n", "src", "dst"}
	if _, err := cli.ParsePMV(args); err == nil {
		t.Fatal("-f and -n should be mutually exclusive")
	}
}

func TestParsePMV_RejectsRecursive(t *testing.T) {
	args := []string{"-r", "src", "dst"}
	if _, err := cli.ParsePMV(args); err == nil {
		t.Fatal("pmv must not accept -r (mv has no -r)")
	}
}

func TestParsePMV_UnknownFlagSurfacesAsUnsupported(t *testing.T) {
	args := []string{"-b", "src", "dst"}
	_, err := cli.ParsePMV(args)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "--fallback") {
		t.Errorf("error should mention --fallback, got: %v", err)
	}
}
```

`contains` 헬퍼는 Plan 1 Task 5에서 정의된 것 재사용.

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestParsePMV`
Expected: FAIL — `ParsePMV` 미정의

- [ ] **Step 3: 구현**

Create `internal/cli/pmv.go`:

```go
package cli

import (
	"fmt"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/spf13/pflag"
)

// ParsePMV는 pmv args를 Plan으로 변환한다. 거부 옵션은 안내 에러로 즉시 반환한다.
func ParsePMV(args []string) (plan.Plan, error) {
	if err := CheckPMVUnsupported(args); err != nil {
		return plan.Plan{}, err
	}

	fs := pflag.NewFlagSet("pmv", pflag.ContinueOnError)
	fs.SortFlags = false
	fs.SetInterspersed(true)

	var (
		c          Common
		overwrite  bool
		verbose    bool
		noClobber  bool
		updateOnly bool
	)
	BindCommon(fs, &c)
	fs.BoolVarP(&overwrite, "force", "f", false, "overwrite existing files (vanilla mv -f)")
	fs.BoolVarP(&verbose, "verbose", "v", false, "print each move operation")
	fs.BoolVarP(&noClobber, "no-clobber", "n", false, "do not overwrite existing files")
	fs.BoolVarP(&updateOnly, "update", "u", false, "move only when src is newer than dst")

	if err := fs.Parse(args); err != nil {
		return plan.Plan{}, fmt.Errorf("pmv: %w", err)
	}
	if err := c.Normalize(); err != nil {
		return plan.Plan{}, err
	}
	if overwrite && noClobber {
		return plan.Plan{}, fmt.Errorf("pmv: -f and -n are mutually exclusive")
	}

	rest := fs.Args()
	if len(rest) != 2 {
		return plan.Plan{}, fmt.Errorf("pmv: SRC and DST are required (got %d args)", len(rest))
	}

	return plan.Plan{
		Op:               plan.OpMove,
		Src:              rest[0],
		Dst:              rest[1],
		Workers:          c.Workers,
		Verbose:          verbose,
		Overwrite:        overwrite,
		NoClobber:        noClobber,
		UpdateOnly:       updateOnly,
		DryRun:           c.DryRun,
		ExitOnError:      c.ExitOnError,
		ErrorLogPath:     c.ErrorLogPath,
		NoProgress:       c.NoProgress,
		StrictOrder:      c.StrictOrder,
		StrictExtensions: c.StrictExtensions,
	}, nil
}
```

mv는 디렉토리 단위로 동작하므로 별도의 `-r`이 없다. pflag에 `-r`을 등록하지 않으므로 자동으로 unknown flag 에러가 난다 (테스트 `TestParsePMV_RejectsRecursive`가 통과).

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/cli -run TestParsePMV`
Expected: PASS

```bash
git add internal/cli/pmv.go internal/cli/pmv_test.go
git commit -m "feat(cli): pmv 플래그 파서 (-f/-v/-n/-u)"
```

---

## Task 4: `fsx/device` 확장 — same-device 판정 헬퍼

**Files:**
- Modify: `internal/fsx/device.go`
- Test: `internal/fsx/device_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestSameDevice_SameTmpdir(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0755); err != nil {
		t.Fatal(err)
	}
	same, err := fsx.SameDevice(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !same {
		t.Error("paths under same tmpdir should be same device")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fsx -run TestSameDevice_SameTmpdir`
Expected: FAIL — `SameDevice` 미정의

- [ ] **Step 3: 구현**

`internal/fsx/device.go`에 추가:

```go
// SameDevice는 두 경로가 같은 device(파일시스템)에 있는지 stat 결과로 판정한다.
// 두 경로 모두 존재해야 한다. 한쪽이 없으면 caller가 부모 디렉토리로 재시도해야 한다.
func SameDevice(a, b string) (bool, error) {
	da, err := DeviceID(a)
	if err != nil {
		return false, err
	}
	db, err := DeviceID(b)
	if err != nil {
		return false, err
	}
	return da == db, nil
}
```

`DeviceID`는 Plan 1 Task 6에서 정의된 것 재사용.

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/fsx`
Expected: PASS

```bash
git add internal/fsx/device.go internal/fsx/device_test.go
git commit -m "feat(fsx): SameDevice 헬퍼 (pmv 사전 검증용)"
```

---

## Task 5: `fsx/move` — same-device rename + EXDEV 폴백

**Files:**
- Create: `internal/fsx/move.go`
- Test: `internal/fsx/move_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
)

func TestRenameOrCopy_SameDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{})
	if err != nil {
		t.Fatalf("RenameOrCopy err: %v", err)
	}
	if n != 5 {
		t.Errorf("bytes=%d, want 5", n)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("src still exists: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("dst content=%q", got)
	}
}

func TestRenameOrCopy_EXDEVFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("xy"), 0600); err != nil {
		t.Fatal(err)
	}
	// fakeRename은 EXDEV를 강제로 반환하는 hook.
	defer fsx.SetRenameForTest(func(_, _ string) error { return syscall.EXDEV })()

	n, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if n != 2 {
		t.Errorf("bytes=%d, want 2", n)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("src must be unlinked after cp+unlink fallback")
	}
}

func TestRenameOrCopy_NoClobberSkipExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := fsx.RenameOrCopy(src, dst, fsx.MoveOpts{NoClobber: true})
	if !errors.Is(err, fsx.ErrSkipExisting) {
		t.Errorf("err=%v, want ErrSkipExisting", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "d" {
		t.Errorf("dst overwritten: %q", got)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should remain untouched on no-clobber skip: %v", err)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fsx -run TestRenameOrCopy`
Expected: FAIL — `RenameOrCopy`, `MoveOpts`, `SetRenameForTest` 미정의

- [ ] **Step 3: 구현**

Create `internal/fsx/move.go`:

```go
package fsx

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// MoveOpts는 RenameOrCopy의 동작을 조절한다.
type MoveOpts struct {
	NoClobber bool // -n: dst가 이미 있으면 skip
	Overwrite bool // -f: dst가 있어도 덮어쓰기
	UpdateOnly bool // -u: src.mtime > dst.mtime일 때만 진행
}

// renameFn은 테스트에서 EXDEV 강제 주입용으로 교체할 수 있는 후크.
var renameFn = os.Rename

// SetRenameForTest는 renameFn을 일시 교체하고 복구 함수를 반환한다.
func SetRenameForTest(f func(string, string) error) func() {
	prev := renameFn
	renameFn = f
	return func() { renameFn = prev }
}

// RenameOrCopy는 src를 dst로 옮긴다.
//   - 우선 os.Rename 시도
//   - EXDEV이면 CopyFile + os.Remove(src)로 폴백
//   - NoClobber: dst가 있으면 ErrSkipExisting 반환 (src는 그대로)
//   - UpdateOnly: dst가 있고 src가 더 새롭지 않으면 ErrSkipExisting
//   - Overwrite: dst가 있어도 진행 (rename은 자체적으로 덮어쓰기)
func RenameOrCopy(src, dst string, opt MoveOpts) (int64, error) {
	if opt.NoClobber {
		if _, err := os.Lstat(dst); err == nil {
			return 0, ErrSkipExisting
		}
	}
	if opt.UpdateOnly {
		if newer, err := IsSrcNewer(src, dst); err != nil {
			return 0, err
		} else if !newer {
			return 0, ErrSkipExisting
		}
	}

	err := renameFn(src, dst)
	if err == nil {
		// rename 성공 — 바이트 수는 stat으로 보고 (best-effort).
		fi, statErr := os.Lstat(dst)
		if statErr != nil || fi.IsDir() {
			return 0, nil
		}
		return fi.Size(), nil
	}

	if !errors.Is(err, syscall.EXDEV) {
		return 0, fmt.Errorf("rename: %w", err)
	}

	// EXDEV — copy+unlink로 폴백.
	n, copyErr := CopyFile(src, dst, CopyOpts{Overwrite: opt.Overwrite, NoClobber: opt.NoClobber})
	if copyErr != nil {
		return 0, copyErr
	}
	if rmErr := os.Remove(src); rmErr != nil {
		return n, fmt.Errorf("unlink src after copy: %w", rmErr)
	}
	return n, nil
}
```

`IsSrcNewer`는 Plan 1 Task 7/8에서 정의된 mtime 비교 헬퍼 재사용. 없다면 `internal/fsx/conflict.go`에 추가:

```go
// IsSrcNewer는 src.ModTime() > dst.ModTime()이면 true. dst가 없으면 true.
func IsSrcNewer(src, dst string) (bool, error) {
	dstFi, err := os.Lstat(dst)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	srcFi, err := os.Lstat(src)
	if err != nil {
		return false, err
	}
	return srcFi.ModTime().After(dstFi.ModTime()), nil
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/fsx -run TestRenameOrCopy`
Expected: PASS

```bash
git add internal/fsx/move.go internal/fsx/move_test.go internal/fsx/conflict.go
git commit -m "feat(fsx): RenameOrCopy — same-device rename + EXDEV cp+unlink 폴백"
```

---

## Task 6: `worker/pmv` — pmv 핸들러

**Files:**
- Create: `internal/worker/pmv.go`
- Test: `internal/worker/pmv_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package worker_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func TestPMVHandler_RenameSameDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	h := worker.PMV(plan.Plan{Op: plan.OpMove, SameDevice: true})
	r := h(plan.Job{Kind: plan.JobRename, Src: src, Dst: dst})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if r.Bytes != 2 {
		t.Errorf("bytes=%d, want 2", r.Bytes)
	}
}

func TestPMVHandler_DryRunNoIO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	h := worker.PMV(plan.Plan{Op: plan.OpMove, DryRun: true, SameDevice: true})
	r := h(plan.Job{Kind: plan.JobRename, Src: src, Dst: dst})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("dry-run should report Skipped")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("dry-run must not move src: %v", err)
	}
}

func TestPMVHandler_NoClobberSkips(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("s"), 0644)
	os.WriteFile(dst, []byte("d"), 0644)

	h := worker.PMV(plan.Plan{Op: plan.OpMove, NoClobber: true, SameDevice: true})
	r := h(plan.Job{Kind: plan.JobRename, Src: src, Dst: dst})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped on no-clobber")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "d" {
		t.Errorf("dst overwritten: %q", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker -run TestPMVHandler`
Expected: FAIL — `worker.PMV` 미정의

- [ ] **Step 3: 구현**

Create `internal/worker/pmv.go`:

```go
package worker

import (
	"errors"
	"fmt"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PMV는 pmv용 Job 핸들러를 만든다. JobRename / JobUnlink / JobCopy 모두 처리.
func PMV(p plan.Plan) Handler {
	return func(j plan.Job) plan.Result {
		switch j.Kind {
		case plan.JobRename:
			return handleRename(p, j)
		case plan.JobCopy:
			// cross-device 흐름의 파일 단위 copy. unlink는 walker post-pass가 일괄 처리.
			return PCP(p)(j)
		case plan.JobUnlink:
			return handleUnlink(p, j)
		}
		return plan.Result{Job: j, Err: fmt.Errorf("pmv: unsupported job kind %s", j.Kind)}
	}
}

func handleRename(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	opts := fsx.MoveOpts{
		NoClobber:  p.NoClobber,
		Overwrite:  p.Overwrite,
		UpdateOnly: p.UpdateOnly,
	}
	n, err := fsx.RenameOrCopy(j.Src, j.Dst, opts)
	if errors.Is(err, fsx.ErrSkipExisting) {
		return plan.Result{Job: j, Skipped: true}
	}
	if err != nil {
		return plan.Result{Job: j, Err: err}
	}
	return plan.Result{Job: j, Bytes: n}
}

func handleUnlink(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	if err := fsx.RemoveFile(j.Src); err != nil {
		return plan.Result{Job: j, Err: err}
	}
	return plan.Result{Job: j}
}
```

`fsx.RemoveFile`는 단순 `os.Remove` wrapper. `internal/fsx/remove.go` (Plan 3에서 본격 사용)에 미리 추가:

```go
package fsx

import "os"

// RemoveFile은 regular file 또는 symlink를 삭제한다. 디렉토리는 RemoveDir 사용.
func RemoveFile(path string) error {
	return os.Remove(path)
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/worker -run TestPMVHandler`
Expected: PASS

```bash
git add internal/worker/pmv.go internal/worker/pmv_test.go internal/fsx/remove.go
git commit -m "feat(worker): pmv 핸들러 (JobRename/JobCopy/JobUnlink)"
```

---

## Task 7: `walk/move` — cross-device 이동 walker

**Files:**
- Create: `internal/walk/move.go`
- Test: `internal/walk/move_test.go`

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

func TestMoveWalker_SameDevice_SingleRenameJob(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.WriteFile(filepath.Join(src, "sub", "f"), []byte("x"), 0644)

	w := walk.NewMove(plan.Plan{Op: plan.OpMove, Src: src, Dst: dst, SameDevice: true})
	jobs := make(chan plan.Job, 4)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()

	got := drainJobs(jobs)
	if len(got) != 1 || got[0].Kind != plan.JobRename {
		t.Fatalf("same-device walker should emit exactly one JobRename, got: %+v", got)
	}
	if got[0].Src != src || got[0].Dst != dst {
		t.Errorf("rename job paths mismatch: %+v", got[0])
	}
}

func TestMoveWalker_CrossDevice_PreOrderMkdirThenFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(filepath.Join(src, "a", "b"), 0755)
	os.WriteFile(filepath.Join(src, "a", "b", "f.txt"), []byte("x"), 0644)

	w := walk.NewMove(plan.Plan{Op: plan.OpMove, Src: src, Dst: dst, SameDevice: false})
	jobs := make(chan plan.Job, 16)
	go func() { _ = w.Walk(context.Background(), jobs); close(jobs) }()
	got := drainJobs(jobs)

	// dst 디렉토리 트리는 walker가 직접 mkdir했어야 한다 — Job으로 안 나와야 한다.
	if _, err := os.Stat(filepath.Join(dst, "a", "b")); err != nil {
		t.Errorf("walker should mkdir dst tree: %v", err)
	}
	// 파일 JobCopy 1개 + 후처리 JobUnlink 1개 + 디렉토리 rmdir(JobDirRemove) 다수.
	var copies, unlinks, rmdirs int
	for _, j := range got {
		switch j.Kind {
		case plan.JobCopy:
			copies++
		case plan.JobUnlink:
			unlinks++
		case plan.JobDirRemove:
			rmdirs++
		}
	}
	if copies != 1 || unlinks != 1 || rmdirs < 1 {
		t.Errorf("counts: copies=%d unlinks=%d rmdirs=%d", copies, unlinks, rmdirs)
	}
}
```

`drainJobs` 헬퍼는 Plan 1 walk 테스트에서 정의된 것 재사용.

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/walk -run TestMoveWalker`
Expected: FAIL — `walk.NewMove` 미정의

- [ ] **Step 3: 구현**

Create `internal/walk/move.go`:

```go
package walk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Move는 pmv용 walker.
//   - SameDevice = true 이면 src 트리 전체를 단일 JobRename 하나로 emit.
//   - SameDevice = false 이면 pcp와 동일한 pre-order 방식으로 mkdir + 파일 JobCopy를
//     emit하고, 각 파일 직후 JobUnlink, post-order에서 JobDirRemove를 emit한다.
type Move struct {
	p plan.Plan
}

// NewMove는 Move walker를 만든다.
func NewMove(p plan.Plan) *Move { return &Move{p: p} }

// Walk는 trees를 따라가며 jobs 채널에 push한다.
func (w *Move) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	if w.p.SameDevice {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- plan.Job{Kind: plan.JobRename, Src: w.p.Src, Dst: w.p.Dst}:
		}
		return nil
	}
	return w.walkCrossDevice(ctx, jobs)
}

func (w *Move) walkCrossDevice(ctx context.Context, jobs chan<- plan.Job) error {
	srcInfo, err := os.Lstat(w.p.Src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		// 단일 파일: copy + unlink만.
		return pushAll(ctx, jobs,
			plan.Job{Kind: plan.JobCopy, Src: w.p.Src, Dst: w.p.Dst},
			plan.Job{Kind: plan.JobUnlink, Src: w.p.Src},
		)
	}

	// 디렉토리 트리: pre-order에서 mkdir + 파일 copy/unlink, post-order에서 rmdir.
	type dirEntry struct {
		src string
		dst string
	}
	var dirStack []dirEntry

	err = filepath.WalkDir(w.p.Src, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(w.p.Src, srcPath)
		dstPath := filepath.Join(w.p.Dst, rel)

		if d.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			dirStack = append(dirStack, dirEntry{src: srcPath, dst: dstPath})
			return nil
		}
		if err := pushAll(ctx, jobs,
			plan.Job{Kind: plan.JobCopy, Src: srcPath, Dst: dstPath},
			plan.Job{Kind: plan.JobUnlink, Src: srcPath},
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// post-order로 rmdir Job emit. dirStack은 push 순서가 pre-order이므로 역순이 post-order.
	for i := len(dirStack) - 1; i >= 0; i-- {
		if err := pushAll(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: dirStack[i].src}); err != nil {
			return err
		}
	}
	return nil
}

// pushAll은 ctx 취소를 존중하며 여러 Job을 직렬로 push한다.
func pushAll(ctx context.Context, jobs chan<- plan.Job, batch ...plan.Job) error {
	for _, j := range batch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- j:
		}
	}
	return nil
}
```

`pushAll`이 다른 walker에도 유용하다면 `walk/util.go`로 분리한다 (지금은 move에만 두고 추후 리팩터).

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/walk -run TestMoveWalker`
Expected: PASS

```bash
git add internal/walk/move.go internal/walk/move_test.go
git commit -m "feat(walk): pmv walker (same-device 단일 rename / cross-device pre+post)"
```

---

## Task 8: `worker/pcp` 확장 — `JobDirRemove` 처리

**Files:**
- Modify: `internal/worker/pcp.go` (또는 `worker/common.go` 신설)
- Test: `internal/worker/pcp_test.go`에 추가

`pmv` cross-device 흐름은 walker가 `JobDirRemove`를 큐에 넣고 워커가 실제 `rmdir`을 호출한다 (이는 같은 src 디렉토리 자식이 모두 unlink된 뒤에만 도달하도록 walker가 post-order 보장).

- [ ] **Step 1: 실패하는 테스트**

```go
func TestPMVHandler_DirRemove(t *testing.T) {
	dir := t.TempDir()
	emptyDir := filepath.Join(dir, "empty")
	os.MkdirAll(emptyDir, 0755)

	h := worker.PMV(plan.Plan{Op: plan.OpMove})
	r := h(plan.Job{Kind: plan.JobDirRemove, Src: emptyDir})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if _, err := os.Stat(emptyDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir should be removed: err=%v", err)
	}
}

func TestPMVHandler_DirRemoveNonEmpty(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "x")
	os.MkdirAll(d, 0755)
	os.WriteFile(filepath.Join(d, "f"), []byte("x"), 0644)

	h := worker.PMV(plan.Plan{Op: plan.OpMove})
	r := h(plan.Job{Kind: plan.JobDirRemove, Src: d})
	if r.Err == nil {
		t.Fatal("rmdir on non-empty dir should fail")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker -run TestPMVHandler_DirRemove`
Expected: FAIL — `JobDirRemove` 분기 미존재

- [ ] **Step 3: 구현**

`internal/worker/pmv.go`의 switch에 분기 추가:

```go
case plan.JobDirRemove:
    return handleDirRemove(p, j)
```

새 함수:

```go
func handleDirRemove(p plan.Plan, j plan.Job) plan.Result {
	if p.DryRun {
		return plan.Result{Job: j, Skipped: true}
	}
	if err := fsx.RemoveDir(j.Src); err != nil {
		return plan.Result{Job: j, Err: err}
	}
	return plan.Result{Job: j}
}
```

`fsx.RemoveDir`을 `internal/fsx/remove.go`에 추가:

```go
// RemoveDir는 빈 디렉토리만 삭제한다 (rmdir(2) 시맨틱).
func RemoveDir(path string) error {
	return os.Remove(path) // os.Remove on a non-empty dir returns ENOTEMPTY
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/worker -run TestPMVHandler_DirRemove`
Expected: PASS

```bash
git add internal/worker/pmv.go internal/fsx/remove.go internal/worker/pmv_test.go
git commit -m "feat(worker): JobDirRemove 처리 (pmv cross-device post-order)"
```

---

## Task 9: `cmd/pmv/main.go` — pmv 진입점

**Files:**
- Create: `cmd/pmv/main.go`

- [ ] **Step 1: 진입점 작성 (테스트는 통합 테스트에서)**

Create `cmd/pmv/main.go`:

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
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// 사전 검증: src 존재 여부
	if _, err := os.Lstat(p.Src); err != nil {
		fmt.Fprintf(os.Stderr, "pmv: %v\n", err)
		return 2
	}

	// 사전 검증: dst의 부모 존재 + same-device 판정
	dstParent := dstParentForStat(p.Dst)
	same, err := fsx.SameDevice(p.Src, dstParent)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := report.NewSignal(cancel)
	sig.Notify(syscall.SIGINT, syscall.SIGTERM)

	errLog, err := report.NewErrorLog(p.ErrorLogPath, "pmv")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pmv: error log: %v\n", err)
		return 2
	}
	defer errLog.Close()

	prog := report.NewProgress("pmv", os.Stderr, p.NoProgress)
	defer prog.Stop()

	verb := report.NewVerbose(os.Stdout, p.Verbose)

	jobs := make(chan plan.Job, p.Workers*4)
	results := make(chan plan.Result, p.Workers*4)

	pool := worker.NewPool(p.Workers, worker.PMV(p))
	var poolWG sync.WaitGroup
	poolWG.Add(1)
	go func() {
		defer poolWG.Done()
		pool.Run(ctx, jobs, results)
	}()

	walker := walk.NewMove(p)
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
				prog.Tick(0, 0)
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
			prog.Tick(1, r.Bytes)
			verb.Print(r.Job)
		}
	}()

	walkWG.Wait()
	poolWG.Wait()
	close(results)
	<-consumed

	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		fmt.Fprintf(os.Stderr, "pmv: walk: %v\n", walkErr)
		nFail++
	}

	if sig.Forced() {
		return 130
	}
	if sig.Triggered() {
		return 130
	}
	if nFail > 0 {
		return 1
	}
	return 0
}

// dstParentForStat은 dst의 부모 디렉토리를 SameDevice 비교용으로 반환한다.
// dst 자체가 존재하면 dst를, 아니면 부모를 반환한다.
func dstParentForStat(dst string) string {
	if _, err := os.Lstat(dst); err == nil {
		return dst
	}
	return parent(dst)
}

func parent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == os.PathSeparator {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return "."
}
```

`report.NewVerbose`, `report.NewProgress`, `report.NewErrorLog`, `report.NewSignal`은 Plan 1에서 정의되어 있다. `worker.NewPool`도 Plan 1.

- [ ] **Step 2: 빌드 검증**

```bash
go build ./cmd/pmv
```
Expected: 컴파일 에러 없음

- [ ] **Step 3: smoke test**

```bash
mkdir -p /tmp/pmv-smoke/src/sub
echo hello > /tmp/pmv-smoke/src/a.txt
echo world > /tmp/pmv-smoke/src/sub/b.txt
./pmv /tmp/pmv-smoke/src /tmp/pmv-smoke/dst
ls -laR /tmp/pmv-smoke/
```
Expected: dst에 트리가 옮겨져 있고 src는 사라짐 (same-device, single rename)

- [ ] **Step 4: 커밋**

```bash
git add cmd/pmv/main.go
git commit -m "feat(pmv): 진입점 — same-device 다운그레이드 안내, walk/pool/report 조립"
```

---

## Task 10: 통합 테스트 — pmv 시나리오

**Files:**
- Create: `tests/integration/pmv_test.go`

- [ ] **Step 1: same-device 시나리오**

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

func TestPMV_SameDevice_DirTree(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustWrite(t, filepath.Join(src, "a.txt"), "AAA")
	mustWrite(t, filepath.Join(src, "sub/b.txt"), "BBB")

	bin := buildPMV(t)
	out, err := exec.Command(bin, src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pmv: %v\n%s", err, out)
	}
	// src는 사라져야 한다.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "sub/b.txt"))
	if !bytes.Equal(got, []byte("BBB")) {
		t.Errorf("dst content mismatch: %q", got)
	}
	// 안내 메시지 확인 (--parallel 없이 호출했으므로 다운그레이드 메시지는 안 나옴 — 워커 1)
}

func TestPMV_SameDevice_DowngradeWarning(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustWrite(t, filepath.Join(src, "f.txt"), "x")

	bin := buildPMV(t)
	cmd := exec.Command(bin, "--parallel=8", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pmv: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("downgrading --parallel to 1")) {
		t.Errorf("expected downgrade warning, got: %s", out)
	}
}

func buildPMV(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pmv")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/pmv")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build pmv: %v\n%s", err, out)
	}
	return bin
}
```

`mustWrite` 헬퍼는 Plan 1 통합 테스트에서 정의된 것 재사용.

- [ ] **Step 2: 실행**

```bash
go test -tags integration ./tests/integration -run TestPMV
```
Expected: PASS

- [ ] **Step 3: 커밋**

```bash
git add tests/integration/pmv_test.go
git commit -m "test(integration): pmv same-device 시나리오 + 다운그레이드 안내"
```

---

## Task 11: README + Plan 상태 업데이트

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 상태 표 갱신**

`README.md`의 상태 섹션을 갱신:

```markdown
## 상태 (2026-05-08)

- ✅ Plan 1: Foundation + `pcp`
- ✅ Plan 2: `pmv` (현재)
- ⏳ Plan 3: `prm`
- ⏳ Plan 4: `--fallback` 모드
```

사용 예시 섹션에 pmv 추가:

```markdown
# Cross-device 이동 (자동 감지, copy+unlink)
pmv --parallel=8 /mnt/disk1/data /mnt/disk2/data

# Same-device 이동 (자동 다운그레이드, rename(2) 한 번)
pmv /tmp/old /tmp/new
```

- [ ] **Step 2: 커밋**

```bash
git add README.md
git commit -m "docs: pmv 추가 (Plan 2 완료)"
```

---

## 마무리 검증

- [ ] **전체 테스트**

```bash
go test -race ./...
go test -tags integration ./tests/integration
```
Expected: 모두 PASS

- [ ] **빌드**

```bash
go build ./cmd/pcp ./cmd/pmv
```
Expected: 두 바이너리 생성

- [ ] **same-device smoke**

```bash
mkdir -p /tmp/pmv-smoke/{src/a,src/b}
echo 1 > /tmp/pmv-smoke/src/a/x.txt
echo 2 > /tmp/pmv-smoke/src/b/y.txt
./pmv /tmp/pmv-smoke/src /tmp/pmv-smoke/dst
diff -r /tmp/pmv-smoke/dst <(echo) || true # dst만 남아 있어야 함
test ! -d /tmp/pmv-smoke/src
```

---

## Plan 2 완료 시 산출물

- 동작하는 `pmv` 바이너리 (`bin/pmv`)
- same-device 자동 다운그레이드 (워커=1, 안내 메시지)
- cross-device 자동 폴백 (copy + unlink), per-job EXDEV도 동일
- 핵심 옵션: `-f`, `-v`, `-n`, `-u`, `--parallel`, `--exit-on-error`, `--dry-run`, `--error-log`
- pcp와 같은 streaming walk + 진행 표시 + 에러 로그 + graceful 시그널

## 다음 plan에서 추가될 것

- **Plan 3 (prm)**: post-order walker + WaitGroup barrier, `JobUnlink`/`JobDirRemove` (이미 자료형은 추가됨), `prm` 진입점
- **Plan 4 (--fallback)**: 자식 프로세스 wrapper. pcp/pmv/prm 세 도구가 미지원 옵션을 자식 명령에 위임
