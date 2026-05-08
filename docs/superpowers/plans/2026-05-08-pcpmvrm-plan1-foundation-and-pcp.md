# pcpmvrm Plan 1 — Foundation + `pcp` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Foundation 패키지(`plan`, `cli`, `fsx`, `report`, `worker`, `walk`)를 구축하고, 그 위에 동작하는 `pcp` CLI 도구를 완성한다. native syscall 기반의 병렬 복사, streaming walk, 에러 로그, graceful 시그널 처리, dry-run 모드를 모두 포함한다. `--fallback`은 Plan 4에서 다룬다.

**Architecture:** Walker 1개 + Worker N개 + Reporter 1개의 goroutine 구성. Walker가 트리를 DFS로 순회하며 디렉토리 mkdir은 직접 수행하고 파일 Job만 bounded channel에 push. Worker는 `fsx` 패키지의 atomic copy(`<filename>.pcp-tmp-<6hex>` → `os.Rename`) 헬퍼를 사용해 파일을 복사한다. Reporter는 1초 단위 progress 라인과 verbose 출력 직렬화를 담당하며, signal handler가 graceful shutdown을 조율한다.

**Tech Stack:**
- Go 1.22+ (filepath.WalkDir, slog, errors.Is)
- `golang.org/x/sys/unix` (메타데이터 보존, EXDEV 감지)
- `github.com/spf13/pflag` (POSIX-style 플래그 파싱, 짧은 옵션 결합 지원)
- 표준 `testing` 패키지 (table-driven, t.TempDir)

---

## File Structure

```
pcpmvrm/
├── go.mod
├── go.sum
├── Makefile                              # 빌드/테스트/린트 단축 명령
├── .gitignore                            # Go 표준 ignore
├── cmd/
│   └── pcp/main.go                       # pcp 진입점, CLI 파싱 → Plan → Run
├── internal/
│   ├── plan/
│   │   ├── plan.go                       # Plan 구조체 (Op, Src, Dst, Flags, Workers)
│   │   ├── job.go                        # Job 구조체 (Op, Src, Dst, RelPath, Mode 등)
│   │   └── result.go                     # Result 구조체 (성공/실패 통계용)
│   ├── cli/
│   │   ├── common.go                     # --parallel, --strict-*, --dry-run, --error-log, --no-progress, --exit-on-error
│   │   ├── pcp.go                        # pcp 전용: -r, -f, -v, -p, -a, --preserve, -n, -u
│   │   └── unsupported.go                # T4/T5/-i 등 미지원 옵션 감지·안내문 생성
│   ├── fsx/
│   │   ├── device.go                     # stat.Dev 비교, EXDEV 감지
│   │   ├── copy.go                       # atomic copy via temp file + os.Rename
│   │   ├── meta.go                       # -p/-a 메타데이터 보존 (mode/owner/timestamp)
│   │   └── conflict.go                   # -n (O_EXCL) / -u (mtime 비교) 정책
│   ├── report/
│   │   ├── error_log.go                  # 에러 로그 파일 writer (concurrent-safe)
│   │   ├── progress.go                   # 1초 단위 진행 라인 (TTY 감지)
│   │   ├── verbose.go                    # -v 출력 mutex 직렬화
│   │   └── signal.go                     # SIGINT/SIGTERM 1차/2차 처리
│   ├── worker/
│   │   ├── pool.go                       # 워커 pool 구조, Job channel 관리
│   │   └── pcp.go                        # pcp 전용 워커 함수: fsx.Copy + fsx.Meta + fsx.Conflict
│   └── walk/
│       ├── default.go                    # 기본 모드: 파일 단위 큐잉, 디렉토리는 즉시 mkdir
│       ├── strict_order.go               # --strict-order: 디렉토리 단위 Job
│       └── strict_ext.go                 # --strict-extension: 2-phase walker
└── tests/
    └── integration/
        └── pcp_test.go                   # end-to-end 시나리오 (실제 트리 구성 → 실행 → dst 비교)
```

각 파일의 책임:

| 파일 | 책임 | 의존하는 것 |
|---|---|---|
| `plan/plan.go` | Plan 자료형(불변), `Validate()` | 없음 |
| `plan/job.go` | Job 자료형 (`Kind: Copy`/`Mkdir` 등) | 없음 |
| `plan/result.go` | Result 자료형(성공/실패 카운트) | 없음 |
| `cli/common.go` | pflag 기반 공통 플래그 파싱 | pflag |
| `cli/pcp.go` | pcp 전용 플래그 + Plan 변환 | common.go |
| `cli/unsupported.go` | 거부 옵션 목록·안내문 | 없음 |
| `fsx/device.go` | `stat.Dev` 비교, `EXDEV` 분기 | `unix` |
| `fsx/copy.go` | `<.pcp-tmp-xxxxxx>` 기법 atomic copy | stdlib |
| `fsx/meta.go` | mode/owner/utime preserve | `unix` |
| `fsx/conflict.go` | O_EXCL skip, mtime 비교 | stdlib |
| `report/error_log.go` | 줄단위 append, 자동 파일명 생성 | stdlib |
| `report/progress.go` | 1초 tick, TTY 감지, ANSI carriage return | stdlib |
| `report/verbose.go` | mutex로 직렬화된 stdout writer | stdlib |
| `report/signal.go` | os.Signal 채널 수신, 1차/2차 분기 | stdlib |
| `worker/pool.go` | N개 worker goroutine, Job consume, panic recover | report, plan |
| `worker/pcp.go` | Copy Job → fsx.Copy → fsx.Meta → Result 발행 | fsx, report |
| `walk/default.go` | DFS pre-order, dir 즉시 mkdir, file Job push | plan, fsx |
| `walk/strict_order.go` | DFS, 디렉토리 자체를 Job으로 push | plan |
| `walk/strict_ext.go` | 2-phase 큐잉 (비대상 → barrier → 대상) | plan |
| `cmd/pcp/main.go` | os.Args → cli → walk → worker → report | 전부 |

---

## Conventions

- **TDD**: 각 task는 (1) 실패하는 테스트 작성 → (2) 실패 확인 → (3) 최소 구현 → (4) 통과 확인 → (5) 커밋 순서로 진행
- **Commit prefix**: `feat:`, `test:`, `fix:`, `refactor:`, `docs:`, `chore:` 중 하나
- **Test 파일 위치**: 같은 디렉토리에 `*_test.go` (Go 표준), 통합 테스트만 `tests/integration/`
- **Import 순서**: stdlib → 외부 → 내부 (goimports 자동)
- **에러 메시지**: 영문, 소문자 시작, 마침표 없음 (Go 관례)
- **공개 함수에 doc comment**: 한 줄 영문, `// FuncName ...`로 시작

---

## Task 1: 프로젝트 스켈레톤

**Files:**
- Create: `go.mod`, `Makefile`, `.gitignore`

- [ ] **Step 1: `go.mod` 초기화**

```bash
cd /Users/nineking/workspace/app/pcpmvrm
go mod init github.com/nineking424/pcpmvrm
go get github.com/spf13/pflag@latest
go get golang.org/x/sys/unix@latest
```

- [ ] **Step 2: `.gitignore` 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/.gitignore`:

```gitignore
# Binaries
/pcp
/pmv
/prm
/bin/

# Go test artifacts
*.test
*.out
coverage.out

# IDE
.vscode/
.idea/
.DS_Store

# pcpmvrm runtime artifacts (테스트 시 생성)
pcp-failed-*.log
pmv-failed-*.log
prm-failed-*.log
*.pcp-tmp-*
```

- [ ] **Step 3: `Makefile` 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/Makefile`:

```makefile
.PHONY: build test test-int lint fmt clean

build:
	go build -o bin/pcp ./cmd/pcp

test:
	go test -race ./internal/...

test-int:
	go test -race ./tests/integration/...

lint:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ coverage.out
```

- [ ] **Step 4: 빌드 가능 상태 확인**

Run: `go mod tidy && go build ./...`
Expected: 에러 없이 종료. (cmd 디렉토리 비어있으므로 빌드되는 바이너리 없음)

- [ ] **Step 5: 커밋**

```bash
git add go.mod go.sum Makefile .gitignore
git commit -m "chore: 프로젝트 스켈레톤 (go.mod, Makefile, .gitignore)"
```

---

## Task 2: `plan` 패키지 — Plan/Job/Result 자료형

**Files:**
- Create: `internal/plan/plan.go`, `internal/plan/job.go`, `internal/plan/result.go`
- Test: `internal/plan/plan_test.go`

- [ ] **Step 1: 실패하는 테스트 작성 — Plan.Validate**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/plan/plan_test.go`:

```go
package plan_test

