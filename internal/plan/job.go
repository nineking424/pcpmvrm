package plan

import "io/fs"

// JobKind classifies a unit of work passed from walker to worker.
type JobKind int

const (
	JobCopy JobKind = iota
	JobUnlink
	JobDirCopy   // strict-order: 디렉토리 단위 복사
	JobDirRemove // prm post-order: 디렉토리 비우기 + rmdir
)

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
}
