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
	Fallback         bool
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
	fs.BoolVar(&c.Fallback, "fallback", false, "delegate to /bin/cp /bin/mv /bin/rm via fork+exec (slower; supports options T4/T5 don't natively)")
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

// collectRawFlags returns options from args that the FlagSet did NOT register.
// Used in --fallback mode to forward unknown options to the system binary verbatim.
//
// Forms handled:
//
//	"--key=value", "-k=value"  → consumed as a single token
//	"--key", "value" / "-k", "value" → consumed as a pair when next token doesn't start with "-"
//	"-rf"  → combined short form: each letter checked individually; emit the whole token if ANY letter is unrecognized
//
// Special-cased OUT (always dropped from raw, even if unknown):
//
//	"--fallback" / "--fallback=..." — our own flag, never forwarded.
func collectRawFlags(args []string, fs *pflag.FlagSet) []string {
	var raw []string
	seenSep := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if seenSep {
			// After "--", everything is positional — not a flag.
			continue
		}
		if a == "--" {
			seenSep = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		// Drop our own --fallback flag (in any form).
		if a == "--fallback" || strings.HasPrefix(a, "--fallback=") {
			continue
		}
		// Resolve the option name (may have "=value" suffix).
		key := a
		hasEq := false
		if eq := strings.Index(a, "="); eq >= 0 {
			key = a[:eq]
			hasEq = true
		}
		var known bool
		if strings.HasPrefix(key, "--") {
			known = fs.Lookup(strings.TrimPrefix(key, "--")) != nil
		} else {
			// Short form: "-r", "-rf" — combined letters all need to be known.
			letters := key[1:]
			allKnown := len(letters) > 0
			for j := 0; j < len(letters); j++ {
				if fs.ShorthandLookup(string(letters[j])) == nil {
					allKnown = false
					break
				}
			}
			known = allKnown
		}
		if known {
			// If recognized but takes a separate value (e.g., "--parallel 4"), pflag will
			// pair them itself. We don't need to skip the next token — fs.Args() already
			// excludes both. Move on.
			continue
		}
		// Unknown — emit the token. If it's "--key" (no =) and the next token looks like
		// a value (not starting with "-"), include it as well.
		raw = append(raw, a)
		if !hasEq && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			raw = append(raw, args[i+1])
			i++
		}
	}
	return raw
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
