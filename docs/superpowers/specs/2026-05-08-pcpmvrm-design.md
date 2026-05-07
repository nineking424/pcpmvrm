# pcpmvrm — Parallel `cp` / `mv` / `rm` 설계 문서

- **작성일**: 2026-05-08
- **상태**: 설계 승인 (구현 전)
- **대상 플랫폼**: Linux (amd64/arm64) 우선, macOS는 best-effort
- **언어**: Go

## 1. 목적과 범위

리눅스 기반 파일 처리 시스템에서 수만~수백만 단위의 파일을 옮기거나 복사·삭제하는 작업을 수행할 때, 바닐라 `cp`/`mv`/`rm`은 단일 스레드로 동작하여 디스크 I/O가 남아도 처리량을 끌어올릴 수 없다. `gnu parallel`이나 `xargs` 기반 즉석 스크립트는 매번 작성하기 번거롭고 휴먼 에러에 취약하다.

`pcpmvrm`은 다음을 제공하는 세 개의 CLI 도구(`pcp`, `pmv`, `prm`)를 만든다.

- 바닐라 `cp`/`mv`/`rm` 시맨틱에 충실하다. 사용자가 기존 명령 앞에 `p`를 붙이는 것만으로 그대로 동작해야 한다.
- 워커 goroutine 기반 병렬 처리로 디스크 I/O 한계까지 끌어올린다.
- 일상적인 옵션은 native syscall로 직접 처리하고, 미지원 옵션은 `--fallback`으로 자식 프로세스에 위임한다.
- 학습 곡선을 최소화한다. 기본값은 바닐라와 동일하게 동작하며, 추가 기능은 명시적 플래그로만 활성화된다.

### 1.1. 비목표 (Out of Scope, 첫 릴리스)

- Windows 지원
- 네트워크 파일시스템 전용 최적화 (rsync delta 알고리즘, rclone S3 등)
- 체크포인트 기반 `--resume`
- T5 옵션의 native 구현 (`--reflink`, `--sparse`, xattr, ACL)
- 분모 포함 진행률(%)·ETA 표시 (streaming walk 모델로 의도적으로 제외)
- 압축·암호화
- TUI 대시보드

## 2. 결정된 정책 요약

| 항목 | 결정 |
|---|---|
| 명령 시그니처 | `pcp/pmv SRC DST`, `prm PATH` — 단일 소스만 받음 |
| 기본 워커 수 | `--parallel=1` (명시적으로 켜야 병렬화) |
| 워커 동작 모드 | Native syscall 직접 구현(B 모드) |
| `--fallback` 모드 | 자식 프로세스(`/bin/cp` 등)에 위임 |
| 미지원 옵션 입력 | 안내 메시지 출력 후 종료 (안내문에 `--fallback`과 성능 저하 경고 포함) |
| 직접 구현 옵션 범위 | T1+T2+T3 (필수, 메타데이터 보존, 충돌 처리) |
| `-i` (interactive prompt) | 거부 — 병렬과 본질적 충돌 |
| 병렬화 단위 (기본) | 파일 단위 |
| 병렬화 단위 (`--strict-order`) | 디렉토리 단위 + 디렉토리 내부 순서 보장 |
| `--strict-extension=LIST` | 트리거 시맨틱: 비대상 파일 완료 후 대상 확장자 직렬 처리. 다중 확장자는 한 그룹 |
| 에러 정책 (기본) | best-effort: 항목 단위 실패는 로그하고 계속 |
| 에러 정책 (`--exit-on-error`) | 첫 에러에서 graceful 중단 |
| 에러 로그 | 자동 (`./<tool>-failed-<timestamp>.log`), `--error-log=PATH`로 경로 지정 가능 |
| `pmv` cross-device | `stat.Dev` 비교 → 자동 폴백 (cp+unlink 흐름) |
| `pmv` same-device | 워커 자동 다운그레이드 (`--parallel=1` 강제) + 안내 |
| Walk 모델 | Streaming (walk와 처리 동시 진행) |
| 진행 표시 | 1초 단위 갱신, `files | bytes | files/s | MB/s | elapsed | errors` |
| 출력 인터리빙 | mutex 직렬화 (줄 단위) |
| `-v` vs `--fallback` 이름 충돌 | `pcp -f` = overwrite (바닐라), `pcp --fallback` = subprocess 위임 |
| `prm` 안전장치 | 바닐라와 동일 (`--no-preserve-root`만) |
| 시그널 처리 | 첫 SIGINT/SIGTERM = graceful, 두 번째 = 즉시 종료 |
| `--dry-run` | 지원 (walk + stderr 출력만, 실제 syscall 없음) |

