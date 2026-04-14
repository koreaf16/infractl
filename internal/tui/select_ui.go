// Package tui
// File: select_ui.go
// Description: 터미널 기반 Gemini CLI 스타일 선택 UI (AI 질문, 옵션 선택)
// Responsibility: 박스형 인라인 선택기 — ↑↓ 네비게이션, Enter 확정, Other 자유입력 제공

package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SelectOption은 선택지 하나를 나타낸다.
type SelectOption struct {
	Label       string
	Description string // 옵션 레이블 아래 표시되는 부가 설명
	Preview     string // 해당 옵션에 포커스 시 우측 패널에 표시할 멀티라인 내용
	Tag         string // 옵션 우측에 표시할 배지 (예: "Recommended", "Default")
	HideOther   bool   // true이면 "Other — 직접 입력" 옵션을 숨긴다
}

// SelectResult는 사용자 선택 결과이다.
type SelectResult struct {
	Index   int    // 선택된 옵션 인덱스 (-1이면 Other)
	Label   string // 선택된 레이블 또는 자유 입력 텍스트
	IsOther bool   // Other 선택 여부
}

// geminiBoxWidth는 박스의 내부 콘텐츠 폭을 계산한다.
// 화면 하단 UI 전체를 덮도록 터미널 너비를 최대한 활용한다.
func geminiBoxWidth(termWidth int) int {
	w := termWidth - 4 // 좌우 테두리, 패딩 고려
	if w < 40 {
		w = 40
	}
	return w
}

// geminiHideOther는 옵션 중 하나라도 HideOther가 true이면 Other를 숨긴다.
func geminiHideOther(options []SelectOption) bool {
	for _, o := range options {
		if o.HideOther {
			return true
		}
	}
	return false
}

// RunSelect는 터미널에 Gemini CLI 스타일 선택 UI를 표시하고 사용자 입력을 받는다.
// header는 박스 상단 카테고리 라벨 (빈 문자열이면 "Answer Questions").
func RunSelect(question string, options []SelectOption, width int) SelectResult {
	return RunSelectWithHeader(question, options, width, "")
}

// RunSelectWithHeader는 header를 직접 지정하여 선택 UI를 실행한다.
func RunSelectWithHeader(question string, options []SelectOption, width int, header string) SelectResult {
	if len(options) == 0 {
		return readFreeText(question, header, width)
	}

	cursor := 0
	hideOther := geminiHideOther(options)
	totalItems := len(options)
	if !hideOther {
		totalItems++
	}

	// raw mode 진입
	oldState, err := enableRawMode()
	if err != nil {
		return fallbackSelect(question, options, header)
	}
	defer restoreTermMode(oldState)

	tw := newTermWriter()
	tw.HideCursor()
	defer tw.ShowCursor()

	renderGeminiSelect(tw, header, question, options, cursor, width, hideOther)

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		case buf[0] == 13 || buf[0] == 10: // Enter
			tw.ClearLive()
			if !hideOther && cursor == len(options) {
				tw.ShowCursor()
				restoreTermMode(oldState)
				return readFreeText(question, header, width)
			}
			return SelectResult{
				Index: cursor,
				Label: options[cursor].Label,
			}

		case buf[0] == 27 && n >= 3: // ESC sequence (화살표)
			if buf[1] == '[' {
				switch buf[2] {
				case 'A': // ↑
					if cursor > 0 {
						cursor--
					}
				case 'B': // ↓
					if cursor < totalItems-1 {
						cursor++
					}
				}
			}

		case buf[0] == 27 && n == 1: // ESC 단독
			tw.ClearLive()
			return SelectResult{Index: -1, Label: ""}

		case buf[0] == 3: // Ctrl+C
			tw.ClearLive()
			return SelectResult{Index: -1, Label: ""}
		}

		tw.ClearLive()
		renderGeminiSelect(tw, header, question, options, cursor, width, hideOther)
	}
}

