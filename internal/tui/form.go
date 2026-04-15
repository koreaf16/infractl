// Package tui
// File: form.go
// Description: 다중 필드 폼 입력 상태, 렌더링, TUI 브릿지 핸들러
// Responsibility: formState 라이프사이클 관리, 상태 박스 렌더링, 채널 기반 goroutine 브릿지

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/tools"
)

type formPhase int

const (
	formPhaseEdit   formPhase = iota
	formPhaseReview formPhase = iota
)

// formState는 다중 필드 폼 입력 모드의 상태를 관리한다.
// 실제 텍스트 입력은 기존 inputBar에 위임하고, 이 구조체는 값 수집과 상태 박스 렌더링만 담당한다.
type formState struct {
	active      bool
	title       string
	headerLabel string
	fields      []tools.FormFieldDef
	values      []string // 필드별 입력값
	cursor      int      // 현재 활성 필드 인덱스
	phase       formPhase
	replyCh     chan FormResult
}

// Activate는 폼 입력 모드를 활성화한다.
func (f *formState) Activate(title string, fields []tools.FormFieldDef, replyCh chan FormResult, header string) {
	f.active = true
	f.title = title
	f.headerLabel = header
	f.fields = fields
	f.values = make([]string, len(fields))
	for i, field := range fields {
		f.values[i] = field.DefaultValue
	}
	f.cursor = 0
	f.phase = formPhaseEdit
	f.replyCh = replyCh
}

// Deactivate는 폼 입력 모드를 비활성화하고 상태를 초기화한다.
func (f *formState) Deactivate() {
	f.active = false
	f.title = ""
	f.headerLabel = ""
	f.fields = nil
	f.values = nil
	f.cursor = 0
	f.phase = formPhaseEdit
	f.replyCh = nil
}

// CurrentFieldLabel은 현재 활성 필드의 라벨을 반환한다.
// 입력란 프롬프트 표시에 사용된다.
func (f *formState) CurrentFieldLabel() string {
	if !f.active || f.cursor >= len(f.fields) {
		return ""
	}
	return f.fields[f.cursor].Label
}

// CurrentPlaceholder는 현재 필드의 placeholder를 반환한다.
func (f *formState) CurrentPlaceholder() string {
	if !f.active || f.cursor >= len(f.fields) {
		return ""
	}
	return f.fields[f.cursor].Placeholder
}

// AcceptValue는 현재 필드에 값을 저장한다.
func (f *formState) AcceptValue(value string) {
	if f.cursor < len(f.values) {
		f.values[f.cursor] = value
	}
}

// AdvanceToNext는 커서를 다음 필드로 이동한다.
// 모든 필드가 입력되면 Review 모드로 전환하고 true를 반환한다.
func (f *formState) AdvanceToNext() bool {
	f.cursor++
	if f.cursor >= len(f.fields) {
		f.cursor = len(f.fields) - 1
		f.phase = formPhaseReview
		return true
	}
	return false
}

// NextField는 다음 필드로 이동한다 (Tab 네비게이션).
func (f *formState) NextField() {
	if f.cursor < len(f.fields)-1 {
		f.cursor++
	}
}

// PrevField는 이전 필드로 이동한다 (Shift+Tab 네비게이션).
func (f *formState) PrevField() {
	if f.cursor > 0 {
		f.cursor--
	}
}

// BuildResult는 현재 입력값으로 FormResult를 구성한다.
func (f *formState) BuildResult() FormResult {
	values := make(map[string]string, len(f.fields))
	for i, field := range f.fields {
		val := ""
		if i < len(f.values) {
			val = f.values[i]
		}
		values[field.Name] = val
	}
	return FormResult{Values: values, Cancelled: false}
}

// View는 폼 상태 박스를 렌더링한다.
// activeValue: 현재 활성 필드에서 타이핑 중인 값 (textarea에서 읽어옴)
// cursorOffset: 활성 필드 내 커서 위치 (rune 단위)
func (f *formState) View(width int, activeValue string, cursorOffset int) string {
	if !f.active {
		return ""
	}
	if f.phase == formPhaseReview {
		return f.viewReview(width)
	}
	return f.viewEdit(width, activeValue, cursorOffset)
}

