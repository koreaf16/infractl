# Phase B — Query Engine 교체 (★ 가장 큰 리스크)

## 1. 목표

기존 `for i<maxToolLoop(50)` 단순 루프를 **state machine + streaming 기반 query engine** 으로 직접 교체한다.  
기존 루프는 보존하지 않고 제거하며, 골든 시나리오 회귀 통과를 교체 안전장치로 삼는다.

종료 시점에 사용자가 체감할 변화:
- 도구 결과의 **즉시 yield** (긴 도구 진행 중 짧은 도구 결과 먼저 전달 → 응답성 향상)
- **sibling abort** (한 mutation 도구 실패 시 형제 도구 자동 중단)
- 명확한 종료 사유 (`completed`, `aborted_streaming`, `prompt_too_long`, `model_error`, `interrupted` 등)

★ 우리 차별점은 그대로 유지: **classify는 query.Engine 진입 전 1회만 실행** (turn 마다 재실행 X).

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 / 라인 |
|---|---|---|
| Query 메인 함수 | `claude_cli/src/query.ts` | `query()` (219), `queryLoop` (241) |
| State machine | `claude_cli/src/query.ts` | `state = {}` 패턴 (205, 1099–1165) |
| 모델 호출 + 스트리밍 | `claude_cli/src/query.ts` | (653–863) |
| 에러 복구 (PTL 등은 Phase C) | `claude_cli/src/query.ts` | (864–1183) |
| Streaming Tool Executor | `claude_cli/src/services/tools/StreamingToolExecutor.ts` | 클래스 전체 (40–519) |
| Tool 파티션/병렬 | `claude_cli/src/services/tools/toolOrchestration.ts` | `getMaxToolUseConcurrency` (8–12), `partitionToolCalls` (91–116), `runToolsConcurrently` (152–177) |
| QueryEngine 통합 | `claude_cli/src/QueryEngine.ts` | `submitMessage` (209–250) — context fetch + query() delegate |
| Sibling abort | `claude_cli/src/tools/BashTool/BashTool.tsx` | `siblingAbortController` 패턴 |

→ Phase B 시작 전 위 파일 모두 Read.

---

## 3. 선행 조건

- [x] Phase A §8 종료 조건 모두 통과
- [x] `internal/context/` `internal/cache/prefix_marker.go` 가 정상 동작 (Phase A 산출물)
- [x] §9 Phase A 종료 시 사용자 질문 5개 답변 완료
- [x] 골든 시나리오 회귀 테스트 데이터 위치 합의 (`testdata/golden/` 권장)
- [x] Query Engine 직접 교체 범위 합의 (`loop_llm.go` 제거 포함)

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/agent/query/
├── engine.go               ← Engine struct, Run(ctx, params) (<-chan QueryEvent, error)
├── state.go                ← QueryState struct (messages, transition, budget, ...)
├── transition.go           ← Continue/Terminal reasons (enum + helper)
├── recovery.go             ← 골격: PTL/max_token/media 분기 (실제 복구는 Phase C)
├── streaming_executor.go   ← StreamingExecutor (병렬 + immediate yield + sibling abort)
├── partition.go            ← partitionToolCalls (read/write 분할 + isConcurrencySafe)
├── concurrency.go          ← max concurrency 결정 (env INFRACTL_MAX_TOOL_CONCURRENCY, default 10)
├── events.go               ← QueryEvent 타입 (StreamStart, AssistantChunk, ToolUse, ToolResult, Terminal, ErrorEvent ...)
└── tool_invoker.go         ← 단일 도구 실행 wrapper (PreToolUse hook 호출 자리만 마련, 실제 활성화는 Phase D)
```

### 수정

```
internal/agent/loop.go
  ← classify 후 systemPrompt 조립 (기존 로직) 까지는 동일
    그 다음은 query.Engine.Run(ctx, params) → 이벤트 소비

internal/agent/loop_llm.go
  ← query.Engine 교체 완료 후 제거

internal/agent/tool_exec.go
  ← partition 로직을 query/partition.go 로 이동
```

### 제거

```
internal/agent/loop_llm.go
  ← 기존 단순 루프 제거. query.Engine 이 단일 실행 경로가 된다.
