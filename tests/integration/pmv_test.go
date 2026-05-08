package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// pmvBin builds the pmv binary into a temp dir and returns its path.
func pmvBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "pmv")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/pmv")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build pmv: %v\n%s", err, out)
	}
	return bin
}

func TestIntegration_PMV_SameDevice_DirTree(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"a.txt":     "AAA",
		"sub/b.txt": "BBB",
	})

	bin := pmvBin(t)
	out, err := exec.Command(bin, src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pmv: %v\n%s", err, out)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after move: stat err=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "sub/b.txt"))
	if err != nil {
		t.Fatalf("read dst file: %v", err)
	}
	if !bytes.Equal(got, []byte("BBB")) {
		t.Errorf("dst content mismatch: got %q want %q", got, "BBB")
	}

	// Default workers=1, so the downgrade notice must NOT fire.
	if bytes.Contains(out, []byte("downgrading --parallel to 1")) {
		t.Errorf("downgrade notice should not appear at default workers=1; output:\n%s", out)
	}
}

func TestIntegration_PMV_SameDevice_DowngradeWarning(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"f.txt": "x",
	})

	bin := pmvBin(t)
	out, err := exec.Command(bin, "--parallel=8", src, dst).CombinedOutput()
	if err != nil {
		t.Fatalf("pmv: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("downgrading --parallel to 1")) {
		t.Errorf("expected downgrade warning in output, got:\n%s", out)
	}

	// Move still succeeded.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after move: stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Errorf("dst file should exist: %v", err)
	}
}
