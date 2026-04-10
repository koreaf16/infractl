package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/yourorg/infractl/internal/executor"
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
