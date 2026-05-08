package fsx_test

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fsx"
)

func TestCopyFile_Basic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	body := make([]byte, 1<<14) // 16 KB
	_, _ = rand.Read(body)
	if err := os.WriteFile(src, body, 0644); err != nil {
		t.Fatal(err)
	}

	n, err := fsx.CopyFile(src, dst, fsx.CopyOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("CopyFile bytes = %d, want %d", n, len(body))
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Error("dst contents differ from src")
	}
}

func TestCopyFile_AtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	// CopyFile은 같은 디렉토리에 .pcp-tmp-XXXXXX 패턴 임시 파일을 만들었다가 rename한다.
	// 정상 종료 후엔 임시 파일이 남으면 안 된다.
	if _, err := fsx.CopyFile(src, dst, fsx.CopyOpts{}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && contains(e.Name(), ".pcp-tmp-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestCopyFile_NoClobber(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := fsx.CopyFile(src, dst, fsx.CopyOpts{NoClobber: true})
	if !errors.Is(err, fsx.ErrSkipExisting) {
		t.Fatalf("want ErrSkipExisting, got %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst was overwritten: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