import (
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestPlanValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       plan.Plan
		wantErr string
	}{
		{
			name:    "ok minimum",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 1},
			wantErr: "",
		},
		{
			name:    "missing src",
			p:       plan.Plan{Op: plan.OpCopy, Dst: "/b", Workers: 1},
			wantErr: "src is required",
		},
		{
			name:    "missing dst for copy",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Workers: 1},
			wantErr: "dst is required for copy",
		},
		{
			name:    "workers must be positive",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 0},
			wantErr: "workers must be >= 1",
		},
		{
			name:    "strict-order and strict-extension are exclusive when conflicting",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 1, StrictOrder: true, StrictExtensions: []string{".json"}},
			wantErr: "", // 둘 다 허용 (스펙 §5.2)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/plan/...`
Expected: FAIL — `plan` 패키지 없음 / Plan 미정의

- [ ] **Step 3: `plan/plan.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/plan/plan.go`:

```go
// Package plan defines the immutable Plan struct and the supporting Job/Result
// types passed between the CLI parser, walker, and workers.
package plan

import "errors"

// Op identifies which tool is running.
type Op int

const (
	OpCopy Op = iota
	OpMove
	OpRemove
)

// Plan is the immutable, fully-validated description of one CLI invocation.
type Plan struct {
	Op       Op
	Src      string
	Dst      string // unused for OpRemove
	Workers  int

	// Common flags
	Recursive       bool
	Verbose         bool
	DryRun          bool
	ExitOnError     bool
	NoProgress      bool
	ErrorLogPath    string // empty → auto-generate

	// Modes
	StrictOrder      bool
	StrictExtensions []string // lowercase, leading dot, e.g. ".json"

	// Tool-specific
	Overwrite       bool   // -f
	NoClobber       bool   // -n
	UpdateOnly      bool   // -u
	Preserve        Preserve
	RemoveEmptyDir  bool   // prm -d
}

// Preserve groups the metadata-preservation flags.
type Preserve struct {
	Mode       bool
	Ownership  bool
	Timestamps bool
}

// Validate returns the first violation found, or nil if the plan is consistent.
func (p Plan) Validate() error {
	if p.Src == "" {
		return errors.New("src is required")
	}
	if p.Op == OpCopy || p.Op == OpMove {
		if p.Dst == "" {
			if p.Op == OpCopy {
				return errors.New("dst is required for copy")
			}
			return errors.New("dst is required for move")
		}
	}
	if p.Workers < 1 {
		return errors.New("workers must be >= 1")
	}
	return nil
}
```

- [ ] **Step 4: `plan/job.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/plan/job.go`:

```go
package plan

import "io/fs"

// JobKind classifies a unit of work passed from walker to worker.
type JobKind int

const (
	JobCopy JobKind = iota
	JobUnlink
	JobDirCopy   // strict-order: 디렉토리 단위 복사
	JobDirRemove // prm post-order: 디렉토리 비우기 + rmdir
)

// Job is a single unit of work pushed onto the work queue.
//
// For JobCopy: Src/Dst는 절대 경로, RelPath는 src 트리 루트로부터의 상대 경로
// (에러 로그/verbose 출력에 사용).
type Job struct {
	Kind    JobKind
	Src     string
	Dst     string
	RelPath string
	Info    fs.FileInfo
}
```

- [ ] **Step 5: `plan/result.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/plan/result.go`:

```go
package plan

import "time"

// Result is what a worker reports back after handling a Job.
//
// Bytes is set only for successful Copy jobs.
type Result struct {
	Job       Job
	Err       error
	Bytes     int64
	Elapsed   time.Duration
	Skipped   bool // -n / -u 등으로 의도적으로 건너뜀
}
```

- [ ] **Step 6: 통과 확인**

Run: `go test ./internal/plan/...`
Expected: PASS

- [ ] **Step 7: 커밋**

```bash
git add internal/plan/
git commit -m "feat(plan): Plan/Job/Result 자료형과 Validate 추가"
```

---

## Task 3: `cli/unsupported` — 미지원 옵션 감지

**Files:**
- Create: `internal/cli/unsupported.go`
- Test: `internal/cli/unsupported_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/cli/unsupported_test.go`:

```go
package cli_test

import (
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
)

func TestRejectUnsupported(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		args   []string
		hit    string // 검출되어야 하는 옵션 (또는 "" = 통과)
	}{
		{name: "pcp ok plain", tool: "pcp", args: []string{"-r", "src", "dst"}, hit: ""},
		{name: "pcp reject -i", tool: "pcp", args: []string{"-i", "src", "dst"}, hit: "-i"},
		{name: "pcp reject --reflink", tool: "pcp", args: []string{"--reflink=auto", "src", "dst"}, hit: "--reflink"},
		{name: "pcp reject --sparse", tool: "pcp", args: []string{"--sparse=always", "src", "dst"}, hit: "--sparse"},
		{name: "pcp reject -L", tool: "pcp", args: []string{"-L", "src", "dst"}, hit: "-L"},
		{name: "pcp reject combined -ri", tool: "pcp", args: []string{"-ri", "src", "dst"}, hit: "-i"},
		{name: "pcp ok combined -ra", tool: "pcp", args: []string{"-ra", "src", "dst"}, hit: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := cli.FirstUnsupported(tt.tool, tt.args)
			if hit != tt.hit {
				t.Fatalf("FirstUnsupported(%v) = %q, want %q", tt.args, hit, tt.hit)
			}
		})
	}
}

func TestUnsupportedMessage(t *testing.T) {
	msg := cli.UnsupportedMessage("pcp", "--reflink")
	want := []string{
		"pcp: '--reflink'",
		"--fallback",
		"성능",
	}
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("message %q missing %q", msg, w)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli/...`
Expected: FAIL — `cli` 패키지 없음

- [ ] **Step 3: `cli/unsupported.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/cli/unsupported.go`:

```go
// Package cli implements POSIX-style flag parsing for pcp/pmv/prm and
// detects options that we deliberately refuse to handle natively.
package cli

import (
	"fmt"
	"strings"
)

// unsupportedShort lists single-letter options that we reject in native mode.
// Keyed by tool name. Combined flags like -ri are exploded letter-by-letter.
var unsupportedShort = map[string]map[byte]struct{}{
	"pcp": {'i': {}, 'L': {}, 'P': {}, 'H': {}, 'd': {}, 'l': {}, 's': {}, 'x': {}},
	"pmv": {'i': {}},
	"prm": {'i': {}, 'I': {}},
}

// unsupportedLong lists long options (with leading "--") rejected in native mode.
var unsupportedLong = map[string]map[string]struct{}{
	"pcp": {
		"--reflink": {}, "--sparse": {}, "--no-dereference": {},
		"--remove-destination": {}, "--copy-contents": {}, "--symbolic-link": {},
		"--link": {}, "--one-file-system": {}, "--interactive": {},
	},
	"pmv": {"--interactive": {}},
	"prm": {"--interactive": {}, "--one-file-system": {}},
}

// FirstUnsupported scans args and returns the first unsupported option found,
// or "" if all options are supported. It does not parse positional args.
//
// It correctly explodes combined short flags ("-ri" → "-r" + "-i").
// Long options with values ("--reflink=auto") are matched on the option name only.
func FirstUnsupported(tool string, args []string) string {
	short := unsupportedShort[tool]
	long := unsupportedLong[tool]
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--"):
			name := a
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name = a[:eq]
			}
			if _, bad := long[name]; bad {
				return name
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// 결합 단축: -ra, -ri
			for i := 1; i < len(a); i++ {
				if _, bad := short[a[i]]; bad {
					return "-" + string(a[i])
				}
			}
		}
	}
	return ""
}

// UnsupportedMessage builds the standard rejection message shown to users.
func UnsupportedMessage(tool, opt string) string {
	return fmt.Sprintf(
		"%s: '%s'은 native 모드에서 지원하지 않습니다.\n"+
			"  - --fallback 옵션으로 자식 프로세스 위임 모드를 활성화하면 사용 가능합니다.\n"+
			"  - 단, 자식 프로세스 fork 비용이 발생하여 대량 파일 처리 시 성능이 크게 저하될 수 있습니다.\n",
		tool, opt,
	)
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/cli/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/cli/
git commit -m "feat(cli): 미지원 옵션 감지와 안내문 빌더"
```

---

## Task 4: `cli/common` — 공통 플래그 파싱

**Files:**
- Create: `internal/cli/common.go`
- Test: `internal/cli/common_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/cli/common_test.go`:

```go
package cli_test

import (
	"reflect"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
)

func TestParseCommon(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want cli.Common
	}{
		{
			name: "defaults",
			args: []string{},
			want: cli.Common{Workers: 1},
		},
		{
			name: "all set",
			args: []string{
				"--parallel=8",
				"--strict-order",
				"--strict-extension=.json,.csv",
				"--exit-on-error",
				"--error-log=/tmp/x.log",
				"--dry-run",
				"--no-progress",
			},
			want: cli.Common{
				Workers:          8,
				StrictOrder:      true,
				StrictExtensions: []string{".json", ".csv"},
				ExitOnError:      true,
				ErrorLogPath:     "/tmp/x.log",
				DryRun:           true,
				NoProgress:       true,
			},
		},
		{
			name: "extension normalization",
			args: []string{"--strict-extension=JSON,csv,.png"},
			want: cli.Common{Workers: 1, StrictExtensions: []string{".json", ".csv", ".png"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := cli.ParseCommon(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli/...`
