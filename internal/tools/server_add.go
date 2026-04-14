// Package tools
// File: server_add.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package tools

import (
	"context"
	"fmt"
	"strings"

	sshconn "github.com/yourorg/infractl/internal/connector/ssh"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

// ServerAddTool registers a new SSH target after validating connectivity.
type ServerAddTool struct {
	Store   store.ServerStore
	Manager *executor.Manager
}

func (t *ServerAddTool) Name() string { return "server_add" }

func (t *ServerAddTool) Description() string {
	return "Register a new SSH server. Tests connectivity before saving. Supports key or password auth."
}

func (t *ServerAddTool) IsReadOnly() bool { return false }
func (t *ServerAddTool) IsEnabled() bool  { return true }

func (t *ServerAddTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Alias for the server (e.g., 'db-server')",
			},
			"host": map[string]interface{}{
				"type":        "string",
				"description": "IP address or hostname",
			},
			"port": map[string]interface{}{
				"type":        "integer",
				"description": "SSH port (default: 22)",
			},
			"user": map[string]interface{}{
				"type":        "string",
				"description": "SSH username",
			},
			"auth_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"key", "password"},
				"description": "Authentication method: 'key' for SSH private key, 'password' for password",
			},
			"credential": map[string]interface{}{
				"type":        "string",
				"description": "For auth_type=key: path to private key file. For auth_type=password: the password.",
			},
		},
		"required": []string{"name", "host", "user", "auth_type", "credential"},
	}
}

func (t *ServerAddTool) RiskLevel() RiskLevel { return RiskLow }

func (t *ServerAddTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	name, err := argString(args, "name", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	host, err := argString(args, "host", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	user, err := argString(args, "user", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	authType, err := argString(args, "auth_type", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	credential, err := argString(args, "credential", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	port := argInt(args, "port", 22)

	cfg := &sshconn.Config{
		Host:     host,
		Port:     port,
		User:     user,
		AuthType: authType,
	}
	if authType == "key" {
		cfg.KeyPath = credential
	} else {
		cfg.Password = credential
	}

	client := sshconn.NewClient(cfg)
	result, runErr := client.Run(ctx, "echo ok")
	if runErr != nil {
		client.Close()
		return fmt.Sprintf("SSH connection test failed for %s@%s:%d: %s", user, host, port, runErr), nil
	}
	if result.ExitCode != 0 {
		client.Close()
		return fmt.Sprintf("SSH test command failed (exit %d): %s", result.ExitCode, result.Stderr), nil
	}

	probeExec := sshconn.NewSSHExecutor(name, client)
	osInfo, _ := executor.DetectOS(ctx, probeExec)

	srv := store.Server{
		Name:       name,
		Host:       host,
		Port:       port,
		User:       user,
		AuthType:   store.AuthType(authType),
		Credential: credential,
		OS:         osInfo,
	}
	if err := t.Store.Add(ctx, srv); err != nil {
		client.Close()
		return fmt.Sprintf("Failed to save server: %s", err), nil
	}

	sshExec := sshconn.NewSSHExecutor(name, client, osInfo)
	t.Manager.Register(name, sshExec)

	if strings.TrimSpace(osInfo) == "" {
		return fmt.Sprintf("Server '%s' registered successfully (%s@%s:%d, auth: %s)", name, user, host, port, authType), nil
	}
	return fmt.Sprintf("Server '%s' registered successfully (%s@%s:%d, auth: %s, os: %s)", name, user, host, port, authType, osInfo), nil
}
