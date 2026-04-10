// Package ssh
// File: executor.go
// Description: SSH-backed executor implementation bridged to the shared executor interfaces.

package ssh

import (
	"context"
	"fmt"

	"github.com/yourorg/infractl/internal/executor"
)

// SSHExecutor executes commands on a remote target via SSH.
type SSHExecutor struct {
	name     string
	client   *Client
	platform executor.Platform
}

// NewSSHExecutor wraps an SSH client with an executor implementation.
func NewSSHExecutor(name string, client *Client, osHint ...string) *SSHExecutor {
	platform := executor.PlatformLinux
	if len(osHint) > 0 {
		if detected := executor.NormalizePlatform(osHint[0]); detected != executor.PlatformUnknown {
			platform = detected
		}
	}
	return &SSHExecutor{name: name, client: client, platform: platform}
}

// Execute runs a command on the remote host.
func (e *SSHExecutor) Execute(ctx context.Context, command string) (executor.ExecResult, error) {
	result, err := e.client.Run(ctx, command)
	if err != nil {
		return executor.ExecResult{}, fmt.Errorf("ssh execute on %s: %w", e.name, err)
	}

	return executor.ExecResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		Duration: result.Duration,
	}, nil
}

// ExecuteStream runs a command and streams stdout line-by-line.
func (e *SSHExecutor) ExecuteStream(ctx context.Context, command string, onLine func(string)) (executor.ExecResult, error) {
	result, err := e.client.RunStream(ctx, command, onLine, nil)
	if err != nil {
		return executor.ExecResult{}, fmt.Errorf("ssh stream on %s: %w", e.name, err)
	}

	return executor.ExecResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		Duration: result.Duration,
	}, nil
}

// ExecuteStreamWithIdle runs a command and exposes idle callbacks for interactive prompts.
func (e *SSHExecutor) ExecuteStreamWithIdle(
	ctx context.Context,
	command string,
	onLine func(string),
	onIdle func(executor.StdinInjector),
) (executor.ExecResult, error) {
	result, err := e.client.RunStream(ctx, command, onLine, onIdle)
	if err != nil {
		return executor.ExecResult{}, fmt.Errorf("ssh stream on %s: %w", e.name, err)
	}

	return executor.ExecResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		Duration: result.Duration,
	}, nil
}

// InjectStdin forwards interactive input to the active SSH session.
func (e *SSHExecutor) InjectStdin(line string) error {
	return e.client.InjectStdin(line)
}

// Target returns the server alias.
func (e *SSHExecutor) Target() string {
	return e.name
}

// Platform returns the remote target platform.
func (e *SSHExecutor) Platform() executor.Platform {
	return e.platform
}

// ShellName returns the preferred shell label for the remote target.
func (e *SSHExecutor) ShellName() string {
	if e.platform == executor.PlatformWindows {
		return "PowerShell"
	}
	return "bash"
}

// Close closes the SSH client connection.
func (e *SSHExecutor) Close() error {
	return e.client.Close()
}
