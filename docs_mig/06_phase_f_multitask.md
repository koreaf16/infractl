# Phase F — 멀티태스크 + 다단계 보강 (백그라운드 / Monitor / subagent 병렬 / 스케줄)

## 1. 목표

InfraCtl 의 **장기 실행 작업** 과 **다단계 작업** 을 claude_cli 수준으로 끌어올린다.

1. **자동 백그라운딩** — 임계 시간 (default 15s) 초과 명령은 자동으로 백그라운드 전환 + 결과 파일화
2. **Monitor 도구** — 백그라운드 작업의 실시간 출력 폴링
3. **Subagent 병렬 실행** — `claude_cli/AgentTool` 동등 (mini-agent 동시 다발)
4. **스케줄 고도화** — oneshot, retention, 로그 회전

종료 시점에 사용자가 체감할 변화:
- `npm install` 같은 5분짜리 명령이 자동 백그라운드 전환되어 다른 작업 가능.
- 여러 서버에 동시 SSH 호출 → 병렬 처리 (직렬 대비 N배 속도).
- `infractl schedule` 로 oneshot 작업 + 로그 자동 회전.

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 / 라인 |
|---|---|---|
| 자동 백그라운딩 | `claude_cli/src/tools/BashTool/BashTool.tsx` | 967-1001 (15s 임계 + 결과 파일화 알림) |
| 백그라운드 세션 | `claude_cli/src/utils/background/remote/remoteSession.ts` | 세션 관리, 출력 버퍼링 |
| TaskOutput | `claude_cli/src/utils/task/TaskOutput.ts` | 진행 폴링 + 부분 출력 yield |
| AgentTool (subagent) | `claude_cli/src/tools/AgentTool/runAgent.ts` | mini-agent fork, 결과 수집 |
| AgentTool 병렬 | `claude_cli/src/tools/AgentTool/AgentTool.tsx` | 동시 다발 호출, errgroup 패턴 |
| Schedule | `claude_cli/src/tools/ScheduleCronTool/CronCreateTool.ts` | cron + oneshot, retention |
| Monitor 도구 | `claude_cli/src/tools/MonitorTool/*` (해당 시) | 백그라운드 task stdout 라인 폴링 |

→ 본 phase 시작 전 위 파일 모두 Read.

---

## 3. 선행 조건

- [ ] Phase B 의 query.Engine + StreamingExecutor 안정 (subagent 병렬은 streaming executor 기반)
- [ ] Phase A 의 hook 시스템 (subagent 가 hook agent backend 와 통합)
- [ ] §9 Phase E 종료 시 사용자 질문 답변 완료 (특히 Q4: 백그라운딩 임계 15s vs 더 길게)

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/background/
├── manager.go              ← 백그라운드 세션 등록/조회/종료
├── file_storage.go         ← 결과 파일 (~/.infractl/bg/<id>.{stdout,stderr,status})
├── poller.go               ← stdout 라인 단위 폴링
├── promotion.go            ← foreground → background 자동 전환 로직
└── output_tool.go          ← 도구 함수: 백그라운드 결과 일부 가져오기

internal/tools/
└── monitor.go              ← Monitor 도구 (백그라운드 진행 폴링)

internal/subagent/
├── parallel.go             ← errgroup 기반 N개 동시 실행
├── isolation.go            ← 각 subagent 의 ctx, llm session 격리
└── result_collector.go     ← yield-as-ready (먼저 끝난 것부터 결과 반환)

internal/schedule/
├── oneshot.go              ← 1회성 작업 (한 번 실행 후 자동 정리)
├── retention.go            ← 결과 파일 보관 정책 (N일 / N개)
└── log.go                  ← 스케줄 실행 로그 회전 (~/.infractl/schedule.log)
```

### 수정

```
internal/tools/shell_exec.go
  ← is_background 플래그 처리
  ← 자동 백그라운딩 (15s 초과 시 promotion.go 호출)