## 3. 아키텍처

### 3.1. 컴포넌트

```
┌─────────────┐
│   CLI args  │  pcp / pmv / prm 진입점, internal/cli 공유
└──────┬──────┘
       │ Plan{Op, Src, Dst, Flags}
       ▼
┌─────────────┐    push    ┌──────────────┐  pop   ┌─────────────┐
│   Walker    │──────────▶│  Work Queue  │──────▶│  Worker(N)  │
│  (1 goroutine,           │ (chan Job,   │        │ goroutines  │
│   DFS)      │  bounded   │  cap = N×4)  │        │             │
└─────────────┘            └──────────────┘        └──────┬──────┘
                                                          │ result
                                                          ▼
                                                  ┌──────────────┐
                                                  │  Reporter    │
                                                  │ progress +   │
                                                  │ error log +  │
                                                  │ verbose mtx  │
                                                  └──────────────┘
```

### 3.2. 컴포넌트별 책임

| 컴포넌트 | 책임 |
|---|---|
| **Walker** | 단일 goroutine. `filepath.WalkDir`로 src 트리를 DFS. 모드별로 큐잉 단위가 달라짐: 기본=파일 Job, `--strict-order`=디렉토리 Job, `--strict-extension`=2-phase walk. **디렉토리 자체의 생성/삭제(`mkdir`, `rmdir`)는 워커에 위임하지 않고 walker가 직접 syscall한다 — 부모-자식 순서 보장이 단순해짐 (§5.7)** |
| **Work Queue** | bounded channel `chan Job`, 용량 `workers * 4`. walker가 워커보다 너무 빨리 가지 않도록 백프레셔. 메모리 사용량을 상수로 유지 |
| **Worker** | N개 goroutine. Job을 받아 syscall 직접 수행. `--fallback` 모드면 자식 프로세스 호출. 결과를 `chan Result`로 푸시 |
| **Reporter** | 단일 goroutine. Result 수신 → 통계 갱신 → 1초 단위 progress 갱신 (TTY 감지). `-v` 출력은 mutex로 직렬화. 실패 항목은 에러 로그에 줄단위 append |
| **Signal handler** | SIGINT/SIGTERM 핸들. 첫 신호 = walker stop + queue drain 폐기 + worker 현재 Job 완료 후 종료. 두 번째 = 즉시 종료 |

### 3.3. 패키지 레이아웃

```
pcpmvrm/
├── cmd/
│   ├── pcp/main.go
│   ├── pmv/main.go
│   └── prm/main.go
├── internal/
│   ├── cli/           # CLI 파싱, 옵션 매트릭스, 미지원 옵션 검증
│   ├── plan/          # Plan, Job, Result 자료형
│   ├── walk/          # Walker (default / strict-order / strict-extension)
│   ├── worker/        # Worker pool, native syscall 구현
│   ├── fallback/      # --fallback 모드 (자식 프로세스 wrapper)
│   ├── report/        # Progress, error log, verbose mutex, signal handling
│   └── fsx/           # syscall 헬퍼 (cross-device 감지, EXDEV 폴백, 메타데이터 보존)
├── docs/superpowers/specs/
└── go.mod
```

### 3.4. 데이터 흐름 — `pcp -r src/ dst/ --parallel=8`

