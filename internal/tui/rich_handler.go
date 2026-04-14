// Package tui
// File: rich_handler.go
// Description: DirectRenderer 기반 EventHandler 구현 (BubbleTea 없음)
// Responsibility: 에이전트 이벤트를 DirectRenderer에 전달하여 직접 ANSI 출력

package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/yourorg/infractl/internal/privilege"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
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

func (h *RichHandler) OnThinking(tier string, model string) {
	if h.renderer != nil {
		h.renderer.OnThinking(tier, model)
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

func (h *RichHandler) OnRAGContext(_ int) {}

func (h *RichHandler) OnJobComplete(jobID int, description string, success bool) {
	if h.renderer == nil {
		return
	}
	bracket := StyleResponseBracket.Render("  ⎿  ")
	var text string
	if success {
		text = fmt.Sprintf("%s백그라운드 작업 #%d 완료: %s", bracket, jobID, description)
	} else {
		text = fmt.Sprintf("%s%s 백그라운드 작업 #%d 완료: %s", bracket, StyleError.Render("✗"), jobID, description)
	}
	h.renderer.PrintNotification(text)
}

// OnActiveServerChange는 활성 서버 변경을 안전하게 출력한다.
// DirectRenderer의 mutex를 통해 스트리밍 중 간섭을 방지한다.
func (h *RichHandler) OnActiveServerChange(srv *store.Server) {
	if h.renderer == nil {
		return
	}
	bracket := StyleResponseBracket.Render("  ⎿  ")
	var text string
	if srv == nil {
		text = bracket + StyleCmdBoxDim.Render("[active server] cleared")
	} else {
		text = bracket + StyleCmdBoxDim.Render(fmt.Sprintf("활성 서버: %s (%s:%d)", srv.Name, srv.Host, srv.Port))
	}
	h.renderer.PrintNotification(text)
}

// --- privilege.PromptHandler 래퍼 ---

// RichPrivilegeHandler는 DirectRenderer를 일시 중단하고 터미널 비밀번호 프롬프트를 안전하게 표시한다.
type RichPrivilegeHandler struct {
	handler *RichHandler
	inner   privilege.PromptHandler
}

// NewPrivilegePromptHandler는 DirectRenderer-aware 비밀번호 프롬프트 핸들러를 생성한다.
// 프롬프트 전 shimmer를 정지하고 라이브 박스를 비워 터미널이 직접 조작 가능하도록 한다.
func (h *RichHandler) NewPrivilegePromptHandler(inner privilege.PromptHandler) privilege.PromptHandler {
	return &RichPrivilegeHandler{handler: h, inner: inner}
}

// RequestPassword는 렌더러를 일시 중단한 뒤 inner 핸들러에 위임한다.
func (p *RichPrivilegeHandler) RequestPassword(ctx context.Context, req privilege.PromptRequest) (privilege.PromptResponse, error) {
	if p.handler.renderer != nil {
		p.handler.renderer.PauseForPrompt()
		defer func() {
			label := p.handler.renderer.thinkingLabel
			if label == "" {
				label = "thinking..."
			}
			p.handler.renderer.ResumeAfterPrompt(label)
		}()
	}
	return p.inner.RequestPassword(ctx, req)
}

// --- tools.QuestionHandler 구현 ---

// RequestQuestion은 DirectRenderer를 일시 중단하고 터미널에서 선택지를 보여준 뒤 입력을 받는다.
func (h *RichHandler) RequestQuestion(ctx context.Context, req tools.QuestionRequest) (tools.QuestionResponse, error) {
	if h.renderer != nil {
		h.renderer.PauseForPrompt()
		defer func() {
			label := h.renderer.thinkingLabel
			if label == "" {
				label = "thinking..."
			}
			h.renderer.ResumeAfterPrompt(label)
		}()
	}

	opts := make([]SelectOption, len(req.Options))
	for i, opt := range req.Options {
		opts[i] = SelectOption{
			Label:       opt.Label,
			Description: opt.Description,
			HideOther:   false,
		}
	}

	width := 80
	if h.state != nil && h.state.Width > 0 {
		width = h.state.Width
	}

	doneCh := make(chan SelectResult, 1)
	go func() {
		doneCh <- RunSelectWithHeader(req.Question, opts, width, req.Header)
	}()

	select {
	case <-ctx.Done():
		fmt.Println()
		return tools.QuestionResponse{SelectedIndex: -1}, nil
	case result := <-doneCh:
		if result.Index < 0 {
			if result.IsOther {
				return tools.QuestionResponse{
					IsOther:   true,
					UserInput: result.Label,
				}, nil
			}
			return tools.QuestionResponse{SelectedIndex: -1}, nil
		}

		return tools.QuestionResponse{
			SelectedIndex: result.Index,
			SelectedLabel: result.Label,
		}, nil
	}
}