```

---

## 5. 소단계 작업

### B.1  events 타입 + State struct
- claude_cli 참조: `query.ts:205` (state 패턴), `query.ts:241-` (yield 종류)
- 작업:
  - `events.go` — `QueryEvent` interface + 구현체:
    - `EventStreamStart`
    - `EventAssistantChunk{Text string, Thinking bool}`
    - `EventToolUseStart{ID, Name, Input}`
    - `EventToolResult{ID, Output, Error}`
    - `EventTerminal{Reason TerminalReason}`
    - `EventError{Err error, Recoverable bool}`
  - `state.go` — `QueryState{Messages, Tier, ToolBudget, TurnIndex, ...}`
  - `transition.go` — `Continue` vs `Terminal` (reason: `completed`/`aborted_streaming`/`prompt_too_long`/`model_error`/`interrupted`/`tool_loop_exceeded`)
- 산출물: 위 3개 파일
- 단위 테스트: state mutation, transition 결정 로직

### B.2  Engine 골격 + 스트리밍 채널
- claude_cli 참조: `query.ts:219, 241`, `QueryEngine.ts:209-250`
- 작업:
  - `engine.go`:
    ```go
    type Engine struct {
      llm     llm.Client
      tools   tools.Registry
      hooks   hooks.Runner
      compact compact.Stack    // Phase C 까지는 stub
      logger  *slog.Logger
    }
    func (e *Engine) Run(ctx context.Context, p Params) (<-chan QueryEvent, error)
    ```
  - 내부 goroutine 에서 무한 루프 (Continue/Terminal 결정)
  - 매 turn:
    1. `compact.Apply(state)` (Phase C 까지는 noop)
    2. `e.llm.ChatStream(...)` 호출
    3. assistant chunk → yield
    4. tool_use 모이면 `streamingExecutor.Add(...)`
    5. tool 결과 yield, state.messages 업데이트
    6. tool_use 없으면 Terminal{completed}
- 산출물: `engine.go` (300줄 초과 시 분할)
- 단위 테스트: mock LLM + mock tool 로 happy path

### B.3  partition + concurrency
- claude_cli 참조: `toolOrchestration.ts:8-12, 91-116, 152-177`
- 작업:
  - `partition.go` — `partitionToolCalls([]ToolUse) (concurrent, sequential []ToolUse)`
    - 도구 인터페이스에 `IsConcurrencySafe()` 추가 가능 (현재는 `IsReadOnly()` 재사용 + 향후 확장 자리만)
  - `concurrency.go` — env `INFRACTL_MAX_TOOL_CONCURRENCY`, default 10
- 산출물: 위 2개
- 단위 테스트: read-only 만 / mutation 만 / 혼합 케이스

### B.4  StreamingExecutor (병렬 + immediate yield + sibling abort)
- claude_cli 참조: `StreamingToolExecutor.ts:40-519`, `BashTool.tsx (siblingAbortController)`
- 작업:
  - `streaming_executor.go`:
    ```go
    type StreamingExecutor struct {
      maxConcurrency int
      siblingCancel  context.CancelFunc   // mutation batch 안에서 공유
      results        chan ToolResult       // 완료 즉시 push
    }
    func (s *StreamingExecutor) Add(tu ToolUse, runFn func(ctx) (Output, error))
    func (s *StreamingExecutor) Results() <-chan ToolResult
    func (s *StreamingExecutor) GetRemaining() []ToolUse  // abort 시 사용
    ```
  - read-only batch: errgroup 으로 병렬, 결과 도착 즉시 채널 push
  - mutation batch: 직렬, 한 도구 실패 시 sibling cancel → 이후 도구 skip + tool_result 에 "skipped due to sibling failure"
- 산출물: `streaming_executor.go`
- 단위 테스트:
  - 병렬 batch 결과 순서 무관, 모든 결과 도착 검증
  - sibling abort 시 후속 도구 skip + 정확한 reason
  - ctx cancel 시 GetRemaining 으로 합성 결과 생성

### B.5  tool_invoker (PreToolUse hook 자리 마련)
- claude_cli 참조: `services/tools/toolExecution.ts` (PreToolUse 통합 부분)
- 작업:
  - `tool_invoker.go`:
    ```go
    func (e *Engine) invokeTool(ctx, tu ToolUse) (Output, error) {
      // PreToolUse hook (Phase A의 runner; Phase D 까지는 hooks.yaml 비어있어 통과)
      if res, err := e.hooks.RunPreToolUse(ctx, tu); err != nil { return nil, err }
      else if res.Denied { return ... }
      else if res.NewInput != nil { tu.Input = res.NewInput }
      
      // 실제 도구 실행
      out, err := e.tools.Get(tu.Name).Execute(ctx, tu.Input)
      
      // PostToolUse hook
      e.hooks.RunPostToolUse(ctx, tu, out, err)
      return out, err
    }
    ```
- 산출물: `tool_invoker.go`
- 단위 테스트: hook deny / newInput 적용 / hook 에러 시 도구 미실행 검증

### B.6  recovery.go 골격
- claude_cli 참조: `query.ts:864-1183`
- 작업:
  - `recovery.go` — 에러 분기만 노출 (`isPTL413`, `isMaxOutputTokens`, `isMedia`, `isFallback`)
  - 실제 복구 로직은 Phase C 의 `compact/recovery.go` 가 채움. 본 phase 에서는 stub + slog 만.
- 산출물: `recovery.go`
- 단위 테스트: 에러 분류 함수 케이스

### B.7  loop.go 직접 교체
- 작업:
  - `internal/agent/loop.go` 끝부분:
    ```go
    events := a.queryEngine.Run(ctx, params)
    return a.consumeEngineEvents(ctx, events)
    ```
  - `internal/agent/loop_llm.go` 제거
- 산출물: loop.go diff, loop_llm.go 제거
- 단위 테스트: Run 흐름이 query.Engine 경로를 사용하고 기존 사용자 흐름을 회귀하지 않는지 검증

### B.8  TUI handler 어댑터
- 작업:
  - 신 `query.Engine` 의 `<-chan QueryEvent` 를 기존 `a.handler.OnToken/OnThinkingToken/OnUsageUpdate/...` 콜백에 매핑
  - 신규 콜백 추가 (예: `OnToolStart`, `OnToolDone`, `OnTerminal{reason}`) — 사용자 합의 후
- 산출물: `internal/agent/handler_adapter.go` (신규)
- 단위 테스트: 이벤트 → 콜백 매핑 회귀

### B.9  골든 시나리오 회귀
- 작업:
  - `testdata/golden/` 에 시나리오 입력/기대 출력 5+ 개:
    1. 단순 echo 도구 1회 호출
    2. 도구 2회 multi-turn
    3. read-only 3개 병렬 + mutation 1개 직렬
    4. mutation 실패 → sibling skip
    5. ctx cancel 중간 발생 → terminal{interrupted}
  - query.Engine 이벤트 시퀀스와 terminal reason 검증
- 산출물: `internal/agent/query/golden_test.go` 또는 `internal/agent/golden_test.go`
- 통과 기준: 5개 시나리오 100% 통과

---

## 6. CLAUDE.md 규칙 준수 포인트

- [x] `engine.go` 가 300줄 초과 우려 — 작업/이벤트 처리/에러 복구 분기를 별도 파일로 (`engine_dispatch.go` 등)
- [x] `chan QueryEvent` 의 close 책임 명확 (sender 가 close, receiver 는 close 하지 않음)
- [x] context.Context 첫 인자, ctx.Done() 모든 select 에 포함
- [x] tool 출력 타입은 인터페이스로 (engine 이 구체 도구 타입 직접 참조 금지)
- [x] PTL/max_token/media 에러 분류 stub 마련 (`recovery.go`; 실제 복구는 Phase C)
- [x] slog: turn index, model name, terminal reason 등 구조화 로그
- [x] 메시지/도구 입력 로그 시 크리덴셜 마스킹 (engine 은 입력 본문을 로그하지 않음)

---

## 7. 검증 방법

### 단위 테스트
- events / state / transition / partition / concurrency 각각
- streaming_executor: 병렬 / sibling abort / ctx cancel
- tool_invoker: hook deny/modify/error
- recovery: 에러 분류

### 통합 테스트
- `//go:build integration`
- 실제 LLM (Fast tier) 한 번 호출 → 도구 시뮬레이션 → 종료