1. CLI 파싱 → `Plan{Op: Copy, Recursive: true, Src: "src/", Dst: "dst/", Workers: 8}`
2. 사전 검증
   - `stat(src)`, `stat(dst)` 검증 (없으면 exit 2)
   - `pmv`라면 device ID 비교, same-device면 워커=1로 강제 + 안내 메시지
   - `-r` 없는데 src가 디렉토리면 거부
   - 미지원 옵션이 있으면 안내 후 exit 2
3. Walker 시작
   - `src/`를 DFS pre-order로 walk
   - 디렉토리를 만나면 walker가 직접 `mkdir(dst/...)` 수행 (워커에 위임 안 함)
   - 파일을 만나면 `copy` Job을 큐에 push
   - 파일을 큐잉하기 전에 그 부모 디렉토리는 walker가 이미 만들어 둔 상태이므로 race 없음
4. 워커 N개가 Job 소비, 결과를 Result channel로 보고
5. Reporter가 1초마다 한 줄 갱신:
   ```
   [pcp]  243,182 files | 1.4 GB | 8,231 files/s | 47 MB/s | 4m12s elapsed | 12 errors
   ```
6. Walker 종료 → 큐 close → 워커 drain → Reporter 최종 요약 (총 처리 수, 실패 수, 에러 로그 경로) → exit

## 4. CLI 인터페이스

### 4.1. 시그니처

```
pcp  [OPTIONS] SRC DST
pmv  [OPTIONS] SRC DST
prm  [OPTIONS] PATH
```

여러 소스 지정(`cp a b c d/`)은 지원하지 않는다. 활용도가 낮고 의도가 모호한 경우가 많아 의도적으로 배제한다.

### 4.2. 공통 플래그

| 플래그 | 기본값 | 의미 |
|---|---|---|
| `--parallel=N` | `1` | 워커 goroutine 수 |
| `--fallback` | `false` | native 대신 자식 프로세스(`/bin/cp` 등)로 위임 |
| `--strict-order` | `false` | 디렉토리 단위로 병렬화. 디렉토리 내부에서는 walk 순서대로 직렬 처리 |
| `--strict-extension=LIST` | 빈 값 | 콤마 구분 확장자 목록(`.json,.csv`). 비대상 파일이 모두 끝난 뒤 대상 확장자 파일만 lexical 순서로 직렬 처리 |
| `--exit-on-error` | `false` | 첫 에러에서 graceful 중단 (기본은 best-effort) |
| `--error-log=PATH` | 자동 | 실패 항목 기록 파일. 미지정 시 `./<tool>-failed-<RFC3339-timestamp>.log` |
| `--dry-run` | `false` | 실제 syscall 안 일으키고 계획만 stderr로 출력 |
| `--no-progress` | `false` | TTY여도 progress 라인 끔 |

### 4.3. 도구별 직접 구현(B) 옵션 — T1+T2+T3

| 도구 | 직접 구현 옵션 |
|---|---|
| `pcp` | `-r`/`-R`/`--recursive`, `-f`(overwrite), `-v`/`--verbose`, `-p`, `-a`, `--preserve=mode,ownership,timestamps`, `-n`/`--no-clobber`, `-u`/`--update` |
| `pmv` | `-f`(overwrite), `-v`, `-n`, `-u` |
| `prm` | `-r`/`-R`, `-f`(no error on missing), `-v`, `-d`(empty dir) |

### 4.4. 거부 옵션과 안내문

T4(심볼릭/하드링크 옵션), T5(고급), `-i`, 그 외 인식하지 못한 옵션은 모두 거부한다. 사용자가 입력하면 stderr에 다음 형식으로 안내 후 exit 2.

```
pcp: '--reflink'은 native 모드에서 지원하지 않습니다.
  - --fallback 옵션으로 자식 프로세스 위임 모드를 활성화하면 사용 가능합니다.
  - 단, 자식 프로세스 fork 비용이 발생하여 대량 파일 처리 시 성능이 크게 저하될 수 있습니다.
```

### 4.5. `-f` vs `--fallback` 이름 충돌 처리

바닐라 `cp -f`는 "overwrite" 의미이며 자주 쓰인다. 우리의 `--fallback`은 자식 프로세스 위임. 충돌을 피하기 위해:

