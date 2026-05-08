package fallback_test

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fallback"
	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestTranslate_PCPCopy(t *testing.T) {
	p := plan.Plan{Op: plan.OpCopy, Recursive: true, Verbose: true,
		Preserve: plan.Preserve{Mode: true, Ownership: true, Timestamps: true},
		RawFlags: []string{"--reflink=auto"},
	}
	j := plan.Job{Kind: plan.JobCopy, Src: "src/file", Dst: "dst/file"}

	bin, args := fallback.Translate(p, j)
	if bin != "/bin/cp" {
		t.Errorf("bin=%s, want /bin/cp", bin)
	}
	preserve := "--preserve=mode,ownership,timestamps"
	if runtime.GOOS != "linux" {
		preserve = "-p"
	}
	want := []string{"-v", preserve, "--reflink=auto", "src/file", "dst/file"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant %v", args, want)
	}
}

func TestTranslate_PCPDirCopy_PassesRecursive(t *testing.T) {
	p := plan.Plan{Op: plan.OpCopy, Recursive: true}
	j := plan.Job{Kind: plan.JobDirCopy, Src: "src/d", Dst: "dst/d"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/cp" {
		t.Errorf("bin=%s", bin)
	}
	if !contains(args, "-r") && !contains(args, "-R") {
		t.Errorf("dir copy must pass -r, got: %v", args)
	}
}

func TestTranslate_PMVRename(t *testing.T) {
	p := plan.Plan{Op: plan.OpMove, Overwrite: true}
	j := plan.Job{Kind: plan.JobRename, Src: "a", Dst: "b"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/mv" {
		t.Errorf("bin=%s", bin)
	}
	want := []string{"-f", "a", "b"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v\nwant %v", args, want)
	}
}

func TestTranslate_PRMUnlink(t *testing.T) {
	p := plan.Plan{Op: plan.OpRemove, Verbose: true, ForceMissing: true}
	j := plan.Job{Kind: plan.JobUnlink, Src: "/x"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/rm" {
		t.Errorf("bin=%s", bin)
	}
	want := []string{"-f", "-v", "/x"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v want %v", args, want)
	}
}

func TestTranslate_PRMDirRemove(t *testing.T) {
	p := plan.Plan{Op: plan.OpRemove}
	j := plan.Job{Kind: plan.JobDirRemove, Src: "/x"}
	bin, args := fallback.Translate(p, j)
	if bin != "/bin/rmdir" {
		t.Errorf("bin=%s, want /bin/rmdir", bin)
	}
	want := []string{"/x"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args=%v want %v", args, want)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