internal/agent/query/streaming_executor.go (Phase B)
  ← subagent 병렬 실행 시 StreamingExecutor.Run 호출 (sibling abort 적용)

internal/schedule/scheduler.go (기존)
  ← oneshot, retention, log 통합
```

### 제거 (Phase F 종료 후)

- 기존 임시 백그라운드 코드 (있다면)
- 단일 subagent 직렬 호출 코드

---

## 5. 소단계 작업

### F.1  background manager + file storage
- 작업:
  - `background/manager.go`:
    - `Start(ctx, cmd, opts) (BgID, error)` — 새 세션 등록
    - `Get(id) (*Session, error)`, `List() []Session`, `Stop(id) error`
  - `background/file_storage.go`:
    - `~/.infractl/bg/<id>.stdout` 등 append-only 쓰기
    - 회전 (10MB 초과 시 .1, .2 ... rotation)
- 단위 테스트: 다중 세션 생성/조회/종료, 파일 출력 검증

### F.2  poller + output_tool
- 작업:
  - `background/poller.go`:
    - `Poll(id, fromOffset) (lines []string, newOffset int64, done bool, err error)`
    - tail 모드 (마지막 N 줄) 지원
  - `background/output_tool.go`:
    - 도구 함수로 LLM 이 호출 (인자: `bg_id`, `tail_lines`)
    - 결과: 최근 N 줄 + done 플래그
- 단위 테스트: 진행 중 / 완료 / 에러 케이스

### F.3  자동 백그라운딩 (promotion)
- claude_cli 참조: `BashTool.tsx:967-1001`
- 작업:
  - `background/promotion.go`:
    - `WatchAndPromote(ctx, cmd, threshold, runner)`:
      - threshold (default 15s) 도달 → background.Start 로 이전
      - tool_result 에 명시 알림: "백그라운드 전환됨, bg_id=xxx"
  - `tools/shell_exec.go` 가 호출
- 단위 테스트: 1초 명령 (foreground 유지) / 30초 명령 (자동 promote)

### F.4  Monitor 도구
- 작업:
  - `tools/monitor.go`:
    - 인자: `bg_id`, `poll_interval`, `max_duration`
    - 결과: 진행 출력 streaming yield (LLM 컨텍스트에 단계적 추가)
    - 출력량 제한 (한 번에 max KB) — Phase E 의 Q5 답변 따름
- 단위 테스트: 폴링 주기 / 최대 출력량 / 강제 종료

### F.5  subagent 병렬 (errgroup + isolation)
- claude_cli 참조: `AgentTool/runAgent.ts`, `AgentTool.tsx`
- 작업:
  - `subagent/parallel.go`:
    ```go
    func RunParallel(ctx, prompts []Prompt, opts Opts) ([]Result, error) {
      g, ctx := errgroup.WithContext(ctx)
      results := make([]Result, len(prompts))
      for i, p := range prompts {
        g.Go(func() error {
          r, err := runOne(ctx, p, opts)
          results[i] = r
          return err
        })
      }
      return results, g.Wait()
    }
    ```
  - `subagent/isolation.go`:
    - 각 subagent 가 자기만의 ctx, llm session, tool registry 가짐
    - 부모 세션과 격리 (도구 결과 / LLM history 공유 X)
- 단위 테스트: 5 subagent 동시 실행 → 모두 성공 / 1 실패 시 sibling cancel

### F.6  result collector (yield-as-ready)
- 작업:
  - `subagent/result_collector.go`:
    - `Collect(ch chan SubagentEvent) []Result`
    - 먼저 끝난 subagent 부터 즉시 yield (전체 완료 대기 X)
    - 진행률 알림 (3/5 완료)
- 단위 테스트: 시간차 다른 5 subagent → yield 순서 검증

### F.7  schedule oneshot + retention + log
- claude_cli 참조: `CronCreateTool.ts`
- 작업:
  - `schedule/oneshot.go`:
    - `ScheduleOnce(at time.Time, cmd) (TaskID, error)` — 한 번 실행 후 자동 삭제
  - `schedule/retention.go`:
    - 정책: `keep_days: 7, max_results: 100`
    - 매일 자정 정리 job
  - `schedule/log.go`:
    - 모든 schedule 실행을 `~/.infractl/schedule.log` 에 append
    - 100MB 초과 시 .1 회전
- 단위 테스트: oneshot 1회 실행 / retention 7일 정책 / 로그 회전

### F.8  query.Engine subagent hook 통합
- 작업:
  - hook agent backend (`hooks/backend/agent.go`) 가 `subagent/parallel.RunOne` 호출
  - mini-agent 의 결과 → hook output 으로 변환
- 단위 테스트: hook 호출 → mini-agent 실행 → hook output 결과 검증

---

## 6. CLAUDE.md 규칙 준수 포인트

- [ ] 각 파일 300줄 이내 (manager / poller / promotion 분리)
- [ ] file header DocBlock
- [ ] 모든 함수 첫 인자 `ctx context.Context`
- [ ] errgroup 사용 시 ctx 전파 (한 subagent 실패 → 모든 sibling cancel)
- [ ] background 결과 파일 권한 (0600 — 사용자 외 읽기 금지)
- [ ] schedule 로그에 크리덴셜 마스킹 (cmd 안에 비밀 가능)
- [ ] slog: bg_id, subagent_id, duration 구조화 로그

---

## 7. 검증 방법

### 단위 테스트
- background manager / poller / promotion
- monitor 도구
- subagent 병렬 / isolation / collector
- schedule oneshot / retention / log

### 통합 테스트
- `//go:build integration`
- 실제 30초 명령 → 자동 백그라운딩 → Monitor 폴링 → 완료
- 실제 LLM 으로 5 subagent 병렬 → 결과 수집

