// Package tui
// File: shell_box.go
// Description: Shell 실행 결과를 RoundedBorder 박스로 렌더링한다.
// Responsibility: 실행 중(→)/완료(√)/실패(✗) 상태의 Shell stdout 박스 렌더링

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	shellBoxMaxLines     = 12
	shellBoxDisplayLines = 10
)

// boxIcon은 Shell 박스 헤더의 상태 아이콘이다.
type boxIcon int

const (
	boxIconRunning boxIcon = iota
	boxIconDone
	boxIconFailed
)

// isShellBoxTool은 Shell 박스 렌더링 대상 도구인지 반환한다.
func isShellBoxTool(name string) bool {
	return name == "shell_exec" || name == "file_transfer"
}

// renderShellBoxCompleted는 완료된 Shell 명령을 박스로 렌더링한다 (scrollback용).
//
//	╭──────────────────────────────────────────────╮
//	│ √ Shell powershell -Command "Get-PSDrive..." │
//	│                                              │
//	│ Used   : 125214920704                        │
//	│ FreeGB : 182.43                              │
//	╰──────────────────────────────────────────────╯
func renderShellBoxCompleted(name string, args map[string]any, lines []string, duration time.Duration, success bool, width int) string {
	icon := boxIconDone
	if !success {
		icon = boxIconFailed
	}
	return renderShellBoxContent(name, args, lines, icon, duration, width)
}

// renderShellBoxRunning는 실행 중인 Shell 명령을 박스로 렌더링한다 (live view용).
//
//	╭──────────────────────────────────────────────╮
//	│ → Shell powershell -Command "Get-CimInst..." │
//	│                                              │
//	│ TotalMemoryGB FreeMemoryGB                   │
//	│      19.53         8.66                      │
//	╰──────────────────────────────────────────────╯
func renderShellBoxRunning(name string, args map[string]any, lines []string, elapsed time.Duration, width int) string {
	return renderShellBoxContent(name, args, lines, boxIconRunning, elapsed, width)
}

// renderShellBoxContent는 Shell 박스의 공통 렌더링 로직이다.
func renderShellBoxContent(name string, args map[string]any, outputLines []string, icon boxIcon, dur time.Duration, width int) string {
	boxW := width - 4
	if boxW < 30 {
		boxW = 30
	}
	innerW := boxW - 4 // border(2) + padding(2)

	header := shellBoxHeaderLine(name, args, icon, dur, innerW)

	var content strings.Builder
	content.WriteString(header)

	if len(outputLines) > 0 {
		content.WriteString("\n") // 헤더와 출력 사이 빈 줄

		display := outputLines
		overflow := 0
		if len(display) > shellBoxDisplayLines {
			overflow = len(display) - shellBoxDisplayLines
			display = display[len(display)-shellBoxDisplayLines:]
		}
		// 잘린 오래된 줄은 상단에 표시 — 최신 줄이 항상 하단에 보인다
		if overflow > 0 {
			content.WriteString("\n" + StyleCmdBoxDim.Render(fmt.Sprintf("... +%d lines", overflow)))
		}
		for _, line := range display {
			runes := []rune(line)
			if len(runes) > innerW {
				line = string(runes[:innerW-3]) + "..."
			}
			content.WriteString("\n" + line)
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSubtle).
		PaddingLeft(1).
		PaddingRight(1).
		Width(boxW).
		Render(content.String())
}

// shellBoxHeaderLine은 박스 헤더 한 줄을 생성한다.
// 형식: √ Shell powershell -Command "..." (1.2s)
func shellBoxHeaderLine(name string, args map[string]any, icon boxIcon, dur time.Duration, maxW int) string {
	var iconStr string
	switch icon {
	case boxIconRunning:
		iconStr = StyleClaude().Render("→")
	case boxIconDone:
		iconStr = StyleSuccess.Render("√")
	case boxIconFailed:
		iconStr = StyleError.Render("✗")
	}

	displayName := StyleCmdBoxToolName.Render(toolDisplayName(name))

	// 실제 명령어 미리보기 (shell_exec → command, 그 외 → description)
	cmdPreview := shellBoxCmdPreview(name, args)

	header := iconStr + " " + displayName
	if cmdPreview != "" {
		used := lipgloss.Width(iconStr) + 1 + lipgloss.Width(displayName) + 1
		remaining := maxW - used - 8 // 시간 표시용 여유
		if remaining > 15 {
			header += " " + StyleCmdBoxDim.Render(truncateStr(cmdPreview, remaining))
		}
	}

	if dur > 0 && icon != boxIconRunning {
		header += " " + StyleCmdBoxDim.Render("("+formatElapsedShort(dur)+")")
	}

	return header
}

// shellBoxCmdPreview는 박스 헤더에 표시할 명령어/인자 미리보기를 반환한다.
// shell_exec / file_transfer는 전용 포맷을 사용하고, 나머지는 toolDisplayArg로 폴백한다.
func shellBoxCmdPreview(name string, args map[string]any) string {
	if args == nil {
		return ""
	}
	switch name {
	case "shell_exec":
		if cmd, ok := args["command"].(string); ok {
			return cmd
		}
	case "file_transfer":
		if desc, ok := args["description"].(string); ok && desc != "" {
			return desc
		}
		action, _ := args["action"].(string)
		local, _ := args["local_path"].(string)
		remote, _ := args["remote_path"].(string)
		if action != "" {
			return action + " " + local + " → " + remote
		}
	default:
		return toolDisplayArg(name, args)
	}
	return ""
}

// buildRoundedBorderTitleLine은 RoundedBorder 상단 줄에 제목을 안전하게 끼워 넣는다.
func buildRoundedBorderTitleLine(totalWidth int, title string) string {
	if totalWidth < 4 || title == "" {
		return ""
	}

	titleStr := StyleCmdBoxToolName.Render(title)
	titleWidth := lipgloss.Width(titleStr)
	innerWidth := totalWidth - 2
	if innerWidth < 2 {
		return ""
	}

	if titleWidth >= innerWidth {
		plain := truncateStr(title, max(1, innerWidth-2))
		titleStr = StyleCmdBoxToolName.Render(plain)
		titleWidth = lipgloss.Width(titleStr)
	}

	leftFill := 1
	rightFill := innerWidth - leftFill - titleWidth
	if rightFill < 1 {
		rightFill = 1
		leftFill = max(0, innerWidth-rightFill-titleWidth)
	}
	if leftFill < 1 {
		leftFill = 1
	}

	borderStyle := lipgloss.NewStyle().Foreground(ColorSubtle)
	left := borderStyle.Render("╭" + strings.Repeat("─", leftFill))
	right := borderStyle.Render(strings.Repeat("─", max(0, rightFill)) + "╮")
	return left + titleStr + right
}

// appendShellLine은 shell stdout 링 버퍼에 한 줄을 추가한다.
func appendShellLine(lines []string, line string, max int) []string {
	lines = append(lines, line)
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return lines
}