// renderGeminiSelect는 Gemini CLI 스타일 박스형 선택 UI를 렌더링한다.
//
// 출력 예:
//
//	╭ Answer Questions ────────────────────────────────────╮
//	│                                                      │
//	│  '질문 내용 텍스트'                                  │
//	│                                                      │
//	│    1. 옵션 A                                         │
//	│       서브 설명                                      │
//	│  ● 2. 옵션 B                                         │
//	│       서브 설명                                      │
//	│    3. Enter a custom value                           │
//	│                                                      │
//	│  Enter to select · ↑/↓ to navigate · Esc to cancel  │
//	╰──────────────────────────────────────────────────────╯
func renderGeminiSelect(tw *termWriter, header, question string, options []SelectOption, cursor, width int, hideOther bool) {
	innerW := geminiBoxWidth(width)

	// 헤더 라벨 결정
	hdr := header
	if hdr == "" {
		hdr = "Answer Questions"
	}

	// 내부 콘텐츠 빌드
	var content strings.Builder

	// 질문 라인
	content.WriteString("\n")
	content.WriteString(wrapText("  "+question, innerW) + "\n")
	content.WriteString("\n")

	// 옵션 목록
	for i, opt := range options {
		num := fmt.Sprintf("%d.", i+1)
		if i == cursor {
			bullet := StyleGeminiBullet.Render("●")
			label := StyleGeminiSelected.Render(num + " " + opt.Label)
			content.WriteString(fmt.Sprintf("  %s %s", bullet, label))
			if opt.Tag != "" {
				content.WriteString("  " + StyleSelectionTag.Render(opt.Tag))
			}
			content.WriteString("\n")
			if opt.Description != "" {
				content.WriteString(StyleGeminiSubDesc.Render("       "+opt.Description) + "\n")
			}
		} else {
			line := StyleGeminiOption.Render("    "+num+" "+opt.Label)
			content.WriteString(line)
			if opt.Tag != "" {
				content.WriteString("  " + StyleInfoBarDim.Render("["+opt.Tag+"]"))
			}
			content.WriteString("\n")
			if opt.Description != "" {
				content.WriteString(StyleGeminiSubDesc.Render("       "+opt.Description) + "\n")
			}
		}
	}

	// "Other" 옵션
	if !hideOther {
		otherIdx := len(options)
		if cursor == otherIdx {
			bullet := StyleGeminiBullet.Render("●")
			label := StyleGeminiSelected.Render(fmt.Sprintf("%d.", otherIdx+1) + " Enter a custom value")
			content.WriteString(fmt.Sprintf("  %s %s\n", bullet, label))
		} else {
			content.WriteString(StyleGeminiOption.Render(fmt.Sprintf("    %d. Enter a custom value", otherIdx+1)) + "\n")
		}
	}

	// 하단 힌트
	content.WriteString("\n")
	hint := StyleGeminiHint.Render("  Enter to select · ↑/↓ to navigate · Esc to cancel")
	content.WriteString(hint + "\n")

	// 박스 렌더링 — 헤더를 박스 테두리 타이틀로 사용
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGeminiBox).
		PaddingLeft(1).
		PaddingRight(1).
		Width(innerW + 2)

	// 타이틀 라인: ╭ Header ────────╮
	titleLine := " " + StyleGeminiHeader.Render(hdr) + " "
	rendered := boxStyle.Render(content.String())

	// lipgloss가 타이틀을 지원하지 않으므로, 첫째 줄을 헤더로 교체한다
	lines := strings.SplitN(rendered, "\n", 2)
	if len(lines) >= 2 {
		firstLine := lines[0]
		// 첫 줄의 ╭ 다음에 헤더 삽입
		if len(firstLine) > 0 {
			headerRendered := buildBoxHeader(titleLine, lipgloss.Width(firstLine))
			rendered = headerRendered + "\n" + lines[1]
		}
	}

	// 줄 수 계산 후 출력
	lineCount := strings.Count(rendered, "\n") + 1
	tw.prevLines = lineCount
	tw.Print(rendered + "\n")
}

// buildBoxHeader는 박스 첫 줄을 헤더 텍스트가 삽입된 형태로 만든다.
// 예: ╭ Answer Questions ─────────────────────────────╮
func buildBoxHeader(title string, totalWidth int) string {
	const (
		tlCorner = "╭"
		trCorner = "╮"
		hLine    = "─"
	)
	// ANSI 코드 제거 후 실제 표시 폭 계산
	plainTitle := lipgloss.NewStyle().Render("") // strip용 초기화
	_ = plainTitle
	visibleTitle := stripANSI(title)
	titleLen := len([]rune(visibleTitle))
	// 남은 대시 수: totalWidth - 좌코너(1) - 제목 - 우코너(1) - 양쪽 각 1 패딩
	dashCount := totalWidth - 2 - titleLen - 2
	if dashCount < 0 {
		dashCount = 0
	}
	dashes := strings.Repeat(hLine, dashCount)
	// ColorGeminiBox 색상으로 테두리 렌더링
	borderColor := lipgloss.NewStyle().Foreground(ColorGeminiBox)
	return borderColor.Render(tlCorner) +
		borderColor.Render(" ") +
		title +
		borderColor.Render(" ") +
		borderColor.Render(dashes) +
		borderColor.Render(trCorner)
}

