// Package tui
// File: selection.go
// Description: BubbleTea 네이티브 인터랙티브 선택 컴포넌트
// Responsibility: footer 아래 확장되는 화살표 키 기반 선택 UI 제공 (단순/리치 2단 레이아웃)

package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

// selectionState는 인터랙티브 선택 컴포넌트의 상태이다.
type selectionState struct {
	active   bool
	question string
	options  []SelectOption
	cursor   int
	replyCh  chan SelectResult
}

// Activate는 선택 UI를 활성화한다.
func (s *selectionState) Activate(question string, options []SelectOption, replyCh chan SelectResult) {
	s.active = true
	s.question = question
	s.options = options
	s.cursor = 0
	s.replyCh = replyCh
}

// Deactivate는 선택 UI를 비활성화하고 상태를 초기화한다.
func (s *selectionState) Deactivate() {
	s.active = false
	s.question = ""
	s.options = nil
	s.cursor = 0
	s.replyCh = nil
}

// hasHideOther는 옵션 중 하나라도 HideOther가 true이면 Other를 숨긴다.
func (s *selectionState) hasHideOther() bool {
	for _, o := range s.options {
		if o.HideOther {
			return true
		}
	}
	return false
}

// hasPreview는 미리보기 패널이 필요한지 확인한다.
func (s *selectionState) hasPreview() bool {
	for _, o := range s.options {
		if o.Preview != "" {
			return true
		}
	}
	return false
}

// handleKey는 활성화 상태에서 키 입력을 처리한다.
// 처리된 경우 handled=true, 응답이 확정된 경우 msg!=nil을 반환한다.
func (s *selectionState) handleKey(msg tea.KeyMsg) (handled bool, result tea.Msg) {
	if !s.active {
		return false, nil
	}

	showOther := !s.hasHideOther()
	totalItems := len(s.options)
	if showOther {
		totalItems++
	}

	switch msg.Type {
	case tea.KeyUp:
		if s.cursor > 0 {
			s.cursor--
		}
		return true, nil

	case tea.KeyDown:
		if s.cursor < totalItems-1 {
			s.cursor++
		}
		return true, nil

	case tea.KeyEnter:
		if showOther && s.cursor == len(s.options) {
			return true, SelectResponseMsg{
				Result:  SelectResult{Index: -1, Label: "", IsOther: true},
				ReplyCh: s.replyCh,
			}
		}
		if s.cursor < len(s.options) {
			return true, SelectResponseMsg{
				Result:  SelectResult{Index: s.cursor, Label: s.options[s.cursor].Label},
				ReplyCh: s.replyCh,
			}
		}
		return true, nil

	case tea.KeyEsc:
		return true, SelectResponseMsg{
			Result:  SelectResult{Index: -1, Label: ""},
			ReplyCh: s.replyCh,
		}
	}

	return true, nil
}

// View는 선택 UI를 렌더링한다.
// 미리보기 패널이 있으면 좌(옵션 목록) + 우(프리뷰 박스) 2단 레이아웃을 사용한다.
func (s *selectionState) View(width int) string {
	if !s.active {
		return ""
	}
	if s.hasPreview() {
		return s.viewRich(width)
	}
	return s.viewSimple(width)
}

// viewSimple은 기존 단순 목록 형태의 렌더링이다.
func (s *selectionState) viewSimple(width int) string {
	var sb strings.Builder
	sep := strings.Repeat("─", max(width-4, 10))

	sb.WriteString("  " + StyleSeparator.Render(sep) + "\n")
	sb.WriteString("  " + StyleClaude().Render("?") + " " +
		StyleSelectionQuestion.Render(s.question) + "\n")

	showOther := !s.hasHideOther()
	for i, opt := range s.options {
		s.writeOption(&sb, i, opt, false)
	}

	if showOther {
		otherIdx := len(s.options)
		if s.cursor == otherIdx {
			sb.WriteString(StyleSelectionCursor.Render("  ❯ ") +
				StyleSelectionSelected.Render("Other") +
				StyleInfoBarDim.Render(" — 직접 입력") + "\n")
		} else {
			sb.WriteString(StyleSelectionOption.Render("    Other") +
				StyleInfoBarDim.Render(" — 직접 입력") + "\n")
		}
	}

	sb.WriteString("  " + StyleSelectionHint.Render("↑↓ 이동 · Enter 선택 · Esc 취소"))
	return strings.TrimRight(sb.String(), "\n")
}

