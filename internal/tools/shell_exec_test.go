// Package tools
// File: shell_exec_test.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/privilege"
)

type shellExecTestExecutor struct {
	target string
	result executor.ExecResult
	err    error
}

func (e shellExecTestExecutor) Execute(context.Context, string) (executor.ExecResult, error) {
	return e.result, e.err
}

func (e shellExecTestExecutor) Target() string {
	return e.target
}

type fakePrivilegePrompter struct {
	password string
	calls    int
}

func (p *fakePrivilegePrompter) RequestPassword(context.Context, privilege.PromptRequest) (privilege.PromptResponse, error) {
	p.calls++
	return privilege.PromptResponse{Password: p.password}, nil
}

type fakeInteractiveExecutor struct {
	target            string
	platform          executor.Platform
	results           map[string]executor.ExecResult
	interactiveResult executor.ExecResult
	interactiveChunks []string
	injected          []string
}

type fakePersistentPrivilegeExecutor struct {
	target          string
	platform        executor.Platform
	plainResult     executor.ExecResult
	sessions        []executor.SessionInfo
	sessionCommands []string
	elevations      []string
	sessionResult   executor.ShellRunResult
}

func (e *fakePersistentPrivilegeExecutor) Execute(context.Context, string) (executor.ExecResult, error) {
	return e.plainResult, nil
}

func (e *fakePersistentPrivilegeExecutor) Target() string { return e.target }

func (e *fakePersistentPrivilegeExecutor) Platform() executor.Platform {
	if e.platform == "" {
		return executor.PlatformLinux
	}
	return e.platform
}

func (e *fakePersistentPrivilegeExecutor) ShellName() string { return "bash" }

func (e *fakePersistentPrivilegeExecutor) SessionExecute(_ context.Context, sessionID, command string, _ time.Duration, _ func([]string) (string, bool)) (executor.ShellRunResult, error) {
	e.sessionCommands = append(e.sessionCommands, sessionID+":"+command)
	result := e.sessionResult
	if result.CurrentUser == "" {
		result.CurrentUser = "root"
	}
	if result.CurrentDir == "" {
		result.CurrentDir = "/root"
	}
	return result, nil
}

func (e *fakePersistentPrivilegeExecutor) SessionElevate(_ context.Context, sessionID, elevationCmd string, _ time.Duration, _ func([]string) (string, bool)) (executor.ShellRunResult, error) {
	e.elevations = append(e.elevations, sessionID+":"+elevationCmd)
	e.sessions = append(e.sessions, executor.SessionInfo{SessionID: sessionID, CurrentUser: sessionID, Alive: true, LastUsed: time.Now()})
	return executor.ShellRunResult{ExitCode: 0, CurrentUser: sessionID, CurrentDir: "/"}, nil
}

func (e *fakePersistentPrivilegeExecutor) SessionClose(context.Context, string) error { return nil }

func (e *fakePersistentPrivilegeExecutor) SessionList(context.Context) ([]executor.SessionInfo, error) {
	return append([]executor.SessionInfo(nil), e.sessions...), nil
}

func (e *fakeInteractiveExecutor) Execute(_ context.Context, command string) (executor.ExecResult, error) {
	if result, ok := e.results[command]; ok {
		return result, nil
	}
	return executor.ExecResult{}, nil
}

func (e *fakeInteractiveExecutor) ExecuteInteractive(_ context.Context, spec executor.InteractiveSpec, onChunk func(string)) (executor.ExecResult, error) {
	for _, chunk := range e.interactiveChunks {
		if onChunk != nil {
			onChunk(chunk)
		}
	}
	return e.interactiveResult, nil
}

func (e *fakeInteractiveExecutor) InjectStdin(line string) error {
	e.injected = append(e.injected, line)
	return nil
}

func (e *fakeInteractiveExecutor) Target() string {
	return e.target
}

func (e *fakeInteractiveExecutor) Platform() executor.Platform {
	return e.platform
}

func (e *fakeInteractiveExecutor) ShellName() string {
	if e.platform == executor.PlatformWindows {
		return "PowerShell"
	}
	return "bash"
}

