package tui

import (
	"fmt"
	"strings"
	"time"
)

func (m AppModel) View() string {
	if m.width == 0 {
		return "initializing..."
	}

	if m.histOverlay.isActive() {
		overlay := m.histOverlay.View(&m.history, m.width)
		if m.parker != nil {
			m.parker.SetTarget(0, 0, false)
		}
		return "\033[?25l" + overlay
	}

	var parts []string

	if m.busy && len(m.streamLines) > 0 {
		parts = append(parts, strings.Join(m.streamLines, "\n"))
	}
	// 실행 중 도구 박스 표시 (→ ToolName <arg> + streaming output)
	if m.busy {
		if st := m.activeTools.MostRecentForeground(); st != nil {
			elapsed := time.Since(st.startTime)
			parts = append(parts, renderShellBoxRunning(st.toolName, st.args, st.shellLines, elapsed, m.width))
		}
	}
	if m.queue.Len() > 0 {
		parts = append(parts, m.queue.View(m.width))
	}
	// 채팅 영역과 thinking 표시 사이에 항상 빈 줄을 하나 예약한다.
	parts = append(parts, "")
	// shimmer 라인은 항상 인풋박스 바로 위에 고정 예약한다.
	// busy 중 아닐 때는 빈 줄로 유지해 레이아웃이 흔들리지 않게 한다.
	shimmerLine := ""
	if m.busy && m.shimmer.active {
		shimmerLine = m.shimmer.View()
	}
	parts = append(parts, shimmerLine)
	borderColor := ColorPromptBorder
	if m.busy {
		borderColor = ColorDim
	}
	switch {
	case m.privilege.active:
		parts = append(parts, renderInputBox(m.width, m.privilege.render(m.width), ColorWarning, ""))
	default:
		parts = append(parts, renderInputBox(m.width, m.input.View(), borderColor, ""))
	}

	footerSt := footerIdle
	if m.busy {
		footerSt = footerBusy
	}
	if m.privilege.active {
		footerSt = footerSecretPrompt
	}
	if m.selection.active {
		footerSt = footerSelection
	}
	parts = append(parts, renderFooter(footerSt, m.width))

	if m.selection.active {
		parts = append(parts, m.selection.View(m.width))
	}
	parts = append(parts, m.statusBar.View())

	if m.parker != nil && !m.selection.active && !m.privilege.active {
		m.updateCursorTarget()
	} else if m.parker != nil {
		m.parker.SetTarget(0, 0, false)
	}

	return "\033[?25l" + strings.Join(parts, "\n")
}

func (m AppModel) updateCursorTarget() {
	linesAbove := 1
	if m.busy && len(m.streamLines) > 0 {
		linesAbove += len(m.streamLines)
	}
	if m.queue.Len() > 0 {
		linesAbove++
	}
	linesAbove += 2 // 빈 줄 separator + shimmer 라인 (항상 2줄 예약)

	inputH := inputBoxHeight(m.input.View())
	linesBelow := 1
	if m.selection.active {
		linesBelow += m.selection.Height()
	}
	linesBelow++

	totalLines := linesAbove + inputH + linesBelow
	textareaRow := m.input.ti.Line()
	charOffset := m.input.ti.LineInfo().CharOffset

	cursorViewLine := linesAbove + 1 + textareaRow
	linesUp := totalLines - 1 - cursorViewLine
	col := charOffset + 4
	m.parker.SetTarget(linesUp, col, true)
}

func (m AppModel) visibleShellBox() (string, []string, bool) {
	if recent := m.activeTools.MostRecentForeground(); recent != nil && len(recent.shellLines) > 0 {
		title := toolDisplayName(recent.toolName)
		fgCount := m.activeTools.RunningCount() - m.activeTools.BackgroundCount()
		if extra := fgCount - 1; extra > 0 {
			title = fmt.Sprintf("%s (+%d)", title, extra)
		}
		return title, recent.shellLines, true
	}
	if !m.busy {
		if last, ok := m.history.LatestShell(); ok {
			return toolDisplayName(last.toolName), last.shellLines, true
		}
	}
	return "", nil, false
}