- 짧은 옵션 `-f`는 바닐라 의미(overwrite, no-error-on-missing) 그대로 유지한다.
- fallback 모드는 긴 옵션 `--fallback`만 사용한다. 짧은 alias는 두지 않는다.

`man` 페이지와 `--help`에 명시한다.

## 5. 시맨틱 디테일

### 5.1. `--strict-order`

큐의 단위가 "파일 Job"이 아니라 "디렉토리 Job"이 된다.

- Walker는 디렉토리를 발견하면 그 디렉토리 자체를 Job으로 큐잉한다.
- Worker는 디렉토리 Job을 받으면 그 안의 항목들을 lexical 순서로 직렬 처리한다.
- 서브디렉토리도 마찬가지로 그 워커가 직렬로 들어간다 (워커 내부에서 재귀).
- 결과적으로 디렉토리 간에는 병렬, 디렉토리 내부에서는 순서 보장.

### 5.2. `--strict-extension=LIST`

두 페이즈로 walk를 나눈다.

**Phase 1**: walk 중 확장자가 LIST에 포함되지 않은 파일만 큐잉. 워커들이 `--parallel=N`으로 병렬 처리.

**Phase 2**: Phase 1의 모든 워커가 drain된 후 시작. Walker가 LIST 확장자 파일을 lexical 순서로 큐잉. 워커는 1로 다운그레이드되어 직렬 처리.

다중 확장자(`.json,.csv`)는 한 그룹으로 묶여 phase 2에서 함께 lexical 직렬 처리된다.

`--strict-order`와 동시 사용 시: `--strict-extension`이 우선. Phase 1 안에서만 `--strict-order`가 적용된다 (디렉토리 단위 병렬, 디렉토리 내 순서). Phase 2는 항상 직렬이므로 `--strict-order` 의미가 자동 충족.

### 5.3. `pmv` cross-device 동작

시작 시 `stat(src).Dev` vs `stat(parent(dst)).Dev` 비교.

- **same-device**: `rename(2)` 한 번으로 끝나는 atomic 모드. 워커=1로 강제. 사용자가 `--parallel=N` 지정해도 다운그레이드되며 안내 메시지 출력.
- **cross-device**: `pcp` 흐름과 동일하게 동작 (copy + unlink). `--parallel=N` 그대로 적용.
- **walk 도중 마운트포인트 경계 만남**: per-job EXDEV 발생 시 해당 Job만 cp+unlink로 폴백.

### 5.4. 충돌 처리 (`-n`, `-u`) — 병렬 환경

- **`-n` (no-clobber)**: 단순 `stat`+`open`은 race가 있으므로 `O_CREAT | O_EXCL`로 dst 파일 생성. 이미 존재하면 skip. race-free.
- **`-u` (update only newer)**: `stat(src).ModTime()` > `stat(dst).ModTime()` 비교 후 진행. race가 있어도 시맨틱은 "마지막에 더 새로운 mtime이 dst에 보장"이며, 이는 충분히 안전.

### 5.5. `--dry-run`

- Walker는 정상 walk
- Worker는 syscall 대신 stderr에 한 줄씩 출력 (`pcp src/file → dst/file`)
- 진행률·통계는 정상 표시
- 에러 로그 파일은 만들지 않음
- `--fallback` 모드와 결합 시: 자식 프로세스의 dry-run을 사용하지 않고, 자체 walker가 출력만 한다 (이유: 자식 프로세스 호출 자체가 비용이며 dry-run이라는 의미를 흐림)

### 5.6. `pcp` 임시 파일 전략

`pcp`는 dst를 직접 쓰지 않는다. 같은 디렉토리에 `<filename>.pcp-tmp-<6hex>`로 쓴 뒤 `os.Rename`으로 atomic하게 교체.

- **이유 1**: 부분 파일이 dst에 남는 사고 방지
- **이유 2**: atomic 가시성 — 다운스트림 소비자가 절반 쓰여진 파일을 보지 않음
- **이유 3**: 시그널/패닉 시 `defer cleanup()`으로 안전하게 정리
- **제약**: 같은 파일시스템 내에서만 atomic. 임시 파일은 dst와 같은 디렉토리에 생성되므로 권한도 동일.