### E2E 시나리오
- 시나리오 1: 단순 명령 ("ls 실행해줘") → terminal{completed}
- 시나리오 2: 기존 사용자 흐름과 동일 입력 → 출력/도구 결과/terminal reason 회귀 없음
- 시나리오 3: 도구 5개 병렬 (read-only) → 결과 순서 무관 모두 도착
- 시나리오 4: mutation 실패 → sibling skip + reason 정확
- 시나리오 5: 100 turn 장기 세션 → state machine 안정성
- 시나리오 6: ctx cancel (Ctrl+C) → terminal{interrupted}, 부분 결과 보존
- ★ 시나리오 7 (분류 보존): classify 가 query 진입 전 1회만 호출되는지 검증 (counter)
- ★ 시나리오 8 (창 표시 보존): vim/top 같은 interactive 명령이 query.Engine 경로에서도 PTY 창 정상 표시

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 기존 사용자 시나리오 회귀 0

---

## 8. 종료 조건

- [x] 단위/통합 테스트 100%
- [x] 골든 시나리오 5개 100% 통과
- [x] E2E 시나리오 8개 통과 (특히 분류 1회 / 창 표시 보존)
- [x] `docs/design/query-engine.md` 작성 (Phase B 시작 전 별도 세션 작성 → 종료 시 결과 반영)
- [x] `docs/infractl-architecture.md` 갱신
- [x] `docs_mig/README.md` 진행 현황 update
- [x] 기존 루프 코드(`internal/agent/loop_llm.go`) 제거 완료

---

## 9. 다음 phase (C / D / F 병렬 가능) 진입 전 사용자 질문 항목

```
[ ] Q1. C/D/F 를 정말 병렬로 진행? 한 작업자 가정이라 순차 (C → D → F) 가 안전할 수 있음
[ ] Q2. compaction 4중 스택 도입 시 micro/collapse 의 요약 모델은 Fast tier 로 고정?
[ ] Q3. Phase D (precheck → hook 이관) 시 preflight/* 제거를 D 끝에 할지, F 종료 후로 미룰지
[ ] Q4. Phase F 의 자동 백그라운딩 임계 시간 (claude_cli 는 15s) — 우리는 동일? 다른 값?
```

---

## 끝.
