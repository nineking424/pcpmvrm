package fallback

import (
	"runtime"
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

// preserveArg는 OS별로 cp(1)의 메타데이터 보존 옵션을 생성한다.
// Linux(GNU coreutils): `--preserve=mode,ownership,timestamps` 형태로 세분 지정.
// 그 외(BSD cp on macOS 등): `-p` 단일 옵션으로 모드/소유자/시간 모두 보존.
func preserveArg(pres plan.Preserve) string {
	if !pres.Mode && !pres.Ownership && !pres.Timestamps {
		return ""
	}
	if runtime.GOOS != "linux" {
		return "-p"
	}
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
	return "--preserve=" + strings.Join(parts, ",")
}
