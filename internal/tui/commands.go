// Package tui
// File: commands.go
// Description: slash command handling for TUI mode
// Responsibility: implement /servers, /server, /sessions, /history, /mcp and helpers

package tui

import (
	"context"
	"fmt"
	"sort"
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
		if len(parts) >= 2 && parts[1] == "remove" {
			if len(parts) < 3 {
				printErr("사용법: /servers remove <서버명>")
				return m, nil
			}
			return m, m.removeServerCmd(parts[2])
		}
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
	case "/services":
		serverFilter := ""
		if len(parts) >= 2 {
			serverFilter = strings.TrimSpace(parts[1])
		}
		println(renderSystemLine(m.buildServicesView(context.Background(), serverFilter)))
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
	case "/knowledge":
		println(renderSystemLine(m.handleKnowledge(parts)))
	case "/rag":
		msg, cmd := m.handleRAG(parts)
		println(renderSystemLine(msg))
		if cmd != nil {
			return m, cmd
		}
	case "/cost":
		println(renderSystemLine(m.handleCost(parts)))
	case "/checkpoints":
		println(renderSystemLine(m.handleCheckpoints(parts)))
	case "/hooks":
		msg, cmd := m.handleHooks(parts)
		println(renderSystemLine(msg))
		if cmd != nil {
			return m, cmd
		}
	case "/schedules":
		msg, cmd := m.handleSchedules(parts)
		println(renderSystemLine(msg))
		if cmd != nil {
			return m, cmd
		}
	case "/ospermission", "/osessions":
		println(renderSystemLine(m.formatOSPermissions()))
	case "/yoro":
		active := m.ag.ToggleYOROMode()
		if active {
			println(renderSystemLine("YORO 모드 활성화 — 확인 없이 바로 실행합니다."))
		} else {
			println(renderSystemLine("YORO 모드 비활성화 — 위험 작업 시 확인합니다."))
		}
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
		"  /help                        - 이 도움말\n" +
		"  /tools                       - 도구 목록\n" +
		"  /servers                     - 서버 목록\n" +
		"  /servers remove <이름>        - 서버 삭제\n" +
		"  /server [name|clear]         - 활성 서버 설정\n" +
		"  /services [서버명]            - 서버→서비스→서브인스턴스 3계층 목록\n" +
		"  /connectors                  - 현재 활성 커넥터 상태\n" +
		"  /mcp                         - MCP 서버 상태\n" +
		"  /mcp reconnect <name>        - MCP 재연결\n" +
		"  /sessions                    - 최근 세션 목록\n" +
		"  /sessions restore <N>        - 세션 복원\n" +
		"  /history                     - 도구 실행 이력\n" +
		"  /knowledge                   - 지식 목록\n" +
		"  /knowledge search <쿼리>      - 지식 검색\n" +
		"  /knowledge delete <ID>       - 지식 삭제\n" +
		"  /rag                         - RAG 소스 목록\n" +
		"  /rag delete <ID>             - RAG 소스 삭제\n" +
		"  /rag priority <ID> <N>       - RAG 우선순위 변경\n" +
		"  /cost                        - 이번 달 비용 요약\n" +
		"  /cost week                   - 최근 7일 비용\n" +
		"  /cost detail                 - 일별 상세 비용\n" +
		"  /checkpoints [서버]           - 체크포인트 목록\n" +
		"  /hooks                       - 훅 목록\n" +
		"  /hooks enable/disable/delete <id>\n" +
		"  /schedules                   - 스케줄 목록\n" +
		"  /schedules enable/disable/delete <id>\n" +
		"  /osessions                   - 취득된 OS 세션 및 권한 캐시 조회\n" +
		"  /yoro                        - YORO 모드 토글\n" +
		"  /clear                       - 대화 히스토리 초기화\n" +
		"  /model                       - 현재 모델 확인\n" +
		"  /quit                        - 종료"
}

func connectorStatusIcon(status string) string {
	switch status {
	case "connected":
		return "+"
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
				return SystemMsg(fmt.Sprintf("MCP '%s' reconnected", name))
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
		return SystemMsg(fmt.Sprintf("Restored session '%s'", conv.Title))
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
		result := "+"
		if !l.Success {
			result = "✗"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s %-20s %s  %s\n",
			i+1, result, l.ToolName, l.TargetServer,
			l.Timestamp.Format(time.Kitchen)))
	}
	return sb.String()
}

