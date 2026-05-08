package integration_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// pcpBin은 'go test -run -build' 흐름 대신 binary를 명시적으로 빌드한다.
func pcpBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "pcp")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/pcp")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build pcp: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd() // tests/integration
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func mkTree(t *testing.T, root string, paths map[string]string) {
	t.Helper()
	for rel, body := range paths {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func collectFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out
}

func TestIntegration_PCP_RecursiveBasic(t *testing.T) {
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"a":       "1",
		"sub/b":   "2",
		"sub/c/d": "3",
		"e.txt":   "5",
	})

	cmd := exec.Command(bin, "-r", "--parallel=4", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pcp failed: %v\n%s", err, out)
	}

	want := []string{"a", "e.txt", "sub/b", "sub/c/d"}
	got := collectFiles(t, dst)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files mismatch\nwant: %v\ngot:  %v", want, got)
	}
}

func TestIntegration_PCP_BestEffortOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"good":        "ok",
		"bad/secret":  "x",
		"more/normal": "y",
	})
	// 'bad' 디렉토리 권한 박탈 → 자식 읽기 실패
	if err := os.Chmod(filepath.Join(src, "bad"), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(src, "bad"), 0755)

	logPath := filepath.Join(root, "errs.log")
	cmd := exec.Command(bin, "-r", "--parallel=2", "--error-log="+logPath, src, dst)
	out, _ := cmd.CombinedOutput()

	// best-effort 모드이므로 다른 파일은 정상 복사되어야 한다.
	for _, p := range []string{"good", "more/normal"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("expected dst/%s: %v\n--- pcp output ---\n%s", p, err, out)
		}
	}
	// 에러 로그가 존재하고 비어있지 않아야 한다.
	st, err := os.Stat(logPath)
	if err != nil || st.Size() == 0 {
		t.Errorf("error log missing or empty: %v size=%d", err, sizeOrZero(st))
	}
}

func TestIntegration_PCP_StrictExtensionTriggerOrder(t *testing.T) {
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mkTree(t, src, map[string]string{
		"img/1.jpg":   "i1",
		"img/2.jpg":   "i2",
		"data/m.json": "manifest",
	})

	cmd := exec.Command(bin, "-r", "-v", "--parallel=2", "--strict-extension=.json", src, dst)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("pcp failed: %v", err)
	}

	// verbose 출력에서 .json은 .jpg보다 뒤에 등장해야 한다 (트리거 시맨틱)
	out := stdout.String()
	jsonAt := strings.Index(out, "data/m.json")
	jpg1At := strings.Index(out, "img/1.jpg")
	jpg2At := strings.Index(out, "img/2.jpg")
	if jsonAt < 0 || jpg1At < 0 || jpg2At < 0 {
		t.Fatalf("missing log lines:\n%s", out)
	}
	if jsonAt < jpg1At || jsonAt < jpg2At {
		t.Errorf(".json should follow all .jpg lines\n%s", out)
	}
}

// TestIntegration_PCP_StrictExtensionTriggerOrder_LexFirst pins phase-boundary
// behavior: even when the trigger file is lexically FIRST in DFS order
// (so a shared worker pool would interleave it with phase-1 work), the
// strict-extension semantics must still complete every non-target file
// before any target file (spec §5.2).
func TestIntegration_PCP_StrictExtensionTriggerOrder_LexFirst(t *testing.T) {
	bin := pcpBin(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")

	// Large payloads + many files so that a shared-pool implementation will
	// have phase-1 work still in flight when phase-2 emits the lex-first
	// trigger. (Spec §5.2 requires phase 2 to start only after phase 1
	// has fully drained.)
	tree := map[string]string{
		"aaa.json": "trigger",
	}
	bigPayload := strings.Repeat("x", 1024*1024) // 1 MiB
	for i := 0; i < 30; i++ {
		// non-target files come AFTER the trigger lexically.
		tree[fmt.Sprintf("payload/f%03d.bin", i)] = bigPayload
	}
	mkTree(t, src, tree)

	cmd := exec.Command(bin, "-r", "-v", "--parallel=4", "--strict-extension=.json", src, dst)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("pcp failed: %v\n%s", err, stdout.String())
	}
	out := stdout.String()

	// trigger의 'ok' 라인은 모든 non-target 'ok' 라인보다 뒤에 있어야 한다.
	jsonAt := strings.Index(out, "ok   aaa.json")
	if jsonAt < 0 {
		t.Fatalf("trigger ok-line missing:\n%s", out)
	}
	for i := 0; i < 30; i++ {
		needle := fmt.Sprintf("ok   payload/f%03d.bin", i)
		at := strings.Index(out, needle)
		if at < 0 {
			t.Fatalf("payload ok-line missing for %d:\n%s", i, out)
		}
		if at > jsonAt {
			t.Fatalf("phase-boundary violated: payload f%03d.bin (offset %d) completed AFTER trigger aaa.json (offset %d)", i, at, jsonAt)
		}
	}
}

func sizeOrZero(st fs.FileInfo) int64 {
	if st == nil {
		return 0
	}
	return st.Size()
}

var _ = errors.New // silence unused import in some build configs
