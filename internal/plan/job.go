package plan

import "io/fs"

// JobKind classifies a unit of work passed from walker to worker.
type JobKind int

const (
	JobCopy JobKind = iota
	JobUnlink
	JobDirCopy   // strict-order: 디렉토리 단위 복사
	JobDirRemove // prm post-order: 디렉토리 비우기 + rmdir
	JobRename    // pmv same-device: os.Rename 한 번
)

// String returns the canonical string representation used in error logs.
func (k JobKind) String() string {
	switch k {
	case JobCopy:
		return "copy"
	case JobUnlink:
		return "unlink"
	case JobDirCopy:
		return "dir-copy"
	case JobDirRemove:
		return "dir-remove"
	case JobRename:
		return "rename"
	}
	return "?"
}

// Job is a single unit of work pushed onto the work queue.
//
// For JobCopy: Src/Dst는 절대 경로, RelPath는 src 트리 루트로부터의 상대 경로
// (에러 로그/verbose 출력에 사용).
type Job struct {
	Kind    JobKind
	Src     string
	Dst     string
	RelPath string
	Info    fs.FileInfo
	// Done is invoked by the worker pool after this Job is processed
	// (success or failure). nil is safe — used by the prm walker to coordinate
	// "parent rmdir after all children unlink" via sync.WaitGroup.
	Done func()
}

// Finish invokes Done if non-nil. Called by the worker pool exactly once per Job.
func (j Job) Finish() {
	if j.Done != nil {
		j.Done()
	}
}
