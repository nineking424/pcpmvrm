# pcpmvrm

병렬 cp / mv / rm 도구 모음. 바닐라 명령에 `p`만 붙이면 동일한 시맨틱으로 병렬 실행됩니다.

## 상태 (2026-05-08)

- ✅ Plan 1: Foundation + `pcp`
- ✅ Plan 2: `pmv`
- ⏳ Plan 3: `prm`
- ⏳ Plan 4: `--fallback` 모드 (자식 프로세스 위임)

## 빌드

```bash
make build                       # bin/pcp 생성
go build -o bin/pmv ./cmd/pmv    # bin/pmv 생성
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

## 설계 문서

- [`docs/superpowers/specs/2026-05-08-pcpmvrm-design.md`](docs/superpowers/specs/2026-05-08-pcpmvrm-design.md)
- [`docs/superpowers/specs/2026-05-08-pcpmvrm-brainstorming-log.md`](docs/superpowers/specs/2026-05-08-pcpmvrm-brainstorming-log.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan1-foundation-and-pcp.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan1-foundation-and-pcp.md)
- [`docs/superpowers/plans/2026-05-08-pcpmvrm-plan2-pmv.md`](docs/superpowers/plans/2026-05-08-pcpmvrm-plan2-pmv.md)

## 라이선스

TBD
