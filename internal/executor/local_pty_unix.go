//go:build !windows

// Package executor
// File: local_pty_unix.go
// Description: PTY-backed stream execution for local commands on Unix
// Responsibility: Allocate a Unix PTY so interactive prompts (sudo, su, scp) appear in stdout

package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

// ExecuteStreamPTY runs a local command in a PTY while streaming lines.
// The PTY ensures password prompts from sudo/su/scp reach stdout,
// enabling the idle handler to detect and auto-respond.
func (e *LocalExecutor) ExecuteStreamPTY(ctx context.Context, command string, onLine func(string)) (ExecResult, error) {
	start := time.Now()

	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); ok {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
	}
	defer cancel()

	cmd, err := buildCommand(ctx, command)
	if err != nil {
		return ExecResult{}, fmt.Errorf("build command: %w", err)
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return ExecResult{}, fmt.Errorf("start local pty: %w", err)
	}
	defer ptmx.Close()

	e.setActivePTY(ptmx)
	defer e.clearActiveStdin(ptmx)

	lineCh := StartLineAssembler(StartPipeReader(ptmx))

	var lines []string
	for line := range lineCh {
		line = StripANSI(line)
		lines = append(lines, line)
		if onLine != nil {
			onLine(line)
		}
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecResult{}, fmt.Errorf("wait local pty command: %w", waitErr)
		}
	}

	return ExecResult{
		Stdout:   TruncateOutput(strings.Join(lines, "\n"), MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}
