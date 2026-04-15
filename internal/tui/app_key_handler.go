package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m AppModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !msg.Paste {

		if m.selection.active {
			handled, respMsg := m.selection.handleKey(msg)
			if handled {
				if respMsg != nil {
					return m.Update(respMsg)
				}
				return m, nil
			}
		}

		if m.form.active {
			switch msg.Type {
			case tea.KeyTab:
				m.form.NextField()
				m.input.Reset()
				m.input.ti.Placeholder = m.form.CurrentPlaceholder()
				return m, nil
			case tea.KeyShiftTab:
				m.form.PrevField()
				m.input.Reset()
				m.input.ti.Placeholder = m.form.CurrentPlaceholder()
				return m, nil
			case tea.KeyEsc:
				if m.form.phase == formPhaseReview {
					m.form.phase = formPhaseEdit
					m.input.Reset()
					m.input.ti.Placeholder = m.form.CurrentPlaceholder()
					return m, nil
				}
				return m.Update(FormResponseMsg{
					Result:  FormResult{Cancelled: true},
					ReplyCh: m.form.replyCh,
				})
			}
			// 나머지 키는 inputBar로 fall-through (일반 타이핑/Enter)
		}

		if m.privilege.active {
			handled, respMsg := m.privilege.handleKey(msg)
			if handled {
				if respMsg != nil {
					return m.Update(respMsg)
				}
				return m, nil
			}
		}

		if msg.Type != tea.KeyCtrlC && m.ctrlCCount > 0 {
			m.ctrlCCount = 0
			m.input.ti.Placeholder = "Press Ctrl+C again to quit."
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.ctrlCCount == 0 {
				m.ctrlCCount = 1
				m.input.ti.SetValue("")
				m.input.ti.Placeholder = "Press Ctrl+C again to quit."
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

		if msg.String() == "ctrl+b" && m.busy {
			count := m.activeTools.BackgroundAll()
			if count > 0 {
				m.shimmer.bgCount = m.activeTools.BackgroundCount()
				if m.box != nil {
					m.box.Println(renderSystemLine(
						fmt.Sprintf("%d tool(s) moved to background", count)))
				}
			}
			return m, nil
		}

		if msg.String() == "ctrl+y" {
			m.yoloMode = m.ag.ToggleYoroMode()
			m.statusBar.setYoloMode(m.yoloMode)
			return m, nil
		}

		if msg.Type == tea.KeyShiftTab {
			m.planMode = m.ag.TogglePlanMode()
			m.statusBar.setPlanMode(m.planMode)
			return m, nil
		}

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
			if m.box != nil {
				if queueLen > 0 {
					m.box.Println(renderSystemLine(
						fmt.Sprintf("cancelled by user (%d queued items cleared)", queueLen)))
				} else {
					m.box.Println(renderSystemLine("cancelled by user"))
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m = m.resize()
	return m, cmd
}

func welcomeBanner(model string, serverCount int) string {
	title := StyleBannerTitle.Render("Infractl") + " " + StyleBannerInfo.Render("v1.0.0")
	info := StyleInfoBarDim.Render(fmt.Sprintf("model: %s | %d servers", model, serverCount))
	return fmt.Sprintf("\n  %s\n  %s\n", title, info)
}
