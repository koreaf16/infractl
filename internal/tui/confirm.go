// Package tui
// File: confirm.go
// Description: 위험 작업 확인 오버레이 UI 컴포넌트
// Responsibility: 위험도별 사용자 확인 요청 표시 및 채널 기반 응답 전달

package tui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourorg/infractl/internal/agent"
)

// ConfirmRequestMsg는 에이전트에서 TUI로 확인 요청을 보낼 때 사용하는 메시지이다.
type ConfirmRequestMsg struct {
	Request  agent.ConfirmRequest
	ReplyCh  chan agent.ConfirmResponse
}

// ConfirmResponseMsg는 사용자 응답을 TUI에서 처리한 후 채널로 전달하는 메시지이다.
type ConfirmResponseMsg struct {
	Response agent.ConfirmResponse
	ReplyCh  chan agent.ConfirmResponse
}

// confirmState는 확인 오버레이의 현재 상태이다.
type confirmState struct {
	active      bool
	request     agent.ConfirmRequest
	replyCh     chan agent.ConfirmResponse
	input       string // high 위험 시 이름 입력
	selectedOpt int    // 0=예, 1=모두허용, 2=아니요 (방향키 선택)
}

// height는 confirm 오버레이가 차지하는 줄 수를 반환한다.
func (cs *confirmState) height() int {
	if !cs.active {
		return 0
	}
	if cs.request.NeedInput {
		// riskColor + sep + 작업 + 도구/대상/위험도 + sep + 입력안내 + 입력줄 = 7줄
		return 7
	}
	// riskColor + sep + 작업 + 도구/대상/위험도 + sep + 계속? + 예 + 모두허용 + 아니요 = 9줄
	return 9
}

// renderConfirmOverlay는 확인 요청 오버레이를 렌더링한다.
func (cs *confirmState) render(width int) string {
	if !cs.active {
		return ""
	}
	req := cs.request

	var sb strings.Builder
	sep := strings.Repeat("─", width-4)

	riskColor := StyleWarning
	prefix := "⚠️"
	if req.RiskLevel == "high" {
		riskColor = StyleError
		prefix = "⚠️⚠️⚠️"
	}

	sb.WriteString(riskColor.Render(fmt.Sprintf(" %s 확인 [%d/%d단계]", prefix, req.Step, req.TotalSteps)) + "\n")
	sb.WriteString(StyleSeparator.Render(" "+sep) + "\n")
	sb.WriteString(fmt.Sprintf(" 작업: %s\n", req.Description))
	sb.WriteString(fmt.Sprintf(" 도구: %s  대상: %s  위험도: %s\n", req.ToolName, req.Target, req.RiskLevel))
	sb.WriteString(StyleSeparator.Render(" "+sep) + "\n")

	if req.NeedInput {
		sb.WriteString(fmt.Sprintf(" 대상 이름을 직접 입력하세요 (예상: %s):\n", req.TargetName))
		sb.WriteString(fmt.Sprintf(" > %s_\n", cs.input))
	} else {
		sb.WriteString(" 계속하시겠습니까?\n")
		marks := [3]string{"  ", "  ", "  "}
		marks[cs.selectedOpt] = "❯ "
		sb.WriteString(riskColor.Render(marks[0]+"예  (y)") + "\n")
		sb.WriteString(StyleWarning.Render(marks[1]+"모두 허용 (a)") + StyleInfoBarDim.Render(" — 이후 확인 없이 자동 승인") + "\n")
		sb.WriteString(StyleInfoBarDim.Render(marks[2]+"아니요 (n)") + "\n")
	}

	return sb.String()
}

