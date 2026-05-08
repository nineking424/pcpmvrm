package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFallback_PCP_PassesRawFlagToCp(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("--reflink=auto is Linux-only; raw flag passthrough tested on Linux CI")
	}
	root := t.TempDir()
	mkTree(t, root, map[string]string{"src.txt": "hi"})
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "dst.txt")

	bin := pcpBin(t)
	out, err := exec.Command(bin, "--fallback", "--reflink=auto", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pcp --fallback --reflink: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, []byte("hi")) {
		t.Errorf("dst=%q", got)
	}
}

func TestFallback_PMV_RenamesViaMv(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{"a": "x"})
	src := filepath.Join(root, "a")
	dst := filepath.Join(root, "b")

	bin := pmvBin(t)
	out, err := exec.Command(bin, "--fallback", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pmv --fallback: %v\n%s", err, out)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be moved")
	}
}

func TestFallback_PRM_RemovesViaRm(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{"f": "x"})
	f := filepath.Join(root, "f")

	bin := prmBin(t)
	out, err := exec.Command(bin, "--fallback", f).CombinedOutput()
	if err != nil {
		t.Fatalf("prm --fallback: %v\n%s", err, out)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("file should be gone")
	}
}

func TestFallback_PCP_RecursiveTreePreservesContent(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"a/f1":   "AAA",
		"a/b/f2": "BBB",
	})

	bin := pcpBin(t)
	out, err := exec.Command(bin, "-r", "--parallel=2", "--fallback", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pcp -r --fallback: %v\n%s", err, out)
	}
	cmp := exec.Command("diff", "-r", src, dst)
	if outDiff, err := cmp.CombinedOutput(); err != nil || len(outDiff) > 0 {
		t.Errorf("diff src/dst: err=%v out=%s", err, outDiff)
	}
}
