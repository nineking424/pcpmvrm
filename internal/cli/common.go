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