// handleConfirmKey는 확인 오버레이가 활성화된 상태에서 키 입력을 처리한다.
// 응답이 확정되면 ConfirmResponseMsg를 반환한다.
func (cs *confirmState) handleKey(key tea.KeyMsg) (handled bool, msg tea.Msg) {
	if !cs.active {
		return false, nil
	}

	req := cs.request

	if req.NeedInput {
		switch key.Type {
		case tea.KeyEnter:
			// 입력값과 대상 이름이 일치하는지 확인
			confirmed := cs.input == req.TargetName || req.TargetName == ""
			cs.active = false
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: confirmed, UserInput: cs.input},
				ReplyCh:  cs.replyCh,
			}
		case tea.KeyBackspace:
			if len(cs.input) > 0 {
				cs.input = cs.input[:len(cs.input)-1]
			}
			return true, nil
		case tea.KeyEsc:
			cs.active = false
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: false},
				ReplyCh:  cs.replyCh,
			}
		case tea.KeyRunes:
			cs.input += string(key.Runes)
			return true, nil
		}
		return true, nil
	}

	// y/n/a + 방향키 입력 처리 (0=예, 1=모두허용, 2=아니요)
	switch key.Type {
	case tea.KeyUp:
		if cs.selectedOpt > 0 {
			cs.selectedOpt--
		}
		return true, nil
	case tea.KeyDown:
		if cs.selectedOpt < 2 {
			cs.selectedOpt++
		}
		return true, nil
	case tea.KeyEnter:
		cs.active = false
		switch cs.selectedOpt {
		case 0: // 예
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: true},
				ReplyCh:  cs.replyCh,
			}
		case 1: // 모두 허용
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: true, AllowAll: true},
				ReplyCh:  cs.replyCh,
			}
		default: // 아니요
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: false},
				ReplyCh:  cs.replyCh,
			}
		}
	case tea.KeyRunes:
		ch := strings.ToLower(string(key.Runes))
		switch ch {
		case "y":
			cs.active = false
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: true},
				ReplyCh:  cs.replyCh,
			}
		case "a":
			cs.active = false
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: true, AllowAll: true},
				ReplyCh:  cs.replyCh,
			}
		case "n", "q":
			cs.active = false
			return true, ConfirmResponseMsg{
				Response: agent.ConfirmResponse{Confirmed: false},
				ReplyCh:  cs.replyCh,
			}
		}
		return true, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		cs.active = false
		return true, ConfirmResponseMsg{
			Response: agent.ConfirmResponse{Confirmed: false},
			ReplyCh:  cs.replyCh,
		}
	}
	return true, nil
}

// TUIConfirmHandler는 TUI용 ConfirmationHandler 구현체이다.
// SELECT UI를 통해 사용자 확인을 처리한다.
type TUIConfirmHandler struct {
	program       *tea.Program
	selectHandler *TUISelectHandler
	yoloMode      int32 // atomic: 0=off, 1=on
}

// SetProgram은 tea.Program을 지연 주입한다 (p 생성 후 호출).
func (h *TUIConfirmHandler) SetProgram(p *tea.Program) { h.program = p }

// SetSelectHandler는 SELECT UI 핸들러를 주입한다.
func (h *TUIConfirmHandler) SetSelectHandler(s *TUISelectHandler) { h.selectHandler = s }

// EnableYolo는 YOLO 모드를 활성화한다 — 이후 확인 요청은 즉시 승인된다.
func (h *TUIConfirmHandler) EnableYolo() { atomic.StoreInt32(&h.yoloMode, 1) }

// DisableYolo는 YOLO 모드를 비활성화한다.
func (h *TUIConfirmHandler) DisableYolo() { atomic.StoreInt32(&h.yoloMode, 0) }

// IsYolo는 현재 YOLO 모드 상태를 반환한다.
func (h *TUIConfirmHandler) IsYolo() bool { return atomic.LoadInt32(&h.yoloMode) == 1 }

// RequestConfirm은 하단 SELECT UI를 통해 사용자 확인을 요청한다.
// YOLO 모드가 활성화된 경우 즉시 승인 응답을 반환한다.
func (h *TUIConfirmHandler) RequestConfirm(ctx context.Context, req agent.ConfirmRequest) (agent.ConfirmResponse, error) {
	if h.IsYolo() {
		return agent.ConfirmResponse{Confirmed: true}, nil
	}
	if h.selectHandler != nil {
		return h.requestViaSelect(ctx, req)
	}
	// 헤드리스 폴백: medium/high 위험은 자동 거부
	if req.RiskLevel == "medium" || req.RiskLevel == "high" {
		return agent.ConfirmResponse{Confirmed: false}, nil
	}
	return agent.ConfirmResponse{Confirmed: true}, nil
}

// requestViaSelect는 하단 SELECT UI를 통해 확인 요청을 처리한다.
func (h *TUIConfirmHandler) requestViaSelect(ctx context.Context, req agent.ConfirmRequest) (agent.ConfirmResponse, error) {
	prefix := "⚠️"
	if req.RiskLevel == "high" {
		prefix = "⚠️⚠️"
	}
	question := fmt.Sprintf("%s [%s] %s", prefix, req.RiskLevel, req.Description)

	options := []SelectOption{
		{Label: "예 — 계속 진행", HideOther: true},
		{Label: "모두 허용 — 이후 확인 없이 자동 승인", HideOther: true},
		{Label: "아니요 — 취소", HideOther: true},
	}

	result, err := h.selectHandler.RequestSelectCtx(ctx, question, options)
	if err != nil {
		return agent.ConfirmResponse{Confirmed: false}, err
	}
	switch result.Index {
	case 0:
		return agent.ConfirmResponse{Confirmed: true}, nil
	case 1:
		return agent.ConfirmResponse{Confirmed: true, AllowAll: true}, nil
	default:
		return agent.ConfirmResponse{Confirmed: false}, nil
	}
}
