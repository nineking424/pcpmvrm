package walk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Remove는 prm용 post-order DFS walker.
// 파일은 JobUnlink, 디렉토리는 JobDirRemove로 emit한다.
// parent *sync.WaitGroup barrier를 통해 자식 unlink + 자식 rmdir이 모두
// 완료된 뒤에야 부모 rmdir이 큐에 push된다.
type Remove struct{ p plan.Plan }

// NewRemove는 Remove walker를 만든다.
func NewRemove(p plan.Plan) *Remove { return &Remove{p: p} }

// Walk는 src 경로를 검사하고 jobs 채널에 push한다.
func (w *Remove) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	fi, err := os.Lstat(w.p.Src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && w.p.ForceMissing {
			return nil
		}
		return err
	}
	if !fi.IsDir() {
		return pushJob(ctx, jobs, plan.Job{Kind: plan.JobUnlink, Src: w.p.Src})
	}
	if !w.p.Recursive {
		if w.p.RemoveEmptyDir {
			return pushJob(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: w.p.Src})
		}
		return fmt.Errorf("prm: %s is a directory (use -r or -d)", w.p.Src)
	}
	return w.walkDir(ctx, jobs, w.p.Src, nil)
}

// walkDir은 DFS post-order로 dir을 순회한다.
// 호출자가 local.Add(1)을 미리 해 두고, 이 함수는 자기 JobDirRemove의
// Done에 parent.Done을 넣어 호출자의 wg를 해제한다.
// ReadDir 실패 시 parent.Done()을 직접 호출해 보상한다.
func (w *Remove) walkDir(ctx context.Context, jobs chan<- plan.Job, dir string, parent *sync.WaitGroup) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if parent != nil {
			parent.Done()
		}
		return err
	}

	var local sync.WaitGroup
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			local.Add(1)
			if err := w.walkDir(ctx, jobs, path, &local); err != nil {
				return err
			}
			continue
		}
		local.Add(1)
		if err := pushJob(ctx, jobs, plan.Job{Kind: plan.JobUnlink, Src: path, Done: local.Done}); err != nil {
			local.Done()
			return err
		}
	}

	// 자식 unlink + 자식 rmdir이 모두 끝나길 기다린다.
	waitCh := make(chan struct{})
	go func() { local.Wait(); close(waitCh) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh:
	}

	// 자기 rmdir Job emit. parent wg가 있으면 Done을 연결한다.
	var doneFn func()
	if parent != nil {
		doneFn = parent.Done
	}
	return pushJob(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: dir, Done: doneFn})
}

// pushJob은 ctx 취소를 존중하며 단일 Job을 push한다.
func pushJob(ctx context.Context, jobs chan<- plan.Job, j plan.Job) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case jobs <- j:
		return nil
	}
}
