package cli_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestParsePMV_Minimum(t *testing.T) {
	p, err := cli.ParsePMV([]string{"src", "dst"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if p.Op != plan.OpMove {
		t.Errorf("Op=%v, want OpMove", p.Op)
	}
	if p.Src != "src" || p.Dst != "dst" {
		t.Errorf("Src/Dst=%q/%q", p.Src, p.Dst)
	}
	if p.Workers != 1 {
		t.Errorf("Workers=%d, want 1", p.Workers)
	}
}

func TestParsePMV_AllSupportedFlags(t *testing.T) {
	p, err := cli.ParsePMV([]string{
		"-f", "-v", "-u",
		"--parallel=4",
		"--exit-on-error",
		"--dry-run",
		"src", "dst",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !p.Overwrite || !p.Verbose || !p.UpdateOnly {
		t.Errorf("flags not set: %+v", p)
	}
	if p.Workers != 4 {
		t.Errorf("Workers=%d, want 4", p.Workers)
	}
	if !p.DryRun || !p.ExitOnError {
		t.Errorf("common flags not set: %+v", p)
	}
}

func TestParsePMV_NoClobberFlag(t *testing.T) {
	p, err := cli.ParsePMV([]string{"-n", "src", "dst"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.NoClobber {
		t.Error("NoClobber should be true with -n")
	}
}

func TestParsePMV_ForceAndNoClobberConflict(t *testing.T) {
	_, err := cli.ParsePMV([]string{"-f", "-n", "src", "dst"})
	if err == nil {
		t.Fatal("-f and -n should be mutually exclusive")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutual exclusivity, got: %v", err)
	}
}

func TestParsePMV_RejectsRecursive(t *testing.T) {
	_, err := cli.ParsePMV([]string{"-r", "src", "dst"})
	if err == nil {
		t.Fatal("pmv must not accept -r (mv has no -r)")
	}
}

func TestParsePMV_RejectsUnsupported(t *testing.T) {
	_, err := cli.ParsePMV([]string{"-b", "src", "dst"})
	if err == nil {
		t.Fatal("expected error for -b")
	}
	if !strings.Contains(err.Error(), "--fallback") {
		t.Errorf("error should mention --fallback, got: %v", err)
	}
}

func TestParsePMV_RequiresTwoPositionals(t *testing.T) {
	_, err := cli.ParsePMV([]string{"src"})
	if err == nil {
		t.Fatal("expected error for single positional")
	}
}

func TestParsePMV_FallbackPreservesUnknownFlags(t *testing.T) {
	args := []string{"--fallback", "--backup=numbered", "-f", "src", "dst"}
	p, err := cli.ParsePMV(args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !p.Fallback {
		t.Fatal("Fallback should be true")
	}
	want := map[string]bool{"--backup=numbered": true}
	got := map[string]bool{}
	for _, f := range p.RawFlags {
		got[f] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RawFlags=%v want %v", p.RawFlags, want)
	}
	for _, f := range p.RawFlags {
		if f == "-f" || f == "--fallback" {
			t.Errorf("recognized/our flag leaked into RawFlags: %s", f)
		}
	}
}

func TestParsePMV_NoFallback_UnknownFlagStillRejected(t *testing.T) {
	args := []string{"--backup=numbered", "src", "dst"}
	if _, err := cli.ParsePMV(args); err == nil {
		t.Error("unknown flag without --fallback must error")
	}
}
