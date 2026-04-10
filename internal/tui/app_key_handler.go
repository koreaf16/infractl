// Package tui
// File: app_key_handler.go
// Description: AppModel의 키보드 입력 처리 및 시작 배너
// Responsibility: 키 이벤트 라우팅, Ctrl+C/O/Y, Esc 중단, 오버레이 스크롤

package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyMsg는 키보드 입력을 처리한다.
func (m AppModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !msg.Paste {
		// 셀렉션 활성 시 키 입력 우선 라우팅
		if m.selection.active {
			handled, respMsg := m.selection.handleKey(msg)
			if handled {
				if respMsg != nil {
					return m.Update(respMsg)
				}
				return m, nil
			}
		}

		if m.idle.active {
			handled, respMsg := m.idle.handleKey(msg)
			if handled {
				if respMsg != nil {
					return m.Update(respMsg)
				}
				return m, nil
			}
		}

		if m.confirm.active {
			handled, respMsg := m.confirm.handleKey(msg)
			if handled {
				if respMsg != nil {
					return m.Update(respMsg)
				}
				return m, nil
			}
		}

		if msg.Type != tea.KeyCtrlC && m.ctrlCCount > 0 {
			m.ctrlCCount = 0
			m.input.ti.Placeholder = "자연어로 인프라를 제어하세요..."
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.ctrlCCount == 0 {
				m.ctrlCCount = 1
				m.input.ti.SetValue("")
				m.input.ti.Placeholder = "종료하려면 Ctrl+C를 한 번 더 누르세요."
				return m, nil
			}
			m.cancel()
			return m, tea.Quit
		}

		if msg.String() == "ctrl+o" {
			if m.histOverlay.isActive() {
				m.histOverlay.close()
			} else {
				m.histOverlay.open(m.history.Len())
			}
			return m, nil
		}

		if msg.String() == "ctrl+y" {
			m.yoloMode = !m.yoloMode
			if m.confirmHandler != nil {
				if m.yoloMode {
					m.confirmHandler.EnableYolo()
				} else {
					m.confirmHandler.DisableYolo()
				}
			}
			m.statusBar.setYoloMode(m.yoloMode)
			return m, nil
		}

		// 오버레이 스크롤 (Ctrl+O가 열려있을 때)
		if m.histOverlay.isActive() {
			switch msg.Type {
			case tea.KeyUp:
				m.histOverlay.scrollUp()
				return m, nil
			case tea.KeyDown:
				m.histOverlay.scrollDown(m.history.Len())
				return m, nil
			case tea.KeyEsc:
				m.histOverlay.close()
				return m, nil
			}
		}

		if msg.String() == "esc" && m.busy {
			m.cancel()
			ctx, cancel := context.WithCancel(context.Background())
			m.ctx = ctx
			m.cancel = cancel
			m.busy = false
			queueLen := m.queue.Len()
			m.queue.Clear()
			m.streamTokens = ""
			m.streamLines = nil
			m.streamCache.Reset()
			if queueLen > 0 {
				m.box.Println(renderSystemLine(
					fmt.Sprintf("사용자에 의해 중단되었습니다. (%d개 대기 작업도 취소됨)", queueLen)))
			} else {
				m.box.Println(renderSystemLine("사용자에 의해 중단되었습니다."))
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m = m.resize()
	return m, cmd
}

// welcomeBanner는 시작 시 표시되는 환영 메시지를 생성한다.
func welcomeBanner(model string, serverCount int) string {
	title := StyleBannerTitle.Render("Infractl") + " " + StyleBannerInfo.Render("v0.3.0")
	info := StyleInfoBarDim.Render(fmt.Sprintf("model: %s · %d servers", model, serverCount))
	return fmt.Sprintf("\n  %s\n  %s\n", title, info)
}
