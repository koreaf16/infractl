# Plan: Interactive Hook Permission Bypass (Phase G+)

현재 `infractl`은 로컬 시스템 접근 등 민감한 작업이 Hook에 의해 차단될 때 (`DecisionDeny` 또는 `DecisionAsk`), LLM에게 에러를 반환하고 즉시 종료합니다. 사용자가 "질문해서 우회"할 수 있도록, `DecisionAsk` 발생 시 TUI에서 즉시 승인 여부를 묻는 인터랙티브 프롬프트 기능을 추가합니다.

## 핵심 변경 사항

### 1. Hook Decision 처리 고도화
- `internal/hooks/event.go`의 `HookOutput.IsDeny()`가 `DecisionAsk`를 포함하지 않도록 수정합니다.
- `DecisionAsk`는 별도의 흐름(승인 대기)으로 처리합니다.

### 2. ToolInvoker에 AskCallback 추가
- `internal/agent/query/tool_invoker.go`에 사용자 승인을 요청할 수 있는 콜백 인터페이스를 추가합니다.
- `Invoke` 메서드에서 `DecisionAsk` 수신 시 이 콜백을 호출하여 실행을 일시 중단하고 사용자 입력을 기다립니다.

### 3. TUI 인터랙티브 승인 구현
- `internal/tui/privilege_prompt.go`와 유사한 방식으로 `HookPermissionRequestMsg`를 정의합니다.
- Hook이 승인을 요청하면 TUI에 전용 카드(또는 메시지 박스)를 띄우고 사용자의 `y/n` 입력을 기다립니다.
- 승인 시 `Invoke` 루틴이 재개되어 도구가 실행됩니다.

## 상세 작업 단계

### Phase 1: Hook 스키마 및 Invoker 수정
- [ ] `internal/hooks/event.go`: `IsDeny()` 수정 (`DecisionAsk` 제외).
- [ ] `internal/agent/query/tool_invoker.go`:
    - `AskCallback` 타입 정의: `func(ctx, tc, reason) bool`
    - `Invoke()` 내부에 `DecisionAsk` 발생 시 콜백 호출 로직 추가.

### Phase 2: TUI 및 루프 엔진 연동
- [ ] `internal/tui/messages.go`: `HookPermissionRequestMsg` 추가.
- [ ] `internal/tui/app_tool_events.go` (또는 전용 핸들러): 승인 요청 메시지 처리 및 UI 렌더링 로직 추가.
- [ ] `internal/agent/loop_engine.go`: `ToolInvoker` 생성 시 TUI로 이벤트를 보내고 응답을 기다리는 `AskCallback` 구현체 주입.

### Phase 3: 검증
- [ ] 로컬 파일 탐색(`ls` 등) 시 Hook이 `DecisionAsk`를 반환하도록 설정(테스트용)한 후, TUI에서 정상적으로 팝업이 뜨고 승인 시 실행되는지 확인.
- [ ] `DecisionDeny`인 경우에는 기존처럼 즉시 차단되는지 확인.

## 기대 효과
- 보안 정책에 의해 차단되더라도 사용자가 명시적으로 허용할 수 있는 유연한 권한 체계 구축.
- Claude CLI와 동일한 수준의 인터랙티브 사용자 경험 제공.
