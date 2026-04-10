// Package tui
// File: println_render.go
// Description: Program.Println()용 채팅 이벤트 렌더링 함수 모음
// Responsibility: 사용자 입력, 도구 실행, 응답, 에러를 스크롤백 출력용 문자열로 변환

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/subagent"
)

// renderUserInputLine은 사용자 입력을 스크롤백 출력용으로 렌더링한다.
// 형식: ❯ You\n  입력 내용
func renderUserInputLine(display string) string {
	header := StyleInputPrompt.Render("❯ ") + lipgloss.NewStyle().Bold(true).Render("You")
	body := StyleUserMsg.Render("  " + display)
	return "\n" + header + "\n" + body
}

// renderToolHeaderLine은 도구 시작을 스크롤백 출력용으로 렌더링한다.
// 형식: ● toolName(arg)
func renderToolHeaderLine(name, target string, args map[string]any) string {
	_ = target
	bullet := StyleClaude().Render("●")
	toolName := StyleCmdBoxToolName.Render(toolDisplayName(name))
	arg := toolDisplayArg(name, args)
	if arg != "" {
		return bullet + " " + toolName + StyleCmdBoxDim.Render("(") + arg + StyleCmdBoxDim.Render(")")
	}
	return bullet + " " + toolName
}

// renderToolSummaryLine은 도구 완료를 스크롤백 출력용으로 렌더링한다.
// 형식: ⎿ Wrote N lines to path.go
func renderToolSummaryLine(name string, args map[string]any, result string, duration time.Duration, success bool) string {
	return toolSummary(name, args, result, duration, success)
}

// renderResponseText는 최종 응답을 스크롤백 출력용으로 렌더링한다.
// 형식: ⎿ 첫 줄\n     나머지 줄...
func renderResponseText(content string, md *mdRenderer) string {
	lines := md.Render(content)
	if len(lines) == 0 {
		return ""
	}
	bracket := StyleResponseBracket.Render("  ⎿  ")
	indent := "     "
	var sb strings.Builder
	for i, line := range lines {
		if i == 0 {
			sb.WriteString(bracket + line)
		} else {
			sb.WriteString(indent + line)
		}
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderStreamingPreview는 스트리밍 중 미리보기를 렌더링한다.
// View()의 busy 상태에서 마지막 N줄을 표시할 때 사용한다.
func renderStreamingPreview(content string, md *mdRenderer, cache *stableCache, maxLines int) []string {
	if content == "" {
		return nil
	}
	lines := md.RenderStreaming(content, cache)
	if len(lines) == 0 {
		return nil
	}
	bracket := StyleResponseBracket.Render("  ⎿  ")
	indent := "     "
	var rendered []string
	for i, line := range lines {
		if i == 0 {
			rendered = append(rendered, bracket+line)
		} else {
			rendered = append(rendered, indent+line)
		}
	}
	// 마지막 maxLines 줄만 반환
	if maxLines > 0 && len(rendered) > maxLines {
		rendered = rendered[len(rendered)-maxLines:]
	}
	return rendered
}

// renderErrorLine은 에러를 스크롤백 출력용으로 렌더링한다.
func renderErrorLine(err error) string {
	return StyleError.Render(fmt.Sprintf("  Error: %s", err.Error()))
}

// renderSystemLine은 시스템 메시지를 스크롤백 출력용으로 렌더링한다.
func renderSystemLine(msg string) string {
	return StyleInfoBarDim.Render(msg)
}

// renderSubagentEvent는 서브에이전트 진행 이벤트를 스크롤백용 문자열로 렌더링한다.
// 형식:
//
//	EventAgentStart → "  ⎿  ● DB 에이전트 시작"
//	EventToolCall   → "     ⎿  toolName(arg)"
//	EventAgentDone  → "  ⎿  ✓ DB — Done (3.2s · 3 tool uses · 2.1k tokens)"
func renderSubagentEvent(e subagent.Event) string {
	prefix1 := StyleResponseBracket.Render("  ⎿  ")
	prefix2 := "     " + StyleResponseBracket.Render("⎿  ")
	label := strings.ToUpper(string(e.AgentType))
	if e.Server != "" && e.Server != "localhost" {
		label += "@" + e.Server
	}

	switch e.Type {
	case subagent.EventAgentStart:
		return prefix1 + StyleClaude().Render("●") + " " +
			StyleCmdBoxToolName.Render(label) + StyleCmdBoxDim.Render(" 에이전트 시작")

	case subagent.EventToolCall:
		arg := e.ToolArg
		if len(arg) > 60 {
			arg = arg[:57] + "..."
		}
		toolName := StyleCmdBoxToolName.Render(toolDisplayName(e.ToolName))
		if arg != "" {
			return prefix2 + toolName + StyleCmdBoxDim.Render("("+arg+")")
		}
		return prefix2 + toolName

	case subagent.EventAgentDone:
		dur := formatElapsedShort(e.Duration)
		var parts []string
		if e.ToolUses > 0 {
			parts = append(parts, fmt.Sprintf("%d tool uses", e.ToolUses))
		}
		total := e.InputTokens + e.OutputTokens
		if total > 0 {
			parts = append(parts, formatTokenCount(total))
		}
		parts = append(parts, dur)
		summary := strings.Join(parts, " · ")
		if e.Success {
			return prefix1 + StyleSuccess.Render("✓") + " " +
				StyleCmdBoxToolName.Render(label) + StyleCmdBoxDim.Render(" — Done ("+summary+")")
		}
		return prefix1 + StyleError.Render("✗") + " " +
			StyleCmdBoxToolName.Render(label) + StyleCmdBoxDim.Render(" — Failed ("+summary+")")
	}
	return ""
}
