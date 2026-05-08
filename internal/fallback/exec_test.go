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

func TestRunCmd_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel
	_, err := fallback.RunCmd(ctx, "/bin/sleep", []string{"5"})
	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
}
