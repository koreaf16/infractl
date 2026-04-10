package tools

import (
	"context"
	"fmt"

	"github.com/yourorg/infractl/internal/executor"
)

// ProcessListTool lists processes on the target system.
type ProcessListTool struct{}

func (t *ProcessListTool) Name() string { return "process_list" }

func (t *ProcessListTool) Description() string {
	return "List running processes with their resource usage (CPU, memory, PID).\n" +
		"Use for: checking if a service is running, identifying resource-hungry processes.\n" +
		"For service discovery, this is the first step before activating a connector."
}

func (t *ProcessListTool) IsReadOnly() bool { return true }
func (t *ProcessListTool) IsEnabled() bool  { return true }

func (t *ProcessListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filter": map[string]interface{}{
				"type":        "string",
				"description": "Filter processes by name (optional)",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
		},
	}
}

func (t *ProcessListTool) RiskLevel() RiskLevel { return RiskNone }

func (t *ProcessListTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	filter, _ := argString(args, "filter", false)

	cmd := buildProcessListCommand(filter, executor.CommandPlatform(exec))
	result, err := exec.Execute(ctx, cmd)
	if err != nil {
		return fmt.Sprintf("Execution failed: %s", err), nil
	}

	if result.ExitCode != 0 {
		return fmt.Sprintf("Error listing processes (exit %d):\n%s", result.ExitCode, result.Stderr), nil
	}
	return result.Stdout, nil
}

func buildProcessListCommand(filter string, platform executor.Platform) string {
	switch platform {
	case executor.PlatformWindows:
		if filter != "" {
			return fmt.Sprintf("tasklist | findstr /i %q", filter)
		}
		return "tasklist"
	default:
		if filter != "" {
			return fmt.Sprintf("ps aux | grep %s | grep -v grep", executor.QuotePOSIX(filter))
		}
		return "ps aux"
	}
}
