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
	active      bool
	question    string
	options     []SelectOption
	cursor      int
	replyCh     chan SelectResult
	headerLabel string // Gemini 박스 타이틀 (빈 문자열이면 "Answer Questions")
}

// Activate는 선택 UI를 활성화한다.
func (s *selectionState) Activate(question string, options []SelectOption, replyCh chan SelectResult) {
	s.active = true
	s.question = question
	s.options = options
	s.cursor = 0
	s.replyCh = replyCh
	s.headerLabel = ""
}

// ActivateWithHeader는 카테고리 헤더를 지정하여 선택 UI를 활성화한다.
func (s *selectionState) ActivateWithHeader(question string, options []SelectOption, replyCh chan SelectResult, header string) {
	s.Activate(question, options, replyCh)
	s.headerLabel = header
}

// Deactivate는 선택 UI를 비활성화하고 상태를 초기화한다.
func (s *selectionState) Deactivate() {
	s.active = false
	s.question = ""
	s.options = nil
	s.cursor = 0
	s.replyCh = nil
	s.headerLabel = ""
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

// viewSimple은 Gemini CLI 박스 스타일의 단순 목록 렌더링이다.
func (s *selectionState) viewSimple(width int) string {
	return s.renderGeminiBox(width, false)
}

// viewRich는 Gemini CLI 박스 스타일로 미리보기 패널을 포함한 렌더링이다.
// 미리보기가 없으면 단순 박스로 표시한다.
func (s *selectionState) viewRich(width int) string {
	return s.renderGeminiBox(width, true)
}

// renderGeminiBox는 Gemini CLI 스타일 박스형 선택 UI를 렌더링한다.
func (s *selectionState) renderGeminiBox(width int, richMode bool) string {
	hdr := s.headerLabel
	if hdr == "" {
		hdr = "Answer Questions"
	}

	showOther := !s.hasHideOther()
	innerW := width - 6
	if innerW < 30 {
		innerW = 30
	}

	// ── 왼쪽 콘텐츠 빌드 ──
	var leftSB strings.Builder
	leftSB.WriteString("\n")
	leftSB.WriteString(StyleSelectionQuestion.Render("  "+s.question) + "\n")
	leftSB.WriteString("\n")

	for i, opt := range s.options {
		s.writeGeminiOption(&leftSB, i, opt, richMode)
	}

	if showOther {
		otherIdx := len(s.options)
		if s.cursor == otherIdx {
			bullet := StyleGeminiBullet.Render("●")
			label := StyleGeminiSelected.Render(fmt.Sprintf("%d. Enter a custom value", otherIdx+1))
			leftSB.WriteString(fmt.Sprintf("  %s %s\n", bullet, label))
		} else {
			leftSB.WriteString(StyleGeminiOption.Render(fmt.Sprintf("    %d. Enter a custom value", otherIdx+1)) + "\n")
		}
	}

	// 미리보기 패널 (richMode + preview 있을 때)
	preview := ""
	if richMode && s.cursor < len(s.options) {
		preview = s.options[s.cursor].Preview
	}

	hint := StyleGeminiHint.Render("  Enter to select · ↑/↓ to navigate · Esc to cancel")

	var bodyContent string
	if preview != "" {
		leftWidth := innerW * 55 / 100
		rightWidth := innerW - leftWidth - 2
		if rightWidth < 20 {
			rightWidth = 20
		}
		innerPreview := rightWidth - 4
		if innerPreview < 10 {
			innerPreview = 10
		}
		previewContent := wrapPreviewLines(preview, innerPreview)
		rightCol := StylePreviewBox.Width(rightWidth).Render(previewContent)
		body := lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(leftWidth).Render(leftSB.String()),
			rightCol,
		)
		bodyContent = body + "\n" + hint + "\n"
	} else {
		leftSB.WriteString("\n")
		leftSB.WriteString(hint + "\n")
		bodyContent = leftSB.String()
	}

	// 박스 렌더링
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGeminiBox).
		PaddingLeft(1).
		PaddingRight(1).
		Width(innerW + 2)

	renderedBox := boxStyle.Render(bodyContent)

	// 박스 첫째 줄을 헤더 타이틀로 교체
	lines := strings.SplitN(renderedBox, "\n", 2)
	if len(lines) >= 2 {
		titleLine := " " + StyleGeminiHeader.Render(hdr) + " "
		firstLineW := lipgloss.Width(lines[0])
		headerLine := buildBoxHeader(titleLine, firstLineW)
		return headerLine + "\n" + lines[1]
	}
	return renderedBox
}

// writeGeminiOption은 Gemini CLI 스타일로 옵션 한 줄을 렌더링하여 sb에 쓴다.
func (s *selectionState) writeGeminiOption(sb *strings.Builder, i int, opt SelectOption, richMode bool) {
	selected := i == s.cursor
	num := fmt.Sprintf("%d.", i+1)

	if selected {
		bullet := StyleGeminiBullet.Render("●")
		label := StyleGeminiSelected.Render(num + " " + opt.Label)
		line := fmt.Sprintf("  %s %s", bullet, label)
		if richMode && opt.Tag != "" {
			line += "  " + StyleSelectionTag.Render(opt.Tag)
		}
		sb.WriteString(line + "\n")
		if opt.Description != "" {
			sb.WriteString(StyleGeminiSubDesc.Render("       "+opt.Description) + "\n")
		}
	} else {
		line := StyleGeminiOption.Render("    " + num + " " + opt.Label)
		if richMode && opt.Tag != "" {
			line += "  " + StyleInfoBarDim.Render("["+opt.Tag+"]")
		}
		sb.WriteString(line + "\n")
		if opt.Description != "" {
			sb.WriteString(StyleGeminiSubDesc.Render("       "+opt.Description) + "\n")
		}
	}
}

// writeOption은 하위 호환을 위해 유지하는 구버전 렌더링 메서드이다.
// 새 코드에서는 writeGeminiOption을 사용한다.
func (s *selectionState) writeOption(sb *strings.Builder, i int, opt SelectOption, richMode bool) {
	s.writeGeminiOption(sb, i, opt, richMode)
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
