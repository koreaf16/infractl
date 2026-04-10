// Package cli
// File: commands.go
// Description: REPL 슬래시 명령 헬퍼 함수 및 출력 유틸리티
// Responsibility: /connectors, /mcp, /sessions, /history 등 명령 출력 로직

package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/config"
)

func printBanner(cfg *config.Config) {
	fmt.Printf("\033[1m● infractl v%s\033[0m | %s | localhost\n\n", version, cfg.GeneralLLM().Model)
	fmt.Println("자연어로 인프라를 제어하세요. /help로 명령 목록 확인.")
	fmt.Println()
}

// printModelInfo는 등록된 LLM 티어, 임베딩, 리랭커 정보를 출력한다.
func printModelInfo(cfg *config.Config) {
	fmt.Println("LLM Tiers:")
	if cfg.Models.Reasoning != nil {
		fmt.Printf("  reasoning : %s (%s)\n", cfg.Models.Reasoning.Model, hostOnly(cfg.Models.Reasoning.Endpoint))
	}
	generalCfg := cfg.GeneralLLM()
	fmt.Printf("  general   : %s (%s)  [active]\n", generalCfg.Model, hostOnly(generalCfg.Endpoint))
	if cfg.Models.Fast != nil {
		fmt.Printf("  fast      : %s (%s)\n", cfg.Models.Fast.Model, hostOnly(cfg.Models.Fast.Endpoint))
	}

	if cfg.Embedding.Model != "" {
		fmt.Printf("Embedding  : %s (%s, %s)\n", cfg.Embedding.Model, cfg.Embedding.Provider, hostOnly(cfg.Embedding.Endpoint))
	}
	if cfg.Reranker.Model != "" {
		fmt.Printf("Reranker   : %s (%s)\n", cfg.Reranker.Model, hostOnly(cfg.Reranker.Endpoint))
	} else {
		fmt.Println("Reranker   : (미설정)")
	}
}