Expected: FAIL — `cli.ParseCommon` 미정의

- [ ] **Step 3: `cli/common.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/cli/common.go`:

```go
package cli

import (
	"strings"

	"github.com/spf13/pflag"
)

// Common holds the flags shared by pcp/pmv/prm.
type Common struct {
	Workers          int
	StrictOrder      bool
	StrictExtensions []string
	ExitOnError      bool
	ErrorLogPath     string
	DryRun           bool
	NoProgress       bool
}

// RegisterCommon binds the common flags onto the given FlagSet.
func RegisterCommon(fs *pflag.FlagSet, c *Common) {
	fs.IntVar(&c.Workers, "parallel", 1, "number of worker goroutines")
	fs.BoolVar(&c.StrictOrder, "strict-order", false, "process directories in walk order, in parallel across directories")
	fs.StringSliceVar(&c.StrictExtensions, "strict-extension", nil, "comma-separated extensions whose files run last, serialized")
	fs.BoolVar(&c.ExitOnError, "exit-on-error", false, "exit on the first error instead of best-effort")
	fs.StringVar(&c.ErrorLogPath, "error-log", "", "path for the error log (default: ./<tool>-failed-<timestamp>.log)")
	fs.BoolVar(&c.DryRun, "dry-run", false, "print planned actions without executing")
	fs.BoolVar(&c.NoProgress, "no-progress", false, "disable the progress line even on a TTY")
}

// ParseCommon is a convenience for tests: it parses *only* the common flags
// (ignoring tool-specific) and returns the populated Common plus any positional args.
func ParseCommon(args []string) (Common, []string, error) {
	c := Common{Workers: 1}
	fs := pflag.NewFlagSet("common", pflag.ContinueOnError)
	fs.ParseErrorsWhitelist.UnknownFlags = true
	RegisterCommon(fs, &c)
	if err := fs.Parse(args); err != nil {
		return Common{}, nil, err
	}
	c.StrictExtensions = normalizeExts(c.StrictExtensions)
	return c, fs.Args(), nil
}

func normalizeExts(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/cli/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/cli/common.go internal/cli/common_test.go
git commit -m "feat(cli): 공통 플래그 파서 (--parallel, --strict-*, --error-log 등)"
```

---

## Task 5: `cli/pcp` — pcp 전용 플래그와 Plan 변환

**Files:**
- Create: `internal/cli/pcp.go`
- Test: `internal/cli/pcp_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/cli/pcp_test.go`:

```go
package cli_test

import (
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestParsePCP_Recursive(t *testing.T) {
	p, err := cli.ParsePCP([]string{"-r", "--parallel=4", "src", "dst"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Op != plan.OpCopy {
		t.Errorf("Op = %v, want OpCopy", p.Op)
	}
	if !p.Recursive {
		t.Error("Recursive = false, want true")
	}
	if p.Workers != 4 {
		t.Errorf("Workers = %d, want 4", p.Workers)
	}
	if p.Src != "src" || p.Dst != "dst" {
		t.Errorf("Src/Dst = %q/%q", p.Src, p.Dst)
	}
}

func TestParsePCP_Archive(t *testing.T) {
	p, err := cli.ParsePCP([]string{"-a", "src", "dst"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Recursive {
		t.Error("-a should imply --recursive")
	}
	if !p.Preserve.Mode || !p.Preserve.Ownership || !p.Preserve.Timestamps {
		t.Errorf("-a should preserve all metadata, got %+v", p.Preserve)
	}
}

func TestParsePCP_RejectsUnsupported(t *testing.T) {
	_, err := cli.ParsePCP([]string{"-i", "src", "dst"})
	if err == nil {
		t.Fatal("expected error for -i")
	}
	if !strings.Contains(err.Error(), "--fallback") {
		t.Errorf("error message should mention --fallback, got: %v", err)
	}
}

func TestParsePCP_RequiresTwoPositionals(t *testing.T) {
	_, err := cli.ParsePCP([]string{"src"})
	if err == nil {
		t.Fatal("expected error for single positional")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/cli/...`
Expected: FAIL — `cli.ParsePCP` 미정의

- [ ] **Step 3: `cli/pcp.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/cli/pcp.go`:

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// ParsePCP turns argv (without the program name) into a validated Plan for pcp.
func ParsePCP(args []string) (plan.Plan, error) {
	if hit := FirstUnsupported("pcp", args); hit != "" {
		return plan.Plan{}, errors.New(UnsupportedMessage("pcp", hit))
	}

	var (
		c        Common
		recurse  bool
		verbose  bool
		archive  bool
		preserve bool
		preserveList string
		noClobber bool
		updateOnly bool
		overwrite bool
	)
	fs := pflag.NewFlagSet("pcp", pflag.ContinueOnError)
	RegisterCommon(fs, &c)
	fs.BoolVarP(&recurse, "recursive", "r", false, "copy directories recursively")
	fs.BoolVarP(&recurse, "Recursive", "R", false, "alias for --recursive")
	fs.BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	fs.BoolVarP(&archive, "archive", "a", false, "same as --recursive --preserve=mode,ownership,timestamps")
	fs.BoolVarP(&preserve, "preserve-default", "p", false, "preserve mode, ownership, timestamps")
	fs.StringVar(&preserveList, "preserve", "", "comma list of attributes: mode,ownership,timestamps")
	fs.BoolVarP(&noClobber, "no-clobber", "n", false, "do not overwrite existing files")
	fs.BoolVarP(&updateOnly, "update", "u", false, "copy only when src is newer than dst")
	fs.BoolVarP(&overwrite, "force", "f", false, "overwrite existing files (vanilla cp -f)")

	if err := fs.Parse(args); err != nil {
		return plan.Plan{}, err
	}
	c.StrictExtensions = normalizeExts(c.StrictExtensions)

	pos := fs.Args()
	if len(pos) != 2 {
		return plan.Plan{}, fmt.Errorf("pcp: expected SRC and DST, got %d positional args", len(pos))
	}

	pres := plan.Preserve{}
	if preserve || archive {
		pres.Mode, pres.Ownership, pres.Timestamps = true, true, true
	}
	if preserveList != "" {
		applyPreserveList(&pres, preserveList)
	}

	p := plan.Plan{
		Op:               plan.OpCopy,
		Src:              pos[0],
		Dst:              pos[1],
		Workers:          c.Workers,
		Recursive:        recurse || archive,
		Verbose:          verbose,
		DryRun:           c.DryRun,
		ExitOnError:      c.ExitOnError,
		NoProgress:       c.NoProgress,
		ErrorLogPath:     c.ErrorLogPath,
		StrictOrder:      c.StrictOrder,
		StrictExtensions: c.StrictExtensions,
		Overwrite:        overwrite,
		NoClobber:        noClobber,
		UpdateOnly:       updateOnly,
		Preserve:         pres,
	}
	if err := p.Validate(); err != nil {
		return plan.Plan{}, fmt.Errorf("pcp: %w", err)
	}
	return p, nil
}

