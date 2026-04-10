// Package agent
// File: compaction_restore.go
// Description: compaction 완료 후 활성 서버/커넥터 상태를 히스토리에 재주입
// Responsibility: compaction으로 손실될 수 있는 운영 컨텍스트(activeServer, 커넥터)를 즉시 복원

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/llm"
)

// injectPostCompactContext는 compaction 완료 직후 활성 서버와 커넥터 상태를
// 히스토리에 user/assistant 쌍으로 재주입한다.
//
// 주입 조건: activeServer가 설정되어 있거나 활성 커넥터가 1개 이상 존재할 때.
// 시스템 프롬프트에도 이 정보가 포함되지만, 히스토리에 명시적으로 존재하면
// LLM이 현재 컨텍스트를 더 확실하게 인지한다.
func (a *Agent) injectPostCompactContext(_ context.Context) {
	var parts []string

	if a.activeServer != nil {
		parts = append(parts, fmt.Sprintf(
			"[Active Server] %s (%s@%s:%d) — OS: %s",
			a.activeServer.Name,
			a.activeServer.User,
			a.activeServer.Host,
			a.activeServer.Port,
			a.activeServer.OS,
		))
		if a.activeServer.EnvProfile != "" {
			parts = append(parts, "  Environment: "+a.activeServer.EnvProfile)
		}
	}

	if a.connectorMgr != nil {
		var activeConns []string
		for _, cs := range a.connectorMgr.States() {
			if cs.Status == connector.StatusConnected {
				activeConns = append(activeConns, fmt.Sprintf(
					"%s/%s/%s (%d tools)",
					cs.ServerName, cs.Type, cs.ServiceName, len(cs.Tools),
				))
			}
		}
		if len(activeConns) > 0 {
			parts = append(parts, "[Active Connectors]")
			for _, c := range activeConns {
				parts = append(parts, "  - "+c)
			}
		}
	}

	if len(parts) == 0 {
		return
	}

	note := "[Context restored after compaction]\n" + strings.Join(parts, "\n")

	a.history = append(a.history,
		llm.Message{Role: llm.RoleUser, Content: note},
		llm.Message{Role: llm.RoleAssistant, Content: "Context noted. Active server and connector state restored."},
	)

	serverName := ""
	if a.activeServer != nil {
		serverName = a.activeServer.Name
	}
	slog.Info("post-compact context injected", "active_server", serverName)
}