func TestShellExecToolPrefixesExecutionContextForLocalRuns(t *testing.T) {
	tool := &ShellExecTool{}
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "Get-ChildItem",
	}, shellExecTestExecutor{
		target: "localhost",
		result: executor.ExecResult{
			Stdout:   "file1\nfile2",
			ExitCode: 0,
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(out, "Execution Context: localhost ("+executor.LocalShellName()+")") {
		t.Fatalf("missing local execution context in output:\n%s", out)
	}
	if !strings.Contains(out, "[Exit Code: 0]") {
		t.Fatalf("missing exit code in output:\n%s", out)
	}
}

func TestShellExecToolPrefixesExecutionContextForRemoteRuns(t *testing.T) {
	tool := &ShellExecTool{}
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "ls",
	}, shellExecTestExecutor{
		target: "db-server",
		result: executor.ExecResult{
			Stdout:   "oracle_home",
			ExitCode: 0,
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(out, "Execution Context: db-server (ssh)") {
		t.Fatalf("missing remote execution context in output:\n%s", out)
	}
}

func TestShellExecToolDescriptionEncouragesNonInteractiveCommands(t *testing.T) {
	tool := &ShellExecTool{}
	desc := tool.Description()

	for _, want := range []string{
		"non-interactive",
		"`sudo -n`",
		"`runuser -l`",
		"`bash -lc`",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("expected %q in description:\n%s", want, desc)
		}
	}
}

func TestShellExecToolCachesPrivilegePasswordInMemory(t *testing.T) {
	plan, err := privilege.BuildPlan(privilege.MethodSudo, "root", "id")
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	cache := privilege.NewCache()
	prompter := &fakePrivilegePrompter{password: "secret123"}
	tool := &ShellExecTool{
		PrivilegeCache: cache,
		PromptHandler:  prompter,
	}

	exec1 := &fakeInteractiveExecutor{
		target:   "db-server",
		platform: executor.PlatformLinux,
		results: map[string]executor.ExecResult{
			plan.ValidateCommand: {ExitCode: 1, Stderr: "sudo: a password is required"},
		},
		interactiveChunks: []string{plan.PromptToken, "uid=0(root)\n"},
		interactiveResult: executor.ExecResult{ExitCode: 0},
	}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command":       "id",
		"become_method": "sudo",
		"become_user":   "root",
	}, exec1)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "uid=0(root)") {
		t.Fatalf("expected command output in result:\n%s", out)
	}
	if strings.Contains(out, plan.PromptToken) {
		t.Fatalf("expected sanitized prompt token, got:\n%s", out)
	}
	if got := len(exec1.injected); got != 1 || exec1.injected[0] != "secret123" {
		t.Fatalf("expected one injected password, got %#v", exec1.injected)
	}
	if prompter.calls != 1 {
		t.Fatalf("expected one prompt call, got %d", prompter.calls)
	}

	exec2 := &fakeInteractiveExecutor{
		target:   "db-server",
		platform: executor.PlatformLinux,
		results: map[string]executor.ExecResult{
			plan.ValidateCommand: {ExitCode: 1, Stderr: "sudo: a password is required"},
		},
		interactiveChunks: []string{plan.PromptToken, "cached-ok\n"},
		interactiveResult: executor.ExecResult{ExitCode: 0},
	}

	out, err = tool.Execute(context.Background(), map[string]interface{}{
		"command":       "id",
		"become_method": "sudo",
		"become_user":   "root",
	}, exec2)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if !strings.Contains(out, "cached-ok") {
		t.Fatalf("expected cached execution output in result:\n%s", out)
	}
	if prompter.calls != 1 {
		t.Fatalf("expected cached password to avoid a second prompt, got %d prompt calls", prompter.calls)
	}
	if got := len(exec2.injected); got != 1 || exec2.injected[0] != "secret123" {
		t.Fatalf("expected cached password injection, got %#v", exec2.injected)
	}
}

func TestShellExecToolReusesRootSessionForBecomeUser(t *testing.T) {
	tool := &ShellExecTool{}
	exec := &fakePersistentPrivilegeExecutor{
		target: "db-server",
		sessions: []executor.SessionInfo{
			{SessionID: "root", CurrentUser: "root", Alive: true, LastUsed: time.Now()},
		},
		sessionResult: executor.ShellRunResult{
			Stdout:      "oracle-ok",
			ExitCode:    0,
			CurrentUser: "root",
			CurrentDir:  "/root",
		},
	}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command":       "id",
		"become_method": "su",
		"become_user":   "oracle",
	}, exec)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "oracle-ok") {
		t.Fatalf("expected session command output:\n%s", out)
	}
	if !strings.Contains(out, "Privilege Reused: root session") {
		t.Fatalf("expected privilege reuse note:\n%s", out)
	}
	if len(exec.sessionCommands) == 0 || !strings.Contains(exec.sessionCommands[0], "su - 'oracle'") {
		t.Fatalf("expected command to run through root su, got %#v", exec.sessionCommands)
	}
}

func TestShellExecToolRetriesPlainPermissionFailureWithRootSession(t *testing.T) {
	tool := &ShellExecTool{}
	exec := &fakePersistentPrivilegeExecutor{
		target:      "db-server",
		plainResult: executor.ExecResult{ExitCode: 1, Stderr: "cat: /etc/shadow: Permission denied"},
		sessions: []executor.SessionInfo{
			{SessionID: "root", CurrentUser: "root", Alive: true, LastUsed: time.Now()},
		},
		sessionResult: executor.ShellRunResult{
			Stdout:      "root-only-content",
			ExitCode:    0,
			CurrentUser: "root",
			CurrentDir:  "/root",
		},
	}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cat /etc/shadow",
	}, exec)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "root-only-content") {
		t.Fatalf("expected root retry output:\n%s", out)
	}
	if !strings.Contains(out, "permission failure retried in root session") {
		t.Fatalf("expected root retry note:\n%s", out)
	}
	if len(exec.sessionCommands) != 1 || !strings.Contains(exec.sessionCommands[0], "cat /etc/shadow") {
		t.Fatalf("expected one root session retry, got %#v", exec.sessionCommands)
	}
}
