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
	Op      Op
	Src     string
	Dst     string // unused for OpRemove
	Workers int

	// Common flags
	Recursive    bool
	Verbose      bool
	DryRun       bool
	ExitOnError  bool
	NoProgress   bool
	ErrorLogPath string // empty → auto-generate

	// Modes
	StrictOrder      bool
	StrictExtensions []string // lowercase, leading dot, e.g. ".json"

	// Tool-specific
	Overwrite      bool // -f
	NoClobber      bool // -n
	UpdateOnly     bool // -u
	Preserve       Preserve
	RemoveEmptyDir bool // prm -d
	ForceMissing   bool // prm -f: 존재하지 않는 파일에 대해 에러 안 냄

	// SameDevice는 pmv가 사전 stat 결과로 same-device임을 확인했을 때 true.
	// true이면 Workers는 1로 다운그레이드된 상태이며 walker가 단일 JobRename을 emit한다.
	SameDevice bool

	// Fallback이 true이면 워커가 native syscall 대신 자식 프로세스를 호출한다.
	Fallback bool
	// RawFlags는 --fallback 모드에서 자식 프로세스에 그대로 전달할 옵션들이다.
	// pflag가 인식한 long/short 옵션은 보존된 형태(예: "--reflink=auto", "-d")로 들어간다.
	RawFlags []string
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
