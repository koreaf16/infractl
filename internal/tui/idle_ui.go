// Package tui
// File: idle_ui.go
// Description: 쉘 명령 유휴(인터랙티브 프롬프트) 감지 시 사용자 입력 오버레이 UI
// Responsibility: 유휴 상태 컴포넌트, TUI 핸들러, 메시지 타입 제공

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourorg/infractl/internal/agent"
)

// IdleInputRequestMsg는 에이전트에서 TUI로 유휴 입력 요청을 보낼 때 사용하는 메시지이다.
type IdleInputRequestMsg struct {
	Request agent.IdleInputRequest
	ReplyCh chan agent.IdleInputResponse
}

// IdleInputResponseMsg는 사용자 응답을 TUI에서 처리한 후 채널로 전달하는 메시지이다.
type IdleInputResponseMsg struct {
	Response agent.IdleInputResponse
	ReplyCh  chan agent.IdleInputResponse
}

// idleState는 유휴 입력 오버레이의 현재 상태이다.
type idleState struct {
	active  bool
	request agent.IdleInputRequest
	replyCh chan agent.IdleInputResponse
	input   string // 사용자가 입력 중인 텍스트
}

// render는 유휴 입력 오버레이를 렌더링한다.
func (s *idleState) render(width int) string {
	if !s.active {
		return ""
	}

	var sb strings.Builder
	sep := strings.Repeat("─", max(width-4, 10))

	sb.WriteString(StyleWarning.Render(" ⏸ 명령이 입력을 기다리고 있습니다") + "\n")
	sb.WriteString(StyleSeparator.Render(" "+sep) + "\n")
	sb.WriteString(fmt.Sprintf(" 명령: %s\n", s.request.Command))
	if s.request.Target != "" && s.request.Target != "localhost" {
		sb.WriteString(fmt.Sprintf(" 대상: %s\n", s.request.Target))
	}

	// 마지막 출력 라인 컨텍스트
	if len(s.request.LastLines) > 0 {
		sb.WriteString(StyleSeparator.Render(" "+sep) + "\n")
		for _, line := range s.request.LastLines {
			sb.WriteString(StyleCmdBoxDim.Render(" │ "+line) + "\n")
		}
	}

	sb.WriteString(StyleSeparator.Render(" "+sep) + "\n")
	sb.WriteString(StyleCmdBoxDim.Render(" 전송할 내용 입력 (Enter=빈 줄 전송, Esc=중단):") + "\n")
	sb.WriteString(fmt.Sprintf(" > %s_\n", s.input))

	return sb.String()
}

// handleKey는 유휴 입력 오버레이가 활성화된 상태에서 키 입력을 처리한다.
// 응답이 확정되면 IdleInputResponseMsg를 반환한다.
func (s *idleState) handleKey(key tea.KeyMsg) (handled bool, msg tea.Msg) {
	if !s.active {
		return false, nil
	}

	switch key.Type {
	case tea.KeyEnter:
		s.active = false
		return true, IdleInputResponseMsg{
			Response: agent.IdleInputResponse{Input: s.input},
			ReplyCh:  s.replyCh,
		}
	case tea.KeyBackspace, tea.KeyDelete:
		if len(s.input) > 0 {
			s.input = s.input[:len(s.input)-1]
		}
		return true, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		s.active = false
		return true, IdleInputResponseMsg{
			Response: agent.IdleInputResponse{Abort: true},
			ReplyCh:  s.replyCh,
		}
	case tea.KeyRunes:
		s.input += string(key.Runes)
		return true, nil
	}
	return true, nil
}

// ───────────────────────── TUIIdleInputHandler ─────────────────────────────

// TUIIdleInputHandler는 TUI용 IdleInputHandler 구현체이다.
// 채널 기반으로 에이전트 goroutine을 블로킹한다.
type TUIIdleInputHandler struct {
	program *tea.Program
}

// NewTUIIdleInputHandler는 새 TUIIdleInputHandler를 생성한다.
func NewTUIIdleInputHandler(p *tea.Program) *TUIIdleInputHandler {
	return &TUIIdleInputHandler{program: p}
}

// RequestIdleInput은 TUI에 유휴 입력 요청을 보내고 사용자 응답을 대기한다.
// agent.IdleInputHandler를 구현한다.
func (h *TUIIdleInputHandler) RequestIdleInput(ctx context.Context, req agent.IdleInputRequest) (agent.IdleInputResponse, error) {
	replyCh := make(chan agent.IdleInputResponse, 1)
	h.program.Send(IdleInputRequestMsg{Request: req, ReplyCh: replyCh})

	select {
	case <-ctx.Done():
		return agent.IdleInputResponse{Abort: true}, nil
	case resp := <-replyCh:
		return resp, nil
	}
}
