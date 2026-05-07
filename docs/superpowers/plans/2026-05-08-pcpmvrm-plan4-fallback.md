# pcpmvrm Plan 4 — `--fallback` Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Plan 1–3에서 만든 native syscall 워커 옆에 자식 프로세스 위임 모드를 추가한다. 사용자가 `--fallback`을 지정하면 native handler 대신 `/bin/cp`, `/bin/mv`, `/bin/rm`에 모든 옵션(인식한 옵션 + 미지원 옵션 모두)을 그대로 전달해 fork+exec로 처리한다. T4/T5 같은 native 미지원 옵션이 필요한 사용자가 병렬화는 그대로 유지하고 싶을 때 사용한다.

**Architecture:** `internal/fallback` 패키지가 단일 핸들러 팩토리 `Fallback(p Plan)` 을 제공한다. 워커 풀은 핸들러 함수만 알면 되므로 entry point에서 `--fallback` 플래그가 켜졌을 때 `worker.PCP/PMV/PRM` 대신 `fallback.Build(p)`를 사용한다. fallback handler는 Job 단위로 자식 프로세스를 띄우며 (Job 1개 = 1 fork+exec), 자식의 stdout/stderr는 capture해서 verbose/에러 로그로 흘려보낸다. 미지원 옵션은 `internal/cli/unsupported`의 검사를 우회하기 위해 `--fallback`이 켜진 경우 raw args를 별도 store에 보관해 fallback handler가 자식 프로세스 인자에 합쳐서 전달한다.

**Tech Stack:**
- Plan 1–3과 동일
- 추가: `os/exec` (자식 프로세스 spawn), `bytes.Buffer` (출력 캡처)

**Dependencies:** Plans 1–3 모두 완료되어 있어야 한다.

---

## File Structure

```
pcpmvrm/
├── internal/
│   ├── cli/
│   │   ├── common.go                     # `--fallback` 플래그 + 미지원 옵션 통과 모드 (수정)
│   │   ├── pcp.go / pmv.go / prm.go      # ParseXxxRaw 추가: 인식 못 한 옵션도 보존 (수정)
│   │   └── unsupported.go                # --fallback이면 검사 skip (수정)
│   ├── fallback/
│   │   ├── exec.go                       # 자식 프로세스 spawn 헬퍼 (신규)
│   │   ├── translate.go                  # Job + Plan + raw flags → []string args (신규)
│   │   └── handler.go                    # fallback.Build(p plan.Plan) worker.Handler (신규)
│   └── plan/
│       └── plan.go                       # Plan에 `Fallback bool`, `RawFlags []string` 추가 (수정)
└── tests/
    └── integration/
        └── fallback_test.go              # pcp/pmv/prm 각각 --fallback 시나리오 (신규)
```

수정되는 기존 파일:

| 파일 | 변경 |
|---|---|
| `internal/plan/plan.go` | `Fallback bool`, `RawFlags []string` 필드 추가. Validate에서 Fallback=true이면 일부 검사 완화 |
| `internal/cli/common.go` | `--fallback` 플래그 등록 |
| `internal/cli/pcp.go` | `--fallback`이 있으면 unknown flag도 raw에 보존 (`fs.ParseErrorsWhitelist.UnknownFlags = true`) |
| `internal/cli/pmv.go` | 동일 |
| `internal/cli/prm.go` | 동일 |
| `internal/cli/unsupported.go` | `--fallback`이 args에 있으면 거부 검사 skip |
| `cmd/pcp/main.go` | Plan.Fallback이면 `fallback.Build(p)`를 핸들러로 사용 |
| `cmd/pmv/main.go` | 동일 |
| `cmd/prm/main.go` | 동일 |

각 신규 파일의 책임:

| 파일 | 책임 | 의존하는 것 |
|---|---|---|
| `fallback/exec.go` | `RunCmd(name, args, env)` — 자식 spawn, 출력 캡처, 종료 코드 분류 | `os/exec`, `report` |
| `fallback/translate.go` | Job + Plan → 자식 명령 인자 ([]string). pcp/pmv/prm 별 분기 | `plan` |
| `fallback/handler.go` | `Build(p plan.Plan) worker.Handler` 팩토리 | exec, translate, plan |

