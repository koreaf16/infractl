# Phase C — Compaction 4중 Stack + Recovery

## 1. 목표

현재 2단계 (mild/aggressive) compaction 을 **4중 stack (auto/reactive/micro/collapse) + recovery** 로 확장한다.  
목표: 100+ turn 장기 세션에서 **토큰 효율 30~40% 추가 절감**, PTL 413 발생 시 안정적 복구.

종료 시점에 사용자가 체감할 변화:
- 긴 세션에서 컨텍스트 초과로 인한 강제 종료가 줄어든다.
- 장기 세션에서 응답 품질이 유지된다 (오래된 도구 결과만 짧게 요약).

★ 보존: **circuit breaker (compactBreaker)** 그대로 유지 → 요약 LLM 연속 실패 시 자동 차단.

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 / 라인 |
|---|---|---|
| Compact stack (proactive) | `claude_cli/src/query.ts` | (396–544) |
| Recovery (reactive) | `claude_cli/src/query.ts` | (1062–1183) |
| autocompact | `claude_cli/src/services/compact/*` | 디렉토리 전체 |
| microcompact | `claude_cli/src/services/compact/*` | message-level 요약, tool_use_id 단위 |
| collapse drain | `claude_cli/src/services/compact/*` | staged collapse |
| Token budget | `claude_cli/src/...` | `applyToolResultBudget` (query.ts:379), `calculateTokenWarningState` |
| Snip module | `claude_cli/src/services/compact/*` | `snipCompactIfNeeded` (query.ts:403) |

→ 시작 전 위 파일 모두 Read.

---

## 3. 선행 조건

- [ ] Phase B §8 종료 조건 통과
- [ ] `internal/agent/query/Engine` 가 매 turn 진입 시 `compact.Stack.Apply(state)` 호출하도록 stub 이 들어가 있음
- [ ] §9 Phase B 종료 시 사용자 질문 답변 완료 (특히 Q3: 요약 모델 = Fast tier)

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/agent/compact/
├── stack.go              ← Stack 구조체 + Apply(state) 순차 적용
├── auto.go               ← proactive auto compaction (token warning 기반)
├── reactive.go           ← PTL 413 reactive compaction
├── micro.go              ← message-level (tool_use_id 단위) 요약
├── collapse.go           ← staged collapse + drain
├── budget.go             ← token budget tracker (applyToolResultBudget 동등)
├── snip.go               ← 오래된 turn 통째 요약
├── breaker.go            ← circuit breaker (★ 기존 코드 이전)
├── recovery.go           ← PTL/max_token/media 복구 분기 (Phase B의 stub 채움)
└── types.go              ← TokenState 열거, CompactionResult struct
```

### 수정

```
internal/agent/compaction.go
  ← 기존 코드를 internal/agent/compact/ 로 분할 이전
    이전 후 본 파일은 deprecated 주석 + 신 패키지로 forward (Phase B 종료 후 제거)

internal/agent/query/engine.go
  ← Phase B 의 stub 호출이 실제 compact.Stack 구현체로 wire up

internal/agent/query/recovery.go
  ← 본 phase 종료 시 compact/recovery.go 호출로 위임
