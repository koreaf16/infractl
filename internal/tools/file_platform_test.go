// Package tools
// File: file_platform_test.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/yourorg/infractl/internal/executor"
)

type platformTestExecutor struct {
	target   string
	platform executor.Platform
}

func (e platformTestExecutor) Execute(context.Context, string) (executor.ExecResult, error) {
	return executor.ExecResult{}, nil
}

func (e platformTestExecutor) Target() string {
	return e.target
}

func (e platformTestExecutor) Platform() executor.Platform {
	return e.platform
}

func (e platformTestExecutor) ShellName() string {
	if e.platform == executor.PlatformWindows {
		return "PowerShell"
	}
	return "bash"
}

func TestBuildReadCommandLocalWindowsUsesRawPowerShell(t *testing.T) {
	cmd := buildReadCommand(platformTestExecutor{
		target:   "localhost",
		platform: executor.PlatformWindows,
	}, `C:\temp\notes.txt`, 10)

	if !strings.Contains(cmd, "Get-Content -LiteralPath") {
		t.Fatalf("expected raw Get-Content command, got %q", cmd)
	}
	if strings.Contains(cmd, "EncodedCommand") || strings.HasPrefix(strings.ToLower(cmd), "powershell ") {
		t.Fatalf("expected local windows command to avoid nested powershell wrapper, got %q", cmd)
	}
}

func TestBuildReadCommandRemoteWindowsUsesEncodedPowerShell(t *testing.T) {
	cmd := buildReadCommand(platformTestExecutor{
		target:   "win-app-01",
		platform: executor.PlatformWindows,
	}, `C:\temp\notes.txt`, 10)

	if !strings.Contains(cmd, "powershell -NoProfile -NonInteractive -EncodedCommand ") {
		t.Fatalf("expected encoded remote powershell wrapper, got %q", cmd)
	}
}

func TestBuildBackupCommandRemoteWindowsUsesEncodedPowerShell(t *testing.T) {
	cmd := buildBackupCmd(platformTestExecutor{
		target:   "win-db-01",
		platform: executor.PlatformWindows,
	}, `C:\data\prod.ini`)

	if !strings.Contains(cmd, "EncodedCommand") {
		t.Fatalf("expected encoded remote powershell wrapper, got %q", cmd)
	}
	if strings.Contains(cmd, "prod.ini.bak") {
		// The path should only appear inside the encoded payload, not as unescaped shell text.
		t.Fatalf("expected encoded payload without plaintext path leakage, got %q", cmd)
	}
}
