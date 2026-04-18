// Package precheck
// File: checker_dir_writable.go
// Description: 원격 호스트의 디렉토리 쓰기 권한을 사전 검증하는 Checker 구현
// Responsibility: test -w를 사용해 현재 실행 계정이 대상 디렉토리에 쓸 수 있는지 확인한다.

package precheck

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
)

// DirWritableChecker verifies that the current user can write to a directory.
// Detects permission mismatches (e.g., running as sandbox when dir is owned by oracle).
type DirWritableChecker struct {
	dir      string
	severity Severity
}

// NewDirWritableChecker constructs a DirWritableChecker for the given remote directory path.
func NewDirWritableChecker(dir string, sev Severity) *DirWritableChecker {
	return &DirWritableChecker{dir: dir, severity: sev}
}

func (c *DirWritableChecker) Kind() CheckKind    { return KindDirWritable }
func (c *DirWritableChecker) Subject() string    { return c.dir }
func (c *DirWritableChecker) Severity() Severity { return c.severity }

// Run executes `test -w <dir>` on the remote host.
// If the directory does not exist yet, the check passes (DirExistsChecker handles that separately).
func (c *DirWritableChecker) Run(ctx context.Context, exec executor.Executor) CheckResult {
	dir := strings.TrimSpace(c.dir)
	if dir == "" {
		return CheckResult{Kind: c.Kind(), Subject: c.dir, OK: true, Severity: c.severity}
	}

	// Skip if the directory doesn't exist — DirExistsChecker reports that separately.
	existCmd := fmt.Sprintf("test -d %s", shellQuote(dir))
	existResult, err := exec.Execute(ctx, existCmd)
	if err != nil || existResult.ExitCode != 0 {
		return CheckResult{Kind: c.Kind(), Subject: c.dir, OK: true, Severity: c.severity}
	}

	writeCmd := fmt.Sprintf("test -w %s", shellQuote(dir))
	writeResult, err := exec.Execute(ctx, writeCmd)
	ok := err == nil && writeResult.ExitCode == 0
	if ok {
		return CheckResult{Kind: c.Kind(), Subject: c.dir, OK: true, Severity: c.severity}
	}

	// Fetch the owner for a more actionable error message.
	ownerCmd := fmt.Sprintf("stat -c '%%U' %s 2>/dev/null", shellQuote(dir))
	ownerResult, _ := exec.Execute(ctx, ownerCmd)
	owner := strings.TrimSpace(ownerResult.Stdout)

	msg := fmt.Sprintf(
		"directory %q is not writable by the current user — re-run as the directory owner (sudo -u %s ...)",
		dir, owner,
	)
	if owner == "" {
		msg = fmt.Sprintf("directory %q is not writable by the current user — check ownership and permissions", dir)
	}

	return CheckResult{
		Kind:     c.Kind(),
		Subject:  c.dir,
		OK:       false,
		Severity: c.severity,
		Message:  msg,
	}
}