---

## Conventions

Plan 1과 동일.

---

## Task 1: `plan` 확장 — `Fallback`, `RawFlags`

**Files:**
- Modify: `internal/plan/plan.go`
- Test: `internal/plan/plan_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestPlan_Validate_FallbackRequiresExecutable(t *testing.T) {
	p := plan.Plan{
		Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 1,
		Fallback: true,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("fallback plan should validate: %v", err)
	}
}

func TestPlan_RawFlagsRoundtrip(t *testing.T) {
	p := plan.Plan{
		Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 1,
		Fallback: true,
		RawFlags: []string{"--reflink=auto", "-d"},
	}
	if got := p.RawFlags; len(got) != 2 || got[0] != "--reflink=auto" {
		t.Errorf("RawFlags=%v", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/plan -run TestPlan_(Validate_Fallback|RawFlags)`
Expected: FAIL — 필드 미정의

- [ ] **Step 3: 구현**

`internal/plan/plan.go`에 추가:

```go
type Plan struct {
	// ... 기존 필드 ...

	// Fallback이 true이면 워커가 native syscall 대신 자식 프로세스를 호출한다.
	Fallback bool
	// RawFlags는 --fallback 모드에서 자식 프로세스에 그대로 전달할 옵션들이다.
	// pflag가 인식한 long/short 옵션은 보존된 형태(예: "--reflink=auto", "-d")로 들어간다.
	RawFlags []string
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/plan -run TestPlan_(Validate_Fallback|RawFlags)`
Expected: PASS

```bash
git add internal/plan/plan.go internal/plan/plan_test.go
git commit -m "feat(plan): Fallback 플래그 + RawFlags 슬라이스"
```

---

## Task 2: `cli/common` — `--fallback` 플래그 등록

**Files:**
- Modify: `internal/cli/common.go`
- Test: `internal/cli/common_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestParseCommon_Fallback(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var c cli.Common
	cli.BindCommon(fs, &c)
	if err := fs.Parse([]string{"--fallback"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !c.Fallback {
		t.Error("expected Fallback=true")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestParseCommon_Fallback`
Expected: FAIL — `Common.Fallback` 미정의

- [ ] **Step 3: 구현**

`internal/cli/common.go`의 `Common` 구조체에 추가:

```go
Fallback bool
```

`BindCommon`에 등록:

```go
fs.BoolVar(&c.Fallback, "fallback", false, "delegate to /bin/cp /bin/mv /bin/rm via fork+exec (slower; supports T4/T5 options)")
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/cli -run TestParseCommon_Fallback`
Expected: PASS

```bash
git add internal/cli/common.go internal/cli/common_test.go
git commit -m "feat(cli): --fallback 플래그 (자식 프로세스 위임)"
```

---

## Task 3: `cli/unsupported` — `--fallback`이면 검사 우회

**Files:**
- Modify: `internal/cli/unsupported.go`
- Test: `internal/cli/unsupported_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
func TestUnsupported_BypassedWithFallback(t *testing.T) {
	args := []string{"--fallback", "--reflink=auto", "-r", "src", "dst"}
	if err := cli.CheckPCPUnsupported(args); err != nil {
		t.Errorf("--fallback should bypass unsupported check, got: %v", err)
	}
}

func TestUnsupported_BlockedWithoutFallback(t *testing.T) {
	args := []string{"--reflink=auto", "-r", "src", "dst"}
	if err := cli.CheckPCPUnsupported(args); err == nil {
		t.Error("--reflink without --fallback should be unsupported")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestUnsupported_(Bypassed|Blocked)`
Expected: FAIL — 우회 로직 없음

- [ ] **Step 3: 구현**

`internal/cli/unsupported.go`의 `checkUnsupported` 헬퍼에 우회 추가:

