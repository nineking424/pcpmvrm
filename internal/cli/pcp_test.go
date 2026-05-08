package cli_test

import (
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestParsePCP_Recursive(t *testing.T) {
	p, err := cli.ParsePCP([]string{"-r", "--parallel=4", "src", "dst"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Op != plan.OpCopy {
		t.Errorf("Op = %v, want OpCopy", p.Op)
	}
	if !p.Recursive {
		t.Error("Recursive = false, want true")
	}
	if p.Workers != 4 {
		t.Errorf("Workers = %d, want 4", p.Workers)
	}
	if p.Src != "src" || p.Dst != "dst" {
		t.Errorf("Src/Dst = %q/%q", p.Src, p.Dst)
	}
}

func TestParsePCP_Archive(t *testing.T) {
	p, err := cli.ParsePCP([]string{"-a", "src", "dst"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Recursive {
		t.Error("-a should imply --recursive")
	}
	if !p.Preserve.Mode || !p.Preserve.Ownership || !p.Preserve.Timestamps {
		t.Errorf("-a should preserve all metadata, got %+v", p.Preserve)
	}
}

func TestParsePCP_RejectsUnsupported(t *testing.T) {
	_, err := cli.ParsePCP([]string{"-i", "src", "dst"})
	if err == nil {
		t.Fatal("expected error for -i")
	}
	if !strings.Contains(err.Error(), "--fallback") {
		t.Errorf("error message should mention --fallback, got: %v", err)
	}
}

func TestParsePCP_RequiresTwoPositionals(t *testing.T) {
	_, err := cli.ParsePCP([]string{"src"})
	if err == nil {
		t.Fatal("expected error for single positional")
	}
}
