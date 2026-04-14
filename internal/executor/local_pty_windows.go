//go:build windows

// Package executor
// File: local_pty_windows.go
// Description: ConPTY-backed stream execution for local commands on Windows
// Responsibility: Allocate a Windows ConPTY so interactive prompts (scp, ssh) appear in stdout

package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

// ExecuteStreamPTY runs a local command in a ConPTY while streaming lines.
// ConPTY ensures password prompts from scp/ssh reach stdout,
// enabling the idle handler to detect and auto-respond.
// Falls back to regular ExecuteStream if ConPTY is unavailable.
func (e *LocalExecutor) ExecuteStreamPTY(ctx context.Context, command string, onLine func(string)) (ExecResult, error) {
	start := time.Now()

	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); ok {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
	}
	defer cancel()

	// ConPTY에서는 -NonInteractive를 제거하여 자식 프로세스가 PTY로 상호작용 가능하게 한다.
	cmd := buildCommandPTY(ctx, command)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Warn("conpty start failed, falling back to pipe execution (password prompts may not appear)", "err", err)
		return e.ExecuteStream(ctx, command, onLine)
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
			return ExecResult{}, fmt.Errorf("wait local conpty command: %w", waitErr)
		}
	}

	return ExecResult{
		Stdout:   TruncateOutput(strings.Join(lines, "\n"), MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// buildCommandPTY creates a command for ConPTY execution.
// -NonInteractive를 제거하여 자식 프로세스의 콘솔 입출력이 ConPTY를 통과하게 한다.
func buildCommandPTY(ctx context.Context, command string) *exec.Cmd {
	psCmd := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
		"$OutputEncoding = [System.Text.Encoding]::UTF8; " + command
	return exec.CommandContext(ctx,
		"powershell.exe",
		"-NoProfile",
		"-Command", psCmd,
	)
}
