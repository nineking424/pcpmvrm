package walk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Move는 pmv용 walker.
//   - SameDevice = true 이면 src 트리 전체를 단일 JobRename 하나로 emit.
//   - SameDevice = false 이면 pcp와 동일한 pre-order 방식으로 mkdir + 파일 JobCopy를
//     emit하고, 각 파일 직후 JobUnlink, post-order에서 JobDirRemove를 emit한다.
type Move struct {
	p plan.Plan
}

// NewMove는 Move walker를 만든다.
func NewMove(p plan.Plan) *Move { return &Move{p: p} }

// Walk는 trees를 따라가며 jobs 채널에 push한다.
func (w *Move) Walk(ctx context.Context, jobs chan<- plan.Job) error {
	if w.p.SameDevice {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- plan.Job{Kind: plan.JobRename, Src: w.p.Src, Dst: w.p.Dst}:
		}
		return nil
	}
	return w.walkCrossDevice(ctx, jobs)
}

func (w *Move) walkCrossDevice(ctx context.Context, jobs chan<- plan.Job) error {
	srcInfo, err := os.Lstat(w.p.Src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		// 단일 파일: copy + unlink만.
		return pushAll(ctx, jobs,
			plan.Job{Kind: plan.JobCopy, Src: w.p.Src, Dst: w.p.Dst},
			plan.Job{Kind: plan.JobUnlink, Src: w.p.Src},
		)
	}

	// 디렉토리 트리: pre-order에서 mkdir + 파일 copy/unlink, post-order에서 rmdir.
	type dirEntry struct {
		src string
		dst string
	}
	var dirStack []dirEntry

	err = filepath.WalkDir(w.p.Src, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(w.p.Src, srcPath)
		dstPath := filepath.Join(w.p.Dst, rel)

		if d.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			dirStack = append(dirStack, dirEntry{src: srcPath, dst: dstPath})
			return nil
		}
		if err := pushAll(ctx, jobs,
			plan.Job{Kind: plan.JobCopy, Src: srcPath, Dst: dstPath},
			plan.Job{Kind: plan.JobUnlink, Src: srcPath},
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// post-order로 rmdir Job emit. dirStack은 push 순서가 pre-order이므로 역순이 post-order.
	for i := len(dirStack) - 1; i >= 0; i-- {
		if err := pushAll(ctx, jobs, plan.Job{Kind: plan.JobDirRemove, Src: dirStack[i].src}); err != nil {
			return err
		}
	}
	return nil
}

// pushAll은 ctx 취소를 존중하며 여러 Job을 직렬로 push한다.
func pushAll(ctx context.Context, jobs chan<- plan.Job, batch ...plan.Job) error {
	for _, j := range batch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- j:
		}
	}
	return nil
}
