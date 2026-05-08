package fallback_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/fallback"
)

func TestRunCmd_Success(t *testing.T) {
	out, err := fallback.RunCmd(context.Background(), "/bin/echo", []string{"hello"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.Stdout, "hello") {
		t.Errorf("stdout=%q", out.Stdout)
	}
	if out.ExitCode != 0 {
		t.Errorf("exit=%d", out.ExitCode)
	}
}

func TestRunCmd_NonZeroExit(t *testing.T) {
	out, err := fallback.RunCmd(context.Background(), "/bin/sh", []string{"-c", "exit 7"})
	if err == nil {
		t.Fatal("expected non-zero exit error")
	}
	if out.ExitCode != 7 {
		t.Errorf("exit=%d, want 7", out.ExitCode)
	}
}

func TestRunCmd_NotFound(t *testing.T) {
	_, err := fallback.RunCmd(context.Background(), "/no/such/binary", nil)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestRunCmd_MultiLineStderrIsFlattened(t *testing.T) {
	// 비-0 종료 + 멀티라인 stderr → 에러 메시지에는 newline/tab이 들어가면 안 된다
	// (errLog 단일 라인 탭 포맷이 깨지지 않도록).
	_, err := fallback.RunCmd(context.Background(), "/bin/sh",
		[]string{"-c", "printf 'line1\\nline2\\tcol\\nline3' >&2; exit 1"})
	if err == nil {
		t.Fatal("expected non-zero exit")
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") {
		t.Errorf("error message contains newline: %q", msg)
	}
	if strings.Contains(msg, "\t") {
		t.Errorf("error message contains tab: %q", msg)
	}
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %q", want, msg)
		}
	}
}

func TestRunCmd_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel
	_, err := fallback.RunCmd(ctx, "/bin/sleep", []string{"5"})
	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
}
