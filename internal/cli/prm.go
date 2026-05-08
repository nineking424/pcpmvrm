package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// ParsePRM turns argv (without the program name) into a validated Plan for prm.
func ParsePRM(args []string) (plan.Plan, error) {
	if hit := FirstUnsupported("prm", args); hit != "" {
		return plan.Plan{}, errors.New(UnsupportedMessage("prm", hit))
	}

	fallback := hasFlag(args, "--fallback")

	var (
		c         Common
		recurse   bool
		forceMiss bool
		verbose   bool
		emptyDir  bool
	)
	fs := pflag.NewFlagSet("prm", pflag.ContinueOnError)
	if fallback {
		fs.ParseErrorsWhitelist.UnknownFlags = true
	}
	RegisterCommon(fs, &c)
	fs.BoolVarP(&recurse, "recursive", "r", false, "recursively remove directories")
	fs.BoolVarP(&recurse, "RECURSIVE", "R", false, "alias for --recursive")
	fs.BoolVarP(&forceMiss, "force", "f", false, "no error on missing files")
	fs.BoolVarP(&verbose, "verbose", "v", false, "print each removal")
	fs.BoolVarP(&emptyDir, "dir", "d", false, "remove empty directories")

	if err := fs.Parse(args); err != nil {
		return plan.Plan{}, err
	}
	c.StrictExtensions = normalizeExts(c.StrictExtensions)

	pos := fs.Args()
	if len(pos) != 1 {
		return plan.Plan{}, fmt.Errorf("prm: exactly one PATH required (got %d args)", len(pos))
	}

	p := plan.Plan{
		Op:               plan.OpRemove,
		Src:              pos[0],
		Workers:          c.Workers,
		Recursive:        recurse,
		ForceMissing:     forceMiss,
		Verbose:          verbose,
		RemoveEmptyDir:   emptyDir,
		DryRun:           c.DryRun,
		ExitOnError:      c.ExitOnError,
		NoProgress:       c.NoProgress,
		ErrorLogPath:     c.ErrorLogPath,
		StrictOrder:      c.StrictOrder,
		StrictExtensions: c.StrictExtensions,
		Fallback:         c.Fallback,
	}
	if c.Fallback {
		p.RawFlags = collectRawFlags(args, fs)
	}
	if err := p.Validate(); err != nil {
		return plan.Plan{}, fmt.Errorf("prm: %w", err)
	}
	return p, nil
}
