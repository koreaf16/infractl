// Package connector
// File: activate_tool.go
// Description: connector_activate tool
// Responsibility: validate target/discovery alignment and activate connector

package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

// ActivateTool activates a connector from discovered service info and credentials.
type ActivateTool struct {
	Manager        *Manager
	ServerStore    store.ServerStore
	DiscoveryStore store.DiscoveryStore
	Disambiguate   DisambiguateHandler // optional UI adapter
}

var _ tools.Tool = (*ActivateTool)(nil)

func (t *ActivateTool) Name() string { return "connector_activate" }

func (t *ActivateTool) Description() string {
	return "서비스 커넥터를 활성화합니다. " +
		"먼저 connector_probe_os_auth를 호출해 OS 인증 가능 여부를 확인하세요. " +
		"OS 인증이 실패한 경우에만 username/password로 활성화하세요. " +
		"성공 시 Oracle/MySQL/PostgreSQL/Tomcat/WebLogic 전용 도구가 등록됩니다."
}

func (t *ActivateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"server": map[string]interface{}{
				"type":        "string",
				"description": "서버 alias 이름 (server_list에서 확인)",
			},
			"service_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"oracle", "mysql", "postgresql", "tomcat", "weblogic"},
				"description": "서비스 타입",
			},
			"service_name": map[string]interface{}{
				"type":        "string",
				"description": "서비스 이름 (discover_services 결과와 일치해야 함)",
			},
			"username": map[string]interface{}{
				"type":        "string",
				"description": "접속 계정",
			},
			"password": map[string]interface{}{
				"type":        "string",
				"description": "접속 비밀번호",
			},
			"role": map[string]interface{}{
				"type":        "string",
				"description": "Oracle role (예: sysdba)",
			},
			"port": map[string]interface{}{
				"type":        "integer",
				"description": "서비스 포트. 생략 시 discovery 포트를 사용",
			},
			"save_mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"permanent", "session", "none"},
				"description": "저장 방식 (기본: session)",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "실행 target. server와 동일해야 함",
			},
		},
		"required": []string{"server", "service_type", "service_name"},
	}
}

func (t *ActivateTool) RiskLevel() tools.RiskLevel { return tools.RiskLow }
func (t *ActivateTool) IsReadOnly() bool           { return false }
func (t *ActivateTool) IsEnabled() bool            { return true }

// Execute validates request consistency, hydrates discovery data, and activates connector.
func (t *ActivateTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	server, _ := args["server"].(string)
	serviceType, _ := args["service_type"].(string)
	serviceName, _ := args["service_name"].(string)
	username, _ := args["username"].(string)
	password, _ := args["password"].(string)

	server = strings.TrimSpace(server)
	serviceType = strings.ToLower(strings.TrimSpace(serviceType))
	serviceName = strings.TrimSpace(serviceName)

	if server == "" || serviceType == "" || serviceName == "" {
		return "server, service_type, service_name는 필수입니다", nil
	}

	if t.ServerStore != nil {
		resolved, _, _ := checkNameConflict(ctx, t.ServerStore, t.Disambiguate, server, serviceType, exec.Target())
		server = resolved
	}

	if err := validateDiscoveryTarget(exec.Target(), server); err != nil {
		return err.Error(), nil
	}

	entry, err := validateRecentDiscovery(ctx, t.DiscoveryStore, server, serviceType, serviceName)
	if err != nil {
		return err.Error(), nil
	}

	role, _ := args["role"].(string)
	saveModeStr, _ := args["save_mode"].(string)
	if strings.TrimSpace(saveModeStr) == "" {
		saveModeStr = string(SaveSession)
	}
	port := parseOptionalInt(args["port"])

	info := buildServiceInfoFromDiscovery(server, serviceType, serviceName, port, entry)
	creds := Credentials{
		Username: username,
		Password: password,
		Role:     role,
	}
	saveMode := SaveMode(saveModeStr)

	if err := t.Manager.Activate(ctx, info, creds, saveMode); err != nil {
		return fmt.Sprintf("커넥터 활성화 실패: %s", err), nil
	}

	states := t.Manager.States()
	for _, s := range states {
		if s.ServerName == info.ServerName && s.Type == info.ServiceType && s.ServiceName == info.Name {
			return fmt.Sprintf("✓ %s %s 커넥터 활성화 완료 (%d개 도구 등록)\n저장 방식: %s\n활성 도구: %v",
				info.ServiceType, info.Name, len(s.Tools), saveMode, s.Tools), nil
		}
	}

	return fmt.Sprintf("✓ %s %s 커넥터 활성화 완료 (save_mode: %s)", info.ServiceType, info.Name, saveMode), nil
}

func parseOptionalInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