```go
func checkUnsupported(tool string, args []string, table map[string]string) error {
	if hasFlag(args, "--fallback") {
		return nil // fallback 모드는 모든 옵션 허용 (자식 프로세스가 처리)
	}
	for _, a := range args {
		// ... 기존 거부 로직 ...
	}
	return nil
}

// hasFlag는 args에 정확히 일치하는 옵션이 있으면 true. 인자값 분리(`--key=val`)도 허용.
func hasFlag(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.HasPrefix(a, want+"=") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/cli -run TestUnsupported`
Expected: PASS

```bash
git add internal/cli/unsupported.go internal/cli/unsupported_test.go
git commit -m "feat(cli): --fallback 시 미지원 옵션 검사 우회"
```

---

## Task 4: pcp/pmv/prm 파서 — unknown flag을 RawFlags로 보존

**Files:**
- Modify: `internal/cli/pcp.go`, `internal/cli/pmv.go`, `internal/cli/prm.go`
- Test: 각 *_test.go에 추가

- [ ] **Step 1: 실패하는 테스트 (pcp 기준)**

```go
func TestParsePCP_FallbackPreservesUnknownFlags(t *testing.T) {
	args := []string{"--fallback", "--reflink=auto", "-r", "-Z", "src", "dst"}
	p, err := cli.ParsePCP(args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !p.Fallback {
		t.Fatal("Fallback should be true")
	}
	want := map[string]bool{"--reflink=auto": true, "-Z": true}
	got := map[string]bool{}
	for _, f := range p.RawFlags {
		got[f] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RawFlags=%v want %v", p.RawFlags, want)
	}
	// 인식한 옵션(-r, --fallback)은 RawFlags에 들어가지 않아야 한다.
	for _, f := range p.RawFlags {
		if f == "-r" || f == "--fallback" {
			t.Errorf("recognized flag leaked into RawFlags: %s", f)
		}
	}
}

func TestParsePCP_NoFallback_UnknownFlagStillRejected(t *testing.T) {
	args := []string{"--reflink=auto", "-r", "src", "dst"}
	if _, err := cli.ParsePCP(args); err == nil {
		t.Error("unknown flag without --fallback must error")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli -run TestParsePCP_Fallback`
Expected: FAIL — RawFlags 채워지지 않음

- [ ] **Step 3: 구현**

`internal/cli/pcp.go`의 `ParsePCP`를 수정:

```go
func ParsePCP(args []string) (plan.Plan, error) {
	if err := CheckPCPUnsupported(args); err != nil {
		return plan.Plan{}, err
	}

	fallback := hasFlag(args, "--fallback")

	fs := pflag.NewFlagSet("pcp", pflag.ContinueOnError)
	fs.SortFlags = false

	if fallback {
		// fallback 모드면 인식 못 한 플래그를 무시하지 말고 raw에 모은다.
		fs.ParseErrorsWhitelist.UnknownFlags = true
	}

	// ... 기존 BindCommon, BoolVarP 등록 ...

	if err := fs.Parse(args); err != nil {
		return plan.Plan{}, fmt.Errorf("pcp: %w", err)
	}
	if err := c.Normalize(); err != nil {
		return plan.Plan{}, err
	}

	rest := fs.Args()
	if len(rest) != 2 {
		return plan.Plan{}, fmt.Errorf("pcp: SRC and DST are required")
	}

	p := plan.Plan{
		// ... 기존 필드 ...
		Fallback: c.Fallback,
	}

	if c.Fallback {
		p.RawFlags = collectRawFlags(args, fs)
	}
	return p, nil
}
```

`collectRawFlags` 헬퍼를 `internal/cli/common.go`에 추가:

