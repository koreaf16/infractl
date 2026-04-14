// Package checkpoint
// File: tool.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package checkpoint

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

// ListTool lists recent checkpoints.
type ListTool struct {
	Manager *Manager
}

func (t *ListTool) Name() string { return "checkpoint_list" }

func (t *ListTool) Description() string {
	return "List recent checkpoints. Use the optional server filter to scope results."
}

func (t *ListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"server": map[string]interface{}{
				"type":        "string",
				"description": "Optional server name filter",
			},
		},
	}
}

func (t *ListTool) RiskLevel() tools.RiskLevel { return tools.RiskNone }
func (t *ListTool) IsReadOnly() bool           { return true }
func (t *ListTool) IsEnabled() bool            { return true }

func (t *ListTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	server, _ := args["server"].(string)
	cps, err := t.Manager.List(ctx, server, 20)
	if err != nil {
		return "", fmt.Errorf("list checkpoints: %w", err)
	}
	if len(cps) == 0 {
		return "No checkpoints found.", nil
	}

	var sb strings.Builder
	sb.WriteString("Checkpoints:\n")
	for _, cp := range cps {
		sb.WriteString(fmt.Sprintf("  #%d [%s] %s - %s\n",
			cp.ID, cp.Server, cp.CreatedAt.Format("2006-01-02 15:04:05"), cp.Description))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// RollbackTool executes the rollback command stored in a checkpoint.
type RollbackTool struct {
	Manager *Manager
}

func (t *RollbackTool) Name() string { return "checkpoint_rollback" }

func (t *RollbackTool) Description() string {
	return "Rollback to a checkpoint. checkpoint_id may be a number or 'latest'."
}

func (t *RollbackTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"checkpoint_id": map[string]interface{}{
				"description": "Checkpoint ID (integer) or 'latest'",
			},
			"server": map[string]interface{}{
				"type":        "string",
				"description": "Required when checkpoint_id is 'latest'. Must match the checkpoint owner server.",
			},
		},
		"required": []string{"checkpoint_id"},
	}
}

func (t *RollbackTool) RiskLevel() tools.RiskLevel { return tools.RiskMedium }
func (t *RollbackTool) IsReadOnly() bool           { return false }
func (t *RollbackTool) IsEnabled() bool            { return true }

func (t *RollbackTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	cpIDRaw := args["checkpoint_id"]
	server, _ := args["server"].(string)

	cp, err := t.resolveCheckpoint(ctx, cpIDRaw, server)
	if err != nil {
		return "", err
	}
	if server != "" && !strings.EqualFold(strings.TrimSpace(server), strings.TrimSpace(cp.Server)) {
		return "", fmt.Errorf("checkpoint #%d belongs to server %q, not %q", cp.ID, cp.Server, server)
	}

	actualTarget := strings.TrimSpace(exec.Target())
	if actualTarget == "" {
		actualTarget = "localhost"
	}
	if !strings.EqualFold(actualTarget, strings.TrimSpace(cp.Server)) {
		return "", fmt.Errorf("checkpoint #%d belongs to server %q but current executor targets %q", cp.ID, cp.Server, actualTarget)
	}

	rollbackCmd := t.Manager.BuildRollbackCommand(cp)
	if rollbackCmd == "" {
		return fmt.Sprintf("Checkpoint #%d has no rollback command.\nDescription: %s", cp.ID, cp.Description), nil
	}

	result, err := exec.Execute(ctx, rollbackCmd)
	if err != nil {
		return "", fmt.Errorf("execute rollback: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Sprintf("Rollback failed (exit %d):\n%s", result.ExitCode, result.Stderr), nil
	}

	return fmt.Sprintf("Checkpoint #%d rolled back successfully\nDescription: %s\nCommand: %s", cp.ID, cp.Description, rollbackCmd), nil
}

func (t *RollbackTool) resolveCheckpoint(ctx context.Context, cpIDRaw interface{}, server string) (store.Checkpoint, error) {
	if cpIDRaw == "latest" || cpIDRaw == nil {
		if strings.TrimSpace(server) == "" {
			return store.Checkpoint{}, fmt.Errorf("server is required when checkpoint_id is 'latest'")
		}
		cp, err := t.Manager.GetLatest(ctx, server)
		if err != nil {
			return store.Checkpoint{}, fmt.Errorf("load latest checkpoint: %w", err)
		}
		return cp, nil
	}

	var id int64
	switch v := cpIDRaw.(type) {
	case float64:
		id = int64(v)
	case int64:
		id = v
	case int:
		id = int64(v)
	default:
		return store.Checkpoint{}, fmt.Errorf("checkpoint_id must be an integer or 'latest'")
	}

	cp, err := t.Manager.Get(ctx, id)
	if err != nil {
		return store.Checkpoint{}, fmt.Errorf("load checkpoint: %w", err)
	}
	return cp, nil
}

