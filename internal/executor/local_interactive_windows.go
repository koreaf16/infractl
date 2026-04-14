//go:build windows

package executor

import (
	"context"
	"fmt"
)

// ExecuteInteractive is not supported on Windows local execution.
func (e *LocalExecutor) ExecuteInteractive(context.Context, InteractiveSpec, func(string)) (ExecResult, error) {
	return ExecResult{}, fmt.Errorf("interactive PTY execution is not supported on Windows")
}
