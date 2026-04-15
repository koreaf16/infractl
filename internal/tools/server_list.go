// Package tools
// File: server_list.go
// Description: 등록된 SSH 서버 목록 조회 도구 구현
// Responsibility: SQLite에 저장된 서버 목록을 테이블 형식으로 반환

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

// ServerListTool은 등록된 서버 목록을 반환하는 도구이다.
type ServerListTool struct {
	Store store.ServerStore
}

func (t *ServerListTool) Name() string { return "server_list" }

func (t *ServerListTool) Description() string {
	return "List all registered SSH servers with their connection details"
}

func (t *ServerListTool) IsReadOnly() bool { return true }
func (t *ServerListTool) IsEnabled() bool  { return true }

func (t *ServerListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ServerListTool) Execute(ctx context.Context, _ map[string]interface{}, _ executor.Executor) (string, error) {
	servers, err := t.Store.List(ctx)
	if err != nil {
		return fmt.Sprintf("Failed to list servers: %s", err), nil
	}

	if len(servers) == 0 {
		return "No servers registered. Use server_add to register a server.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-15s %-20s %-6s %-12s %-10s\n",
		"NAME", "HOST", "PORT", "USER", "AUTH"))
	sb.WriteString(strings.Repeat("-", 65) + "\n")

	for _, srv := range servers {
		sb.WriteString(fmt.Sprintf("%-15s %-20s %-6d %-12s %-10s\n",
			srv.Name, srv.Host, srv.Port, srv.User, srv.AuthType))
	}

	return sb.String(), nil
}