```go
// collectRawFlags는 args 중 fs가 인식하지 않은 옵션을 분리해 반환한다.
// "-r", "--key=val", "--key", "val" 형태를 모두 처리한다.
// 인식 여부는 fs.Lookup / fs.ShorthandLookup으로 판단한다.
func collectRawFlags(args []string, fs *pflag.FlagSet) []string {
	var raw []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			continue
		}
		// "--key=value" 또는 "-k=value"
		key := a
		if eq := strings.Index(a, "="); eq >= 0 {
			key = a[:eq]
		}
		// 짧은 옵션 결합 (-rf, -ra) — 인식 가능한 것만 모두 정의되어 있다고 가정.
		// 인식되지 않은 짧은 옵션 결합은 매우 드물고, 사용자가 --fallback에서는 보통 long form을 씀.
		var found bool
		if strings.HasPrefix(key, "--") {
			found = fs.Lookup(strings.TrimPrefix(key, "--")) != nil
		} else if len(key) == 2 {
			found = fs.ShorthandLookup(string(key[1])) != nil
		}
		if !found {
			raw = append(raw, a)
			// 옵션이 별도 인자값을 갖는 경우(`--key val`) 다음 토큰도 raw에 포함.
			// 안전하게: 다음 토큰이 `-`로 시작하지 않으면 같이 가져간다.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				raw = append(raw, args[i+1])
				i++
			}
		}
	}
	return raw
}
```

`pmv.go`, `prm.go`에도 동일 패턴 적용:
- `fallback := hasFlag(args, "--fallback")`
- `if fallback { fs.ParseErrorsWhitelist.UnknownFlags = true }`
- 마지막에 `p.Fallback = c.Fallback; if c.Fallback { p.RawFlags = collectRawFlags(args, fs) }`

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/cli`
Expected: PASS

```bash
git add internal/cli/
git commit -m "feat(cli): --fallback 시 unknown flag을 RawFlags로 보존"
```

---

## Task 5: `fallback/exec` — 자식 프로세스 spawn 헬퍼

**Files:**
- Create: `internal/fallback/exec.go`
- Test: `internal/fallback/exec_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package fallback_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fallback"
)

