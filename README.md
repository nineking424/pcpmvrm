# pcpmvrm

병렬 cp / mv / rm 도구 모음. 바닐라 명령에 `p`만 붙이면 동일한 시맨틱으로 병렬 실행됩니다.

## 상태 (2026-05-08)

- ✅ Plan 1: Foundation + `pcp`
- ✅ Plan 2: `pmv`
- ✅ Plan 3: `prm`
- ✅ Plan 4: `--fallback` 모드 (자식 프로세스 위임)

## 빌드

```bash
make build                       # bin/pcp 생성
go build -o bin/pmv ./cmd/pmv    # bin/pmv 생성
go build -o bin/prm ./cmd/prm    # bin/prm 생성
go test ./...                    # 단위 + 통합 테스트
```

## 사용 예시

```bash
# 단일 워커 (바닐라 cp와 동일한 처리량)
pcp -r src/ dst/

# 8 워커 병렬 복사
pcp -r --parallel=8 src/ dst/

# 메타데이터 보존(-a) + 병렬
pcp -ra --parallel=8 src/ dst/

# 트리거 파일은 마지막에
pcp -r --parallel=8 --strict-extension=.json src/ dst/

# 첫 에러에서 중단
pcp -r --parallel=8 --exit-on-error src/ dst/

# 사전 계획 확인 (실제 syscall 없음)
pcp -r --parallel=8 --dry-run src/ dst/
```

### pmv

```bash
# Cross-device 이동 (자동 감지, copy+unlink)
pmv --parallel=8 /mnt/disk1/data /mnt/disk2/data

# Same-device 이동 (자동 다운그레이드, rename(2) 한 번)
pmv /tmp/old /tmp/new
```

### prm

```bash
# 대량 삭제, 첫 에러에서 중단
prm -rf --parallel=16 --exit-on-error /var/cache/old/

# 빈 디렉토리만 (-d, 비어있지 않으면 ENOTEMPTY로 실패)
prm -d /tmp/maybe-empty/

# 사전 계획 확인 (실제 syscall 없음)
prm -r --parallel=8 --dry-run /var/log/old/
```

### `--fallback` (자식 프로세스 위임 모드)

native 모드가 지원하지 않는 옵션(`--reflink`, `--sparse`, `-Z` 등)을 써야 하거나
플랫폼 특수 동작이 필요할 때 `--fallback`을 사용합니다. Job마다 `/bin/cp`,
`/bin/mv`, `/bin/rm`, `/bin/rmdir`을 fork+exec로 호출하므로 native 대비 처리량이
크게 떨어집니다. 일반 워크로드에는 native 모드를 권장합니다.

```bash
# native가 거부하는 옵션을 그대로 cp(1)로 전달
pcp -r --parallel=8 --fallback --reflink=auto src/ dst/

# SELinux 컨텍스트 유지하며 이동
pmv --fallback -Z /etc/foo /etc/bar

# unknown short flag(-x 등)가 다음 인자를 값으로 소비하는 pflag 동작 회피용:
# `--` 구분자로 positional 인자를 명시
pcp --fallback -- -x /path/with/leading-dash dst/
```

`--fallback`이 켜지면 `pcp`/`pmv`/`prm`의 미지원 옵션 검사는 전부 우회됩니다.
unknown long flag(`--reflink=auto`처럼 `=값` 형태)는 그대로 전달되지만,
unknown short flag(`-x`)는 pflag의 한계로 다음 positional 인자를 값으로 소비할
수 있습니다. 이 경우 `--`로 옵션 끝을 명시하세요.

## 설계 문서

- [`docs/superpowers/specs/2026-05-08-pcpmvrm-design.md`](docs/superpowers/specs/2026-05-08-pcpmvrm-design.md)
- [`docs/superpowers/specs/2026-05-08-pcpmvrm-brainstorming-log.md`](docs/superpowers/specs/2026-05-08-pcpmvrm-brainstorming-log.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan1-foundation-and-pcp.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan1-foundation-and-pcp.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan2-pmv.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan2-pmv.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan3-prm.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan3-prm.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan4-fallback.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan4-fallback.md)

## 라이선스

TBD
