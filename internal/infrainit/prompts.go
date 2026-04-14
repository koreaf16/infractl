// Package infrainit
// File: prompts.go
// Description: 위저드용 터미널 I/O 헬퍼 함수 모음
// Responsibility: 표준 입력으로부터 텍스트, Y/N, 번호 선택을 읽는 함수 제공

package infrainit

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/tui"
	"golang.org/x/term"
)

// promptText는 라벨을 출력하고 한 줄을 읽어 반환한다.
// 빈 입력이면 defaultVal을 반환한다.
func promptText(label, defaultVal string) string {
	q := label
	if defaultVal != "" {
		q = fmt.Sprintf("%s [%s]", label, defaultVal)
	}

	result := tui.RunSelect(q, []tui.SelectOption{}, 80)
	if result.Label == "" {
		return defaultVal
	}
	return result.Label
}

// promptSecret는 Gemini 박스 스타일로 API Key 등 존감한 값을 입력받는다.
func promptSecret(label string) string {
	borderColor := lipgloss.NewStyle().Foreground(tui.ColorGeminiBox)
	fmt.Println(borderColor.Render("╭") + " " + tui.StyleGeminiHeader.Render("🔐 Secret Input"))
	fmt.Println(borderColor.Render("│"))
	fmt.Print(borderColor.Render("│  ") + tui.StyleGeminiSubDesc.Render(label+": "))
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // ReadPassword는 개행을 출력하지 않으므로 직접 출력
	fmt.Println(borderColor.Render("╰" + strings.Repeat("─", 50) + "╯"))
	if err != nil {
		res := tui.RunSelect(label, []tui.SelectOption{}, 80)
		return strings.TrimSpace(res.Label)
	}
	return strings.TrimSpace(string(b))
}

// promptYN은 Y/N 질문을 출력하고 bool을 반환한다.
// defaultYes=true이면 빈 입력을 Yes로 처리한다.
func promptYN(question string, defaultYes bool) bool {
	opts := []tui.SelectOption{
		{Label: "Yes", HideOther: true},
		{Label: "No", HideOther: true},
	}
	if !defaultYes {
		opts = []tui.SelectOption{
			{Label: "No", HideOther: true},
			{Label: "Yes", HideOther: true},
		}
	}
	
	result := tui.RunSelect(question, opts, 80)
	return result.Label == "Yes" || strings.ToLower(result.Label) == "y" || strings.ToLower(result.Label) == "yes"
}

// promptSelect는 목록을 번호로 출력하고 선택된 0-based 인덱스를 반환한다.
// 유효하지 않은 번호 입력 시 재시도한다.
func promptSelect(options []string) int {
	opts := make([]tui.SelectOption, len(options))
	for i, o := range options {
		opts[i] = tui.SelectOption{Label: o, HideOther: true}
	}
	result := tui.RunSelect("선택하세요", opts, 80)
	if result.Index < 0 {
		return 0
	}
	return result.Index
}

// printSectionHeader는 Gemini 스타일로 단계 헤더를 출력한다.
func printSectionHeader(step, title string) {
	borderColor := lipgloss.NewStyle().Foreground(tui.ColorGeminiBox)
	sep := strings.Repeat("─", 50)
	fmt.Println()
	fmt.Println(borderColor.Render("╭"+sep+"╮"))
	fmt.Println(borderColor.Render("│") + " " +
		tui.StyleGeminiHeader.Render(step) + 
		tui.StyleGeminiSubDesc.Render(" — "+title))
	fmt.Println(borderColor.Render("╰"+sep+"╯"))
}

// printSuccess는 Gemini 스타일로 성공 메시지를 출력한다.
func printSuccess(msg string) {
	fmt.Println()
	fmt.Println(tui.StyleSuccess.Render("✓ ") + tui.StyleGeminiHeader.Render(msg))
}