// hostOnly는 URL에서 호스트:포트 부분만 추출한다.
func hostOnly(endpoint string) string {
	// "http://localhost:11434/v1" → "localhost:11434"
	s := strings.TrimPrefix(endpoint, "https://")
	s = strings.TrimPrefix(s, "http://")
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

func printHelp() {
	fmt.Println("사용 가능한 명령:")
	fmt.Println("  /help                 — 이 도움말 출력")
	fmt.Println("  /tools                — 등록된 도구 목록")
	fmt.Println("  /servers              — 등록된 서버 목록")
	fmt.Println("  /servers remove <이름> — 서버 삭제")
	fmt.Println("  /connectors           — 활성 커넥터 및 상태 목록")
	fmt.Println("  /mcp                  — MCP 서버 런타임 연결 상태")
	fmt.Println("  /mcp reconnect <이름>  — MCP 서버 수동 재연결")
	fmt.Println("  /sessions             — 최근 대화 세션 목록")
	fmt.Println("  /history              — 최근 도구 실행 이력")
	fmt.Println("  /knowledge            — 저장된 지식 목록")
	fmt.Println("  /knowledge search <쿼리> — 지식 베이스 검색")
	fmt.Println("  /knowledge delete <ID>   — 지식 항목 삭제")
	fmt.Println("  /rag                  — 등록된 RAG 소스 목록")
	fmt.Println("  /rag delete <ID>      — RAG 소스 삭제")
	fmt.Println("  /rag priority <ID> <N> — RAG 소스 우선순위 변경")
	fmt.Println("  /cost                 — 이번 달 LLM 비용 요약")
	fmt.Println("  /cost week            — 최근 7일 비용 요약")
	fmt.Println("  /cost detail          — 일별 상세 비용")
	fmt.Println("  /checkpoints          — 체크포인트 목록 (전체)")
	fmt.Println("  /checkpoints <서버>   — 서버별 체크포인트 목록")
	fmt.Println("  /hooks                — 등록된 훅 목록")
	fmt.Println("  /hooks enable <id>    — 훅 활성화")
	fmt.Println("  /hooks disable <id>   — 훅 비활성화")
	fmt.Println("  /hooks delete <id>    — 훅 삭제")
	fmt.Println("  /schedules            — 등록된 스케줄 목록")
	fmt.Println("  /schedules enable <id> — 스케줄 활성화")
	fmt.Println("  /schedules disable <id> — 스케줄 비활성화")
	fmt.Println("  /schedules delete <id> — 스케줄 삭제")
	fmt.Println("  /yoro                 — 확인 대화 없이 바로 실행 (YORO 모드 토글)")
	fmt.Println("  /auto-confirm         — 인터랙티브 프롬프트 자동 처리 방식 선택")
	fmt.Println("  /clear                — 대화 히스토리 초기화")
	fmt.Println("  /model                — 현재 LLM 모델 확인")
	fmt.Println("  /quit                 — 종료")
	fmt.Println()
	fmt.Println("서버 추가: '192.168.1.50 서버 추가해줘' 처럼 자연어로 요청하세요.")
	fmt.Println("서비스 탐지: 'db-server에 뭐 떠있어?' 처럼 자연어로 요청하세요.")
}

func printTools(toolList []toolInfo) {
	fmt.Println("등록된 도구:")
	for _, t := range toolList {
		fmt.Printf("  %-20s — %s\n", t.name, t.description)
	}
}

type toolInfo struct {
	name        string
	description string
}

func connectorStatusIcon(status string) string {
	switch status {
	case "connected":
		return "✓"
	case "connecting":
		return "⟳"
	case "error":
		return "✗"
	default:
		return " "
	}
}

// printConnectors는 활성 커넥터 목록과 상태를 출력한다.
func (r *REPL) printConnectors() {
	if r.connectorMgr == nil {
		fmt.Println("커넥터 매니저가 초기화되지 않았습니다.")
		return
	}
	states := r.connectorMgr.States()
	if len(states) == 0 {
		fmt.Println("활성 커넥터 없음. discover_services 도구를 사용하거나 LLM에게 서비스 탐색을 요청하세요.")
		return
	}
	fmt.Println("활성 커넥터:")
	for _, s := range states {
		icon := connectorStatusIcon(string(s.Status))
		fmt.Printf("  %s %-10s %-12s %-15s — %s (%d개 도구)\n",
			icon, s.Type, s.ServiceName, s.ServerName, s.Status, len(s.Tools))
	}
}

// printMCPStatus는 MCP 서버 런타임 연결 상태를 출력한다.
func (r *REPL) printMCPStatus() {
	if len(r.mcpClients) == 0 {
		if len(r.cfg.MCPServers) == 0 {
			fmt.Println("설정된 MCP 서버 없음. ~/.infractl/config.yaml의 mcp_servers 섹션에 추가하세요.")
		} else {
			fmt.Println("MCP 서버가 초기화되지 않았습니다.")
		}
		return
	}
	fmt.Printf("MCP 서버 연결 상태 (%d개):\n", len(r.mcpClients))
	for _, c := range r.mcpClients {
		icon := connectorStatusIcon(string(c.Status))
		fmt.Printf("  %s %-15s — %s\n", icon, c.Name, c.Status)
	}
}

// reconnectMCP는 이름으로 MCP 서버를 재연결한다.
func (r *REPL) reconnectMCP(ctx context.Context, name string) {
	for _, c := range r.mcpClients {
		if c.Name == name {
			fmt.Printf("MCP 서버 '%s' 재연결 중...\n", name)
			if err := c.Reconnect(ctx); err != nil {
				fmt.Printf("✗ 재연결 실패: %s\n", err)
			} else {
				fmt.Printf("✓ '%s' 재연결 완료\n", name)
			}
			return
		}
	}
	fmt.Printf("✗ MCP 서버를 찾을 수 없음: %s\n", name)
}

// printSessions는 최근 대화 세션 목록을 출력한다.
func (r *REPL) printSessions(ctx context.Context) {
	if r.sessionStore == nil {
		fmt.Println("세션 저장소가 초기화되지 않았습니다.")
		return
	}
	convs, err := r.sessionStore.ListConversations(ctx, 20)
	if err != nil {
		fmt.Printf("세션 목록 조회 실패: %s\n", err)
		return
	}
	if len(convs) == 0 {
		fmt.Println("저장된 세션 없음.")
		return
	}
	fmt.Println("최근 대화 세션:")
	fmt.Printf("  %-4s %-40s %-20s\n", "번호", "제목", "마지막 대화")
	fmt.Println("  " + strings.Repeat("-", 68))
	for i, c := range convs {
		title := c.Title
		if len(title) > 38 {
			title = title[:38] + ".."
		}
		fmt.Printf("  %-4d %-40s %-20s\n", i+1, title, c.UpdatedAt.Format("01/02 15:04"))
	}
	fmt.Println()
	fmt.Println("세션 복원: '3번 이어서 해줘' 또는 '/sessions restore 3' 으로 요청하세요.")
}

// printHistory는 최근 도구 실행 이력을 출력한다.
func (r *REPL) printHistory(ctx context.Context) {
	if r.execLogStore == nil {
		fmt.Println("실행 이력 저장소가 초기화되지 않았습니다.")
		return
	}
	logs, err := r.execLogStore.ListExecutionLogs(ctx, 0, 20)
	if err != nil {
		fmt.Printf("실행 이력 조회 실패: %s\n", err)
		return
	}
	if len(logs) == 0 {
		fmt.Println("저장된 실행 이력 없음.")
		return
	}
	fmt.Println("최근 도구 실행 이력:")
	fmt.Printf("  %-4s %-20s %-12s %-8s %-6s\n", "번호", "도구명", "서버", "위험도", "결과")
	fmt.Println("  " + strings.Repeat("-", 54))
	for i, l := range logs {
		result := "✓"
		if !l.Success {
			result = "✗"
		}
		ts := l.Timestamp.Format(time.Kitchen)
		fmt.Printf("  %-4d %-20s %-12s %-8s %s  %s\n",
			i+1, l.ToolName, l.TargetServer, l.RiskLevel, result, ts)
	}
}

// handleServersCommand는 /servers 서브커맨드를 처리한다.
func (r *REPL) handleServersCommand(ctx context.Context, parts []string) {
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "":
		r.printServers(ctx)
	case "remove":
		if len(parts) < 3 {
			fmt.Println("사용법: /servers remove <서버명>")
			return
		}
		name := parts[2]
		if err := r.manager.Remove(name); err != nil {
			fmt.Printf("매니저에서 제거 실패 (무시): %s\n", err)
		}
		if err := r.store.Remove(ctx, name); err != nil {
			fmt.Printf("✗ 서버 삭제 실패: %s\n", err)
			return
		}
		fmt.Printf("✓ 서버 '%s'를 삭제했습니다.\n", name)
	default:
		fmt.Printf("알 수 없는 /servers 서브커맨드: %s\n사용법: /servers | /servers remove <이름>\n", sub)
	}
}