// stripANSI는 ANSI 이스케이프 시퀀스를 제거하여 순수 텍스트 폭을 계산한다.
func stripANSI(s string) string {
	// lipgloss Width가 내부적으로 처리하는 방식을 이용
	// 간단 구현: \033[ ... m 패턴 제거
	result := strings.Builder{}
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		if runes[i] == '\033' && i+1 < len(runes) && runes[i+1] == '[' {
			// ESC [ ... m 스킵
			i += 2
			for i < len(runes) && runes[i] != 'm' {
				i++
			}
			i++ // 'm' 스킵
		} else {
			result.WriteRune(runes[i])
			i++
		}
	}
	return result.String()
}

// wrapText는 maxWidth 폭에 맞게 단순 텍스트를 줄 바꿈한다.
func wrapText(text string, maxWidth int) string {
	if maxWidth <= 0 || len([]rune(text)) <= maxWidth {
		return text
	}
	var result strings.Builder
	runes := []rune(text)
	for len(runes) > maxWidth {
		result.WriteString(string(runes[:maxWidth]) + "\n")
		runes = runes[maxWidth:]
	}
	if len(runes) > 0 {
		result.WriteString(string(runes))
	}
	return result.String()
}

// readFreeText는 Gemini 스타일 박스로 자유 텍스트 입력을 받는다.
// 하단 프롬프트창을 덮는 것처럼 보이게 박스와 프롬프트를 렌더링한다.
func readFreeText(question, header string, width int) SelectResult {
	hdr := header
	if hdr == "" {
		hdr = "Answer Questions"
	}

	innerW := geminiBoxWidth(width)
	borderColor := lipgloss.NewStyle().Foreground(ColorGeminiBox)

	// 박스 스타일은 renderGeminiSelect와 동일하게 유지
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGeminiBox).
		PaddingLeft(1).
		PaddingRight(1).
		Width(innerW + 2)

	// 상단 헤더 조립
	titleLine := " " + StyleGeminiHeader.Render(hdr) + " "
	topLine := buildBoxHeader(titleLine, innerW+2)

	// 내부 콘텐츠 (입력 전 상단 부분만 먼저 출력하고 싶지만, bufio를 위해 전체 구조를 먼저 잡음)
	fmt.Println(topLine)
	fmt.Println(borderColor.Render("│"))
	fmt.Println(borderColor.Render("│  ") + StyleGeminiSelected.Render(question))
	fmt.Println(borderColor.Render("│"))

	// 프롬프트 출력 (채팅 UI와 동일한 '> ' 프롬프트)
	fmt.Print(borderColor.Render("│  ") + StyleInputPrompt.Render("> "))

	// 입력 대기 (이 시점에서 커서가 프롬프트 바로 뒤에 위치함)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	text := strings.TrimSpace(line)

	// 박스 하단 닫기 (너비를 맞춰서)
	dashCount := innerW
	if dashCount < 0 {
		dashCount = 0
	}
	fmt.Println(borderColor.Render("╰" + strings.Repeat("─", dashCount) + "╯"))

	return SelectResult{Index: -1, Label: text, IsOther: true}
}

// fallbackSelect는 raw mode 실패 시 번호 입력 방식 폴백이다.
func fallbackSelect(question string, options []SelectOption, header string) SelectResult {
	hdr := header
	if hdr == "" {
		hdr = "Answer Questions"
	}
	fmt.Println(StyleGeminiHeader.Render("╭ "+hdr))
	fmt.Println("│")
	fmt.Println("│  " + StyleGeminiSelected.Render(question))
	fmt.Println("│")
	for i, opt := range options {
		desc := ""
		if opt.Description != "" {
			desc = "\n│     " + StyleGeminiSubDesc.Render(opt.Description)
		}
		fmt.Printf("│    %d. %s%s\n", i+1, opt.Label, desc)
	}
	fmt.Printf("│    %d. Enter a custom value\n", len(options)+1)
	fmt.Println("│")
	fmt.Print("│  " + StyleGeminiHint.Render("선택 (번호): "))

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	text := strings.TrimSpace(line)
	fmt.Println("╰" + strings.Repeat("─", 50) + "╯")

	for i, opt := range options {
		if text == fmt.Sprintf("%d", i+1) {
			return SelectResult{Index: i, Label: opt.Label}
		}
	}
	if text == fmt.Sprintf("%d", len(options)+1) {
		return readFreeText(question, header, 80)
	}
	return SelectResult{Index: -1, Label: text, IsOther: true}
}

func descPart(desc string) string {
	if desc == "" {
		return ""
	}
	return " — " + desc
}
