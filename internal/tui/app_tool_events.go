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
		m.activeTools.Add(msg.ToolID, msg.Name, msg.Target, msg.Args)
		desc, _ := msg.Args["description"].(string)
		if phaseID, phaseName, ok := parsePhaseFromDescription(desc); ok {
			m.progress.AddToolWithPhase(msg.ToolID, msg.Name, msg.Target, phaseID, phaseName, msg.Args)
		} else {
			m.progress.AddTool(msg.ToolID, msg.Name, msg.Target, msg.Args)
		}
		m.shimmer.bgCount = max(0, m.activeTools.RunningCount()-1)
		// 스트리밍 미리보기를 영구 출력 후 초기화 (도구 실행 전 LLM 텍스트 유지)
		if m.streamTokens != "" && m.box != nil {
			m.box.Println(renderResponseText(m.streamTokens, m.mdRend))
			m.streamTokens = ""
			m.streamLines = nil
			m.streamCache.Reset()
		} else {
			m.streamLines = nil
		}
		return m, nil, true

	case ShellOutputMsg:
		m.shimmer.RecordActivity()
		trimmed := strings.TrimRight(msg.Line, "\r\n")
		if trimmed != "" {
			m.progress.SetOutput(msg.ToolID, trimmed)
			if !m.activeTools.IsBackgrounded(msg.ToolID) {
				m.activeTools.AppendOutput(msg.ToolID, trimmed)
			}
		}
		return m, nil, true

	case ToolEndMsg:
		isBackground := m.activeTools.IsBackgrounded(msg.ToolID)
		var capturedLines []string
		totalLines := 0
		if toolState, ok := m.activeTools.states[msg.ToolID]; ok {
			capturedLines = toolState.shellLines
			totalLines = toolState.shellTotal
		}
		if isShellBoxTool(msg.Name) {
			capturedLines, totalLines = resolveShellBoxOutput(msg.Name, capturedLines, totalLines, msg.Result)
		}
		m.activeTools.Remove(msg.ToolID)
		m.progress.CompleteTool(msg.ToolID, msg.Duration, msg.Success)
		m.shimmer.bgCount = m.activeTools.BackgroundCount()
		m.stats.AddToolUse()
		m.history.Add(toolHistoryEntry{
			toolID:     msg.ToolID,
			toolName:   msg.Name,
			result:     msg.Result,
			duration:   msg.Duration,
			success:    msg.Success,
			shellLines: capturedLines,
			shellTotal: totalLines,
		})
		if m.activeTools.RunningCount() == 0 {
			label := m.thinkingLabel
			if label == "" {
				label = "thinking..."
			}
			m.shimmer.SetText(label)
			m.streamLines = nil
		}
		if m.box != nil {
			if isBackground {
				m.box.Println(renderBackgroundDone(msg.Name, msg.Duration, msg.Success))
			} else {
				var args map[string]any
				for _, item := range m.progress.items {
					if item.toolID == msg.ToolID {
						args = item.args
						break
					}
				}
				contentLines := capturedLines
				if !isShellBoxTool(msg.Name) {
					contentLines = toolBoxContent(msg.Name, args, msg.Result, msg.Success)
				}
				m.box.Println(renderShellBoxCompleted(msg.Name, args, contentLines, totalLines, msg.Duration, msg.Success, m.width))
			}
		}
		return m, nil, true

	case ResponseDoneMsg:
		m.shimmer.RecordActivity()
		// streamTokens 여부와 무관하게 내용이 있으면 항상 렌더링한다.
		// LLM이 스트리밍 없이 최종 응답을 반환하는 경우도 처리한다.
		if content := string(msg); content != "" {
			m.box.Println(renderResponseText(content, m.mdRend))
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
		// reqID가 다르면 이미 취소된 이전 요청의 완료 신호 — 무시한다.
		if msg.ReqID != m.reqID {
			return m, nil, true
		}
		// ResponseDoneMsg보다 먼저 처리될 경우를 대비한 안전망
		if m.streamTokens != "" && m.box != nil {
			m.box.Println(renderResponseText(m.streamTokens, m.mdRend))
		}
		m.activeTools.Clear()
		m.shimmer.Stop()
		m.progress.Reset()
		m.streamTokens = ""
		m.streamLines = nil
		m.streamCache.Reset()
		if entry, ok := m.queue.Dequeue(); ok {
			m.reqID++ // 큐에서 꺼낸 요청도 새 세대
			m.progress.Reset()
			m.stats.Start()
			if m.box != nil {
				m.box.Println(renderTurnSeparator(m.width))
			}
			m.box.Println(renderUserInputLine(entry.displayInput, m.width))
			m.turnCount++
			label := m.thinkingLabel
			if label == "" {
				label = "thinking..."
			}
			shimmerCmd := m.shimmer.Start(label)
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
