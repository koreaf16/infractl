// Package tools
// File: server_focus_tool.go
// Description: server_focus 도구 — 현재 작업할 서버를 지정하고 활성 컨텍스트를 설정
// Responsibility: 활성 서버 전환 요청을 받아 OnChange 콜백과 선택 UI를 통해 처리

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

// FocusSelectOption은 server_focus 선택 UI의 선택지 하나를 나타낸다.
// tui 패키지 의존성 없이 독립적으로 정의한다.
type FocusSelectOption struct {
	Label       string
	Description string
	Preview     string
}

// ServerFocusTool은 현재 작업할 서버를 전환하는 도구이다.
// OnChange가 설정되면 선택된 서버를 에이전트와 TUI에 통보한다.
// SelectFn이 설정되면 서버가 여러 개일 때 인터랙티브 선택 UI를 표시한다.
type ServerFocusTool struct {
	Store    store.ServerStore
	OnChange func(srv *store.Server)                                      // nil=클리어
	SelectFn func(question string, opts []FocusSelectOption) (int, error) // optional: 선택 UI
}

var _ Tool = (*ServerFocusTool)(nil)

func (t *ServerFocusTool) Name() string { return "server_focus" }

func (t *ServerFocusTool) Description() string {
	return "현재 작업할 서버를 설정합니다. " +
		"server 파라미터를 지정하면 해당 서버가 활성 서버가 됩니다. " +
		"server를 생략하면 등록된 서버 목록에서 선택할 수 있습니다. " +
		"활성 서버가 설정되면 이후 모든 명령의 기본 대상이 됩니다. " +
		"사용자가 특정 서버에서 작업하겠다고 하거나, 어느 서버에서 실행할지 불명확할 때 호출하세요."
}

func (t *ServerFocusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"server": map[string]interface{}{
				"type":        "string",
				"description": "활성화할 서버 이름. 생략하면 등록된 서버 목록에서 선택합니다.",
			},
		},
		"required": []string{},
	}
}

func (t *ServerFocusTool) IsReadOnly() bool { return true }
func (t *ServerFocusTool) IsEnabled() bool      { return true }

// Execute는 활성 서버를 설정한다.
func (t *ServerFocusTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	serverName, _ := args["server"].(string)

	servers, err := t.Store.List(ctx)
	if err != nil {
		return fmt.Sprintf("서버 목록 조회 실패: %s", err), nil
	}

	// server 인자가 지정된 경우: 직접 설정
	if serverName != "" {
		switch strings.ToLower(strings.TrimSpace(serverName)) {
		case "clear", "none", "localhost", "local":
			if t.OnChange != nil {
				t.OnChange(nil)
			}
			return "활성 서버를 해제했습니다. 기본 target은 localhost입니다.", nil
		}
		for _, s := range servers {
			if strings.EqualFold(s.Name, serverName) {
				srv := s
				if t.OnChange != nil {
					t.OnChange(&srv)
				}
				return fmt.Sprintf("✓ 활성 서버: %s (%s:%d)", s.Name, s.Host, s.Port), nil
			}
		}
		return fmt.Sprintf("서버 '%s'를 찾을 수 없습니다. server_list로 등록된 서버를 확인하세요.", serverName), nil
	}

	// server 인자 없음: 서버 선택 처리
	if len(servers) == 0 {
		return "등록된 서버가 없습니다. server_add로 서버를 먼저 등록하세요.", nil
	}

	if len(servers) == 1 {
		// 서버가 1개면 자동 선택
		srv := servers[0]
		if t.OnChange != nil {
			t.OnChange(&srv)
		}
		return fmt.Sprintf("✓ 활성 서버: %s (%s:%d)", srv.Name, srv.Host, srv.Port), nil
	}

	// 서버가 여러 개: 선택 UI 표시
	if t.SelectFn == nil {
		// SelectFn 없으면 텍스트로 목록 안내
		var list string
		for _, s := range servers {
			list += fmt.Sprintf("  - %s (%s:%d)\n", s.Name, s.Host, s.Port)
		}
		return fmt.Sprintf("등록된 서버가 여러 개입니다. server 파라미터를 지정하세요:\n%s", list), nil
	}

	opts := make([]FocusSelectOption, len(servers))
	for i, s := range servers {
		desc := fmt.Sprintf("%s@%s:%d", s.User, s.Host, s.Port)
		preview := fmt.Sprintf("Host: %s\nPort: %d\nUser: %s", s.Host, s.Port, s.User)
		if s.OS != "" {
			preview += "\nOS: " + s.OS
		}
		if s.EnvProfile != "" {
			preview += "\nEnv: " + s.EnvProfile
		}
		opts[i] = FocusSelectOption{
			Label:       s.Name,
			Description: desc,
			Preview:     preview,
		}
	}

	idx, err := t.SelectFn("작업할 서버를 선택하세요", opts)
	if err != nil || idx < 0 {
		return "취소됨", nil
	}
	if idx >= len(servers) {
		return "잘못된 선택입니다", nil
	}

	srv := servers[idx]
	if t.OnChange != nil {
		t.OnChange(&srv)
	}
	return fmt.Sprintf("✓ 활성 서버: %s (%s:%d)", srv.Name, srv.Host, srv.Port), nil
}
