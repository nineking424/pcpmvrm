package report_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestErrorLog_WriteAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "errs.log")

	log, err := report.NewErrorLog(path, "pcp")
	if err != nil {
		t.Fatal(err)
	}
	log.Record("copy", "src/a → dst/a", errors.New("permission denied"))
	log.Record("mkdir", "dst/x", errors.New("file exists"))
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	body := string(data)
	for _, want := range []string{"pcp", "copy", "src/a → dst/a", "permission denied", "mkdir", "file exists"} {
		if !strings.Contains(body, want) {
			t.Errorf("log missing %q\n--- log ---\n%s", want, body)
		}
	}
}

func TestErrorLog_AutoPath(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	_ = os.Chdir(dir)

	log, err := report.NewErrorLog("", "pcp")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if !strings.HasPrefix(filepath.Base(log.Path()), "pcp-failed-") {
		t.Errorf("auto path not prefixed correctly: %s", log.Path())
	}
}

func TestErrorLog_Concurrent(t *testing.T) {
	dir := t.TempDir()
	log, err := report.NewErrorLog(filepath.Join(dir, "c.log"), "pcp")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			log.Record("copy", "x", errors.New("e"))
		}(i)
	}
	wg.Wait()
	_ = log.Close()
}