// viewRich는 오른쪽 미리보기 패널을 포함한 2단 레이아웃 렌더링이다.
//
//	? 자동 처리 방식
//	❯ 1. 패턴 매칭 자동응답  [Recommended]   ┌─────────────────────┐
//	     패턴 기반 즉시 응답                  │ 감지 패턴 예시:      │
//	  2. LLM 판단 위임                        │ [Y/n] → y 전송       │
//	  3. 항상 TUI 다이얼로그                  └─────────────────────┘
//	↑↓ 이동 · Enter 선택 · Esc 취소
func (s *selectionState) viewRich(width int) string {
	// 패널 폭 분배: 좌 55%, 우 나머지
	leftWidth := width * 55 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := width - leftWidth - 2
	if rightWidth < 20 {
		rightWidth = 20
	}

	sep := strings.Repeat("─", max(width-4, 10))
	header := "  " + StyleSeparator.Render(sep) + "\n" +
		"  " + StyleClaude().Render("?") + " " +
		StyleSelectionQuestion.Render(s.question)

	// ── 왼쪽: 옵션 목록
	var leftSB strings.Builder
	showOther := !s.hasHideOther()
	for i, opt := range s.options {
		s.writeOption(&leftSB, i, opt, true)
	}
	if showOther {
		otherIdx := len(s.options)
		if s.cursor == otherIdx {
			leftSB.WriteString(StyleSelectionCursor.Render("  ❯ ") +
				StyleSelectionSelected.Render("Other") +
				StyleInfoBarDim.Render(" — 직접 입력") + "\n")
		} else {
			leftSB.WriteString(StyleSelectionOption.Render("    Other") +
				StyleInfoBarDim.Render(" — 직접 입력") + "\n")
		}
	}
	leftSB.WriteString("  " + StyleSelectionHint.Render("↑↓ 이동 · Enter 선택 · Esc 취소"))

	// ── 오른쪽: 포커스 옵션의 미리보기
	preview := ""
	if s.cursor < len(s.options) {
		preview = s.options[s.cursor].Preview
	}

	if preview == "" {
		return header + "\n" + strings.TrimRight(leftSB.String(), "\n")
	}

	innerWidth := rightWidth - 4 // 테두리+패딩 제외
	if innerWidth < 10 {
		innerWidth = 10
	}
	previewContent := wrapPreviewLines(preview, innerWidth)
	rightCol := StylePreviewBox.Width(rightWidth).Render(previewContent)

	joined := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(leftSB.String()),
		rightCol,
	)
	return header + "\n" + strings.TrimRight(joined, "\n")
}

// writeOption은 옵션 한 줄을 렌더링하여 sb에 쓴다.
// richMode=true이면 Tag 배지와 Description 서브라인을 추가한다.
func (s *selectionState) writeOption(sb *strings.Builder, i int, opt SelectOption, richMode bool) {
	selected := i == s.cursor
	num := fmt.Sprintf("%d. ", i+1)

	if selected {
		line := StyleSelectionCursor.Render("  ❯ ") + StyleSelectionSelected.Render(num+opt.Label)
		if richMode && opt.Tag != "" {
			line += " " + StyleSelectionTag.Render(opt.Tag)
		}
		sb.WriteString(line + "\n")
		if richMode && opt.Description != "" {
			sb.WriteString(StyleSelectionSub.Render("       "+opt.Description) + "\n")
		}
	} else {
		line := StyleSelectionOption.Render("    " + num + opt.Label)
		if richMode && opt.Tag != "" {
			line += " " + StyleInfoBarDim.Render("["+opt.Tag+"]")
		}
		sb.WriteString(line + "\n")
	}
}

// wrapPreviewLines는 미리보기 텍스트를 maxWidth에 맞게 줄 바꿈한다.
func wrapPreviewLines(text string, maxWidth int) string {
	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= maxWidth {
			result.WriteString(line + "\n")
			continue
		}
		for len(line) > maxWidth {
			result.WriteString(line[:maxWidth] + "\n")
			line = line[maxWidth:]
		}
		if line != "" {
			result.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(result.String(), "\n")
}

// Height는 선택 UI가 차지하는 줄 수를 반환한다.
func (s *selectionState) Height() int {
	if !s.active {
		return 0
	}
	showOther := !s.hasHideOther()
	// separator(1) + question(1) + options (description 포함 시 2줄) + other(1) + hint(1)
	h := 3 // sep + question + hint
	for _, opt := range s.options {
		h++
		if opt.Description != "" {
			h++
		}
	}
	if showOther {
		h++
	}
	return h
}

// TUISelectHandler는 TUI용 선택 핸들러 구현체이다.
// 채널 기반으로 에이전트 goroutine을 블로킹한다.
type TUISelectHandler struct {
	program *tea.Program
}

// NewTUISelectHandler는 새 TUISelectHandler를 생성한다.
func NewTUISelectHandler(p *tea.Program) *TUISelectHandler {
	return &TUISelectHandler{program: p}
}

// SetProgram은 지연 초기화를 위해 program을 사후 주입한다.
// tea.Program 생성 전에 핸들러를 AppOptions에 전달하고 이후에 p를 설정할 때 사용한다.
func (h *TUISelectHandler) SetProgram(p *tea.Program) { h.program = p }

// RequestSelect는 TUI에 선택 요청을 보내고 사용자 응답을 대기한다.
func (h *TUISelectHandler) RequestSelect(question string, options []SelectOption) (SelectResult, error) {
	replyCh := make(chan SelectResult, 1)
	h.program.Send(SelectRequestMsg{
		Question: question,
		Options:  options,
		ReplyCh:  replyCh,
	})
	result := <-replyCh
	return result, nil
}

// RequestSelectCtx는 ctx 취소를 지원하는 선택 요청이다.
func (h *TUISelectHandler) RequestSelectCtx(ctx context.Context, question string, options []SelectOption) (SelectResult, error) {
	if h.program == nil {
		return SelectResult{Index: -1}, nil
	}
	replyCh := make(chan SelectResult, 1)
	h.program.Send(SelectRequestMsg{
		Question: question,
		Options:  options,
		ReplyCh:  replyCh,
	})
	select {
	case <-ctx.Done():
		return SelectResult{Index: -1}, nil
	case result := <-replyCh:
		return result, nil
	}
}
