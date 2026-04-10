// Package tui
// File: select_ui.go
// Description: 터미널 기반 선택 UI (AI 질문, 옵션 선택)
// Responsibility: ↑↓ 네비게이션, Enter 확정, Other 자유입력을 제공하는 인라인 선택기

package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// SelectOption은 선택지 하나를 나타낸다.
type SelectOption struct {
	Label       string
	Description string // 옵션 레이블 아래 표시되는 부가 설명 (예: "Recommended")
	Preview     string // 해당 옵션에 포커스 시 우측 패널에 표시할 멀티라인 내용
	Tag         string // 옵션 우측에 표시할 배지 (예: "Recommended", "Default")
	HideOther   bool   // true이면 "Other — 직접 입력" 옵션을 숨긴다
}

// SelectResult는 사용자 선택 결과이다.
type SelectResult struct {
	Index    int    // 선택된 옵션 인덱스 (-1이면 Other)
	Label    string // 선택된 레이블 또는 자유 입력 텍스트
	IsOther  bool   // Other 선택 여부
}

// RunSelect는 터미널에 선택 UI를 표시하고 사용자 입력을 받는다.
// ANSI 이스케이프로 인라인 렌더링, ↑↓ Enter로 선택.
func RunSelect(question string, options []SelectOption, width int) SelectResult {
	if len(options) == 0 {
		return readFreeText(question)
	}

	cursor := 0
	totalItems := len(options) + 1 // +1 for "Other"

	// raw mode 진입 (키 입력 직접 읽기)
	oldState, err := enableRawMode()
	if err != nil {
		// raw mode 실패 시 폴백: 번호 입력 방식
		return fallbackSelect(question, options)
	}
	defer restoreTermMode(oldState)

	// 초기 렌더링
	tw := newTermWriter()
	tw.HideCursor()
	defer tw.ShowCursor()

	renderSelect(tw, question, options, cursor, width)

	// 키 입력 루프
	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		case buf[0] == 13 || buf[0] == 10: // Enter
			tw.ClearLive()
			if cursor == len(options) {
				// "Other" 선택
				tw.ShowCursor()
				restoreTermMode(oldState)
				return readFreeText(question)
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

		// 재렌더링
		tw.ClearLive()
		renderSelect(tw, question, options, cursor, width)
	}
}

// renderSelect는 선택 UI를 렌더링한다.
func renderSelect(tw *termWriter, question string, options []SelectOption, cursor int, width int) {
	// 질문 헤더
	header := StyleClaude().Render("?") + " " + StyleCmdBoxToolName.Render(question)
	tw.Println(header)

	// 옵션들
	for i, opt := range options {
		prefix := "  "
		if i == cursor {
			prefix = StyleClaude().Render("❯ ")
		}
		label := opt.Label
		if opt.Description != "" {
			label += StyleCmdBoxDim.Render(" — " + opt.Description)
		}
		if i == cursor {
			tw.Println(prefix + StyleCmdBoxToolName.Render(opt.Label) +
				StyleCmdBoxDim.Render(descPart(opt.Description)))
		} else {
			tw.Println(prefix + label)
		}
	}

	// "Other" 옵션
	otherIdx := len(options)
	prefix := "  "
	if cursor == otherIdx {
		prefix = StyleClaude().Render("❯ ")
		tw.Println(prefix + StyleCmdBoxToolName.Render("Other") +
			StyleCmdBoxDim.Render(" — 직접 입력"))
	} else {
		tw.Println(prefix + StyleCmdBoxDim.Render("Other — 직접 입력"))
	}

	tw.prevLines = len(options) + 2 // header + options + other
}

func descPart(desc string) string {
	if desc == "" {
		return ""
	}
	return " — " + desc
}

// readFreeText는 자유 텍스트 입력을 받는다.
func readFreeText(question string) SelectResult {
	fmt.Print(StyleClaude().Render("?") + " " + question + ": ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	text := strings.TrimSpace(line)
	return SelectResult{Index: -1, Label: text, IsOther: true}
}

// fallbackSelect는 raw mode 실패 시 번호 입력 방식 폴백이다.
func fallbackSelect(question string, options []SelectOption) SelectResult {
	fmt.Println(StyleClaude().Render("?") + " " + question)
	for i, opt := range options {
		desc := ""
		if opt.Description != "" {
			desc = StyleCmdBoxDim.Render(" — " + opt.Description)
		}
		fmt.Printf("  %d. %s%s\n", i+1, opt.Label, desc)
	}
	fmt.Printf("  %d. Other (직접 입력)\n", len(options)+1)
	fmt.Print("선택 (번호): ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	text := strings.TrimSpace(line)

	// 번호 파싱
	for i, opt := range options {
		if text == fmt.Sprintf("%d", i+1) {
			return SelectResult{Index: i, Label: opt.Label}
		}
	}
	if text == fmt.Sprintf("%d", len(options)+1) {
		return readFreeText(question)
	}

	// 번호가 아니면 자유 텍스트로 처리
	return SelectResult{Index: -1, Label: text, IsOther: true}
}
