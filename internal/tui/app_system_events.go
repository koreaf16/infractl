package tui

import tea "github.com/charmbracelet/bubbletea"

func (m AppModel) handleSystemMsg(msg tea.Msg) (AppModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case PrivilegePromptRequestMsg:
		m.privilege = privilegePromptState{
			active:  true,
			request: msg.Request,
			replyCh: msg.ReplyCh,
		}
		return m, nil, true

	case PrivilegePromptResponseMsg:
		go func() {
			msg.ReplyCh <- msg.Response
		}()
		return m, nil, true

	case SelectRequestMsg:
		m.selection.ActivateWithHeader(msg.Question, msg.Options, msg.ReplyCh, msg.Header)
		return m, nil, true

	case SelectResponseMsg:
		go func() {
			msg.ReplyCh <- msg.Result
		}()
		m.selection.Deactivate()
		return m, nil, true

	case FormRequestMsg:
		m.form.Activate(msg.Title, msg.Fields, msg.ReplyCh, msg.Header)
		m.input.Reset()
		if len(msg.Fields) > 0 {
			m.input.ti.Placeholder = msg.Fields[0].Placeholder
		}
		return m, nil, true

	case FormResponseMsg:
		go func() {
			msg.ReplyCh <- msg.Result
		}()
		m.form.Deactivate()
		m.input.Reset()
		m.input.ti.Placeholder = ""
		return m, nil, true

	case ActiveServerMsg:
		m.activeServer = msg.Server
		m.statusBar.setActiveServer(msg.Server)
		return m, nil, true

	}

	return m, nil, false
}
