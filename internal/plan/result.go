package plan

import "time"

// Result is what a worker reports back after handling a Job.
//
// Bytes is set only for successful Copy jobs.
type Result struct {
	Job     Job
	Err     error
	Bytes   int64
	Elapsed time.Duration
	Skipped bool   // -n / -u 등으로 의도적으로 건너뜀
	Stdout  string // fallback 모드에서 자식의 stdout 캡처 (verbose에 표시)
}
