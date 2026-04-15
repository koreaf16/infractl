// Package tools
// File: file_read.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package tools

import (
	"context"
	"fmt"

	"github.com/yourorg/infractl/internal/executor"
)

const maxFileReadLines = 500

// FileReadTool reads a text file from the target system.
type FileReadTool struct{}

func (t *FileReadTool) Name() string { return "file_read" }

func (t *FileReadTool) Description() string {
	return "Read the contents of a file on the target system.\n" +
		"Use for: reading config files, log files, scripts, or any text file.\n" +
		"Supports line limit to avoid reading oversized files."
}

func (t *FileReadTool) IsReadOnly() bool { return true }
func (t *FileReadTool) IsEnabled() bool  { return true }

func (t *FileReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The absolute or relative path to the file",
			},
			"lines": map[string]interface{}{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum number of lines to read (default: all, max: %d)", maxFileReadLines),
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *FileReadTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	path, err := argString(args, "path", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}

	lines := argInt(args, "lines", 0)
	if lines > maxFileReadLines {
		lines = maxFileReadLines
	}

	cmd := buildReadCommand(exec, path, lines)
	result, err := exec.Execute(ctx, cmd)
	if err != nil {
		return fmt.Sprintf("Execution failed: %s", err), nil
	}

	if result.ExitCode != 0 {
		if isPermissionFailure(result, nil) {
			if retryResult, ok := executePlainViaAcquiredRoot(ctx, exec, cmd); ok && retryResult.ExitCode == 0 {
				return stripPrivilegeReuseNote(retryResult.Stdout), nil
			}
		}
		return fmt.Sprintf("Error reading file (exit %d):\n%s", result.ExitCode, result.Stderr), nil
	}
	return result.Stdout, nil
}

func buildReadCommand(exec executor.Executor, path string, lines int) string {
	platform := executor.CommandPlatform(exec)
	switch platform {
	case executor.PlatformWindows:
		script := ""
		if lines > 0 {
			script = fmt.Sprintf(
				"Get-Content -LiteralPath %s -TotalCount %d",
				executor.QuotePowerShell(path), lines,
			)
		} else {
			script = fmt.Sprintf(
				"Get-Content -LiteralPath %s",
				executor.QuotePowerShell(path),
			)
		}
		return executor.PowerShellCommand(exec, script)
	default:
		if lines > 0 {
			return fmt.Sprintf("head -n %d %s", lines, executor.QuotePOSIX(path))
		}
		return fmt.Sprintf("cat %s", executor.QuotePOSIX(path))
	}
}
