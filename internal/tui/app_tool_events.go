// Package tui
// File: app_tool_events.go
// Description: AppModel의 도구 실행 관련 메시지 핸들러
// Responsibility: ToolStart/ShellOutput/ToolEnd/ResponseDone/Error/AgentDone/SubagentEvent 처리

package tui

import (
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleToolMsg는 도구 실행 수명주기 메시지를 처리한다.
// 처리된 경우 (AppModel, cmd, true)를 반환한다.
func (m AppModel) handleToolMsg(msg tea.Msg) (AppModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ToolStartMsg:
		m.activeTools.Add(msg.ToolID, msg.Name, msg.Target)
		m.progress.AddTool(msg.ToolID, msg.Name, msg.Target)
		m.shimmer.SetText(msg.Name)
		m.shimmer.bgCount = max(0, m.activeTools.RunningCount()-1)
		m.streamLines = nil
		if m.box != nil {
			m.box.Println(renderToolHeaderLine(msg.Name, msg.Target, msg.Args))
		}
		return m, nil, true

	case ShellOutputMsg:
		m.shimmer.RecordActivity()
		trimmed := strings.TrimRight(msg.Line, "\r\n")
		if trimmed != "" {
			m.progress.SetOutput(msg.ToolID, trimmed)
			m.activeTools.AppendOutput(msg.ToolID, trimmed)
		}
		return m, nil, true

	case ToolEndMsg:
		var capturedLines []string
		if prev := m.activeTools.MostRecent(); prev != nil && prev.toolID == msg.ToolID {
			capturedLines = prev.shellLines
		}
		m.activeTools.Remove(msg.ToolID)
		m.progress.CompleteTool(msg.ToolID, msg.Duration, msg.Success)
		m.shimmer.bgCount = max(0, m.activeTools.RunningCount()-1)
		m.stats.AddToolUse()
		m.history.Add(toolHistoryEntry{
			toolID:     msg.ToolID,
			toolName:   msg.Name,
			result:     msg.Result,
			duration:   msg.Duration,
			success:    msg.Success,
			shellLines: capturedLines,
		})
		if m.activeTools.RunningCount() == 0 {
			m.shimmer.SetText("thinking...")
			m.streamLines = nil
		}
		if m.box != nil {
			m.box.Println(toolSummary(msg.Name, nil, msg.Result, msg.Duration, msg.Success))
		}
		return m, nil, true

	case ResponseDoneMsg:
		m.shimmer.RecordActivity()
		if m.streamTokens != "" {
			m.box.Println(renderResponseText(string(msg), m.mdRend))
		}
		m.streamTokens = ""
		m.streamLines = nil
		m.streamCache.Reset()
		return m, nil, true

	case ErrorMsg:
		slog.Error("agent error in TUI", "err", msg.Err)
		if m.box != nil {
			m.box.Println(renderErrorLine(msg.Err))
		}
		m.busy = false
		m.shimmer.Stop()
		return m, nil, true

	case AgentDoneMsg:
		m.activeTools.Clear()
		m.shimmer.Stop()
		m.progress.Reset()
		if summary := m.stats.Summary(); summary != "" && m.box != nil {
			m.box.Println(summary)
		}
		m.streamTokens = ""
		m.streamLines = nil
		m.streamCache.Reset()
		if entry, ok := m.queue.Dequeue(); ok {
			m.progress.Reset()
			m.stats.Start()
			m.box.Println(renderUserInputLine(entry.displayInput))
			shimmerCmd := m.shimmer.Start("thinking...")
			return m, tea.Batch(m.runAgent(entry.expandedInput), m.sp.Tick, shimmerCmd), true
		}
		m.busy = false
		return m, nil, true

	case SubagentEventMsg:
		if m.box != nil {
			m.box.Println(renderSubagentEvent(msg.Event))
		}
		return m, nil, true
	}
	return m, nil, false
}