func TestRunCmd_Success(t *testing.T) {
	out, err := fallback.RunCmd(context.Background(), "/bin/echo", []string{"hello"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.Stdout, "hello") {
		t.Errorf("stdout=%q", out.Stdout)
	}
	if out.ExitCode != 0 {
		t.Errorf("exit=%d", out.ExitCode)
	}
}

func TestRunCmd_NonZeroExit(t *testing.T) {
	out, err := fallback.RunCmd(context.Background(), "/bin/sh", []string{"-c", "exit 7"})
	if err == nil {
		t.Fatal("expected non-zero exit error")
	}
	if out.ExitCode != 7 {
		t.Errorf("exit=%d, want 7", out.ExitCode)
	}
}

func TestRunCmd_NotFound(t *testing.T) {
	_, err := fallback.RunCmd(context.Background(), "/no/such/binary", nil)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestRunCmd_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 즉시 취소
	_, err := fallback.RunCmd(ctx, "/bin/sleep", []string{"5"})
	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fallback -run TestRunCmd`
Expected: FAIL — 패키지 없음

- [ ] **Step 3: 구현**

Create `internal/fallback/exec.go`:

```go
package fallback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// CmdOutput은 자식 프로세스 실행 결과를 담는다.
type CmdOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCmd는 name 바이너리를 args로 호출하고 결과를 반환한다.
// 종료 코드가 0이 아니면 에러로 본다 (CmdOutput.ExitCode에 코드 보존).
// ctx 취소 시 자식 프로세스에 SIGKILL이 전송된다.
func RunCmd(ctx context.Context, name string, args []string) (CmdOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := CmdOutput{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		return out, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.ExitCode = exitErr.ExitCode()
		return out, fmt.Errorf("%s exited %d: %s", name, out.ExitCode, trim(out.Stderr))
	}
	// fork 실패, 컨텍스트 취소 등.
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func trim(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/fallback -run TestRunCmd`
Expected: PASS

```bash
git add internal/fallback/exec.go internal/fallback/exec_test.go
git commit -m "feat(fallback): RunCmd 헬퍼 (자식 spawn + 출력 캡처)"
```

---

## Task 6: `fallback/translate` — Job → 자식 명령 인자 변환

**Files:**
- Create: `internal/fallback/translate.go`
- Test: `internal/fallback/translate_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package fallback_test

import (
	"reflect"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fallback"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestTranslate_PCPCopy(t *testing.T) {
	p := plan.Plan{Op: plan.OpCopy, Recursive: true, Verbose: true,
		Preserve: plan.Preserve{Mode: true, Owner: true, Timestamps: true},
		RawFlags: []string{"--reflink=auto"},
	}
	j := plan.Job{Kind: plan.JobCopy, Src: "src/file", Dst: "dst/file"}

	bin, args := fallback.Translate(p, j)
	if bin != "/bin/cp" {
		t.Errorf("bin=%s, want /bin/cp", bin)
	}
	want := []string{"-v", "--preserve=mode,ownership,timestamps", "--reflink=auto", "src/file", "dst/file"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant %v", args, want)
	}
}

func TestTranslate_PCPDirCopy_PassesRecursive(t *testing.T) {
	p := plan.Plan{Op: plan.OpCopy, Recursive: true}
	j := plan.Job{Kind: plan.JobDirCopy, Src: "src/d", Dst: "dst/d"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/cp" {
		t.Errorf("bin=%s", bin)
	}
	if !contains(args, "-r") && !contains(args, "-R") {
		t.Errorf("dir copy must pass -r, got: %v", args)
	}
}

func TestTranslate_PMVRename(t *testing.T) {
	p := plan.Plan{Op: plan.OpMove, Overwrite: true}
	j := plan.Job{Kind: plan.JobRename, Src: "a", Dst: "b"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/mv" {
		t.Errorf("bin=%s", bin)
	}
	want := []string{"-f", "a", "b"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant %v", args, want)
	}
}

func TestTranslate_PRMUnlink(t *testing.T) {
	p := plan.Plan{Op: plan.OpRemove, Verbose: true, ForceMissing: true}
	j := plan.Job{Kind: plan.JobUnlink, Src: "/x"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/rm" {
		t.Errorf("bin=%s", bin)
	}
	want := []string{"-f", "-v", "/x"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v want %v", args, want)
	}
}

func TestTranslate_PRMDirRemove(t *testing.T) {
	p := plan.Plan{Op: plan.OpRemove}
	j := plan.Job{Kind: plan.JobDirRemove, Src: "/x"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/rmdir" {
		t.Errorf("bin=%s, want /bin/rmdir", bin)
	}
	want := []string{"/x"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v want %v", args, want)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fallback -run TestTranslate`
Expected: FAIL — `Translate` 미정의

- [ ] **Step 3: 구현**

Create `internal/fallback/translate.go`:

```go
package fallback

import (
	"strings"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Translate는 Plan + Job → (자식 바이너리, args)로 변환한다.
// pcp/pmv/prm Op과 Job.Kind에 따라 적절한 표준 명령으로 매핑한다.
func Translate(p plan.Plan, j plan.Job) (string, []string) {
	switch p.Op {
	case plan.OpCopy:
		return translateCp(p, j)
	case plan.OpMove:
		return translateMv(p, j)
	case plan.OpRemove:
		return translateRm(p, j)
	}
	return "", nil
}

func translateCp(p plan.Plan, j plan.Job) (string, []string) {
	var args []string
	if j.Kind == plan.JobDirCopy {
		args = append(args, "-r")
	} else if p.Recursive {
		args = append(args, "-r")
	}
	if p.Overwrite {
		args = append(args, "-f")
	}
	if p.NoClobber {
		args = append(args, "-n")
	}
	if p.UpdateOnly {
		args = append(args, "-u")
	}
	if p.Verbose {
		args = append(args, "-v")
	}
	if pres := preserveArg(p.Preserve); pres != "" {
		args = append(args, pres)
	}
	args = append(args, p.RawFlags...)
	args = append(args, j.Src, j.Dst)
	return "/bin/cp", args
}

func translateMv(p plan.Plan, j plan.Job) (string, []string) {
	var args []string
	if p.Overwrite {
		args = append(args, "-f")
	}
	if p.NoClobber {
		args = append(args, "-n")
	}
	if p.UpdateOnly {
		args = append(args, "-u")
	}
	if p.Verbose {
		args = append(args, "-v")
	}
	args = append(args, p.RawFlags...)
	args = append(args, j.Src, j.Dst)
	return "/bin/mv", args
}

func translateRm(p plan.Plan, j plan.Job) (string, []string) {
	switch j.Kind {
	case plan.JobDirRemove:
		// rmdir(1)은 빈 디렉토리만 삭제. -d 옵션이 없는 prm용 흐름과 일치.
		return "/bin/rmdir", append([]string{}, j.Src)
	case plan.JobUnlink:
		var args []string
		if p.ForceMissing {
			args = append(args, "-f")
		}
		if p.Verbose {
			args = append(args, "-v")
		}
		args = append(args, p.RawFlags...)
		args = append(args, j.Src)
		return "/bin/rm", args
	}
	return "", nil
}

func preserveArg(pres plan.Preserve) string {
	var parts []string
	if pres.Mode {
		parts = append(parts, "mode")
	}
	if pres.Owner {
		parts = append(parts, "ownership")
	}
	if pres.Timestamps {
		parts = append(parts, "timestamps")
	}
	if len(parts) == 0 {
		return ""
	}
	return "--preserve=" + strings.Join(parts, ",")
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/fallback -run TestTranslate`
Expected: PASS

```bash
git add internal/fallback/translate.go internal/fallback/translate_test.go
git commit -m "feat(fallback): Translate (Plan+Job → cp/mv/rm/rmdir args)"
```

---

## Task 7: `fallback/handler` — Build 팩토리

**Files:**
- Create: `internal/fallback/handler.go`
- Test: `internal/fallback/handler_test.go`

- [ ] **Step 1: 실패하는 테스트**

```go
package fallback_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fallback"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestBuild_PCPCopiesViaCp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("hello"), 0644)

	h := fallback.Build(plan.Plan{Op: plan.OpCopy, Fallback: true})
	r := h(plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("dst=%q", got)
	}
}

func TestBuild_DryRunNoSpawn(t *testing.T) {
	h := fallback.Build(plan.Plan{Op: plan.OpCopy, Fallback: true, DryRun: true})
	r := h(plan.Job{Kind: plan.JobCopy, Src: "/no/such", Dst: "/x"})
	if r.Err != nil {
		t.Errorf("dry-run shouldn't error: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("dry-run should set Skipped")
	}
}

func TestBuild_PRMRemovesViaRm(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	os.WriteFile(f, []byte("x"), 0644)

	h := fallback.Build(plan.Plan{Op: plan.OpRemove, Fallback: true})
	r := h(plan.Job{Kind: plan.JobUnlink, Src: f})
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("file should be gone")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fallback -run TestBuild`
Expected: FAIL

- [ ] **Step 3: 구현**

Create `internal/fallback/handler.go`:

```go
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
	return func(j plan.Job) plan.Result {
		if p.DryRun {
			return plan.Result{Job: j, Skipped: true}
		}
		bin, args := Translate(p, j)
		if bin == "" {
			return plan.Result{Job: j, Err: fmt.Errorf("fallback: cannot translate job %s", j.Kind)}
		}
		out, err := RunCmd(context.Background(), bin, args)
		if err != nil {
			return plan.Result{Job: j, Err: err}
		}
		// 성공 시 stdout/stderr는 verbose 출력에서 사용 — bytes 카운트는 best-effort 0.
		return plan.Result{Job: j, Stdout: out.Stdout}
	}
}
```

`plan.Result`에 `Stdout string` 필드가 없다면 추가:

```go
type Result struct {
	Job     Job
	Bytes   int64
	Skipped bool
	Err     error
	Stdout  string // fallback 모드에서 자식의 stdout 캡처 (verbose에 표시)
}
```

- [ ] **Step 4: 통과 + 커밋**

Run: `go test ./internal/fallback -run TestBuild`
Expected: PASS

```bash
git add internal/fallback/handler.go internal/fallback/handler_test.go internal/plan/result.go
git commit -m "feat(fallback): Build 팩토리 + Result.Stdout 필드"
```

---

## Task 8: `cmd/pcp` — `--fallback` 분기 추가

**Files:**
- Modify: `cmd/pcp/main.go`
- Test: 통합 테스트로 검증 (다음 task)

- [ ] **Step 1: 핸들러 선택 분기 추가**

`cmd/pcp/main.go`의 핸들러 생성부 수정:

```go
import (
	// ...
	"github.com/nineking424/pcpmvrm/internal/fallback"
)

// ...
var handler worker.Handler
if p.Fallback {
	handler = fallback.Build(p)
} else {
	handler = worker.PCP(p)
}
pool := worker.NewPool(p.Workers, handler)
```

같은 패턴을 `cmd/pmv/main.go`, `cmd/prm/main.go`에도 적용:

```go
// pmv
if p.Fallback {
	handler = fallback.Build(p)
} else {
	handler = worker.PMV(p)
}

// prm
if p.Fallback {
	handler = fallback.Build(p)
} else {
	handler = worker.PRM(p)
}
```

- [ ] **Step 2: 빌드 검증**

```bash
go build ./cmd/...
```
Expected: 컴파일 성공

- [ ] **Step 3: smoke test (pcp)**

```bash
mkdir -p /tmp/fallback-smoke/src
echo hi > /tmp/fallback-smoke/src/f.txt
./pcp -r --parallel=2 --fallback /tmp/fallback-smoke/src /tmp/fallback-smoke/dst
diff -r /tmp/fallback-smoke/src /tmp/fallback-smoke/dst
```
Expected: diff 출력 없음

- [ ] **Step 4: 커밋**

```bash
git add cmd/pcp/main.go cmd/pmv/main.go cmd/prm/main.go
git commit -m "feat(cmd): --fallback 시 fallback.Build 핸들러 사용"
```

---

## Task 9: 통합 테스트 — fallback 시나리오

**Files:**
- Create: `tests/integration/fallback_test.go`

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

func TestFallback_PCP_PassesRawFlagToCp(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "dst.txt")
	mustWrite(t, src, "hi")

	bin := buildPCP(t)
	// `--reflink=auto`는 Linux/macOS에서 cp가 무시하거나 시도. 어느 쪽이든 성공해야 한다.
	out, err := exec.Command(bin, "--fallback", "--reflink=auto", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pcp --fallback --reflink: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, []byte("hi")) {
		t.Errorf("dst=%q", got)
	}
}

func TestFallback_PMV_CrossDeviceLikeBehavior(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a")
	dst := filepath.Join(root, "b")
	mustWrite(t, src, "x")

	bin := buildPMV(t)
	out, err := exec.Command(bin, "--fallback", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pmv --fallback: %v\n%s", err, out)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be moved")
	}
}

func TestFallback_PRM_RemovesViaRm(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "f")
	mustWrite(t, f, "x")

	bin := buildPRM(t)
	out, err := exec.Command(bin, "--fallback", f).CombinedOutput()
	if err != nil {
		t.Fatalf("prm --fallback: %v\n%s", err, out)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("file should be gone")
	}
}

func TestFallback_PCP_RecursiveTreePreservesContent(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustWrite(t, filepath.Join(src, "a/f1"), "AAA")
	mustWrite(t, filepath.Join(src, "a/b/f2"), "BBB")

	bin := buildPCP(t)
	out, err := exec.Command(bin, "-r", "--parallel=2", "--fallback", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pcp -r --fallback: %v\n%s", err, out)
	}
	cmp := exec.Command("diff", "-r", src, dst)
	if outDiff, err := cmp.CombinedOutput(); err != nil || len(outDiff) > 0 {
		t.Errorf("diff src/dst: err=%v out=%s", err, outDiff)
	}
}
```

`buildPCP`, `buildPMV`, `buildPRM`, `mustWrite`는 Plan 1–3에서 정의된 헬퍼 재사용.

- [ ] **Step 2: 실행**

```bash
go test -tags integration ./tests/integration -run TestFallback
```
Expected: PASS

- [ ] **Step 3: 커밋**

```bash
git add tests/integration/fallback_test.go
git commit -m "test(integration): --fallback (pcp/pmv/prm)"
```

---

## Task 10: README + 문서 마무리

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 상태 + 사용 예시 갱신**

```markdown
## 상태 (2026-05-08)

- ✅ Plan 1: Foundation + `pcp`
- ✅ Plan 2: `pmv`
- ✅ Plan 3: `prm`
- ✅ Plan 4: `--fallback` 모드 (현재)

## 사용 예시 (전체)

```bash
# 단일 워커 (바닐라 cp와 동일한 처리량)
pcp -r src/ dst/

# 8 워커 병렬
pcp -r --parallel=8 src/ dst/

# native 미지원 옵션은 fallback으로 (성능 저하)
pcp -r --parallel=8 --fallback --reflink=auto src/ dst/

# Cross-device 이동 (자동 감지)
pmv --parallel=8 /mnt/disk1/data /mnt/disk2/data

# 대량 삭제, 첫 에러에서 중단
prm -rf --parallel=16 --exit-on-error /var/cache/old/

# fallback으로 SELinux 컨텍스트 유지
pmv --fallback -Z /etc/foo /etc/bar
```

## 디자인 트레이드오프

`--fallback`은 옵션 호환성을 100% 보장하지만 Job마다 fork+exec 비용이 발생합니다. 100만 파일 처리 시 native 모드 대비 처리량이 한 자릿수 배수로 떨어질 수 있습니다. T1+T2+T3 옵션만 쓰는 일반적인 워크로드는 기본(native) 모드를 권장합니다.
```

- [ ] **Step 2: 커밋**

```bash
git add README.md
git commit -m "docs: --fallback 추가 (Plan 4 완료) + 트레이드오프 설명"
```

---

## 마무리 검증

- [ ] **전체 테스트**

```bash
go test -race ./...
go test -tags integration ./tests/integration
```
Expected: 모두 PASS

- [ ] **빌드 검증**

```bash
go build ./cmd/...
ls -la pcp pmv prm
```
Expected: 세 바이너리 생성

- [ ] **종합 smoke**

```bash
# native pcp + fallback pcp 결과 일치 확인
mkdir -p /tmp/sm/{src,native,fallback}
echo a > /tmp/sm/src/x.txt
./pcp -rp /tmp/sm/src/ /tmp/sm/native/
./pcp -rp --fallback /tmp/sm/src/ /tmp/sm/fallback/
diff -r /tmp/sm/native /tmp/sm/fallback
```
Expected: diff 출력 없음

- [ ] **최종 push**

```bash
git push origin main
```

---

## Plan 4 완료 시 산출물

- 세 도구 모두 `--fallback` 옵션 지원 (`/bin/cp`, `/bin/mv`, `/bin/rm`, `/bin/rmdir` 자식 호출)
- T4/T5 옵션이 자식 명령에 그대로 전달됨 (`--reflink`, `-Z`, `--sparse`, `-d`(cp), 등)
- 미지원 옵션 검사가 `--fallback` 시 우회됨
- native와 동일한 streaming/병렬/에러 로그/진행 표시/시그널 처리
- Job 단위 fork+exec → 처리량 trade-off 명시 (README)

## 후속 후보 (이 plan 범위 외)

- 자식 프로세스 stderr를 verbose가 아닌 에러 로그에도 함께 기록
- 자식 명령 경로를 환경변수(`PCPMVRM_CP_BIN` 등)로 오버라이드
- macOS BSD `cp`/`mv`/`rm` 차이 흡수 (현재는 GNU coreutils 가정)
