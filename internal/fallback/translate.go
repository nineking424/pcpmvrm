package fallback

import (
	"strings"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

// Translate는 Plan + Job → (자식 바이너리, args)로 변환한다.
// pcp/pmv/prm Op과 Job.Kind에 따라 적절한 표준 명령으로 매핑한다.
func Translate(p plan.Plan, j plan.Job) (string, []string) {
	switch p.Op {
	case plan.OpCopy:
		return translateCp(p, j)
	case plan.OpMove:
		return translateMv(p, j)
	case plan.OpRemove:
		return translateRm(p, j)
	}
	return "", nil
}

func translateCp(p plan.Plan, j plan.Job) (string, []string) {
	var args []string
	if j.Kind == plan.JobDirCopy {
		args = append(args, "-r")
	}
	if p.Overwrite {
		args = append(args, "-f")
	}
	if p.NoClobber {
		args = append(args, "-n")
	}
	if p.UpdateOnly {
		args = append(args, "-u")
	}
	if p.Verbose {
		args = append(args, "-v")
	}
	if pres := preserveArg(p.Preserve); pres != "" {
		args = append(args, pres)
	}
	args = append(args, p.RawFlags...)
	args = append(args, j.Src, j.Dst)
	return "/bin/cp", args
}

func translateMv(p plan.Plan, j plan.Job) (string, []string) {
	var args []string
	if p.Overwrite {
		args = append(args, "-f")
	}
	if p.NoClobber {
		args = append(args, "-n")
	}
	if p.UpdateOnly {
		args = append(args, "-u")
	}
	if p.Verbose {
		args = append(args, "-v")
	}
	args = append(args, p.RawFlags...)
	args = append(args, j.Src, j.Dst)
	return "/bin/mv", args
}

func translateRm(p plan.Plan, j plan.Job) (string, []string) {
	switch j.Kind {
	case plan.JobDirRemove:
		// rmdir(1)은 빈 디렉토리만 삭제. -d 옵션이 없는 prm용 흐름과 일치.
		return "/bin/rmdir", append([]string{}, j.Src)
	case plan.JobUnlink:
		var args []string
		if p.ForceMissing {
			args = append(args, "-f")
		}
		if p.Verbose {
			args = append(args, "-v")
		}
		args = append(args, p.RawFlags...)
		args = append(args, j.Src)
		return "/bin/rm", args
	}
	return "", nil
}

// preserveArg는 GNU coreutils cp의 `--preserve=...` 형식을 생성한다.
// BSD cp(macOS)는 이 형식을 거부하므로 fallback + Preserve 조합은 Linux 전용이다.
// macOS에서 메타데이터 보존이 필요하면 native 모드(--fallback 미사용)를 사용한다.
func preserveArg(pres plan.Preserve) string {
	var parts []string
	if pres.Mode {
		parts = append(parts, "mode")
	}
	if pres.Ownership {
		parts = append(parts, "ownership")
	}
	if pres.Timestamps {
		parts = append(parts, "timestamps")
	}
	if len(parts) == 0 {
		return ""
	}
	return "--preserve=" + strings.Join(parts, ",")
}
