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
