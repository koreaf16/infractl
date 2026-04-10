package tools

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

// SessionContextTool exposes the current local/remote execution context.
type SessionContextTool struct {
	Store        store.ServerStore
	ActiveServer func() *store.Server
}

func (t *SessionContextTool) Name() string { return "session_context" }

func (t *SessionContextTool) Description() string {
	return "Show the current execution context for this infractl session.\n" +
		"Use this before claiming a request is local-only, remote-only, or inaccessible.\n" +
		"It reports the local controller OS, shell, working directory, active server, and default execution rule."
}

func (t *SessionContextTool) IsReadOnly() bool { return true }
func (t *SessionContextTool) IsEnabled() bool  { return true }
func (t *SessionContextTool) RiskLevel() RiskLevel {
	return RiskNone
}

func (t *SessionContextTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SessionContextTool) Execute(ctx context.Context, _ map[string]interface{}, _ executor.Executor) (string, error) {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "(unknown)"
	}

	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		cwd = "(unknown)"
	}

	active := t.snapshotActiveServer()
	mode := "local-controller"
	defaultRule := "omit target => localhost"
	activeLine := "(none)"
	if active != nil {
		mode = "active-server"
		defaultRule = fmt.Sprintf("omit target => active server %q", active.Name)
		activeLine = formatServerLine(*active)
	}

	registered := "(none)"
	if t.Store != nil {
		servers, listErr := t.Store.List(ctx)
		if listErr == nil && len(servers) > 0 {
			lines := make([]string, 0, len(servers))
			for _, srv := range servers {
				lines = append(lines, "- "+formatServerLine(srv))
			}
			registered = strings.Join(lines, "\n")
		}
	}

	return fmt.Sprintf(
		"Mode: %s\nLocal Target: localhost\nLocal OS: %s/%s\nLocal Shell: %s\nHostname: %s\nWorking Directory: %s\nActive Server: %s\nRegistered Servers:\n%s\nDefault Execution Rule: %s",
		mode,
		runtime.GOOS,
		runtime.GOARCH,
		executor.LocalShellName(),
		hostname,
		cwd,
		activeLine,
		registered,
		defaultRule,
	), nil
}

func (t *SessionContextTool) snapshotActiveServer() *store.Server {
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

func formatServerLine(srv store.Server) string {
	parts := []string{fmt.Sprintf("%s (%s@%s:%d)", srv.Name, srv.User, srv.Host, srv.Port)}
	if srv.OS != "" {
		parts = append(parts, "OS="+srv.OS)
	}
	if srv.EnvProfile != "" {
		parts = append(parts, "Env="+srv.EnvProfile)
	}
	return strings.Join(parts, ", ")
}
