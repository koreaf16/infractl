// Package executor
// File: local.go
// Description: 로컬 머신에서 쉘 명령을 실행하는 Executor 구현
// Responsibility: 로컬 bash(Linux/Mac) 또는 PowerShell(Windows) 명령 실행

package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// defaultTimeout은 명령 실행의 기본 타임아웃이다.
	defaultTimeout = 30 * time.Second
)

// LocalExecutor는 로컬 머신에서 명령을 실행하는 Executor 구현체이다.
type LocalExecutor struct {
	timeout time.Duration
}

// NewLocalExecutor는 지정된 타임아웃으로 LocalExecutor를 생성한다.
// timeout이 0이면 defaultTimeout(30초)을 사용한다.
func NewLocalExecutor(timeout time.Duration) *LocalExecutor {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &LocalExecutor{timeout: timeout}
}

// Execute는 로컬 쉘에서 command를 실행하고 결과를 반환한다.
// 비정상 종료(exit code != 0)는 error가 아닌 ExecResult로 반환한다.
func (e *LocalExecutor) Execute(ctx context.Context, command string) (ExecResult, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := buildCommand(ctx, command)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			// 비정상 종료 — exit code만 기록하고 error는 nil 반환
			exitCode = exitErr.ExitCode()
		} else {
			// 실행 자체가 실패 (바이너리 없음, 컨텍스트 취소 등)
			return ExecResult{}, fmt.Errorf("execute command: %w", runErr)
		}
	}

	return ExecResult{
		Stdout:   TruncateOutput(stdoutBuf.String(), MaxOutputBytes),
		Stderr:   TruncateOutput(stderrBuf.String(), MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// Target은 이 executor의 대상 식별자를 반환한다.
func (e *LocalExecutor) Target() string {
	return "localhost"
}

// Platform returns the local controller platform.
func (e *LocalExecutor) Platform() Platform {
	return NormalizePlatform(runtime.GOOS)
}

// ShellName returns the local shell label.
func (e *LocalExecutor) ShellName() string {
	return LocalShellName()
}

// ExecuteStream은 명령을 실행하면서 stdout 라인마다 onLine 콜백을 호출한다.
// stderr는 별도 버퍼에 수집하여 최종 ExecResult에 포함한다.
//
// 유휴 감지: 출력이 defaultIdleThreshold(10초) 동안 없으면 프로세스를 강제 종료한다.
// 로컬 실행은 stdin=/dev/null이라 stdin 주입이 불가하므로 kill 후 명확한 오류를 반환한다.
func (e *LocalExecutor) ExecuteStream(ctx context.Context, command string, onLine func(string)) (ExecResult, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := buildCommand(ctx, command)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("create stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return ExecResult{}, fmt.Errorf("start command: %w", err)
	}

	// 유휴 감지: 출력이 없으면 인터랙티브 프롬프트 대기로 판단해 프로세스를 종료한다.
	watcher := NewIdleWatcher(defaultIdleThreshold)
	watcher.Start(ctx)
	defer watcher.Stop()

	go func() {
		select {
		case <-watcher.Triggered():
			slog.Warn("local command idle timeout, killing process",
				"command", command, "threshold", defaultIdleThreshold)
			cancel() // ctx 취소 → cmd.Wait()이 에러 반환
		case <-ctx.Done():
		}
	}()

	var stdoutLines []string
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, MaxOutputBytes), MaxOutputBytes)
	for scanner.Scan() {
		line := scanner.Text()
		watcher.Ping()
		stdoutLines = append(stdoutLines, line)
		if onLine != nil {
			onLine(line)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && scanErr != io.EOF {
		stderrBuf.WriteString(fmt.Sprintf("\n[scan error: %s]", scanErr))
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	// 유휴 타임아웃에 의한 종료인지 확인 (ctx.Err() == context.Canceled)
	select {
	case <-watcher.Triggered():
		return ExecResult{
			Stdout:   TruncateOutput(strings.Join(stdoutLines, "\n"), MaxOutputBytes),
			Stderr:   fmt.Sprintf("[인터랙티브 입력 대기로 판단되어 %s 후 종료]\n%s", defaultIdleThreshold, stderrBuf.String()),
			ExitCode: -1,
			Duration: duration,
		}, nil
	default:
	}

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecResult{}, fmt.Errorf("wait command: %w", waitErr)
		}
	}

	return ExecResult{
		Stdout:   TruncateOutput(strings.Join(stdoutLines, "\n"), MaxOutputBytes),
		Stderr:   TruncateOutput(stderrBuf.String(), MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// buildCommand는 OS에 맞는 명령 객체를 생성한다.
// Windows: PowerShell 5/7 우선 사용 — Get-* cmdlet 등 PS 명령 직접 실행 가능.
//
//	출력 인코딩을 UTF-8로 강제하여 한글 깨짐을 방지한다.
//
// Linux/Mac: bash -c 사용.
func buildCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// [Console]::OutputEncoding 설정으로 한글 포함 출력이 UTF-8로 전달된다.
		psCmd := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
			"$OutputEncoding = [System.Text.Encoding]::UTF8; " + command
		return exec.CommandContext(ctx,
			"powershell.exe",
			"-NoProfile",
			"-NonInteractive",
			"-Command", psCmd,
		)
	}
	return exec.CommandContext(ctx, "bash", "-c", command)
}