// removeServerCmd는 서버를 삭제하는 Cmd를 반환한다.
func (m AppModel) removeServerCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if err := m.manager.Remove(name); err != nil {
			return SystemMsg(fmt.Sprintf("매니저에서 제거 실패 (무시): %s", err))
		}
		if err := m.store.Remove(context.Background(), name); err != nil {
			return ErrorMsg{Err: fmt.Errorf("서버 삭제 실패: %w", err)}
		}
		return SystemMsg(fmt.Sprintf("서버 '%s' 삭제 완료", name))
	}
}

// handleKnowledge는 /knowledge 명령을 처리하고 출력 문자열을 반환한다.
func (m AppModel) handleKnowledge(parts []string) string {
	if m.knowledgeStore == nil {
		return "지식 저장소가 초기화되지 않았습니다."
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	switch sub {
	case "search":
		if len(parts) < 3 {
			return "사용법: /knowledge search <검색어>"
		}
		query := strings.Join(parts[2:], " ")
		entries, err := m.knowledgeStore.SearchKnowledge(m.ctx, query, 10)
		if err != nil {
			return fmt.Sprintf("검색 실패: %s", err)
		}
		if len(entries) == 0 {
			return fmt.Sprintf("'%s' 검색 결과 없음", query)
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("'%s' 검색 결과 (%d개):\n\n", query, len(entries)))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("  [#%d] %s (%s)\n", e.ID, e.Title, e.Category))
			if e.Situation != "" {
				sb.WriteString(fmt.Sprintf("  상황: %s\n", e.Situation))
			}
			sb.WriteString(fmt.Sprintf("  해결: %s\n\n", e.Resolution))
		}
		return sb.String()
	case "delete":
		if len(parts) < 3 {
			return "사용법: /knowledge delete <ID>"
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Sprintf("유효하지 않은 ID: %s", parts[2])
		}
		if err := m.knowledgeStore.DeleteKnowledge(m.ctx, id); err != nil {
			return fmt.Sprintf("삭제 실패: %s", err)
		}
		return fmt.Sprintf("지식 항목 #%d 삭제 완료", id)
	default:
		entries, err := m.knowledgeStore.ListKnowledge(m.ctx, "", 20)
		if err != nil {
			return fmt.Sprintf("지식 목록 조회 실패: %s", err)
		}
		if len(entries) == 0 {
			return "저장된 지식 없음."
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("저장된 지식 (%d개):\n", len(entries)))
		sb.WriteString(fmt.Sprintf("  %-6s %-16s %-35s %-8s\n", "ID", "카테고리", "제목", "사용횟수"))
		sb.WriteString("  " + strings.Repeat("-", 70) + "\n")
		for _, e := range entries {
			title := e.Title
			if len(title) > 33 {
				title = title[:33] + ".."
			}
			sb.WriteString(fmt.Sprintf("  %-6d %-16s %-35s %d회\n", e.ID, e.Category, title, e.UseCount))
		}
		sb.WriteString("\n삭제: /knowledge delete <ID> | 검색: /knowledge search <키워드>")
		return sb.String()
	}
}

