// Package tools
// File: server_remove.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

type serverConnectorCleanup interface {
	DeactivateServer(ctx context.Context, serverName string) error
}

// ServerRemoveTool removes a registered SSH server and its runtime bindings.
type ServerRemoveTool struct {
	Store               store.ServerStore
	Manager             *executor.Manager
	ConnectorCleanup    serverConnectorCleanup
	ActiveServer        func() *store.Server
	OnActiveServerClear func()
}

func (t *ServerRemoveTool) Name() string { return "server_remove" }

func (t *ServerRemoveTool) Description() string {
	return "Remove a registered SSH server by name"
}

func (t *ServerRemoveTool) IsReadOnly() bool { return false }
func (t *ServerRemoveTool) IsEnabled() bool  { return true }

func (t *ServerRemoveTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The server alias to remove",
			},
		},
		"required": []string{"name"},
	}
}

func (t *ServerRemoveTool) RiskLevel() RiskLevel { return RiskLow }

func (t *ServerRemoveTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	name, err := argString(args, "name", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}

	if t.ConnectorCleanup != nil {
		if err := t.ConnectorCleanup.DeactivateServer(ctx, name); err != nil {
			return fmt.Sprintf("Failed to detach connectors for server '%s': %s", name, err), nil
		}
	}

	if t.Manager != nil && t.Manager.Has(name) {
		if err := t.Manager.Remove(name); err != nil {
			return fmt.Sprintf("Failed to detach runtime executor for server '%s': %s", name, err), nil
		}
	}

	if err := t.Store.Remove(ctx, name); err != nil {
		return fmt.Sprintf("Failed to remove server '%s': %s", name, err), nil
	}

	if active := t.snapshotActiveServer(); active != nil && strings.EqualFold(active.Name, name) && t.OnActiveServerClear != nil {
		t.OnActiveServerClear()
	}

	return fmt.Sprintf("Server '%s' removed.", name), nil
}

func (t *ServerRemoveTool) snapshotActiveServer() *store.Server {
	if t.ActiveServer == nil {
		return nil
	}
	srv := t.ActiveServer()
	if srv == nil {
		return nil
	}
	cp := *srv
	return &cp
}
