package cli_test

import (
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
)

func TestRejectUnsupported(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args []string
		hit  string // 검출되어야 하는 옵션 (또는 "" = 통과)
	}{
		{name: "pcp ok plain", tool: "pcp", args: []string{"-r", "src", "dst"}, hit: ""},
		{name: "pcp reject -i", tool: "pcp", args: []string{"-i", "src", "dst"}, hit: "-i"},
		{name: "pcp reject --reflink", tool: "pcp", args: []string{"--reflink=auto", "src", "dst"}, hit: "--reflink"},
		{name: "pcp reject --sparse", tool: "pcp", args: []string{"--sparse=always", "src", "dst"}, hit: "--sparse"},
		{name: "pcp reject -L", tool: "pcp", args: []string{"-L", "src", "dst"}, hit: "-L"},
		{name: "pcp reject combined -ri", tool: "pcp", args: []string{"-ri", "src", "dst"}, hit: "-i"},
		{name: "pcp ok combined -ra", tool: "pcp", args: []string{"-ra", "src", "dst"}, hit: ""},

		{name: "pmv reject -b", tool: "pmv", args: []string{"-b", "src", "dst"}, hit: "-b"},
		{name: "pmv reject --backup", tool: "pmv", args: []string{"--backup=numbered", "src", "dst"}, hit: "--backup"},
		{name: "pmv reject -T", tool: "pmv", args: []string{"-T", "src", "dst"}, hit: "-T"},
		{name: "pmv reject --no-target-directory", tool: "pmv", args: []string{"--no-target-directory", "src", "dst"}, hit: "--no-target-directory"},
		{name: "pmv reject --strip-trailing-slashes", tool: "pmv", args: []string{"--strip-trailing-slashes", "src", "dst"}, hit: "--strip-trailing-slashes"},
		{name: "pmv reject -Z", tool: "pmv", args: []string{"-Z", "src", "dst"}, hit: "-Z"},
		{name: "pmv reject --context", tool: "pmv", args: []string{"--context=user_u", "src", "dst"}, hit: "--context"},
		{name: "pmv reject -i", tool: "pmv", args: []string{"-i", "src", "dst"}, hit: "-i"},
		{name: "pmv ok plain", tool: "pmv", args: []string{"-f", "src", "dst"}, hit: ""},

		{name: "prm reject -i", tool: "prm", args: []string{"-i", "/tmp/x"}, hit: "-i"},
		{name: "prm reject -I", tool: "prm", args: []string{"-I", "/tmp/x"}, hit: "-I"},
		{name: "prm reject --interactive", tool: "prm", args: []string{"--interactive=once", "/tmp/x"}, hit: "--interactive"},
		{name: "prm reject --one-file-system", tool: "prm", args: []string{"--one-file-system", "-r", "/tmp/x"}, hit: "--one-file-system"},
		{name: "prm reject --no-preserve-root", tool: "prm", args: []string{"--no-preserve-root", "-rf", "/tmp/x"}, hit: "--no-preserve-root"},
		{name: "prm reject --preserve-root=all", tool: "prm", args: []string{"--preserve-root=all", "-rf", "/tmp/x"}, hit: "--preserve-root=all"},
		{name: "prm ok bare --preserve-root", tool: "prm", args: []string{"--preserve-root", "-rf", "/tmp/x"}, hit: ""},
		{name: "prm ok plain", tool: "prm", args: []string{"-rf", "/tmp/x"}, hit: ""},
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

func TestUnsupportedMessage_PMV(t *testing.T) {
	msg := cli.UnsupportedMessage("pmv", "--backup")
	for _, w := range []string{"pmv: '--backup'", "--fallback", "성능"} {
		if !strings.Contains(msg, w) {
			t.Errorf("message %q missing %q", msg, w)
		}
	}
}

func TestFirstUnsupported_BypassedWithFallback(t *testing.T) {
	args := []string{"--fallback", "--reflink=auto", "-r", "src", "dst"}
	if hit := cli.FirstUnsupported("pcp", args); hit != "" {
		t.Errorf("--fallback should bypass unsupported check, got hit: %q", hit)
	}
}

func TestFirstUnsupported_BlockedWithoutFallback(t *testing.T) {
	args := []string{"--reflink=auto", "-r", "src", "dst"}
	if hit := cli.FirstUnsupported("pcp", args); hit != "--reflink" {
		t.Errorf("--reflink without --fallback should be hit (got %q)", hit)
	}
}

func TestFirstUnsupported_BypassedWithFallbackEqualsForm(t *testing.T) {
	// pflag accepts --fallback=true; bypass must trigger for that form too.
	args := []string{"--fallback=true", "-i", "src", "dst"}
	if hit := cli.FirstUnsupported("pmv", args); hit != "" {
		t.Errorf("--fallback=true should bypass, got hit: %q", hit)
	}
}