### 5.7. 디렉토리 생성/삭제는 Walker가 직접 처리

병렬 워커가 디렉토리를 다루면 부모-자식 순서 보장이 까다로워진다 (자식 파일이 부모 디렉토리보다 먼저 처리되거나, 디렉토리 rmdir이 자식 unlink보다 먼저 시도되는 race). 이를 단순화하기 위해 디렉토리에 대한 syscall(`mkdir`, `rmdir`)은 워커에 큐잉하지 않고 walker가 직접 동기적으로 수행한다.

**`pcp -r`**:
- DFS pre-order. 디렉토리를 만나면 walker가 즉시 `mkdir(dst/path)` 후 자식 walk로 진행
- 자식 파일이 큐에 들어가는 시점엔 부모 디렉토리가 이미 dst에 존재
- 디렉토리 메타데이터 보존(`-p`/`-a`)도 walker가 처리: 자식 처리 완료를 기다린 뒤 timestamp 등을 dst에 적용. (timestamp는 자식 항목이 들어가면서 갱신되므로 마지막에 한 번 더 set 필요)

**`prm -r`**:
- DFS post-order. 자식들을 먼저 큐에 다 push한 후, 그 디렉토리에 대한 "barrier" 신호를 큐에 끼워넣어 워커들이 해당 디렉토리의 자식 unlink를 모두 끝낼 때까지 기다림
- 자식들이 끝나면 walker가 `rmdir(path)` 직접 수행
- 구현: barrier는 디렉토리당 하나의 sync.WaitGroup으로 표현. 자식 Job이 done할 때 wg.Done(), walker가 wg.Wait() 후 rmdir
- `--strict-order` 모드는 더 단순함 — 디렉토리 단위 Job이라 워커 한 명이 그 디렉토리의 자식 + rmdir까지 직렬 처리

**`pmv -r`** (cross-device):
- `pcp` + `prm`의 합성. mkdir(dst), 자식 copy + unlink, rmdir(src) 순서

이 정책의 트레이드오프: walker가 syscall 비용을 직접 감당하므로 디렉토리 수가 매우 많은 트리에서는 walker가 병목이 될 수 있다. 그러나 일반적인 워크로드에서 디렉토리 수는 파일 수보다 훨씬 적고, mkdir/rmdir은 빠른 syscall이므로 실용적 문제는 안 된다.

### 5.8. 기본 워커 수 = 1

`--parallel`을 명시하지 않으면 1이다. 즉 기본 동작은 바닐라와 사실상 동일 (병렬화 미발동). 이는 의도적이다 — 사용자가 명시적으로 병렬화를 켜야만 켜지므로 사고 방지.

## 6. 에러 처리

### 6.1. 분류와 동작

| 분류 | 예시 | 기본 동작 (best-effort) | `--exit-on-error` 동작 |
|---|---|---|---|
| 항목 단위 실패 | 권한 거부, 깨진 심볼릭 링크, 디스크 풀, 동시 수정 | 에러 로그에 기록, 다음 Job 계속 | 즉시 모든 워커 graceful 중단 |
| 사전 검증 실패 | src 없음, dst 부모 없음, `-r` 없이 디렉토리 지정 | 즉시 종료 (Job 시작 전) | 동일 |
| 미지원 옵션 | `--reflink`, `-i` 등 | 즉시 종료 + `--fallback` 안내 | 동일 |
| 시스템 신호 | SIGINT/SIGTERM | graceful (§7) | 동일 |
| Worker panic | 내부 버그 | recover → 에러 로그 기록 후 워커 1개만 종료, 다른 워커 계속 | 동일 |

### 6.2. Exit Code

| 코드 | 의미 |
|---|---|
| 0 | 모든 항목 성공 |
| 1 | 일부 항목 실패 (best-effort 모드에서 에러 1건 이상) |
| 2 | 사전 검증 실패 / 미지원 옵션 / 잘못된 인자 |
| 130 | SIGINT/SIGTERM으로 종료 (POSIX 관례) |