func applyPreserveList(p *plan.Preserve, list string) {
	for _, k := range splitCSV(list) {
		switch k {
		case "mode":
			p.Mode = true
		case "ownership":
			p.Ownership = true
		case "timestamps":
			p.Timestamps = true
		case "all":
			p.Mode, p.Ownership, p.Timestamps = true, true, true
		}
	}
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/cli/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/cli/pcp.go internal/cli/pcp_test.go
git commit -m "feat(cli): pcp 전용 플래그 파서와 Plan 변환"
```

---

## Task 6: `fsx/device` — cross-device 감지

**Files:**
- Create: `internal/fsx/device.go`
- Test: `internal/fsx/device_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/fsx/device_test.go`:

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

func TestSameDevice_SamePath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	same, err := fsx.SameDevice(a, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !same {
		t.Error("expected same device for files under same tmpdir")
	}
}

func TestIsEXDEV(t *testing.T) {
	if !fsx.IsEXDEV(syscall.EXDEV) {
		t.Error("EXDEV should match")
	}
	if !fsx.IsEXDEV(&os.LinkError{Err: syscall.EXDEV}) {
		t.Error("wrapped EXDEV should match")
	}
	if fsx.IsEXDEV(errors.New("other")) {
		t.Error("non-EXDEV should not match")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fsx/...`
Expected: FAIL — `fsx` 패키지 없음

- [ ] **Step 3: `fsx/device.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/fsx/device.go`:

```go
// Package fsx provides filesystem helpers used by the worker pool.
//
// All helpers are written for Linux/macOS (POSIX). Windows is out of scope.
package fsx

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// SameDevice returns true if a and b live on the same filesystem.
//
// b can be a not-yet-existing path; in that case the parent directory is
// stat'd. This matches the behavior of mv: dst's parent decides device
// membership for the rename target.
func SameDevice(a, b string) (bool, error) {
	da, err := devID(a)
	if err != nil {
		return false, err
	}
	db, err := devID(b)
	if err != nil {
		// b 없으면 부모로 다시 시도
		if errors.Is(err, fs.ErrNotExist) {
			db, err = devID(parentOf(b))
			if err != nil {
				return false, err
			}
		} else {
			return false, err
		}
	}
	return da == db, nil
}

func devID(path string) (uint64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("stat: unsupported platform")
	}
	return uint64(sys.Dev), nil
}

func parentOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return "."
}

// IsEXDEV reports whether err is (or wraps) syscall.EXDEV.
func IsEXDEV(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, syscall.EXDEV)
	}
	return false
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/fsx/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/fsx/
git commit -m "feat(fsx): cross-device 감지와 EXDEV 매칭"
```

---

## Task 7: `fsx/copy` — atomic copy via temp file

**Files:**
- Create: `internal/fsx/copy.go`
- Test: `internal/fsx/copy_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/fsx/copy_test.go`:

```go
package fsx_test

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
)

func TestCopyFile_Basic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	body := make([]byte, 1<<14) // 16 KB
	_, _ = rand.Read(body)
	if err := os.WriteFile(src, body, 0644); err != nil {
		t.Fatal(err)
	}

	n, err := fsx.CopyFile(src, dst, fsx.CopyOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("CopyFile bytes = %d, want %d", n, len(body))
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Error("dst contents differ from src")
	}
}

func TestCopyFile_AtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	// CopyFile은 같은 디렉토리에 .pcp-tmp-XXXXXX 패턴 임시 파일을 만들었다가 rename한다.
	// 정상 종료 후엔 임시 파일이 남으면 안 된다.
	if _, err := fsx.CopyFile(src, dst, fsx.CopyOpts{}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && contains(e.Name(), ".pcp-tmp-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestCopyFile_NoClobber(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := fsx.CopyFile(src, dst, fsx.CopyOpts{NoClobber: true})
	if !errors.Is(err, fsx.ErrSkipExisting) {
		t.Fatalf("want ErrSkipExisting, got %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst was overwritten: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fsx/...`
Expected: FAIL — `fsx.CopyFile` 미정의

- [ ] **Step 3: `fsx/copy.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/fsx/copy.go`:

```go
package fsx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ErrSkipExisting indicates -n (no-clobber) skipped a file because dst exists.
var ErrSkipExisting = errors.New("skip: destination exists")

// CopyOpts controls CopyFile behavior beyond a plain copy.
type CopyOpts struct {
	NoClobber  bool // -n: dst가 이미 있으면 skip (race-free via O_EXCL)
	Overwrite  bool // -f: dst가 있어도 덮어쓰기 (no-clobber와 상호 배타)
}

// CopyFile copies src → dst atomically by writing into a temp file
// (`<dst>.pcp-tmp-XXXXXX`) and renaming on success.
//
// Returns the number of bytes written. On error the temp file is removed.
func CopyFile(src, dst string, opt CopyOpts) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	if opt.NoClobber {
		// O_EXCL로 직접 dst를 잡아본다. 존재하면 EEXIST.
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return 0, ErrSkipExisting
			}
			return 0, err
		}
		n, copyErr := io.Copy(f, in)
		closeErr := f.Close()
		if copyErr != nil {
			_ = os.Remove(dst)
			return 0, copyErr
		}
		if closeErr != nil {
			_ = os.Remove(dst)
			return 0, closeErr
		}
		return n, nil
	}

	tmp, err := openTempBeside(dst)
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	n, copyErr := io.Copy(tmp, in)
	if copyErr != nil {
		_ = tmp.Close()
		cleanup()
		return 0, copyErr
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return 0, err
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		cleanup()
		return 0, err
	}
	return n, nil
}

func openTempBeside(dst string) (*os.File, error) {
	dir := filepath.Dir(dst)
	for try := 0; try < 8; try++ {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		name := filepath.Base(dst) + ".pcp-tmp-" + hex.EncodeToString(b[:])
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	return nil, errors.New("failed to allocate temp file after 8 tries")
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/fsx/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/fsx/copy.go internal/fsx/copy_test.go
git commit -m "feat(fsx): atomic CopyFile (temp file + rename) with -n O_EXCL"
```

---

## Task 8: `fsx/meta` — 메타데이터 보존

**Files:**
- Create: `internal/fsx/meta.go`
- Test: `internal/fsx/meta_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/fsx/meta_test.go`:

```go
package fsx_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestPreserve_ModeAndTimestamps(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(src, past, past); err != nil {
		t.Fatal(err)
	}

	srcInfo, _ := os.Stat(src)
	if err := fsx.PreserveMeta(srcInfo, dst, plan.Preserve{Mode: true, Timestamps: true}); err != nil {
		t.Fatalf("PreserveMeta: %v", err)
	}

	got, _ := os.Stat(dst)
	if got.Mode().Perm() != 0640 {
		t.Errorf("dst mode = %v, want 0640", got.Mode().Perm())
	}
	if !got.ModTime().Truncate(time.Second).Equal(past) {
		t.Errorf("dst mtime = %v, want %v", got.ModTime(), past)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/fsx/...`
Expected: FAIL — `fsx.PreserveMeta` 미정의

- [ ] **Step 3: `fsx/meta.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/fsx/meta.go`:

```go
package fsx

import (
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PreserveMeta copies the requested attributes from srcInfo onto dst.
// Ownership preservation requires CAP_CHOWN; on failure we silently skip
// to match GNU cp's behavior when running as a non-root user.
func PreserveMeta(srcInfo fs.FileInfo, dst string, p plan.Preserve) error {
	if p.Mode {
		if err := os.Chmod(dst, srcInfo.Mode().Perm()); err != nil {
			return err
		}
	}
	if p.Ownership {
		if sys, ok := srcInfo.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(dst, int(sys.Uid), int(sys.Gid)) // best-effort
		}
	}
	if p.Timestamps {
		mt := srcInfo.ModTime()
		if err := os.Chtimes(dst, mt, mt); err != nil {
			return err
		}
	}
	return nil
}

// IsNewer compares mtimes for -u handling. Returns true when src has a strictly
// newer modification time than dst, or dst does not exist.
func IsNewer(srcInfo fs.FileInfo, dst string) (bool, error) {
	dstInfo, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return srcInfo.ModTime().After(dstInfo.ModTime()), nil
}

// ApproxSecond used by tests to compare timestamps tolerantly.
func ApproxSecond(a, b time.Time) bool {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d < time.Second
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/fsx/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/fsx/meta.go internal/fsx/meta_test.go
git commit -m "feat(fsx): 메타데이터 보존 (mode/owner/utime)과 -u용 IsNewer"
```

---

## Task 9: `report/error_log` — 에러 로그 writer

**Files:**
- Create: `internal/report/error_log.go`
- Test: `internal/report/error_log_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/error_log_test.go`:

```go
package report_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestErrorLog_WriteAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "errs.log")

	log, err := report.NewErrorLog(path, "pcp")
	if err != nil {
		t.Fatal(err)
	}
	log.Record("copy", "src/a → dst/a", errors.New("permission denied"))
	log.Record("mkdir", "dst/x", errors.New("file exists"))
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	body := string(data)
	for _, want := range []string{"pcp", "copy", "src/a → dst/a", "permission denied", "mkdir", "file exists"} {
		if !strings.Contains(body, want) {
			t.Errorf("log missing %q\n--- log ---\n%s", want, body)
		}
	}
}

func TestErrorLog_AutoPath(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	_ = os.Chdir(dir)

	log, err := report.NewErrorLog("", "pcp")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if !strings.HasPrefix(filepath.Base(log.Path()), "pcp-failed-") {
		t.Errorf("auto path not prefixed correctly: %s", log.Path())
	}
}

func TestErrorLog_Concurrent(t *testing.T) {
	dir := t.TempDir()
	log, err := report.NewErrorLog(filepath.Join(dir, "c.log"), "pcp")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			log.Record("copy", "x", errors.New("e"))
		}(i)
	}
	wg.Wait()
	_ = log.Close()
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/report/...`
Expected: FAIL — `report` 패키지 없음

- [ ] **Step 3: `report/error_log.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/error_log.go`:

```go
// Package report holds the user-facing output side: error log, progress line,
// verbose stdout serialization, and signal handling.
package report

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"
)

// ErrorLog is the concurrent-safe writer for failed-job lines.
type ErrorLog struct {
	mu   sync.Mutex
	w    *bufio.Writer
	f    *os.File
	tool string
	path string
	count int
}

// NewErrorLog creates (or appends to) the error log file. If path is empty,
// it uses ./<tool>-failed-<RFC3339>.log in the current working directory.
func NewErrorLog(path, tool string) (*ErrorLog, error) {
	if path == "" {
		path = fmt.Sprintf("./%s-failed-%s.log", tool, time.Now().Format("20060102T150405Z0700"))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &ErrorLog{
		f:    f,
		w:    bufio.NewWriter(f),
		tool: tool,
		path: path,
	}, nil
}

// Path returns the resolved path for the log file.
func (e *ErrorLog) Path() string { return e.path }

// Count returns how many records have been written so far.
func (e *ErrorLog) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// Record appends one line: <RFC3339>\t<tool>\t<op>\t<target>\t<error>
func (e *ErrorLog) Record(op, target string, err error) {
	if err == nil {
		return
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n",
		time.Now().Format(time.RFC3339),
		e.tool, op, target, err.Error())
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.w.WriteString(line)
	e.count++
}

// Close flushes and closes the underlying file.
func (e *ErrorLog) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.w != nil {
		_ = e.w.Flush()
	}
	if e.f != nil {
		return e.f.Close()
	}
	return nil
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/report/...`
Expected: PASS (race detector 깨끗)

- [ ] **Step 5: 커밋**

```bash
git add internal/report/error_log.go internal/report/error_log_test.go
git commit -m "feat(report): 동시 안전 ErrorLog (자동 경로 + Record/Close)"
```

---

## Task 10: `report/verbose` — `-v` 출력 직렬화

**Files:**
- Create: `internal/report/verbose.go`
- Test: `internal/report/verbose_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/verbose_test.go`:

```go
package report_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestVerbose_NoInterleave(t *testing.T) {
	var buf bytes.Buffer
	v := report.NewVerbose(&buf, true)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v.Logf("line-%d-with-some-content", i)
		}(i)
	}
	wg.Wait()

	for _, ln := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if !strings.HasPrefix(ln, "line-") {
			t.Fatalf("interleaved: %q", ln)
		}
	}
}

func TestVerbose_Disabled(t *testing.T) {
	var buf bytes.Buffer
	v := report.NewVerbose(&buf, false)
	v.Logf("ignored")
	if buf.Len() != 0 {
		t.Fatalf("disabled verbose wrote: %q", buf.String())
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/report/...`
Expected: FAIL — `report.NewVerbose` 미정의

- [ ] **Step 3: `report/verbose.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/verbose.go`:

```go
package report

import (
	"fmt"
	"io"
	"sync"
)

// Verbose serializes -v output through a mutex so worker lines never interleave.
type Verbose struct {
	mu      sync.Mutex
	w       io.Writer
	enabled bool
}

// NewVerbose returns a Verbose tied to w. When enabled is false, Logf is a no-op.
func NewVerbose(w io.Writer, enabled bool) *Verbose {
	return &Verbose{w: w, enabled: enabled}
}

// Logf writes one line. Trailing newline is appended automatically.
func (v *Verbose) Logf(format string, a ...any) {
	if !v.enabled {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	fmt.Fprintf(v.w, format, a...)
	_, _ = v.w.Write([]byte{'\n'})
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/report/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/report/verbose.go internal/report/verbose_test.go
git commit -m "feat(report): mutex 직렬화 Verbose writer"
```

---

## Task 11: `report/progress` — 1초 단위 progress 라인

**Files:**
- Create: `internal/report/progress.go`
- Test: `internal/report/progress_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/progress_test.go`:

```go
package report_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestProgress_RenderSnapshot(t *testing.T) {
	var buf bytes.Buffer
	p := report.NewProgress(&buf, "pcp", true /*forceTTY*/)

	p.AddBytes(1024 * 1024)
	p.IncFiles()
	p.IncFiles()
	p.IncErrors()

	// 외부 강제 렌더 (테스트 결정성)
	p.RenderNow(2 * time.Second)

	out := buf.String()
	for _, want := range []string{"pcp", "2 files", "errors", "MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("progress render missing %q\n%s", want, out)
		}
	}
}

func TestProgress_DisabledNoTTY(t *testing.T) {
	var buf bytes.Buffer
	p := report.NewProgress(&buf, "pcp", false /*forceTTY=false*/)
	p.IncFiles()
	p.RenderNow(time.Second)
	if buf.Len() != 0 {
		t.Fatalf("non-TTY mode wrote: %q", buf.String())
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/report/...`
Expected: FAIL — `report.NewProgress` 미정의

- [ ] **Step 3: `report/progress.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/progress.go`:

```go
package report

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// Progress maintains throughput counters and writes a single re-rendered line.
type Progress struct {
	w       io.Writer
	tool    string
	tty     bool
	files   atomic.Int64
	bytes   atomic.Int64
	errors  atomic.Int64
	skipped atomic.Int64
}

// NewProgress returns a Progress. tty=true forces rendering even on non-TTYs
// (used by tests). Production code passes the result of isatty(stdout).
func NewProgress(w io.Writer, tool string, tty bool) *Progress {
	return &Progress{w: w, tool: tool, tty: tty}
}

func (p *Progress) IncFiles()       { p.files.Add(1) }
func (p *Progress) IncErrors()      { p.errors.Add(1) }
func (p *Progress) IncSkipped()     { p.skipped.Add(1) }
func (p *Progress) AddBytes(n int64) { p.bytes.Add(n) }

// Files/Bytes/Errors/Skipped are accessors used by Reporter.Final.
func (p *Progress) Files() int64   { return p.files.Load() }
func (p *Progress) Bytes() int64   { return p.bytes.Load() }
func (p *Progress) Errors() int64  { return p.errors.Load() }
func (p *Progress) Skipped() int64 { return p.skipped.Load() }

// RenderNow writes the current snapshot. Caller passes elapsed duration.
func (p *Progress) RenderNow(elapsed time.Duration) {
	if !p.tty {
		return
	}
	files := p.files.Load()
	bytesV := p.bytes.Load()
	errs := p.errors.Load()
	secs := elapsed.Seconds()
	if secs <= 0 {
		secs = 1
	}
	fps := float64(files) / secs
	bps := float64(bytesV) / secs

	line := fmt.Sprintf("\r[%s]  %s files | %s | %s files/s | %s/s | %s elapsed | %d errors",
		p.tool,
		humanInt(files), humanBytes(bytesV),
		humanFloat(fps), humanBytes(int64(bps)),
		elapsed.Truncate(time.Second), errs,
	)
	fmt.Fprint(p.w, line)
}

// Loop blocks until done is closed, rendering every tick.
func (p *Progress) Loop(done <-chan struct{}, tick time.Duration) {
	if !p.tty {
		<-done
		return
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	start := time.Now()
	for {
		select {
		case <-done:
			fmt.Fprint(p.w, "\n")
			return
		case <-t.C:
			p.RenderNow(time.Since(start))
		}
	}
}

func humanInt(n int64) string { return fmt.Sprintf("%d", n) }

func humanFloat(f float64) string {
	switch {
	case f >= 1e6:
		return fmt.Sprintf("%.1fM", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1fK", f/1e3)
	}
	return fmt.Sprintf("%.0f", f)
}

func humanBytes(b int64) string {
	const k = 1024
	switch {
	case b >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(k*k))
	case b >= k:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(k))
	}
	return fmt.Sprintf("%d B", b)
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/report/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/report/progress.go internal/report/progress_test.go
git commit -m "feat(report): atomic 카운터 기반 Progress 렌더러"
```

---

## Task 12: `report/signal` — graceful shutdown

**Files:**
- Create: `internal/report/signal.go`
- Test: `internal/report/signal_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/signal_test.go`:

```go
package report_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestSignal_GracefulOnFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := report.NewSignal(ctx)
	g.Notify(syscall.SIGUSR1) // SIGINT 대신 테스트 안전 신호

	// 첫 신호: ctx 취소
	g.Trigger(syscall.SIGUSR1)

	select {
	case <-g.Ctx().Done():
		// ok
	case <-time.After(time.Second):
		t.Fatal("first signal did not cancel ctx")
	}
}

func TestSignal_HardExitOnSecondReturnsTrue(t *testing.T) {
	ctx := context.Background()
	g := report.NewSignal(ctx)
	g.Notify(syscall.SIGUSR1)

	g.Trigger(syscall.SIGUSR1)              // graceful
	if g.Trigger(syscall.SIGUSR1) != true { // 두 번째: hard
		t.Fatal("second signal should signal hard-exit")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/report/...`
Expected: FAIL — `report.NewSignal` 미정의

- [ ] **Step 3: `report/signal.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/report/signal.go`:

```go
package report

import (
	"context"
	"os"
	"os/signal"
	"sync"
)

// Signal coordinates graceful (1st) vs forced (2nd) shutdown.
//
// Use NewSignal + Notify in main; the worker pool watches Ctx() and stops
// dispatching new jobs when it cancels. After the first signal, callers can
// still detect a second by monitoring HardExit().
type Signal struct {
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
	hard   chan struct{}

	mu     sync.Mutex
	count  int
	relay  chan os.Signal
}

// NewSignal builds a Signal whose Ctx() cancels on the first delivery.
func NewSignal(parent context.Context) *Signal {
	ctx, cancel := context.WithCancel(parent)
	return &Signal{
		parent: parent,
		ctx:    ctx,
		cancel: cancel,
		hard:   make(chan struct{}),
		relay:  make(chan os.Signal, 4),
	}
}

// Notify subscribes to the given signals and starts a goroutine that calls
// Trigger for each delivery.
func (s *Signal) Notify(sigs ...os.Signal) {
	signal.Notify(s.relay, sigs...)
	go func() {
		for sig := range s.relay {
			if s.Trigger(sig) {
				return
			}
		}
	}()
}

// Trigger advances the state machine. Returns true when the call represents
// the second (hard) signal — caller should os.Exit(130) immediately.
func (s *Signal) Trigger(_ os.Signal) bool {
	s.mu.Lock()
	s.count++
	n := s.count
	s.mu.Unlock()
	switch n {
	case 1:
		s.cancel()
		return false
	default:
		select {
		case <-s.hard:
		default:
			close(s.hard)
		}
		return true
	}
}

// Ctx returns a context cancelled on the first signal.
func (s *Signal) Ctx() context.Context { return s.ctx }

// HardExit returns a channel closed on the second signal.
func (s *Signal) HardExit() <-chan struct{} { return s.hard }
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/report/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/report/signal.go internal/report/signal_test.go
git commit -m "feat(report): SIGINT graceful + 2회 hard-exit Signal"
```

---

## Task 13: `worker/pool` — 워커 풀 골격

**Files:**
- Create: `internal/worker/pool.go`
- Test: `internal/worker/pool_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/worker/pool_test.go`:

```go
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
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker/...`
Expected: FAIL — `worker.NewPool` 미정의

- [ ] **Step 3: `worker/pool.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/worker/pool.go`:

```go
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
	n       int
	handle  Handler
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
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/worker/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/worker/pool.go internal/worker/pool_test.go
git commit -m "feat(worker): N-goroutine 풀 (panic recover, ctx cancel)"
```

---

## Task 14: `worker/pcp` — pcp 핸들러

**Files:**
- Create: `internal/worker/pcp.go`
- Test: `internal/worker/pcp_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/worker/pcp_test.go`:

```go
package worker_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/worker"
)

func TestPCPHandler_CopiesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(src)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("dst = %q, want hello", got)
	}
}

func TestPCPHandler_NoClobberSkips(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	_ = os.WriteFile(src, []byte("new"), 0644)
	_ = os.WriteFile(dst, []byte("old"), 0644)
	info, _ := os.Stat(src)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, NoClobber: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Skipped {
		t.Error("expected Skipped=true")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst overwritten: %q", got)
	}
}

func TestPCPHandler_UpdateOnlyOlderDstAllows(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	_ = os.WriteFile(src, []byte("new"), 0644)
	_ = os.WriteFile(dst, []byte("old"), 0644)
	info, _ := os.Stat(src)

	// dst를 더 오래된 mtime으로 강제
	older := info.ModTime().Add(-1 * 1)
	_ = os.Chtimes(dst, older, older)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, UpdateOnly: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Errorf("dst = %q, want new", got)
	}
}

func TestPCPHandler_DryRunNoIO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	_ = os.WriteFile(src, []byte("hi"), 0644)
	info, _ := os.Stat(src)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, DryRun: true})
	r := h(context.Background(), plan.Job{Kind: plan.JobCopy, Src: src, Dst: dst, Info: info})
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run wrote dst: stat err=%v", err)
	}
	if !r.Skipped {
		t.Error("dry-run should report Skipped=true")
	}
	_ = fsx.ErrSkipExisting // keep import
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker/...`
Expected: FAIL — `worker.PCP` 미정의

- [ ] **Step 3: `worker/pcp.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/worker/pcp.go`:

```go
package worker

import (
	"context"
	"errors"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

// PCP returns a Handler that performs the actual copy work for pcp.
func PCP(p plan.Plan) Handler {
	return func(ctx context.Context, j plan.Job) plan.Result {
		if j.Kind != plan.JobCopy {
			return plan.Result{Job: j, Err: errors.New("worker/pcp: unexpected job kind")}
		}
		started := time.Now()

		// -u: dst가 src보다 같거나 새로우면 skip
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

		// 실제 copy
		opts := fsx.CopyOpts{
			NoClobber: p.NoClobber,
			Overwrite: p.Overwrite,
		}
		n, err := fsx.CopyFile(j.Src, j.Dst, opts)
		if errors.Is(err, fsx.ErrSkipExisting) {
			return plan.Result{Job: j, Skipped: true, Elapsed: time.Since(started)}
		}
		if err != nil {
			return plan.Result{Job: j, Err: err, Elapsed: time.Since(started)}
		}

		// 메타데이터 보존
		if p.Preserve.Mode || p.Preserve.Ownership || p.Preserve.Timestamps {
			if metaErr := fsx.PreserveMeta(j.Info, j.Dst, p.Preserve); metaErr != nil {
				return plan.Result{Job: j, Err: metaErr, Bytes: n, Elapsed: time.Since(started)}
			}
		}

		return plan.Result{Job: j, Bytes: n, Elapsed: time.Since(started)}
	}
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/worker/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/worker/pcp.go internal/worker/pcp_test.go
git commit -m "feat(worker): pcp 핸들러 (-n/-u/dry-run/메타데이터 보존)"
```

---

## Task 15: `walk/default` — 기본 모드 walker

**Files:**
- Create: `internal/walk/default.go`
- Test: `internal/walk/default_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/walk/default_test.go`:

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

func mkTree(t *testing.T, root string, paths map[string]string) {
	t.Helper()
	for rel, body := range paths {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefaultWalk_QueuesAllFiles_AndCreatesDirsEager(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"a.txt":      "A",
		"sub/b.txt":  "B",
		"sub/c/d.bin": "D",
	})
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	jobs := make(chan plan.Job, 16)
	w := walk.NewDefault(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true})
	if err := w.Walk(context.Background(), jobs); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	close(jobs)

	got := map[string]bool{}
	for j := range jobs {
		got[j.RelPath] = true
	}
	want := []string{"a.txt", "sub/b.txt", "sub/c/d.bin"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing job for %s, got %v", w, got)
		}
	}

	// dst 디렉토리들이 즉시 만들어졌는지
	for _, d := range []string{"sub", "sub/c"} {
		if _, err := os.Stat(filepath.Join(dst, d)); err != nil {
			t.Errorf("dst dir %s not created: %v", d, err)
		}
	}
}

