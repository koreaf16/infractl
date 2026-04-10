package tools

import (
	"context"
	"fmt"

	"github.com/yourorg/infractl/internal/executor"
)

// NetworkInfoTool reports listening ports or active connections on the target system.
type NetworkInfoTool struct{}

func (t *NetworkInfoTool) Name() string { return "network_info" }

func (t *NetworkInfoTool) Description() string {
	return "Show network information: listening ports or active connections on the target system.\n" +
		"Use for: identifying which services are running on which ports, checking open connections.\n" +
		"Useful for service discovery when combined with process_list."
}

func (t *NetworkInfoTool) IsReadOnly() bool { return true }
func (t *NetworkInfoTool) IsEnabled() bool  { return true }

func (t *NetworkInfoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"listen", "connections"},
				"description": "Information type: 'listen' for listening ports (default), 'connections' for active connections",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
		},
	}
}

func (t *NetworkInfoTool) RiskLevel() RiskLevel { return RiskNone }

func (t *NetworkInfoTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	infoType, _ := argString(args, "type", false)
	if infoType == "" {
		infoType = "listen"
	}

	cmd := buildNetworkCommand(infoType, executor.CommandPlatform(exec))
	result, err := exec.Execute(ctx, cmd)
	if err != nil {
		return fmt.Sprintf("Execution failed: %s", err), nil
	}

	if result.ExitCode != 0 {
		return fmt.Sprintf("Error getting network info (exit %d):\n%s", result.ExitCode, result.Stderr), nil
	}
	return result.Stdout, nil
}

func buildNetworkCommand(infoType string, platform executor.Platform) string {
	switch platform {
	case executor.PlatformWindows:
		if infoType == "connections" {
			return "netstat -ano | findstr ESTABLISHED"
		}
		return "netstat -ano | findstr LISTENING"
	case executor.PlatformDarwin:
		if infoType == "connections" {
			return "netstat -an -p tcp | grep ESTABLISHED"
		}
		return "netstat -an -p tcp | grep LISTEN"
	default:
		if infoType == "connections" {
			return "ss -tnp 2>/dev/null || netstat -tnp 2>/dev/null"
		}
		return "ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null"
	}
}
