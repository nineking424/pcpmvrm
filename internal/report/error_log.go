// Package report holds the user-facing output side: error log, progress line,
// verbose stdout serialization, and signal handling.
package report

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"
)

// ErrorLog is the concurrent-safe writer for failed-job lines.
type ErrorLog struct {
	mu    sync.Mutex
	w     *bufio.Writer
	f     *os.File
	tool  string
	path  string
	count int
}

// NewErrorLog creates (or appends to) the error log file. If path is empty,
// it uses ./<tool>-failed-<RFC3339>.log in the current working directory.
func NewErrorLog(path, tool string) (*ErrorLog, error) {
	if path == "" {
		path = fmt.Sprintf("./%s-failed-%s.log", tool, time.Now().Format("20060102T150405Z0700"))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &ErrorLog{
		f:    f,
		w:    bufio.NewWriter(f),
		tool: tool,
		path: path,
	}, nil
}

// Path returns the resolved path for the log file.
func (e *ErrorLog) Path() string { return e.path }

// Count returns how many records have been written so far.
func (e *ErrorLog) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// Record appends one line: <RFC3339>\t<tool>\t<op>\t<target>\t<error>
func (e *ErrorLog) Record(op, target string, err error) {
	if err == nil {
		return
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n",
		time.Now().Format(time.RFC3339),
		e.tool, op, target, err.Error())
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.w.WriteString(line)
	e.count++
}

// Close flushes and closes the underlying file.
func (e *ErrorLog) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.w != nil {
		_ = e.w.Flush()
	}
	if e.f != nil {
		return e.f.Close()
	}
	return nil
}
