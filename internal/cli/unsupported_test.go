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
