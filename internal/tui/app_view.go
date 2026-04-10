// Package tui
// File: app_view.go
// Description: AppModel의 View 렌더링 및 레이아웃 헬퍼
// Responsibility: 인라인 모드 입력 UI 렌더링, 커서 파킹, 레이아웃 크기 조정

package tui

import (
	"fmt"
	"strings"
)

// View는 인라인 BubbleTea 영역을 렌더링한다.
// 통합 레이아웃: shimmer(선택) → streamPreview(선택) → queue(선택) → inputBox → footer → selection(선택) → statusBar
func (m AppModel) View() string {
	if m.width == 0 {
		return "initializing..."
	}

	// Ctrl+O 도구 이력 오버레이 (활성 시 전체 영역 대체)
	if m.histOverlay.isActive() {
		overlay := m.histOverlay.View(&m.history, m.width)
		if m.parker != nil {
			m.parker.SetTarget(0, 0, false)
		}
		return "\033[?25l" + overlay
	}

	var parts []string

	// 1. 스트리밍 미리보기 (busy일 때만, input box 위)
	if m.busy && len(m.streamLines) > 0 {
		parts = append(parts, strings.Join(m.streamLines, "\n"))
	}

	// 1b. 도구 stdout 박스 (가장 최근 실행 도구 기준 슬라이딩 버퍼)
	if m.busy {
		if recent := m.activeTools.MostRecent(); recent != nil && len(recent.shellLines) > 0 {
			title := toolDisplayName(recent.toolName)
			if extra := m.activeTools.RunningCount() - 1; extra > 0 {
				title = fmt.Sprintf("%s (+%d)", title, extra)
			}
			parts = append(parts, renderShellBox(title, recent.shellLines, m.width))
		}
	}

	// 2. 큐 표시 (대기 항목 있을 때만, input box 위)
	if m.queue.Len() > 0 {
		parts = append(parts, m.queue.View(m.width))
	}

	// 3. thinking 고정 줄 (항상 input box 바로 위)
	// busy 시 shimmer 텍스트 표시
	if m.busy && m.shimmer.active {
		parts = append(parts, m.shimmer.View())
	}

	// 3.5. 확인 오버레이 (input box 바로 위 고정 — input box는 항상 실제 입력 표시)
	if m.confirm.active {
		parts = append(parts, m.confirm.render(m.width))
	}

	// 4. Input box (항상 표시)
	borderColor := ColorPromptBorder
	if m.busy {
		borderColor = ColorDim // 실행 중 시각적 힌트
	}
	if m.idle.active {
		overlay := m.idle.render(m.width)
		parts = append(parts, renderInputBox(m.width, overlay, ColorWarning, ""))
	} else {
		parts = append(parts, renderInputBox(m.width, m.input.View(), borderColor, ""))
	}

	// 5. Footer 힌트 (항상, 상태별 내용 변경)
	footerSt := footerIdle
	if m.busy {
		footerSt = footerBusy
	}
	if m.confirm.active {
		footerSt = footerConfirm
	}
	if m.idle.active {
		footerSt = footerIdlePrompt
	}
	if m.selection.active {
		footerSt = footerSelection
	}
	parts = append(parts, renderFooter(footerSt, m.width))

	// 6. 인터랙티브 셀렉션 (활성화 시만)
	if m.selection.active {
		parts = append(parts, m.selection.View(m.width))
	}

	// 7. Status bar (항상)
	parts = append(parts, m.statusBar.View())

	// 커서 파킹
	if m.parker != nil && !m.idle.active && !m.confirm.active && !m.selection.active {
		m.updateCursorTarget()
	} else if m.parker != nil {
		m.parker.SetTarget(0, 0, false)
	}

	finalView := strings.Join(parts, "\n")
	return "\033[?25l" + finalView
}

// updateCursorTarget은 cursorParkWriter에 커서 위치를 계산하여 설정한다.
//
// View 구조:
//
//	[shimmer line]          ← busy && shimmer.active
//	[streaming preview]     ← busy && streamLines > 0
//	[queue indicator]       ← queue.Len() > 0
//	╭──────── (top border)
//	│ > text|  ← textareaRow=0
//	╰────────
//	footer(1)
//	[selection area]        ← selection.active
//	statusbar(1)            ← BubbleTea \r 후 커서 위치
func (m AppModel) updateCursorTarget() {
	// input box 위 줄 수
	linesAbove := 1 // thinking 고정 줄 (항상 1줄)
	if m.busy && len(m.streamLines) > 0 {
		linesAbove += len(m.streamLines)
	}
	if m.busy {
		if recent := m.activeTools.MostRecent(); recent != nil && len(recent.shellLines) > 0 {
			// shellBox: border(2) + 실제 줄 수
			linesAbove += len(recent.shellLines) + 2
		}
	}
	if m.queue.Len() > 0 {
		linesAbove++
	}
	if m.confirm.active {
		linesAbove += m.confirm.height()
	}

	inputH := inputBoxHeight(m.input.View())

	// input box 아래 줄 수
	linesBelow := 1 // footer
	if m.selection.active {
		linesBelow += m.selection.Height()
	}
	linesBelow++ // status bar

	totalLines := linesAbove + inputH + linesBelow

	textareaRow := m.input.ti.Line()
	charOffset := m.input.ti.LineInfo().CharOffset

	// input box 내 커서 줄: linesAbove + top border(1) + textareaRow
	cursorViewLine := linesAbove + 1 + textareaRow
	// 마지막 줄(statusbar)에서 커서 줄까지 올라갈 줄 수
	linesUp := totalLines - 1 - cursorViewLine
	// 커서 열: space(1) + >(1) + space(1) + charOffset, 1-indexed = charOffset+4
	col := charOffset + 4

	m.parker.SetTarget(linesUp, col, true)
}

// resize는 인라인 모드용 컴포넌트 크기를 재조정한다.
func (m AppModel) resize() AppModel {
	m.input.setWidth(m.width)
	maxInputH := max(1, min(m.height-5, max(3, m.height/3)))
	m.input.setMaxHeight(maxInputH)
	m.statusBar.setWidth(m.width)
	m.shimmer.width = m.width
	return m
}

