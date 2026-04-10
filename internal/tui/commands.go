// Package tui
// File: commands.go
// Description: slash command handling for TUI mode
// Responsibility: implement /servers, /server, /sessions, /history, /mcp and helpers

package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleSlashCommand handles slash commands entered in the input bar.
func (m AppModel) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)

	println := func(s string) {
		m.box.Println(s)
	}
	printErr := func(s string) {
		m.box.Println(renderErrorLine(fmt.Errorf("%s", s)))
	}

	switch parts[0] {
	case "/quit", "/exit", "/q":
		m.cancel()
		return m, tea.Quit
	case "/clear":
		m.ag.ClearHistory()
		println(renderSystemLine("Cleared conversation history."))
	case "/model":
		println(renderSystemLine(fmt.Sprintf("Current model: %s (%s)", m.cfg.LLM.Model, m.cfg.LLM.Endpoint)))
	case "/help":
		println(renderSystemLine(helpText()))
	case "/tools":
		tools := m.ag.Tools()
		var sb strings.Builder
		sb.WriteString("Registered tools:\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("  %-20s - %s\n", t.Name(), t.Description()))
		}
		println(renderSystemLine(sb.String()))
	case "/servers":
		servers, _ := m.store.List(context.Background())
		println(buildServerTable(servers, m.activeServer))
	case "/server":
		if len(parts) >= 2 {
			name := strings.TrimSpace(parts[1])
			switch strings.ToLower(name) {
			case "clear", "none", "localhost", "local":
				m.ag.ClearActiveServer()
				println(renderSystemLine("● Active server cleared (default target: localhost)"))
				return m, nil
			}
			servers, _ := m.store.List(context.Background())
			for _, s := range servers {
				if strings.EqualFold(s.Name, name) {
					srv := s
					m.ag.SetActiveServer(srv)
					println(renderSystemLine("● Active server: " + srv.Name + " (" + srv.Host + ")"))
					return m, nil
				}
			}
			printErr("Server not found: " + name)
		} else {
			if m.selectHandler == nil {
				printErr("/server selection UI is unavailable")
				return m, nil
			}
			return m, m.serverFocusCmd()
		}
	case "/connectors":
		println(renderSystemLine(m.formatConnectors()))
	case "/mcp":
		if len(parts) >= 3 && parts[1] == "reconnect" {
			return m, m.reconnectMCPCmd(parts[2])
		}
		println(renderSystemLine(m.formatMCPStatus()))
	case "/sessions":
		if len(parts) >= 3 && parts[1] == "restore" {
			n, err := strconv.Atoi(parts[2])
			if err == nil {
				return m, m.restoreSessionCmd(n)
			}
			printErr("Usage: /sessions restore <number>")
		} else {
			println(renderSystemLine(m.formatSessions()))
		}
	case "/history":
		println(renderSystemLine(m.formatHistory()))
	default:
		printErr(fmt.Sprintf("Unknown command: %s", parts[0]))
	}
	return m, nil
}

// serverFocusCmd returns a Cmd that asks user to select an active server.
func (m AppModel) serverFocusCmd() tea.Cmd {
	return func() tea.Msg {
		servers, err := m.store.List(context.Background())
		if err != nil || len(servers) == 0 {
			return SystemMsg("No registered servers.")
		}
		if len(servers) == 1 {
			srv := servers[0]
			m.ag.SetActiveServer(srv)
			return nil
		}

		opts := make([]SelectOption, len(servers))
		for i, s := range servers {
			desc := s.Host
			if s.OS != "" {
				desc += " · " + s.OS
			}
			opts[i] = SelectOption{Label: s.Name, Description: desc, HideOther: true}
		}

		result, err := m.selectHandler.RequestSelect("Select active server", opts)
		if err != nil || result.Index < 0 || result.Index >= len(servers) {
			return nil
		}
		srv := servers[result.Index]
		m.ag.SetActiveServer(srv)
		return nil
	}
}

func helpText() string {
	return "Available commands:\n" +
		"  /help                 - show this help\n" +
		"  /tools                - list registered tools\n" +
		"  /servers              - list servers (table)\n" +
		"  /server [name|clear]  - set active server (or clear)\n" +
		"  /connectors           - show connector status\n" +
		"  /mcp                  - show MCP status\n" +
		"  /mcp reconnect <name> - reconnect MCP server\n" +
		"  /sessions             - list recent sessions\n" +
		"  /sessions restore <N> - restore session by number\n" +
		"  /history              - show recent execution logs\n" +
		"  /clear                - clear conversation history\n" +
		"  /model                - show current model\n" +
		"  /quit                 - quit\n" +
		"  Ctrl+C                - quit"
}

