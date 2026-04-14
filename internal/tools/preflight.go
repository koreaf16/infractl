// Package tools
// File: preflight.go
// Description: 원격 파일 작업 전 권한 및 디스크 공간 사전 검사
// Responsibility: 대용량 파일 전송/생성 전 대상 디렉토리의 쓰기 권한과 잔여 용량을 확인한다.

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
)

// checkRemoteWritable verifies the remote directory at dir is writable by the current user.
// Uses `test -w <dir>` via the executor and returns a descriptive error on failure.
func checkRemoteWritable(ctx context.Context, exec executor.Executor, dir string) error {
	quoted := executor.QuotePOSIX(dir)
	result, err := exec.Execute(ctx, fmt.Sprintf("test -w %s && echo writable || echo notwritable", quoted))
	if err != nil {
		return fmt.Errorf("write permission check on %q: %w", dir, err)
	}
	combined := strings.TrimSpace(result.Stdout + result.Stderr)
	if !strings.Contains(combined, "writable") || strings.Contains(combined, "notwritable") {
		return fmt.Errorf("no write permission on remote directory %q (current user lacks write access)", dir)
	}
	return nil
}

// checkRemoteDiskSpace verifies that the filesystem hosting dir has at least requiredBytes free.
// Uses POSIX `df -P <dir>` which reports 1 K-block units in the 4th column of the data row.
func checkRemoteDiskSpace(ctx context.Context, exec executor.Executor, dir string, requiredBytes int64) error {
	if requiredBytes <= 0 {
		return nil
	}
	quoted := executor.QuotePOSIX(dir)
	result, err := exec.Execute(ctx, fmt.Sprintf("df -P %s", quoted))
	if err != nil {
		return fmt.Errorf("disk space check on %q: %w", dir, err)
	}
	available, err := parseDFAvailableBytes(result.Stdout)
	if err != nil {
		return fmt.Errorf("disk space check: %w", err)
	}
	if available < requiredBytes {
		return fmt.Errorf("insufficient disk space: %.1f MB available, %.1f MB required on %q",
			float64(available)/(1<<20), float64(requiredBytes)/(1<<20), dir)
	}
	return nil
}

// parseDFAvailableBytes parses `df -P` stdout and returns the available space in bytes.
// Format: Filesystem  1K-blocks  Used  Available  Use%  Mounted-on
func parseDFAvailableBytes(output string) (int64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected df output: %q", output)
	}
	// The data line is the last non-empty line (handles continuation for long device names).
	dataLine := ""
	for i := len(lines) - 1; i >= 1; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			dataLine = lines[i]
			break
		}
	}
	if dataLine == "" {
		return 0, fmt.Errorf("no data row in df output: %q", output)
	}
	fields := strings.Fields(dataLine)
	if len(fields) < 4 {
		return 0, fmt.Errorf("unexpected df data row: %q", dataLine)
	}
	var kBlocks int64
	if _, scanErr := fmt.Sscanf(fields[3], "%d", &kBlocks); scanErr != nil {
		return 0, fmt.Errorf("parse available blocks from %q: %w", fields[3], scanErr)
	}
	return kBlocks * 1024, nil // df -P reports in 1 K-block units
}