// handleRAG는 /rag 명령을 처리하고 출력 문자열과 선택적 Cmd를 반환한다.
func (m AppModel) handleRAG(parts []string) (string, tea.Cmd) {
	if m.ragSourceStore == nil {
		return "RAG 소스 저장소가 초기화되지 않았습니다.", nil
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	switch sub {
	case "delete":
		if len(parts) < 3 {
			return "사용법: /rag delete <ID>", nil
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Sprintf("잘못된 ID: %s", parts[2]), nil
		}
		return "", func() tea.Msg {
			if err := m.ragSourceStore.DeleteRAGSource(m.ctx, id); err != nil {
				return ErrorMsg{Err: fmt.Errorf("RAG 소스 삭제 실패: %w", err)}
			}
			return SystemMsg(fmt.Sprintf("RAG 소스 %d 삭제 완료", id))
		}
	case "priority":
		if len(parts) < 4 {
			return "사용법: /rag priority <ID> <우선순위>", nil
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Sprintf("잘못된 ID: %s", parts[2]), nil
		}
		priority, err := strconv.Atoi(parts[3])
		if err != nil || priority < 1 {
			return fmt.Sprintf("잘못된 우선순위: %s (1 이상)", parts[3]), nil
		}
		return "", func() tea.Msg {
			if err := m.ragSourceStore.UpdateRAGSourcePriority(m.ctx, id, priority); err != nil {
				return ErrorMsg{Err: fmt.Errorf("우선순위 변경 실패: %w", err)}
			}
			return SystemMsg(fmt.Sprintf("RAG 소스 %d 우선순위 → %d", id, priority))
		}
	default:
		sources, err := m.ragSourceStore.ListRAGSources(m.ctx)
		if err != nil {
			return fmt.Sprintf("RAG 소스 조회 실패: %s", err), nil
		}
		if len(sources) == 0 {
			return "등록된 RAG 소스 없음.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("RAG 소스 목록 (%d건):\n", len(sources)))
		sb.WriteString(fmt.Sprintf("  %-4s %-20s %-10s %-15s %-4s\n", "ID", "이름", "DB타입", "서버", "우선순위"))
		sb.WriteString("  " + strings.Repeat("-", 58) + "\n")
		for _, src := range sources {
			sb.WriteString(fmt.Sprintf("  %-4d %-20s %-10s %-15s %d\n",
				src.ID, src.Name, src.DBType, src.ServerName, src.Priority))
		}
		sb.WriteString("\n명령어: /rag delete <ID> | /rag priority <ID> <숫자>")
		return sb.String(), nil
	}
}

// handleCost는 /cost 명령을 처리하고 출력 문자열을 반환한다.
func (m AppModel) handleCost(parts []string) string {
	if m.costTracker == nil {
		return "비용 추적이 설정되지 않았습니다."
	}
	sub := ""
	if len(parts) >= 2 {
		sub = parts[1]
	}
	switch sub {
	case "week":
		return m.formatCostSummary(7, "최근 7일")
	case "detail":
		return m.formatCostDetail(30)
	default:
		return m.formatCostSummary(30, "이번 달 (30일)")
	}
}

func (m AppModel) formatCostSummary(days int, label string) string {
	s, err := m.costTracker.Summary(m.ctx, days)
	if err != nil {
		return fmt.Sprintf("비용 조회 실패: %s", err)
	}
	return fmt.Sprintf("비용 요약 — %s\n  총 입력 토큰: %s\n  총 출력 토큰: %s\n  총 호출 횟수: %d회\n  예상 비용:    $%.4f",
		label, formatTokens(s.TotalInputTokens), formatTokens(s.TotalOutputTokens), s.CallCount, s.TotalCost)
}

func (m AppModel) formatCostDetail(days int) string {
	daily, err := m.costTracker.DailyCosts(m.ctx, days)
	if err != nil {
		return fmt.Sprintf("일별 비용 조회 실패: %s", err)
	}
	if len(daily) == 0 {
		return "기록된 사용량이 없습니다."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("일별 비용 상세 — 최근 %d일\n", days))
	sb.WriteString(fmt.Sprintf("  %-12s  %12s  %12s  %8s  %8s\n", "날짜", "입력 토큰", "출력 토큰", "호출수", "비용($)"))
	sb.WriteString("  " + strings.Repeat("-", 60) + "\n")
	for _, d := range daily {
		sb.WriteString(fmt.Sprintf("  %-12s  %12s  %12s  %8d  %8.4f\n",
			d.Date, formatTokens(d.InputTokens), formatTokens(d.OutputTokens), d.CallCount, d.EstimatedCost))
	}
	return sb.String()
}


// handleCheckpoints는 /checkpoints 명령을 처리하고 출력 문자열을 반환한다.
func (m AppModel) handleCheckpoints(parts []string) string {
	if m.checkpointMgr == nil {
		return "체크포인트 매니저가 초기화되지 않았습니다."
	}
	server := ""
	if len(parts) >= 2 {
		server = parts[1]
	}
	cps, err := m.checkpointMgr.List(m.ctx, server, 20)
	if err != nil {
		return fmt.Sprintf("체크포인트 목록 조회 실패: %s", err)
	}
	if len(cps) == 0 {
		if server != "" {
			return fmt.Sprintf("서버 '%s'의 체크포인트 없음.", server)
		}
		return "저장된 체크포인트 없음."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("체크포인트 목록 (최근 %d개):\n", len(cps)))
	sb.WriteString(fmt.Sprintf("  %-4s %-12s %-20s %s\n", "ID", "서버", "시각", "설명"))
	sb.WriteString("  " + strings.Repeat("-", 60) + "\n")
	for _, cp := range cps {
		ts := cp.CreatedAt.Format("01/02 15:04:05")
		desc := cp.Description
		if len(desc) > 30 {
			desc = desc[:28] + ".."
		}
		rollback := ""
		if cp.Snapshot.RollbackCommand != "" {
			rollback = " [롤백 가능]"
		}
		sb.WriteString(fmt.Sprintf("  %-4d %-12s %-20s %s%s\n",
			cp.ID, cp.Server, ts, desc, rollback))
	}
	sb.WriteString("\n롤백: LLM에게 '체크포인트 #N으로 롤백해줘' 처럼 요청하세요.")
	return sb.String()
}

// handleHooks는 /hooks 명령을 처리하고 출력 문자열과 선택적 Cmd를 반환한다.
func (m AppModel) handleHooks(parts []string) (string, tea.Cmd) {
	if m.hooksMgr == nil {
		return "훅 매니저가 초기화되지 않았습니다.", nil
	}
	if len(parts) >= 3 {
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Sprintf("유효하지 않은 훅 ID: %s", parts[2]), nil
		}
		switch parts[1] {
		case "enable":
			return "", func() tea.Msg {
				if err := m.hooksMgr.SetEnabled(m.ctx, id, true); err != nil {
					return ErrorMsg{Err: fmt.Errorf("훅 활성화 실패: %w", err)}
				}
				return SystemMsg(fmt.Sprintf("훅 #%d 활성화 완료", id))
			}
		case "disable":
			return "", func() tea.Msg {
				if err := m.hooksMgr.SetEnabled(m.ctx, id, false); err != nil {
					return ErrorMsg{Err: fmt.Errorf("훅 비활성화 실패: %w", err)}
				}
				return SystemMsg(fmt.Sprintf("훅 #%d 비활성화 완료", id))
			}
		case "delete":
			return "", func() tea.Msg {
				if err := m.hooksMgr.Delete(m.ctx, id); err != nil {
					return ErrorMsg{Err: fmt.Errorf("훅 삭제 실패: %w", err)}
				}
				return SystemMsg(fmt.Sprintf("훅 #%d 삭제 완료", id))
			}
		default:
			return fmt.Sprintf("알 수 없는 /hooks 서브커맨드: %s", parts[1]), nil
		}
	}
	hooks, err := m.hooksMgr.List(m.ctx)
	if err != nil {
		return fmt.Sprintf("훅 목록 조회 실패: %s", err), nil
	}
	if len(hooks) == 0 {
		return "등록된 훅 없음.", nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("등록된 훅 (%d개):\n", len(hooks)))
	sb.WriteString(fmt.Sprintf("  %-4s %-18s %-6s %s\n", "ID", "이벤트", "활성", "스크립트"))
	sb.WriteString("  " + strings.Repeat("-", 60) + "\n")
	for _, h := range hooks {
		enabled := "+"
		if !h.Enabled {
			enabled = "✗"
		}
		script := h.ScriptPath
		if len(script) > 35 {
			script = "..." + script[len(script)-32:]
		}
		sb.WriteString(fmt.Sprintf("  %-4d %-18s %-6s %s\n", h.ID, h.Event, enabled, script))
	}
	sb.WriteString("\n비활성화: /hooks disable <id> | 활성화: /hooks enable <id> | 삭제: /hooks delete <id>")
	return sb.String(), nil
}

// handleSchedules는 /schedules 명령을 처리하고 출력 문자열과 선택적 Cmd를 반환한다.
func (m AppModel) handleSchedules(parts []string) (string, tea.Cmd) {
	if m.scheduleMgr == nil {
		return "스케줄 매니저가 초기화되지 않았습니다.", nil
	}
	if len(parts) >= 3 {
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Sprintf("유효하지 않은 스케줄 ID: %s", parts[2]), nil
		}
		switch parts[1] {
		case "enable":
			return "", func() tea.Msg {
				if err := m.scheduleMgr.SetEnabled(m.ctx, id, true); err != nil {
					return ErrorMsg{Err: fmt.Errorf("스케줄 활성화 실패: %w", err)}
				}
				return SystemMsg(fmt.Sprintf("스케줄 #%d 활성화 완료", id))
			}
		case "disable":
			return "", func() tea.Msg {
				if err := m.scheduleMgr.SetEnabled(m.ctx, id, false); err != nil {
					return ErrorMsg{Err: fmt.Errorf("스케줄 비활성화 실패: %w", err)}
				}
				return SystemMsg(fmt.Sprintf("스케줄 #%d 비활성화 완료", id))
			}
		case "delete":
			return "", func() tea.Msg {
				if err := m.scheduleMgr.Delete(m.ctx, id); err != nil {
					return ErrorMsg{Err: fmt.Errorf("스케줄 삭제 실패: %w", err)}
				}
				return SystemMsg(fmt.Sprintf("스케줄 #%d 삭제 완료", id))
			}
		default:
			return fmt.Sprintf("알 수 없는 /schedules 서브커맨드: %s", parts[1]), nil
		}
	}
	schedules, err := m.scheduleMgr.List(m.ctx)
	if err != nil {
		return fmt.Sprintf("스케줄 목록 조회 실패: %s", err), nil
	}
	if len(schedules) == 0 {
		return "등록된 스케줄 없음.", nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("등록된 스케줄 (%d개):\n", len(schedules)))
	sb.WriteString(fmt.Sprintf("  %-4s %-6s %-20s %-15s %s\n", "ID", "활성", "이름", "cron", "마지막 실행"))
	sb.WriteString("  " + strings.Repeat("-", 65) + "\n")
	for _, s := range schedules {
		enabled := "+"
		if !s.Enabled {
			enabled = "✗"
		}
		lastRun := "없음"
		if s.LastRun != nil {
			lastRun = s.LastRun.Format("01/02 15:04")
		}
		name := s.Name
		if len(name) > 18 {
			name = name[:16] + ".."
		}
		sb.WriteString(fmt.Sprintf("  %-4d %-6s %-20s %-15s %s\n", s.ID, enabled, name, s.CronExpr, lastRun))
	}
	return sb.String(), nil
}

