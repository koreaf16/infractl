//go:build !windows

package executor

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

// ExecuteInteractive runs a local command in a PTY and streams raw terminal chunks.
func (e *LocalExecutor) ExecuteInteractive(ctx context.Context, spec InteractiveSpec, onChunk func(string)) (ExecResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd, err := buildCommand(ctx, spec.Command)
	if err != nil {
		return ExecResult{}, fmt.Errorf("build command: %w", err)
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return ExecResult{}, fmt.Errorf("start PTY command: %w", err)
	}
	defer ptmx.Close()

	e.setActivePTY(ptmx)
	defer e.clearActiveStdin(ptmx)

	var output strings.Builder
	buf := make([]byte, 1024)
	for {
		n, readErr := ptmx.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			output.WriteString(chunk)
			if onChunk != nil {
				onChunk(chunk)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return ExecResult{}, fmt.Errorf("read PTY output: %w", readErr)
		}
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecResult{}, fmt.Errorf("wait PTY command: %w", waitErr)
		}
	}

	return ExecResult{
		Stdout:   TruncateOutput(output.String(), MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}
