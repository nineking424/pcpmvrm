package report

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// Progress maintains throughput counters and writes a single re-rendered line.
type Progress struct {
	w       io.Writer
	tool    string
	tty     bool
	files   atomic.Int64
	bytes   atomic.Int64
	errors  atomic.Int64
	skipped atomic.Int64
}

// NewProgress returns a Progress. tty=true forces rendering even on non-TTYs
// (used by tests). Production code passes the result of isatty(stdout).
func NewProgress(w io.Writer, tool string, tty bool) *Progress {
	return &Progress{w: w, tool: tool, tty: tty}
}

func (p *Progress) IncFiles()        { p.files.Add(1) }
func (p *Progress) IncErrors()       { p.errors.Add(1) }
func (p *Progress) IncSkipped()      { p.skipped.Add(1) }
func (p *Progress) AddBytes(n int64) { p.bytes.Add(n) }

// Files/Bytes/Errors/Skipped are accessors used by Reporter.Final.
func (p *Progress) Files() int64   { return p.files.Load() }
func (p *Progress) Bytes() int64   { return p.bytes.Load() }
func (p *Progress) Errors() int64  { return p.errors.Load() }
func (p *Progress) Skipped() int64 { return p.skipped.Load() }

// RenderNow writes the current snapshot. Caller passes elapsed duration.
func (p *Progress) RenderNow(elapsed time.Duration) {
	if !p.tty {
		return
	}
	files := p.files.Load()
	bytesV := p.bytes.Load()
	errs := p.errors.Load()
	secs := elapsed.Seconds()
	if secs <= 0 {
		secs = 1
	}
	fps := float64(files) / secs
	bps := float64(bytesV) / secs

	line := fmt.Sprintf("\r[%s]  %s files | %s | %s files/s | %s/s | %s elapsed | %d errors",
		p.tool,
		humanInt(files), humanBytes(bytesV),
		humanFloat(fps), humanBytes(int64(bps)),
		elapsed.Truncate(time.Second), errs,
	)
	fmt.Fprint(p.w, line)
}

// Loop blocks until done is closed, rendering every tick.
func (p *Progress) Loop(done <-chan struct{}, tick time.Duration) {
	if !p.tty {
		<-done
		return
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	start := time.Now()
	for {
		select {
		case <-done:
			fmt.Fprint(p.w, "\n")
			return
		case <-t.C:
			p.RenderNow(time.Since(start))
		}
	}
}

func humanInt(n int64) string { return fmt.Sprintf("%d", n) }

func humanFloat(f float64) string {
	switch {
	case f >= 1e6:
		return fmt.Sprintf("%.1fM", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1fK", f/1e3)
	}
	return fmt.Sprintf("%.0f", f)
}

func humanBytes(b int64) string {
	const k = 1024
	switch {
	case b >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(k*k))
	case b >= k:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(k))
	}
	return fmt.Sprintf("%d B", b)
}