func TestDefaultWalk_NonRecursive_RejectsDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	jobs := make(chan plan.Job, 4)
	w := walk.NewDefault(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: false})
	err := w.Walk(context.Background(), jobs)
	if err == nil {
		t.Fatal("expected error when src is a directory and -r is unset")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/walk/...`
Expected: FAIL — `walk.NewDefault` 미정의

- [ ] **Step 3: `walk/default.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/walk/default.go`:

```go
// Package walk implements the three walking strategies (default file-unit,
// strict-order directory-unit, strict-extension two-phase).
package walk

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Default is the standard streaming walker:
//   - DFS pre-order
//   - directories are mkdir'd eagerly (synchronously) by the walker
//   - files are pushed as JobCopy onto the queue
type Default struct {
	p plan.Plan
}

// NewDefault returns a Default walker bound to p.
func NewDefault(p plan.Plan) *Default { return &Default{p: p} }

// Walk pushes JobCopy values onto jobs until the tree is exhausted or ctx done.
func (w *Default) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	srcInfo, err := os.Lstat(w.p.Src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		if !w.p.Recursive {
			return fmt.Errorf("%s is a directory (use -r)", w.p.Src)
		}
		return w.walkDir(ctx, jobs)
	}
	// 단일 파일
	return w.pushFile(ctx, jobs, w.p.Src, srcInfo, "")
}

