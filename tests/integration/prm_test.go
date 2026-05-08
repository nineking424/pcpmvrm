package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// prmBin builds the prm binary into a temp dir and returns its path.
func prmBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "prm")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/prm")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build prm: %v\n%s", err, out)
	}
	return bin
}

func TestIntegration_PRM_RecursiveTree(t *testing.T) {
	bin := prmBin(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	mkTree(t, target, map[string]string{
		"a/f1":   "file1",
		"a/b/f2": "file2",
		"c/f3":   "file3",
	})

	cmd := exec.Command(bin, "-r", "--parallel=4", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prm failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target should be gone after recursive remove: stat err=%v", err)
	}
}

func TestIntegration_PRM_MissingFileWithForce(t *testing.T) {
	bin := prmBin(t)

	cmd := exec.Command(bin, "-f", "/no/such/file")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prm -f on missing file should exit 0: %v\n%s", err, out)
	}
}

func TestIntegration_PRM_DirWithoutRecursive(t *testing.T) {
	bin := prmBin(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	mkTree(t, target, map[string]string{
		"f.txt": "content",
	})

	cmd := exec.Command(bin, target)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("prm <dir> without -r should fail, but succeeded")
	}

	if !bytes.Contains(out, []byte("is a directory")) {
		t.Errorf("output should contain 'is a directory', got:\n%s", out)
	}
}