func connectorStatusIcon(status string) string {
	switch status {
	case "connected":
		return "✓"
	case "connecting":
		return "…"
	case "error":
		return "!"
	default:
		return " "
	}
}

func (m AppModel) formatConnectors() string {
	if m.connectorMgr == nil {
		return "Connector manager is not initialized."
	}
	states := m.connectorMgr.States()
	if len(states) == 0 {
		return "No active connectors."
	}
	var sb strings.Builder
	sb.WriteString("Active connectors:\n")
	for _, s := range states {
		icon := connectorStatusIcon(string(s.Status))
		sb.WriteString(fmt.Sprintf("  %s %-10s %-12s %-15s - %s (%d tools)\n",
			icon, s.Type, s.ServiceName, s.ServerName, s.Status, len(s.Tools)))
	}
	return sb.String()
}

func (m AppModel) formatMCPStatus() string {
	if len(m.mcpClients) == 0 {
		return "No configured MCP servers."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("MCP server status (%d):\n", len(m.mcpClients)))
	for _, c := range m.mcpClients {
		icon := connectorStatusIcon(string(c.Status))
		sb.WriteString(fmt.Sprintf("  %s %-15s - %s\n", icon, c.Name, c.Status))
	}
	return sb.String()
}

func (m AppModel) reconnectMCPCmd(name string) tea.Cmd {
	return func() tea.Msg {
		for _, c := range m.mcpClients {
			if c.Name == name {
				if err := c.Reconnect(m.ctx); err != nil {
					return ErrorMsg{Err: fmt.Errorf("MCP '%s' reconnect failed: %w", name, err)}
				}
				return SystemMsg(fmt.Sprintf("✓ MCP '%s' reconnected", name))
			}
		}
		return ErrorMsg{Err: fmt.Errorf("MCP server not found: %s", name)}
	}
}

func (m AppModel) formatSessions() string {
	if m.sessionStore == nil {
		return "Session store is not initialized."
	}
	convs, err := m.sessionStore.ListConversations(m.ctx, 20)
	if err != nil {
		return fmt.Sprintf("Failed to load sessions: %s", err)
	}
	if len(convs) == 0 {
		return "No saved sessions."
	}

	var sb strings.Builder
	sb.WriteString("Recent sessions:\n")
	sb.WriteString(fmt.Sprintf("  %-4s %-40s %-20s\n", "No", "Title", "Updated"))
	sb.WriteString("  " + strings.Repeat("-", 68) + "\n")
	for i, c := range convs {
		title := c.Title
		if len(title) > 38 {
			title = title[:38] + ".."
		}
		sb.WriteString(fmt.Sprintf("  %-4d %-40s %-20s\n", i+1, title, c.UpdatedAt.Format("01/02 15:04")))
	}
	sb.WriteString("\nUse /sessions restore <No> to restore a session.")
	return sb.String()
}

func (m AppModel) restoreSessionCmd(n int) tea.Cmd {
	return func() tea.Msg {
		convs, err := m.sessionStore.ListConversations(m.ctx, 20)
		if err != nil || n < 1 || n > len(convs) {
			return ErrorMsg{Err: fmt.Errorf("invalid session number: %d", n)}
		}
		conv := convs[n-1]
		if err := m.ag.RestoreSession(m.ctx, conv.ID); err != nil {
			return ErrorMsg{Err: fmt.Errorf("restore session failed: %w", err)}
		}
		m.ag.SetSessionID(conv.ID)
		return SystemMsg(fmt.Sprintf("✓ Restored session '%s'", conv.Title))
	}
}

func (m AppModel) formatHistory() string {
	if m.execLogStore == nil {
		return "Execution log store is not initialized."
	}
	logs, err := m.execLogStore.ListExecutionLogs(m.ctx, 0, 20)
	if err != nil {
		return fmt.Sprintf("Failed to load execution logs: %s", err)
	}
	if len(logs) == 0 {
		return "No execution history."
	}

	var sb strings.Builder
	sb.WriteString("Recent tool execution logs:\n")
	for i, l := range logs {
		result := "✓"
		if !l.Success {
			result = "✗"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s %-20s [%s] %s  %s\n",
			i+1, result, l.ToolName, l.RiskLevel, l.TargetServer,
			l.Timestamp.Format(time.Kitchen)))
	}
	return sb.String()
}