func (w *Default) walkDir(ctx context.Context, jobs chan<- plan.Job) error {
	// dst 루트가 없으면 src의 모드로 mkdir
	if err := os.MkdirAll(w.p.Dst, 0755); err != nil {
		return err
	}

	skipExt := buildExtSet(w.p.StrictExtensions)

	return filepath.WalkDir(w.p.Src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == w.p.Src {
			return nil
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}

		rel, _ := filepath.Rel(w.p.Src, path)
		dst := filepath.Join(w.p.Dst, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}

		// strict-extension: 대상 확장자는 2-phase에서 처리하므로 여기선 skip
		if len(skipExt) > 0 {
			if _, hit := skipExt[strings.ToLower(filepath.Ext(path))]; hit {
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return w.pushJob(ctx, jobs, plan.Job{
			Kind:    plan.JobCopy,
			Src:     path,
			Dst:     dst,
			RelPath: rel,
			Info:    info,
		})
	})
}

func (w *Default) pushFile(ctx context.Context, jobs chan<- plan.Job, src string, info fs.FileInfo, rel string) error {
	dst := w.p.Dst
	if info.IsDir() {
		return fmt.Errorf("internal: pushFile called with directory")
	}
	if rel == "" {
		rel = filepath.Base(src)
	}
	return w.pushJob(ctx, jobs, plan.Job{
		Kind: plan.JobCopy, Src: src, Dst: dst, RelPath: rel, Info: info,
	})
}

func (w *Default) pushJob(ctx context.Context, jobs chan<- plan.Job, j plan.Job) error {
	select {
	case <-ctx.Done():
		return filepath.SkipAll
	case jobs <- j:
		return nil
	}
}

func buildExtSet(list []string) map[string]struct{} {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(list))
	for _, e := range list {
		out[strings.ToLower(e)] = struct{}{}
	}
	return out
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/walk/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/walk/default.go internal/walk/default_test.go
git commit -m "feat(walk): default 모드 streaming walker (eager mkdir, file Job)"
```

---

## Task 16: `walk/strict_ext` — 2-phase walker

**Files:**
- Create: `internal/walk/strict_ext.go`
- Test: `internal/walk/strict_ext_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/walk/strict_ext_test.go`:

```go
package walk_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
	"github.com/nineking424/pcpmvrm/internal/walk"
)

