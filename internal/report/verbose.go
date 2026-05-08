package report

import (
	"fmt"
	"io"
	"sync"
)

// Verbose serializes -v output through a mutex so worker lines never interleave.
type Verbose struct {
	mu      sync.Mutex
	w       io.Writer
	enabled bool
}

// NewVerbose returns a Verbose tied to w. When enabled is false, Logf is a no-op.
func NewVerbose(w io.Writer, enabled bool) *Verbose {
	return &Verbose{w: w, enabled: enabled}
}

// Logf writes one line. Trailing newline is appended automatically.
func (v *Verbose) Logf(format string, a ...any) {
	if !v.enabled {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	fmt.Fprintf(v.w, format, a...)
	_, _ = v.w.Write([]byte{'\n'})
}
