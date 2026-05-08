package report_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestVerbose_NoInterleave(t *testing.T) {
	var buf bytes.Buffer
	v := report.NewVerbose(&buf, true)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v.Logf("line-%d-with-some-content", i)
		}(i)
	}
	wg.Wait()

	for _, ln := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if !strings.HasPrefix(ln, "line-") {
			t.Fatalf("interleaved: %q", ln)
		}
	}
}

func TestVerbose_Disabled(t *testing.T) {
	var buf bytes.Buffer
	v := report.NewVerbose(&buf, false)
	v.Logf("ignored")
	if buf.Len() != 0 {
		t.Fatalf("disabled verbose wrote: %q", buf.String())
	}
}
