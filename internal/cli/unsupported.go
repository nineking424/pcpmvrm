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
	"pmv": {'i': {}, 'b': {}, 'T': {}, 'Z': {}},
	"prm": {'i': {}, 'I': {}},
}

// unsupportedLong lists long options (with leading "--") rejected in native mode.
var unsupportedLong = map[string]map[string]struct{}{
	"pcp": {
		"--reflink": {}, "--sparse": {}, "--no-dereference": {},
		"--remove-destination": {}, "--copy-contents": {}, "--symbolic-link": {},
		"--link": {}, "--one-file-system": {}, "--interactive": {},
	},
	"pmv": {
		"--interactive": {}, "--backup": {}, "--no-target-directory": {},
		"--strip-trailing-slashes": {}, "--context": {},
	},
	"prm": {"--interactive": {}, "--one-file-system": {}, "--no-preserve-root": {}},
}

// unsupportedExact lists args rejected only when matching the full form
// (including =value). Used for cases where the bare flag is allowed but
// a specific value form is not, e.g. --preserve-root vs --preserve-root=all.
var unsupportedExact = map[string]map[string]struct{}{
	"prm": {"--preserve-root=all": {}},
}

// FirstUnsupported scans args and returns the first unsupported option found,
// or "" if all options are supported. It does not parse positional args.
//
// If --fallback is present (bare or --fallback=...), returns "" immediately,
// since all options are delegated to the system binary.
//
// It correctly explodes combined short flags ("-ri" → "-r" + "-i").
// Long options with values ("--reflink=auto") are matched on the option name only.
// Exact matches (e.g., "--preserve-root=all") take precedence over name-only matches.
func FirstUnsupported(tool string, args []string) string {
	if hasFlag(args, "--fallback") {
		return ""
	}

	short := unsupportedShort[tool]
	long := unsupportedLong[tool]
	exact := unsupportedExact[tool]
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--"):
			// Check exact match first (e.g., --preserve-root=all)
			if _, bad := exact[a]; bad {
				return a
			}
			// Then check option name only (e.g., --reflink in --reflink=auto)
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

// hasFlag reports whether args contains the long-form `name` (with or without =value).
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
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
