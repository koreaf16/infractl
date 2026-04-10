// Package tui
// File: messages.go
// Description: bubbletea TUI에서 사용하는 커스텀 메시지 타입 정의
// Responsibility: 에이전트 이벤트와 TUI 간의 비동기 통신 타입 제공

package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/subagent"
)

// TokenMsg는 LLM 스트리밍 응답 토큰 하나를 나타낸다.
type TokenMsg string

// ThinkingTokenMsg는 LLM 내부 추론(thinking) 토큰을 나타낸다.
type ThinkingTokenMsg string

// ThinkingStartMsg는 LLM이 응답 생성을 시작했음을 나타낸다.
type ThinkingStartMsg struct{}

// ToolStartMsg는 도구 실행이 시작되었음을 나타낸다.
type ToolStartMsg struct {
	ToolID string
	Name   string
	Target string
	Args   map[string]interface{}
}

// ToolEndMsg는 도구 실행이 완료되었음을 나타낸다.
type ToolEndMsg struct {
	ToolID   string
	Name     string
	Result   string
	Duration time.Duration
	Success  bool
}

// ResponseDoneMsg는 LLM의 최종 텍스트 응답이 완성되었음을 나타낸다.
type ResponseDoneMsg string

// ErrorMsg는 에이전트 루프에서 에러가 발생했음을 나타낸다.
type ErrorMsg struct{ Err error }

// AgentDoneMsg는 에이전트 루프가 종료되었음을 나타낸다.
type AgentDoneMsg struct{}

// ShellOutputMsg는 shell_exec 실행 중 수신된 stdout 라인을 나타낸다.
type ShellOutputMsg struct {
	ToolID string
	Line   string
}

// SystemMsg는 시스템 메시지(슬래시 명령 결과 등)를 나타낸다.
type SystemMsg string

// UsageUpdateMsg는 LLM 호출 후 토큰/비용 정보를 전달한다.
type UsageUpdateMsg struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	DurationMs   int64
}

// ShimmerTickMsg는 shimmer 애니메이션을 구동하는 틱이다.
type ShimmerTickMsg struct{}

// JobCompleteMsg는 백그라운드 작업이 완료되었음을 나타낸다.
type JobCompleteMsg struct {
	JobID       int
	Description string
	Success     bool
}

// AgentProgressMsg는 에이전트 진행 트리 표시를 업데이트한다.
type AgentProgressMsg struct {
	ToolName    string
	Status      string // "running", "done", "error"
	Description string
	ToolUses    int
	Tokens      int
}

// ProgramRefMsg는 tea.Program 참조를 AppModel에 주입하기 위한 메시지다.
// Println()을 통해 채팅 출력을 스크롤백으로 전달하는 데 사용한다.
type ProgramRefMsg struct {
	Program *tea.Program
}

// SelectRequestMsg는 TUI에 인터랙티브 선택 UI 표시를 요청한다.
// SelectOption / SelectResult 타입은 select_ui.go에 정의되어 있다.
type SelectRequestMsg struct {
	Question string
	Options  []SelectOption
	ReplyCh  chan SelectResult
}

// SelectResponseMsg는 사용자의 선택을 에이전트 goroutine으로 전달한다.
type SelectResponseMsg struct {
	Result  SelectResult
	ReplyCh chan SelectResult
}

// ActiveServerMsg는 활성 서버가 변경되었을 때 TUI AppModel에 전달된다.
// Server가 nil이면 활성 서버 해제를 의미한다.
type ActiveServerMsg struct {
	Server *store.Server
}

// SubagentEventMsg는 서브에이전트 실행 중 발생한 진행 이벤트를 전달한다.
type SubagentEventMsg struct {
	Event subagent.Event
}