```

### 제거 (Phase C 종료 후)
- `internal/agent/compaction.go` (기능 이전 완료 후)

---

## 5. 소단계 작업

### C.1  types + budget + snip
- claude_cli 참조: `query.ts:379` (applyToolResultBudget), `query.ts:403` (snipCompactIfNeeded)
- 작업:
  - `types.go` — `TokenState` 열거 (OK/Warning/Critical/Overflow), `CompactionResult{PreCount, PostCount, Reason, Strategy}`
  - `budget.go` — `applyToolResultBudget(state, maxBytes)` 가장 큰 tool_result 절단
  - `snip.go` — 오래된 turn 통째로 짧은 요약으로 교체
- 단위 테스트: 토큰 추정, budget 후 메시지 길이 확인

### C.2  breaker (기존 이전)
- 작업:
  - 기존 `internal/agent/compaction.go` 의 `compactBreaker` 코드를 `compact/breaker.go` 로 이전
  - 인터페이스 동일 (호출 호환)
- 단위 테스트: 3회 연속 실패 → open / half-open 시 한 번 시도 / 성공 시 close

### C.3  auto (proactive)
- claude_cli 참조: `query.ts:454` (autocompact), `services/compact/*`
- 작업:
  - `auto.go`:
    - `calculateTokenWarningState(state) TokenState`
    - `Apply(state)` — Warning 진입 시 mild, Critical 시 aggressive
    - aggressive 시 SessionSummaryManager 호출 (기존 재사용)
    - circuit breaker 보호
- 단위 테스트: TokenState 결정 (절댓값: Warning<20K, Critical<13K, Overflow<3K remaining — "75/87/100%" 표기는 오류)

### C.4  reactive (PTL 413)
- claude_cli 참조: `query.ts:1120` (reactiveCompact.tryReactiveCompact)
- 작업:
  - `reactive.go`:
    - `TryReactiveCompact(state, hasAttempted)` — 1회 한정 시도
    - 실패 시 trim 폴백
- 단위 테스트: 1회 후 hasAttempted 플래그 검증

### C.5  micro (message-level)
- claude_cli 참조: `query.ts:870-892` (CACHED_MICROCOMPACT, pendingCacheEdits)
- 작업:
  - `micro.go`:
    - `CompactIfNeeded(state)` — 오래된 tool_use_id 단위 메시지를 짧은 요약으로 교체
    - 캐시 친화 (boundary 유지) — 최근 N개는 손대지 않음 (예: N=10)
- 단위 테스트: 메시지 N개 미만일 때 noop / 초과 시 오래된 것만 요약

### C.6  collapse (staged drain)
- claude_cli 참조: `query.ts:441` (contextCollapse.applyCollapsesIfNeeded), `query.ts:1094-1117` (collapse drain retry)
- 작업:
  - `collapse.go`:
    - `ApplyIfNeeded(state)` — 한 번에 다 압축하지 않고 staged queue 에 적재
    - `Drain(state)` — 다음 turn 진입 시 staged 1개 적용
    - 우선순위 큐 (가장 큰 메시지부터)
- 단위 테스트: stage 적재 / drain 순서

### C.7  stack (전체 통합)
- claude_cli 참조: `query.ts:396-544` (5단계 순차 호출)
- 작업:
  - `stack.go`:
    ```go
    func (s *Stack) Apply(ctx, state) error {
      s.budget.Apply(state)
      s.snip.Apply(state)
      s.micro.CompactIfNeeded(state)
      s.collapse.ApplyIfNeeded(state)
      s.auto.Apply(state)
      return nil
    }
    ```
  - 각 단계 결과 slog 로 기록 (토큰 변화)
- 단위 테스트: 5단계 모두 호출 / 각 단계 noop 시 다음 단계로 진행

### C.8  recovery (PTL/max_token/media)
- claude_cli 참조: `query.ts:1062-1183`
- 작업:
  - `recovery.go`:
    ```go
    func (r *Recovery) Handle(ctx, state, err) (RecoveryAction, error) {
      switch {
      case isPTL413(err):
         if r.collapse.HasStaged() { return ActionDrainCollapse }
         if !state.TriedReactive { return ActionReactiveCompact }
         return ActionSurfaceError
      case isMaxOutputTokens(err):
         if !state.TriedMaxTokens64k { return ActionRetryMaxTokens(64*1024) }
         return ActionMultiTurnRecovery
      case isMedia(err):
         return ActionStripMedia
      case isFallback(err):
         return ActionStripSignaturesAndRetry
      default:
         return ActionSurfaceError
      }
    }
    ```
  - `query/engine.go` 가 에러 발생 시 `recovery.Handle` 호출, 액션에 따라 retry/yield error
- 단위 테스트: 각 에러 → 액션 결정 케이스

### C.9  query.Engine wire up
- 작업:
  - `engine.go`:
    - 매 turn 진입 시 `s.compact.Apply(ctx, state)` 호출 (Phase B 의 stub 교체)
    - 에러 발생 시 `s.recovery.Handle(ctx, state, err)` 호출
- 단위 테스트: 통합 happy path / PTL 발생 → reactive 호출 → 재시도 성공

---

## 6. CLAUDE.md 규칙 준수 포인트

- [ ] 각 compact 전략 파일 300줄 이내
- [ ] 모든 함수 첫 인자 ctx
- [ ] 요약 LLM 호출 실패 → 에러 wrap, breaker 카운트 증가
- [ ] 크리덴셜 마스킹 — 메시지 요약 시 입력에 포함된 비밀 노출 위험 (마스킹 헬퍼 의무)
- [ ] slog: 매 단계 PreCount, PostCount, Strategy 구조화 로그

---

## 7. 검증 방법

### 단위 테스트
- 각 전략 (auto/reactive/micro/collapse/snip/budget) 단위
- breaker 회귀
- recovery 분기

### 통합 테스트
- `//go:build integration`
- 실제 LLM (Fast tier) 로 요약 호출 → 결과 길이 검증

### E2E 시나리오
- 시나리오 1: 100 turn 장기 세션 → token usage 곡선 측정 (Phase B 대비 30%+ 절감)
- 시나리오 2: PTL 413 강제 발생 (대용량 입력) → reactive 작동 → 다음 turn 정상
- 시나리오 3: max_output_tokens 발생 → 64k 재시도 → 성공
- 시나리오 4: 요약 LLM 의도적 실패 → breaker open → trim 폴백
- 시나리오 5: collapse staged 다수 적재 → 매 turn drain 1개 → 모두 소진

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 기존 사용자 시나리오 회귀 0

---

## 8. 종료 조건

- [ ] §7 단위/통합/E2E 모두 통과
- [ ] 토큰 절감 30%+ 측정값 보고서
- [ ] `internal/agent/compaction.go` deprecated, 신 패키지 사용 검증 후 제거
- [ ] `docs/design/compaction.md` 작성/갱신
- [ ] `docs/infractl-architecture.md` 갱신
- [ ] `docs_mig/README.md` update

---

## 9. 다음 phase 진입 전 사용자 질문 항목

```
[ ] Q1. micro 의 "최근 N개 미보존" 임계값 — 10? 15? 사용자 환경별 조정 가능?
[ ] Q2. collapse 우선순위 — 가장 큰 메시지부터 vs 가장 오래된 것부터?
[ ] Q3. recovery 의 max_tokens 64k 재시도 임계 — 우리 모델별로 다른 값 필요?
[ ] Q4. Phase D (hook 이관) 진입 OK?  preflight/* 제거 시점은 D 끝에?
```

---

## 끝.
