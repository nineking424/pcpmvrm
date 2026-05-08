package report_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/nineking424/pcpmvrm/internal/report"
)

func TestSignal_GracefulOnFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := report.NewSignal(ctx)
	g.Notify(syscall.SIGUSR1) // SIGINT 대신 테스트 안전 신호

	// 첫 신호: ctx 취소
	g.Trigger(syscall.SIGUSR1)

	select {
	case <-g.Ctx().Done():
		// ok
	case <-time.After(time.Second):
		t.Fatal("first signal did not cancel ctx")
	}
}

func TestSignal_HardExitOnSecondReturnsTrue(t *testing.T) {
	ctx := context.Background()
	g := report.NewSignal(ctx)
	g.Notify(syscall.SIGUSR1)

	g.Trigger(syscall.SIGUSR1)              // graceful
	if g.Trigger(syscall.SIGUSR1) != true { // 두 번째: hard
		t.Fatal("second signal should signal hard-exit")
	}
}
