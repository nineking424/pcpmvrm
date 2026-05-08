package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// ParsePMV turns argv (without the program name) into a validated Plan for pmv.
func ParsePMV(args []string) (plan.Plan, error) {
	if hit := FirstUnsupported("pmv", args); hit != "" {
		return plan.Plan{}, errors.New(UnsupportedMessage("pmv", hit))
	}

	fallback := hasFlag(args, "--fallback")

	var (
		c          Common
		overwrite  bool
		verbose    bool
		noClobber  bool
		updateOnly bool
	)
	fs := pflag.NewFlagSet("pmv", pflag.ContinueOnError)
	if fallback {
		fs.ParseErrorsWhitelist.UnknownFlags = true
	}
	RegisterCommon(fs, &c)
	fs.BoolVarP(&overwrite, "force", "f", false, "overwrite existing files (vanilla mv -f)")
	fs.BoolVarP(&verbose, "verbose", "v", false, "print each move operation")
	fs.BoolVarP(&noClobber, "no-clobber", "n", false, "do not overwrite existing files")
	fs.BoolVarP(&updateOnly, "update", "u", false, "move only when src is newer than dst")

	if err := fs.Parse(args); err != nil {
		return plan.Plan{}, fmt.Errorf("pmv: %w", err)
	}
	c.StrictExtensions = normalizeExts(c.StrictExtensions)

	if overwrite && noClobber {
		return plan.Plan{}, fmt.Errorf("pmv: -f and -n are mutually exclusive")
	}

	pos := fs.Args()
	if len(pos) != 2 {
		return plan.Plan{}, fmt.Errorf("pmv: expected SRC and DST, got %d positional args", len(pos))
	}

	p := plan.Plan{
		Op:               plan.OpMove,
		Src:              pos[0],
		Dst:              pos[1],
		Workers:          c.Workers,
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
		Fallback:         c.Fallback,
	}
	if c.Fallback {
		p.RawFlags = collectRawFlags(args, fs)
	}
	if err := p.Validate(); err != nil {
		return plan.Plan{}, fmt.Errorf("pmv: %w", err)
	}
	return p, nil
}
