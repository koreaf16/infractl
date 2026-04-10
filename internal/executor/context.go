package executor

import (
	"fmt"
	"runtime"
	"strings"
)

// LocalShellName returns the shell label used for local execution.
func LocalShellName() string {
	if runtime.GOOS == "windows" {
		return "PowerShell"
	}
	return "bash"
}

// ExecutionContextLabel returns a stable human-readable execution target label.
func ExecutionContextLabel(exec Executor) string {
	if exec == nil {
		return fmt.Sprintf("localhost (%s)", LocalShellName())
	}

	target := strings.TrimSpace(exec.Target())
	if IsLocalTarget(target) {
		return fmt.Sprintf("localhost (%s)", LocalShellName())
	}

	shell := ShellNameForExecutor(exec)
	if shell == "" || shell == "ssh" {
		return fmt.Sprintf("%s (ssh)", target)
	}
	return fmt.Sprintf("%s (%s over ssh)", target, shell)
}
