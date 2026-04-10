package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/yourorg/infractl/internal/executor"
)

// ShellExecTool executes arbitrary shell commands locally or via SSH.
type ShellExecTool struct {
	OutputCb func(string)
}

func (t *ShellExecTool) Name() string { return "shell_exec" }

func (t *ShellExecTool) Description() string {
	return "Execute a shell command on a target server via SSH or locally.\n" +
		"Use for: running OS commands, checking processes, viewing logs, executing scripts.\n" +
		"For database-specific queries, prefer dedicated connector tools (oracle.query, mysql.query) if available.\n" +
		"Always include 'target' field when targeting a specific server.\n" +
		"Always include 'description' field with a brief Korean explanation of what this command does."
}

func (t *ShellExecTool) IsReadOnly() bool { return false }
func (t *ShellExecTool) IsEnabled() bool  { return true }

func (t *ShellExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Command timeout in seconds (default: 30)",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
			"risk_assessment": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"none", "low", "medium", "high"},
				"description": "Risk level of this command. none=read-only, low=modification, medium=deletion/config change, high=destructive (DROP/rm -rf/mkfs). Omit for read-only commands.",
			},
			"pre_backup_command": map[string]interface{}{
				"type":        "string",
				"description": "Command to run BEFORE the main command as a backup step. For high-risk commands (rm -rf, truncate), this is REQUIRED. Example: \"tar czf /backup/data_$(date +%Y%m%d).tar.gz /data\". If this command fails, the main command is aborted.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Brief Korean description of what this command does. Shown to the user in the TUI. Example: 'Oracle DB version and PDB status check', 'disk usage query'",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellExecTool) RiskLevel() RiskLevel { return RiskNone }

func (t *ShellExecTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	command, err := argString(args, "command", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	contextLine := "Execution Context: " + executor.ExecutionContextLabel(exec)

	timeoutSec := argInt(args, "timeout", 30)
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	if preBackup, _ := argString(args, "pre_backup_command", false); preBackup != "" {
		backupCtx, backupCancel := context.WithTimeout(ctx, 5*time.Minute)
		bakRes, bakErr := exec.Execute(backupCtx, preBackup)
		backupCancel()
		if bakErr != nil || bakRes.ExitCode != 0 {
			errMsg := preBackup + " failed"
			if bakErr != nil {
				errMsg = bakErr.Error()
			} else if bakRes.Stderr != "" {
				errMsg = bakRes.Stderr
			}
			return fmt.Sprintf("%s\npre_backup_command failed; main command aborted:\n%s", contextLine, errMsg), nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var result executor.ExecResult
	if se, ok := exec.(executor.StreamExecutor); ok && t.OutputCb != nil {
		result, err = se.ExecuteStream(ctx, command, t.OutputCb)
	} else {
		result, err = exec.Execute(ctx, command)
	}
	if err != nil {
		return fmt.Sprintf("%s\nExecution failed: %s", contextLine, err), nil
	}

	output := fmt.Sprintf("%s\n[Exit Code: %d]\n%s", contextLine, result.ExitCode, result.Stdout)
	if result.Stderr != "" {
		output += fmt.Sprintf("\n[Stderr]\n%s", result.Stderr)
	}
	return output, nil
}