// formatOSPermissions는 취득된 OS 권한(세션/캐시) 목록을 반환한다.
func (m AppModel) formatOSPermissions() string {
	var sb strings.Builder
	sb.WriteString("취득된 OS 권한 목록:\n\n")

	// 활성 persistent shell 세션
	var hasSessions bool
	if m.manager != nil {
		sessions := m.manager.ListPersistentSessions()
		serverNames := make([]string, 0, len(sessions))
		for name := range sessions {
			serverNames = append(serverNames, name)
		}
		sort.Strings(serverNames)
		for _, serverName := range serverNames {
			for _, si := range sessions[serverName] {
				hasSessions = true
				alive := "alive"
				if !si.Alive {
					alive = "dead"
				}
				sb.WriteString(fmt.Sprintf("  [세션] %-15s  session=%-10s  user=%-10s  dir=%s  (%s)\n",
					serverName, si.SessionID, si.CurrentUser, si.CurrentDir, alive))
			}
		}
	}
	if !hasSessions {
		sb.WriteString("  (활성 persistent 세션 없음)\n")
	}
	sb.WriteString("\n")

	// privilege 캐시
	var hasCache bool
	if m.privCache != nil {
		keys := m.privCache.List()
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Target != keys[j].Target {
				return keys[i].Target < keys[j].Target
			}
			return keys[i].User < keys[j].User
		})
		for _, k := range keys {
			hasCache = true
			sb.WriteString(fmt.Sprintf("  [캐시] target=%-15s  method=%-6s  user=%s\n",
				k.Target, k.Method, k.User))
		}
	}
	if !hasCache {
		sb.WriteString("  (캐시된 크리덴셜 없음)\n")
	}
	return sb.String()
}