// printServers는 등록된 서버 목록을 출력한다.
func (r *REPL) printServers(ctx context.Context) {
	servers, err := r.store.List(ctx)
	if err != nil {
		fmt.Printf("서버 목록 조회 실패: %s\n", err)
		return
	}

	fmt.Printf("  %-15s %-20s %-6s %-12s %-10s\n", "NAME", "HOST", "PORT", "USER", "AUTH")
	fmt.Println("  " + strings.Repeat("-", 65))
	fmt.Printf("  %-15s %-20s %-6s %-12s %-10s\n", "localhost", "(this machine)", "-", "-", "-")
	for _, srv := range servers {
		fmt.Printf("  %-15s %-20s %-6d %-12s %-10s\n",
			srv.Name, srv.Host, srv.Port, srv.User, srv.AuthType)
	}
}

// restoreSession은 번호로 세션을 복원한다.
func (r *REPL) restoreSession(ctx context.Context, n int) {
	if r.sessionStore == nil {
		fmt.Println("세션 저장소가 초기화되지 않았습니다.")
		return
	}
	convs, err := r.sessionStore.ListConversations(ctx, 20)
	if err != nil || n < 1 || n > len(convs) {
		fmt.Printf("유효하지 않은 세션 번호: %d\n", n)
		return
	}
	conv := convs[n-1]
	if err := r.agent.RestoreSession(ctx, conv.ID); err != nil {
		fmt.Printf("✗ 세션 복원 실패: %s\n", err)
		return
	}
	r.agent.SetSessionID(conv.ID)
	fmt.Printf("✓ 세션 '%s' 복원 완료 (%d개 메시지)\n",
		conv.Title, len(r.agent.GetHistory()))
}