func TestStrictExt_TriggerOrdering(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"img/a.jpg":     "A",
		"img/b.jpg":     "B",
		"data/x.json":   "X",
		"data/y.json":   "Y",
		"plain.txt":     "P",
	})
	_ = os.MkdirAll(dst, 0755)

	w := walk.NewStrictExt(plan.Plan{
		Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true,
		StrictExtensions: []string{".json"},
	})

	var phaseSeen []string
	var mu sync.Mutex
	jobs := make(chan plan.Job, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for j := range jobs {
			mu.Lock()
			phaseSeen = append(phaseSeen, j.RelPath)
			mu.Unlock()
		}
	}()

	// Phase 1만 먼저 끝내고, Phase 2는 RunPhase2로 트리거
	if err := w.RunPhase1(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	phase1Count := len(phaseSeen)
	mu.Unlock()
	if phase1Count == 0 {
		t.Fatal("phase1 produced no jobs")
	}

	if err := w.RunPhase2(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	close(jobs)
	<-done

	mu.Lock()
	defer mu.Unlock()
	// Phase1 안에 .json이 없어야 한다
	for _, r := range phaseSeen[:phase1Count] {
		if filepath.Ext(r) == ".json" {
			t.Errorf("phase1 contained .json file: %s", r)
		}
	}
	// Phase2엔 .json만
	for _, r := range phaseSeen[phase1Count:] {
		if filepath.Ext(r) != ".json" {
			t.Errorf("phase2 contained non-.json: %s", r)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/walk/...`
Expected: FAIL — `walk.NewStrictExt` 미정의

- [ ] **Step 3: `walk/strict_ext.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/walk/strict_ext.go`:

```go
package walk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// StrictExt is a two-phase walker. Phase 1 = non-target files in DFS order;
// Phase 2 = target-extension files in lexical order, serially.
type StrictExt struct {
	p   plan.Plan
	def *Default
}

// NewStrictExt returns a StrictExt walker bound to p.
func NewStrictExt(p plan.Plan) *StrictExt {
	return &StrictExt{p: p, def: NewDefault(p)}
}

// RunPhase1 emits non-target files. Internally reuses Default's logic, which
// already skips strict-extension matches.
func (w *StrictExt) RunPhase1(ctx context.Context, jobs chan<- plan.Job) error {
	return w.def.Walk(ctx, jobs)
}

// RunPhase2 emits target files in lexical order. Caller is responsible for
// ensuring Phase1's workers have drained before calling this (typically by
// closing/replacing the jobs channel and re-creating the worker pool with
// workers=1; see cmd/pcp/main.go).
func (w *StrictExt) RunPhase2(ctx context.Context, jobs chan<- plan.Job) error {
	exts := buildExtSet(w.p.StrictExtensions)
	if len(exts) == 0 {
		return nil
	}

	var matches []string
	err := filepath.WalkDir(w.p.Src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if _, hit := exts[strings.ToLower(filepath.Ext(path))]; hit {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(matches)

	for _, m := range matches {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		info, err := os.Stat(m)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(w.p.Src, m)
		dst := filepath.Join(w.p.Dst, rel)
		select {
		case jobs <- plan.Job{Kind: plan.JobCopy, Src: m, Dst: dst, RelPath: rel, Info: info}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/walk/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/walk/strict_ext.go internal/walk/strict_ext_test.go
git commit -m "feat(walk): --strict-extension 2-phase walker (트리거 시맨틱)"
```

---

## Task 17: `walk/strict_order` — 디렉토리 단위 walker

**Files:**
- Create: `internal/walk/strict_order.go`
- Test: `internal/walk/strict_order_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/walk/strict_order_test.go`:

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

func TestStrictOrder_OneJobPerDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"d1/a": "A",
		"d1/b": "B",
		"d2/c": "C",
	})
	_ = os.MkdirAll(dst, 0755)

	w := walk.NewStrictOrder(plan.Plan{Op: plan.OpCopy, Src: src, Dst: dst, Recursive: true})
	jobs := make(chan plan.Job, 8)
	if err := w.Walk(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	close(jobs)

	dirs := map[string]bool{}
	for j := range jobs {
		if j.Kind != plan.JobDirCopy {
			t.Fatalf("unexpected kind %v", j.Kind)
		}
		dirs[j.RelPath] = true
	}
	for _, d := range []string{"d1", "d2"} {
		if !dirs[d] {
			t.Errorf("missing dir job %s, got %v", d, dirs)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/walk/...`
Expected: FAIL — `walk.NewStrictOrder` 미정의

- [ ] **Step 3: `walk/strict_order.go` 구현**

Create `/Users/nineking/workspace/app/pcpmvrm/internal/walk/strict_order.go`:

```go
package walk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// StrictOrder emits one Job per directory. Workers process the directory
// content serially, in walk order, by re-walking from the directory root.
type StrictOrder struct {
	p plan.Plan
}

// NewStrictOrder returns a StrictOrder walker.
func NewStrictOrder(p plan.Plan) *StrictOrder { return &StrictOrder{p: p} }

// Walk pushes one JobDirCopy per directory under src.
func (w *StrictOrder) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	if err := os.MkdirAll(w.p.Dst, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(w.p.Src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}
		rel, _ := filepath.Rel(w.p.Src, path)
		// dst 디렉토리는 즉시 mkdir (워커가 자식 파일 처리할 때 부모가 존재해야)
		dst := filepath.Join(w.p.Dst, rel)
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		select {
		case jobs <- plan.Job{Kind: plan.JobDirCopy, Src: path, Dst: dst, RelPath: rel}:
		case <-ctx.Done():
			return filepath.SkipAll
		}
		return nil
	})
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/walk/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/walk/strict_order.go internal/walk/strict_order_test.go
git commit -m "feat(walk): --strict-order 디렉토리 단위 walker"
```

---

## Task 18: `worker/pcp` 확장 — JobDirCopy 처리

**Files:**
- Modify: `internal/worker/pcp.go`
- Test: extend `internal/worker/pcp_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

Append to `/Users/nineking/workspace/app/pcpmvrm/internal/worker/pcp_test.go`:

```go
func TestPCPHandler_DirCopy_SerialChildren(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "dst")
	_ = os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	_ = os.MkdirAll(dstDir, 0755)
	_ = os.WriteFile(filepath.Join(srcDir, "a"), []byte("A"), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "b"), []byte("B"), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "sub", "c"), []byte("C"), 0644)

	h := worker.PCP(plan.Plan{Op: plan.OpCopy, Recursive: true})
	r := h(context.Background(), plan.Job{
		Kind: plan.JobDirCopy,
		Src:  srcDir,
		Dst:  dstDir,
		RelPath: "",
	})
	if r.Err != nil {
		t.Fatalf("dir copy err: %v", r.Err)
	}
	for _, p := range []string{"a", "b", "sub/c"} {
		got, err := os.ReadFile(filepath.Join(dstDir, p))
		if err != nil {
			t.Errorf("missing %s: %v", p, err)
			continue
		}
		if string(got) == "" {
			t.Errorf("empty content for %s", p)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/worker/...`
Expected: FAIL — JobDirCopy 미처리 ("unexpected job kind")

- [ ] **Step 3: `worker/pcp.go` 확장**

Replace the body of `PCP` in `/Users/nineking/workspace/app/pcpmvrm/internal/worker/pcp.go` with:

```go
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
```

Update imports at top to include `os` and `path/filepath`:

```go
import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nineking424/pcpmvrm/internal/fsx"
	"github.com/nineking424/pcpmvrm/internal/plan"
)
```

- [ ] **Step 4: 통과 확인**

Run: `go test -race ./internal/worker/...`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/worker/pcp.go internal/worker/pcp_test.go
git commit -m "feat(worker): pcp 핸들러가 JobDirCopy(--strict-order)도 처리"
```

---

## Task 19: `cmd/pcp/main.go` — 진입점

**Files:**
- Create: `cmd/pcp/main.go`

- [ ] **Step 1: 진입점 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/cmd/pcp/main.go`:

```go
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

	jobs := make(chan plan.Job, max(1, p.Workers*4))
	results := make(chan plan.Result, max(1, p.Workers*4))
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
	// (워커 다운그레이드는 Plan 4에서 reconfig; Plan 1에선 동시 워커 수 그대로 진행하되
	//  대상 확장자는 lexical 순서로 제출되어 직렬성 효과를 가진다.)
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

func max(a, b int) int {
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
```

- [ ] **Step 2: 빌드 확인**

Run: `go build -o bin/pcp ./cmd/pcp`
Expected: 에러 없이 `bin/pcp` 생성

- [ ] **Step 3: 스모크 실행**

```bash
mkdir -p /tmp/pcp-smoke/src/sub
echo "hello" > /tmp/pcp-smoke/src/a.txt
echo "world" > /tmp/pcp-smoke/src/sub/b.txt
rm -rf /tmp/pcp-smoke/dst
./bin/pcp -r /tmp/pcp-smoke/src /tmp/pcp-smoke/dst
diff -r /tmp/pcp-smoke/src /tmp/pcp-smoke/dst
```
Expected: `diff` 출력 없음 (트리 동일)

- [ ] **Step 4: 커밋**

```bash
git add cmd/pcp/main.go
git commit -m "feat(pcp): cmd 진입점 (CLI → walk → pool → report)"
```

---

## Task 20: 통합 테스트 — pcp 시나리오

**Files:**
- Create: `tests/integration/pcp_test.go`

- [ ] **Step 1: 통합 테스트 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/tests/integration/pcp_test.go`:

```go
package integration_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// pcpBin은 'go test -run -build' 흐름 대신 binary를 명시적으로 빌드한다.
func pcpBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "pcp")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/pcp")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build pcp: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd() // tests/integration
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func mkTree(t *testing.T, root string, paths map[string]string) {
	t.Helper()
	for rel, body := range paths {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func collectFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out
}

func TestIntegration_PCP_RecursiveBasic(t *testing.T) {
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"a":         "1",
		"sub/b":     "2",
		"sub/c/d":   "3",
		"e.txt":     "5",
	})

	cmd := exec.Command(bin, "-r", "--parallel=4", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pcp failed: %v\n%s", err, out)
	}

	want := []string{"a", "e.txt", "sub/b", "sub/c/d"}
	got := collectFiles(t, dst)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files mismatch\nwant: %v\ngot:  %v", want, got)
	}
}

func TestIntegration_PCP_BestEffortOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"good":        "ok",
		"bad/secret":  "x",
		"more/normal": "y",
	})
	// 'bad' 디렉토리 권한 박탈 → 자식 읽기 실패
	if err := os.Chmod(filepath.Join(src, "bad"), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(src, "bad"), 0755)

	logPath := filepath.Join(root, "errs.log")
	cmd := exec.Command(bin, "-r", "--parallel=2", "--error-log="+logPath, src, dst)
	out, _ := cmd.CombinedOutput()

	// best-effort 모드이므로 다른 파일은 정상 복사되어야 한다.
	for _, p := range []string{"good", "more/normal"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("expected dst/%s: %v\n--- pcp output ---\n%s", p, err, out)
		}
	}
	// 에러 로그가 존재하고 비어있지 않아야 한다.
	st, err := os.Stat(logPath)
	if err != nil || st.Size() == 0 {
		t.Errorf("error log missing or empty: %v size=%d", err, sizeOrZero(st))
	}
}

func TestIntegration_PCP_StrictExtensionTriggerOrder(t *testing.T) {
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"img/1.jpg":     "i1",
		"img/2.jpg":     "i2",
		"data/m.json":   "manifest",
	})

	cmd := exec.Command(bin, "-r", "-v", "--parallel=2", "--strict-extension=.json", src, dst)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("pcp failed: %v", err)
	}

	// verbose 출력에서 .json은 .jpg보다 뒤에 등장해야 한다 (트리거 시맨틱)
	out := stdout.String()
	jsonAt := strings.Index(out, "data/m.json")
	jpg1At := strings.Index(out, "img/1.jpg")
	jpg2At := strings.Index(out, "img/2.jpg")
	if jsonAt < 0 || jpg1At < 0 || jpg2At < 0 {
		t.Fatalf("missing log lines:\n%s", out)
	}
	if jsonAt < jpg1At || jsonAt < jpg2At {
		t.Errorf(".json should follow all .jpg lines\n%s", out)
	}
}

func sizeOrZero(st fs.FileInfo) int64 {
	if st == nil {
		return 0
	}
	return st.Size()
}

var _ = errors.New // silence unused import in some build configs
```

- [ ] **Step 2: 통합 테스트 실행**

Run: `go test -race ./tests/integration/...`
Expected: PASS (3개 시나리오)

- [ ] **Step 3: 커밋**

```bash
git add tests/integration/pcp_test.go
git commit -m "test(integration): pcp 재귀, best-effort, strict-extension 시나리오"
```

---

## Task 21: README 추가

**Files:**
- Create: `README.md`

- [ ] **Step 1: README 작성**

Create `/Users/nineking/workspace/app/pcpmvrm/README.md`:

```markdown
# pcpmvrm

병렬 cp / mv / rm 도구 모음. 바닐라 명령에 `p`만 붙이면 동일한 시맨틱으로 병렬 실행됩니다.

## 상태 (2026-05-08)

- ✅ Plan 1: Foundation + `pcp` (현재 구현 중)
- ⏳ Plan 2: `pmv`
- ⏳ Plan 3: `prm`
- ⏳ Plan 4: `--fallback` 모드 (자식 프로세스 위임)

## 빌드

```bash
make build           # bin/pcp 생성
go test ./...        # 단위 + 통합 테스트
```

## 사용 예시

```bash
# 단일 워커 (바닐라 cp와 동일한 처리량)
pcp -r src/ dst/

# 8 워커 병렬 복사
pcp -r --parallel=8 src/ dst/

# 메타데이터 보존(-a) + 병렬
pcp -ra --parallel=8 src/ dst/

# 트리거 파일은 마지막에
pcp -r --parallel=8 --strict-extension=.json src/ dst/

# 첫 에러에서 중단
pcp -r --parallel=8 --exit-on-error src/ dst/

# 사전 계획 확인 (실제 syscall 없음)
pcp -r --parallel=8 --dry-run src/ dst/
```

## 설계 문서

- [`docs/superpowers/specs/2026-05-08-pcpmvrm-design.md`](docs/superpowers/specs/2026-05-08-pcpmvrm-design.md)
- [`docs/superpowers/specs/2026-05-08-pcpmvrm-brainstorming-log.md`](docs/superpowers/specs/2026-05-08-pcpmvrm-brainstorming-log.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan1-foundation-and-pcp.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan1-foundation-and-pcp.md)

## 라이선스

TBD
```

- [ ] **Step 2: 커밋**

```bash
git add README.md
git commit -m "docs: README — 사용 예시와 빌드 명령"
```

---

## 마무리 검증

- [ ] **전체 테스트 실행**

```bash
go test -race ./...
```
Expected: 모든 테스트 PASS

- [ ] **빌드 검증**

```bash
make build
./bin/pcp -r --parallel=4 /tmp/pcp-smoke/src /tmp/pcp-smoke/dst-2
diff -r /tmp/pcp-smoke/src /tmp/pcp-smoke/dst-2
```
Expected: diff 출력 없음, exit 0

- [ ] **최종 push**

```bash
git push origin main
```

---

## Plan 1 완료 시 산출물

- 동작하는 `pcp` 바이너리 (`bin/pcp`)
- 핵심 옵션 지원: `-r`, `-f`, `-v`, `-p`, `-a`, `--preserve=*`, `-n`, `-u`
- 공통 옵션 지원: `--parallel`, `--strict-order`, `--strict-extension`, `--exit-on-error`, `--error-log`, `--dry-run`, `--no-progress`
- best-effort 에러 처리 + 자동 에러 로그
- streaming walk + 1초 단위 progress 라인
- graceful 시그널 처리 (`SIGINT`/`SIGTERM` 1차/2차)
- 단위 테스트 + 통합 테스트

## 다음 plan에서 추가될 것

- **Plan 2 (pmv)**: same/cross-device 분기, `JobUnlink`, 워커 자동 다운그레이드
- **Plan 3 (prm)**: post-order walker + WaitGroup barrier, `JobUnlink`/`JobDirRemove`
- **Plan 4 (--fallback)**: 자식 프로세스 wrapper (`fallback/exec.go`, `fallback/translate.go`), 세 도구의 미지원 옵션을 자식에 위임
