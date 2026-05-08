package cli_test

import (
	"reflect"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestParsePRM_Minimum(t *testing.T) {
	p, err := cli.ParsePRM([]string{"/tmp/x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := plan.Plan{Op: plan.OpRemove, Src: "/tmp/x", Workers: 1}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan=%+v want %+v", p, want)
	}
}

func TestParsePRM_AllSupportedFlags(t *testing.T) {
	args := []string{
		"-rf", "-v", "-d",
		"--parallel=4",
		"--exit-on-error",
		"--dry-run",
		"/tmp/x",
	}
	p, err := cli.ParsePRM(args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := plan.Plan{
		Op: plan.OpRemove, Src: "/tmp/x", Workers: 4,
		Recursive: true, ForceMissing: true, Verbose: true, RemoveEmptyDir: true,
		ExitOnError: true, DryRun: true,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan=%+v\nwant %+v", p, want)
	}
}

func TestParsePRM_RejectsExtraArgs(t *testing.T) {
	if _, err := cli.ParsePRM([]string{"/a", "/b"}); err == nil {
		t.Fatal("prm should accept exactly one PATH")
	}
}

func TestParsePRM_RejectsRecursiveOnFile(t *testing.T) {
	// 이는 사전 검증 단계 — parser는 통과시키고 main.go가 lstat 후 거부.
	// 여기서는 단순히 -r과 PATH 둘 다 받아서 통과해야 함.
	if _, err := cli.ParsePRM([]string{"-r", "/tmp/x"}); err != nil {
		t.Errorf("parser should accept -r + PATH: %v", err)
	}
}

func TestParsePRM_FallbackPreservesUnknownFlags(t *testing.T) {
	args := []string{"--fallback", "-i", "-r", "/tmp/x"}
	p, err := cli.ParsePRM(args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !p.Fallback {
		t.Fatal("Fallback should be true")
	}
	want := map[string]bool{"-i": true}
	got := map[string]bool{}
	for _, f := range p.RawFlags {
		got[f] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RawFlags=%v want %v", p.RawFlags, want)
	}
	for _, f := range p.RawFlags {
		if f == "-r" || f == "--fallback" {
			t.Errorf("recognized/our flag leaked into RawFlags: %s", f)
		}
	}
}

func TestParsePRM_NoFallback_UnknownFlagStillRejected(t *testing.T) {
	args := []string{"-i", "/tmp/x"}
	if _, err := cli.ParsePRM(args); err == nil {
		t.Error("unknown flag without --fallback must error")
	}
}
