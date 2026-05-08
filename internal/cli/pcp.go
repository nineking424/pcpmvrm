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

	fallback := hasFlag(args, "--fallback")

	var (
		c            Common
		recurse      bool
		verbose      bool
		archive      bool
		preserve     bool
		preserveList string
		noClobber    bool
		updateOnly   bool
		overwrite    bool
	)
	fs := pflag.NewFlagSet("pcp", pflag.ContinueOnError)
	if fallback {
		fs.ParseErrorsWhitelist.UnknownFlags = true
	}
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
		Fallback:         c.Fallback,
	}
	if c.Fallback {
		p.RawFlags = collectRawFlags(args, fs)
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
