// Package tools
// File: disk_usage.go
// Description: 디스크 사용량 조회 전용 도구
// Responsibility: du, df 등 디스크 관련 조회 명령을 단일 도구로 대체

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
)

// DiskUsageTool은 디스크 사용량 정보를 조회한다.
type DiskUsageTool struct{}

func (t *DiskUsageTool) Name() string { return "disk_usage" }

func (t *DiskUsageTool) Description() string {
	return "Show disk usage for directories or mount points.\n" +
		"Use this INSTEAD of shell_exec with 'du -sh', 'du --max-depth', 'df -h' commands.\n" +
		"Supports summary, detail, and top-N modes."
}

func (t *DiskUsageTool) IsReadOnly() bool     { return true }
func (t *DiskUsageTool) IsEnabled() bool      { return true }
func (t *DiskUsageTool) RiskLevel() RiskLevel { return RiskNone }

func (t *DiskUsageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory path to check (default: /). Use 'mounts' to show mount point usage (df -h).",
				"default":     "/",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Mode: summary (total size), detail (subdirectory sizes), top (largest items). Default: summary.",
				"enum":        []string{"summary", "detail", "top"},
				"default":     "summary",
			},
			"depth": map[string]interface{}{
				"type":        "integer",
				"description": "Directory depth for detail mode (default: 1).",
				"default":     1,
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
		},
	}
}

func (t *DiskUsageTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	path, _ := argString(args, "path", false)
	if path == "" {
		path = "/"
	}
	mode, _ := argString(args, "mode", false)
	if mode == "" {
		mode = "summary"
	}
	depth := argInt(args, "depth", 1)

	platform := executor.CommandPlatform(exec)
	cmd := buildDiskUsageCommand(path, mode, depth, platform)

	result, err := exec.Execute(ctx, cmd)
	if err != nil {
		return fmt.Sprintf("execution failed: %s", err), nil
	}
	if result.ExitCode != 0 && result.Stdout == "" {
		return fmt.Sprintf("error (exit %d):\n%s", result.ExitCode, result.Stderr), nil
	}
	return strings.TrimSpace(result.Stdout), nil
}

func buildDiskUsageCommand(path, mode string, depth int, platform executor.Platform) string {
	if path == "mounts" {
		if platform == executor.PlatformWindows {
			return "wmic logicaldisk get DeviceID,Size,FreeSpace /format:list"
		}
		return "df -h"
	}

	switch platform {
	case executor.PlatformWindows:
		return buildDiskUsageWindows(path, mode)
	default:
		return buildDiskUsageLinux(path, mode, depth)
	}
}

func buildDiskUsageLinux(path, mode string, depth int) string {
	quoted := executor.QuotePOSIX(path)
	switch mode {
	case "detail":
		return fmt.Sprintf("du -h --max-depth=%d %s 2>/dev/null | sort -rh", depth, quoted)
	case "top":
		return fmt.Sprintf("du -h --max-depth=1 %s 2>/dev/null | sort -rh | head -20", quoted)
	default: // summary
		return fmt.Sprintf("du -sh %s 2>/dev/null", quoted)
	}
}

func buildDiskUsageWindows(path, mode string) string {
	switch mode {
	case "detail", "top":
		return fmt.Sprintf("Get-ChildItem -Path %q -Directory | ForEach-Object { $size = (Get-ChildItem $_.FullName -Recurse -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum; [PSCustomObject]@{Name=$_.Name; SizeMB=[math]::Round($size/1MB,1)} } | Sort-Object SizeMB -Descending | Format-Table -AutoSize", path)
	default:
		return fmt.Sprintf("Get-ChildItem -Path %q -Recurse -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum | ForEach-Object { '{0:N2} MB' -f ($_.Sum / 1MB) }", path)
	}
}