### 6.3. 에러 로그 형식

각 줄은 RFC3339 timestamp + tool + op + path + 에러 메시지의 탭 구분.

```
2026-05-08T15:23:01+09:00	pcp	copy	src/foo/bar.jpg → dst/foo/bar.jpg	permission denied
2026-05-08T15:23:02+09:00	pcp	mkdir	dst/foo/baz	file exists
```

## 7. 시그널 처리

### 7.1. 첫 SIGINT/SIGTERM (graceful)

1. Walker stop — 큐에 새 Job push 안 함
2. 큐를 drain하지 않음 — 남은 Job은 폐기
3. In-flight 워커들은 현재 Job 끝까지 진행
4. `pcp`의 in-flight 임시 파일은 `defer`에서 unlink
5. Reporter가 "Interrupted: X processed, Y skipped" 출력
6. exit 130

### 7.2. 두 번째 SIGINT/SIGTERM (force)

1. `os.Exit(130)` 즉시
2. In-flight 임시 파일은 정리되지 않음 (이름 패턴 `<filename>.pcp-tmp-<6hex>`로 사용자가 식별 가능)
3. 사용자에게 "Forced exit. Some temporary files may remain in dst" 안내

## 8. 테스트 전략

| 레이어 | 도구 | 검증 대상 |
|---|---|---|
| Unit | `testing` | walk 모드별 큐잉 순서, fsx 헬퍼(EXDEV 폴백, 메타데이터 보존), 옵션 파서, 미지원 옵션 거부 |
| Integration | `testing` + tmp 디렉토리 | 실제 트리 구성 → `pcp/pmv/prm` 실행 → dst 비교. table-driven |
| Cross-device | loopback mount (Linux 한정, CI 옵셔널) | `pmv` EXDEV 폴백. macOS는 unit으로 대체 |
| Strict modes | tmp 트리 + 결정적 sleep 주입 | `--strict-order` 디렉토리 내 순서, `--strict-extension` phase 분리 |
| 시그널 | child process를 띄우고 SIGINT | 임시 파일 정리, exit 130, in-flight 무결성 |
| Stress (옵셔널) | 100k 파일 생성 → 처리 → 검증 | regression. CI 기본 skip, `make stress` |
| 바닐라 동치성 (golden) | 같은 입력으로 `cp` vs `pcp --parallel=1` 결과 비교 (mode/timestamp/내용) | "p만 붙이면 동작" 회귀 방지 |

## 9. 빌드와 배포

- 단일 모듈, 세 개의 진입점: `go build ./cmd/...` → `pcp`, `pmv`, `prm` 세 바이너리
- 첫 릴리스: Linux amd64/arm64. macOS는 best-effort
- 의존성: 표준 라이브러리 + `golang.org/x/sys/unix`만. CLI 파싱도 표준 `flag` 또는 가벼운 라이브러리로 한정

## 10. 사용 예시

```bash
# 바닐라 cp 자리에 그대로 (병렬 미발동, --parallel 기본 1)
pcp -r src/ dst/

# 8 워커로 병렬 복사
pcp -r --parallel=8 src/ dst/

# 메타데이터 보존 + 8 워커
pcp -ra --parallel=8 src/ dst/

# 트리거 파일은 마지막에 (이미지 모두 옮긴 뒤 manifest)
pcp -r --parallel=8 --strict-extension=.json src/ dst/

# native 미지원 옵션은 fallback으로 (성능 저하)
pcp -r --parallel=8 --fallback --reflink=auto src/ dst/

# Cross-device 이동 (자동 감지, copy+unlink)
pmv --parallel=8 /mnt/disk1/data /mnt/disk2/data

# 같은 파일시스템 mv (워커=1로 자동 다운그레이드)
pmv --parallel=8 /tmp/old /tmp/new

# 대량 삭제, 첫 에러에서 중단
prm -rf --parallel=16 --exit-on-error /var/cache/old/

# 실행 전 dry-run으로 확인
pcp -r --parallel=8 --dry-run src/ dst/
```
