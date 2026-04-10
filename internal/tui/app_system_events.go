// Package tui
// File: app_system_events.go
// Description: AppModel의 시스템 이벤트 메시지 핸들러
// Responsibility: Confirm/IdleInput/Select/ActiveServer 상태 전환 처리

package tui

import tea "github.com/charmbracelet/bubbletea"

// handleSystemMsg는 시스템 상태 전환 메시지를 처리한다.
// 처리된 경우 (AppModel, cmd, true)를 반환한다.
func (m AppModel) handleSystemMsg(msg tea.Msg) (AppModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ConfirmRequestMsg:
		m.confirm = confirmState{
			active:  true,
			request: msg.Request,
			replyCh: msg.ReplyCh,
		}
		return m, nil, true

	case ConfirmResponseMsg:
		if msg.Response.AllowAll {
			m.yoloMode = true
			if m.confirmHandler != nil {
				m.confirmHandler.EnableYolo()
			}
			m.statusBar.setYoloMode(true)
		}
		go func() {
			msg.ReplyCh <- msg.Response
		}()
		return m, nil, true

	case IdleInputRequestMsg:
		m.idle = idleState{
			active:  true,
			request: msg.Request,
			replyCh: msg.ReplyCh,
		}
		return m, nil, true

	case IdleInputResponseMsg:
		go func() {
			msg.ReplyCh <- msg.Response
		}()
		return m, nil, true

	case SelectRequestMsg:
		m.selection.Activate(msg.Question, msg.Options, msg.ReplyCh)
		return m, nil, true

	case SelectResponseMsg:
		go func() {
			msg.ReplyCh <- msg.Result
		}()
		m.selection.Deactivate()
		return m, nil, true

	case ActiveServerMsg:
		m.activeServer = msg.Server
		m.statusBar.setActiveServer(msg.Server)
		return m, nil, true
	}
	return m, nil, false
}