func (f *formState) viewEdit(width int, activeValue string, cursorOffset int) string {
	innerW := width - 6
	if innerW < 30 {
		innerW = 30
	}

	var body strings.Builder
	body.WriteString("\n")

	// 제목을 박스 안쪽 첫 줄 콘텐츠로 표시
	hdr := strings.TrimSpace(f.headerLabel)
	if hdr == "" {
		hdr = strings.TrimSpace(f.title)
	}
	if hdr == "" {
		hdr = "Answer Questions"
	}
	body.WriteString(StyleGeminiHeader.Render("  "+hdr) + "\n")
	body.WriteString("\n")

	// fake cursor 블록: 역상(reverse video) 공백으로 실제 커서처럼 보임
	cursorBlock := lipgloss.NewStyle().Reverse(true).Render(" ")

	for i, field := range f.fields {
		val := ""
		if i < len(f.values) {
			val = f.values[i]
		}

		label := field.Label + ":"
		// label을 일정 너비로 패딩하여 값이 정렬되도록
		labelPadded := fmt.Sprintf("%-16s", label)

		switch {
		case i < f.cursor:
			// 입력 완료 필드
			checkMark := StyleSuccess.Render("✓")
			labelStr := StyleGeminiSubDesc.Render(labelPadded)
			valStr := StyleGeminiSelected.Render(val)
			body.WriteString(fmt.Sprintf("  %s %s %s\n", checkMark, labelStr, valStr))
		case i == f.cursor:
			// 현재 활성 필드: fake cursor를 activeValue 내 cursorOffset 위치에 삽입
			arrow := StyleGeminiBullet.Render("→")
			labelStr := StyleGeminiSubDesc.Render(labelPadded)
			var displayVal string
			if activeValue == "" && field.Placeholder != "" {
				// 빈 값: 커서 + placeholder 힌트
				displayVal = cursorBlock + StyleGeminiSubDesc.Render(field.Placeholder)
			} else {
				// 타이핑 중: value[:offset] + 커서 + value[offset:]
				runes := []rune(activeValue)
				off := cursorOffset
				if off > len(runes) {
					off = len(runes)
				}
				before := StyleGeminiOption.Render(string(runes[:off]))
				after := StyleGeminiOption.Render(string(runes[off:]))
				displayVal = before + cursorBlock + after
			}
			body.WriteString(fmt.Sprintf("  %s %s %s\n", arrow, labelStr, displayVal))
		default:
			// 미입력 필드
			circle := StyleGeminiOption.Render("○")
			labelStr := StyleGeminiOption.Render(labelPadded)
			displayVal := ""
			if val != "" {
				displayVal = StyleGeminiOption.Render(val)
			}
			body.WriteString(fmt.Sprintf("  %s %s %s\n", circle, labelStr, displayVal))
		}
	}

	body.WriteString("\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGeminiBox).
		PaddingLeft(1).
		PaddingRight(1).
		Width(innerW + 2)

	return boxStyle.Render(body.String())
}

func (f *formState) viewReview(width int) string {
	innerW := width - 6
	if innerW < 30 {
		innerW = 30
	}

	var body strings.Builder
	body.WriteString("\n")
	body.WriteString(StyleSelectionQuestion.Render("  입력 내용을 확인하세요:") + "\n")
	body.WriteString("\n")

	for i, field := range f.fields {
		val := ""
		if i < len(f.values) {
			val = f.values[i]
		}
		label := fmt.Sprintf("%-16s", field.Label+":")
		labelStr := StyleGeminiSubDesc.Render(label)
		valStr := StyleGeminiSelected.Render(val)
		body.WriteString(fmt.Sprintf("  %s %s\n", labelStr, valStr))
	}

	body.WriteString("\n")
	body.WriteString(StyleGeminiHint.Render("  Enter confirm | Esc go back to edit") + "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGeminiBox).
		PaddingLeft(1).
		PaddingRight(1).
		Width(innerW + 2)

	renderedBox := boxStyle.Render(body.String())
	lines := strings.SplitN(renderedBox, "\n", 2)
	if len(lines) >= 2 {
		titleLine := " " + StyleGeminiHeader.Render("Review Answers") + " "
		firstLineW := lipgloss.Width(lines[0])
		headerLine := buildBoxHeader(titleLine, firstLineW)
		return headerLine + "\n" + lines[1]
	}
	return renderedBox
}

// Height는 현재 폼 박스의 예상 줄 수를 반환한다.
func (f *formState) Height() int {
	if !f.active {
		return 0
	}
	if f.phase == formPhaseReview {
		return len(f.fields) + 7
	}
	return len(f.fields) + 5
}

// ─── TUIFormHandler ────────────────────────────────────────────────────────

// TUIFormHandler는 agent goroutine과 TUI 간의 채널 기반 폼 입력 브릿지다.
type TUIFormHandler struct {
	program *tea.Program
}

// NewTUIFormHandler는 tea.Program을 참조하는 핸들러를 생성한다.
func NewTUIFormHandler(p *tea.Program) *TUIFormHandler {
	return &TUIFormHandler{program: p}
}

// SetProgram은 tea.Program 참조를 갱신한다.
func (h *TUIFormHandler) SetProgram(p *tea.Program) { h.program = p }

// RequestForm은 TUI에 폼 입력 UI를 요청하고 사용자 입력을 기다린다.
func (h *TUIFormHandler) RequestForm(ctx context.Context, req tools.FormRequest) (tools.FormResponse, error) {
	if h.program == nil {
		return tools.FormResponse{Cancelled: true}, nil
	}
	replyCh := make(chan FormResult, 1)
	h.program.Send(FormRequestMsg{
		Title:   req.Title,
		Header:  req.Header,
		Fields:  req.Fields,
		ReplyCh: replyCh,
	})
	select {
	case <-ctx.Done():
		return tools.FormResponse{Cancelled: true}, nil
	case result := <-replyCh:
		return tools.FormResponse{
			Values:    result.Values,
			Cancelled: result.Cancelled,
		}, nil
	}
}
