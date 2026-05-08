package report_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestProgress_RenderSnapshot(t *testing.T) {
	var buf bytes.Buffer
	p := report.NewProgress(&buf, "pcp", true /*forceTTY*/)

	p.AddBytes(1024 * 1024)
	p.IncFiles()
	p.IncFiles()
	p.IncErrors()

	// 외부 강제 렌더 (테스트 결정성)
	p.RenderNow(2 * time.Second)

	out := buf.String()
	for _, want := range []string{"pcp", "2 files", "errors", "MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("progress render missing %q\n%s", want, out)
		}
	}
}

func TestProgress_DisabledNoTTY(t *testing.T) {
	var buf bytes.Buffer
	p := report.NewProgress(&buf, "pcp", false /*forceTTY=false*/)
	p.IncFiles()
	p.RenderNow(time.Second)
	if buf.Len() != 0 {
		t.Fatalf("non-TTY mode wrote: %q", buf.String())
	}
}
