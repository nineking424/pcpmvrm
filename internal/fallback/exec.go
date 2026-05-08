// Package fallback delegates Job execution to system binaries (cp, mv, rm)
// when --fallback is set, supporting options the native worker doesn't implement.
package fallback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// CmdOutput captures stdout, stderr, and exit code of a child process.
type CmdOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCmd runs `name` with `args`, capturing output. Non-zero exit returns an error
// with the exit code preserved on CmdOutput.ExitCode. ctx cancellation kills the child.
func RunCmd(ctx context.Context, name string, args []string) (CmdOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := CmdOutput{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		return out, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.ExitCode = exitErr.ExitCode()
		return out, fmt.Errorf("%s exited %d: %s", name, out.ExitCode, flattenStderr(out.Stderr))
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

// flattenStderr는 자식의 stderr를 errLog의 단일-라인 탭 구분 포맷에
// 안전하게 끼워 넣을 수 있도록 newline/tab을 ` | `로 치환하고 길이를 제한한다.
func flattenStderr(s string) string {
	const max = 2000
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " | ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