### E2E 시나리오
- 시나리오 1: `npm install` (5분) → 15s 후 자동 백그라운딩 → 사용자가 다른 작업 → 5분 후 Monitor 로 결과 확인
- 시나리오 2: 5 서버에 동시 ssh `df -h` → subagent 병렬 → 5개 결과 수집
- 시나리오 3: 1 서버 ssh 실패 → sibling 4개 cancel → 부분 결과 + 에러 보고
- 시나리오 4: schedule oneshot 1시간 후 backup 실행 → 완료 후 자동 정리
- 시나리오 5: schedule 100회 실행 → retention 정책 적용 → 100개 결과만 보관
- 시나리오 6: schedule.log 50MB 도달 → 회전 검증
- 시나리오 7: hook agent backend → mini-agent 실행 → hook output 정상

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 회귀 0

---

## 8. 종료 조건

- [ ] §7 모든 검증 통과
- [ ] 5 subagent 병렬 시 직렬 대비 3x+ 속도 측정값 보고
- [ ] background 자동 promotion E2E 시나리오 통과
- [ ] `docs/design/multitask.md` 작성/갱신
- [ ] `docs/infractl-architecture.md` Background/Schedule 섹션 갱신
- [ ] `docs_mig/README.md` update

---

## 9. 다음 phase (G) 진입 전 사용자 질문 항목

```
[ ] Q1. 자동 백그라운딩 임계 — claude_cli 의 15s 그대로 vs 우리 환경 (서버 작업) 30s/60s?
[ ] Q2. background 결과 파일 보관 — 기본 7일? 사용자 설정 가능?
[ ] Q3. subagent 병렬 동시 실행 상한 — 5? 10? CPU/메모리 한도 검토 필요
[ ] Q4. Monitor 도구 출력량 — 한 번에 max KB? 한 세션당 누적 max KB?
[ ] Q5. Phase G (Plan Mode + 사용자 hook + CLI) 진입 OK?
```

---

## 끝.
