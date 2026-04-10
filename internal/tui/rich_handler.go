// Package tui
// File: rich_handler.go
// Description: DirectRenderer 기반 EventHandler 구현 (BubbleTea 없음)
// Responsibility: 에이전트 이벤트를 DirectRenderer에 전달하여 직접 ANSI 출력

package tui

import (
	"context"
	"time"

	"github.com/yourorg/infractl/internal/agent"
)

// RichHandler는 DirectRenderer를 통해 리치 렌더링을 제공하는 핸들러이다.
// agent.EventHandler와 agent.ConfirmationHandler를 구현한다.
type RichHandler struct {
	renderer *DirectRenderer
	state    *SessionState
	done     chan struct{}
}

// NewRichHandler는 새 RichHandler를 생성한다.
func NewRichHandler(state *SessionState) *RichHandler {
	return &RichHandler{state: state}
}

// StartTurn은 에이전트 실행 시작 시 DirectRenderer를 생성한다.
// 반환된 채널은 Finish() 호출 시 닫힌다.
func (h *RichHandler) StartTurn(_ context.Context) chan struct{} {
	h.renderer = NewDirectRenderer(h.state)
	h.done = make(chan struct{})
	return h.done
}

// FinishTurn은 턴을 종료하고 정리한다.
func (h *RichHandler) FinishTurn() {
	if h.renderer != nil {
		h.renderer.Finish()
		h.renderer = nil
	}
	if h.done != nil {
		close(h.done)
		h.done = nil
	}
}

// --- agent.EventHandler 구현 ---

func (h *RichHandler) OnThinking() {
	if h.renderer != nil {
		h.renderer.OnThinking()
	}
}

func (h *RichHandler) OnThinkingToken(_ string) {
	if h.renderer != nil {
		h.renderer.RecordActivity()
	}
}

func (h *RichHandler) OnToken(token string) {
	if h.renderer != nil {
		h.renderer.OnToken(token)
	}
}

func (h *RichHandler) OnToolOutput(toolID, line string) {
	if h.renderer != nil {
		h.renderer.OnToolOutput(toolID, line)
	}
}

func (h *RichHandler) OnToolStart(toolID, toolName, target string, args map[string]any) {
	if h.renderer != nil {
		h.renderer.OnToolStart(toolID, toolName, target, args)
	}
}

func (h *RichHandler) OnToolEnd(toolID, toolName, result string, duration time.Duration, success bool) {
	if h.renderer != nil {
		h.renderer.OnToolEnd(toolID, toolName, result, duration, success)
	}
}

func (h *RichHandler) OnResponse(content string) {
	if h.renderer != nil {
		h.renderer.OnResponse(content)
	}
}

func (h *RichHandler) OnError(err error) {
	if h.renderer != nil {
		h.renderer.OnError(err)
	}
}

func (h *RichHandler) OnUsageUpdate(inputTokens, outputTokens int, costUSD float64, _ int64) {
	if h.renderer != nil {
		h.renderer.OnUsageUpdate(inputTokens, outputTokens, costUSD)
	}
}

func (h *RichHandler) OnJobComplete(jobID int, description string, success bool) {
	// 현재 미구현
}

// --- agent.ConfirmationHandler 구현 ---

func (h *RichHandler) RequestConfirm(_ context.Context, req agent.ConfirmRequest) (agent.ConfirmResponse, error) {
	// DirectRenderer 모드에서는 stdout으로 직접 확인 요청
	// 향후 구현 — 현재는 자동 거부
	return agent.ConfirmResponse{Confirmed: false}, nil
}
