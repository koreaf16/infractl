// Package tui
// File: shell_box.go
// Description: 도구 실행 중 stdout을 실시간으로 표시하는 고정 높이 스크롤 박스
// Responsibility: 최대 N줄 슬라이딩 버퍼를 lipgloss RoundedBorder 박스로 렌더링

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const shellBoxMaxLines = 12

// renderShellBox는 도구 실행 중 stdout 줄을 고정 높이 박스로 렌더링한다.
// title: 현재 도구 표시명 (박스 상단 타이틀)
// lines: 최대 shellBoxMaxLines 줄의 stdout 버퍼
// width: 터미널 폭
func renderShellBox(title string, lines []string, width int) string {
	if len(lines) == 0 {
		return ""
	}

	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}

	var sb strings.Builder
	for _, line := range lines {
		// 줄 너비 초과 시 잘라서 표시
		vis := line
		if lipgloss.Width(vis) > boxW-2 {
			runes := []rune(vis)
			cut := 0
			w := 0
			for _, r := range runes {
				rw := 1
				if r > 0x2E7F { // 간단한 CJK 폭 추정
					rw = 2
				}
				if w+rw > boxW-5 {
					break
				}
				w += rw
				cut++
			}
			vis = string(runes[:cut]) + "…"
		}
		sb.WriteString(StyleCmdBoxDim.Render(vis) + "\n")
	}

	titleStr := StyleCmdBoxToolName.Render(title)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSubtle).
		Width(boxW).
		Render(strings.TrimRight(sb.String(), "\n"))

	// 타이틀을 박스 상단 테두리에 삽입
	boxLines := strings.Split(box, "\n")
	if len(boxLines) > 0 {
		top := boxLines[0]
		// "──" 뒤에 타이틀 삽입
		if len([]rune(top)) > 2 {
			prefix := "── "
			suffix := " " + strings.Repeat("─", max(0, lipgloss.Width(top)-lipgloss.Width(prefix)-lipgloss.Width(titleStr)-3)) + "─"
			boxLines[0] = prefix + titleStr + suffix
		}
		return strings.Join(boxLines, "\n")
	}
	return box
}

// appendShellLine은 슬라이딩 버퍼에 줄을 추가하고 최대 길이를 유지한다.
func appendShellLine(lines []string, line string, max int) []string {
	lines = append(lines, line)
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return lines
}
