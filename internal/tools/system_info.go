// Package tools
// File: system_info.go
// Description: 시스템 정보 조회 전용 도구 — OS, CPU, 메모리, 디스크, uptime 통합
// Responsibility: uname, free, df, lscpu 등 다수의 shell 명령을 단일 도구로 대체

package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/infractl/internal/executor"
)

// SystemInfoTool은 대상 시스템의 OS, CPU, 메모리, 디스크, uptime 정보를 조회한다.
type SystemInfoTool struct{}

func (t *SystemInfoTool) Name() string { return "system_info" }

func (t *SystemInfoTool) Description() string {
	return "Retrieve system information including OS, kernel, uptime, CPU, memory, and disk usage.\n" +
		"Use this INSTEAD of shell_exec with uname, hostname, uptime, free, df, lscpu, " +
		"/proc/meminfo, /proc/cpuinfo, Get-ComputerInfo, or systeminfo commands.\n" +
		"Prefer this tool for any system overview or health check request."
}

func (t *SystemInfoTool) IsReadOnly() bool { return true }
func (t *SystemInfoTool) IsEnabled() bool  { return true }

func (t *SystemInfoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"sections": map[string]interface{}{
				"type":        "string",
				"description": "Comma-separated sections to retrieve: os, cpu, memory, disk, uptime, all (default: all)",
				"default":     "all",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
		},
	}
}

func (t *SystemInfoTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	sectionsStr, _ := argString(args, "sections", false)
	if sectionsStr == "" {
		sectionsStr = "all"
	}

	platform := executor.CommandPlatform(exec)
	sections := parseSections(sectionsStr)

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		parts = make(map[string]string)
		errs  []string
	)

	slog.Info("system_info_start", "server", exec.Target(), "sections", sections)

	for _, section := range sections {
		wg.Add(1)
		go func(sec string) {
			defer wg.Done()
			slog.Info("task_start", "task", sec)
			start := time.Now()

			cmd := buildSystemInfoCommand(sec, platform)
			if cmd == "" {
				return
			}
			result, err := exec.Execute(ctx, cmd)
			dur := time.Since(start)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("[%s] Error: %s", sec, err))
				slog.Info("task_fail", "task", sec, "dur", dur)
				return
			}
			if result.ExitCode != 0 {
				errs = append(errs, fmt.Sprintf("[%s] Error (exit %d):\n%s", sec, result.ExitCode, result.Stderr))
				slog.Info("task_fail", "task", sec, "dur", dur)
				return
			}
			parts[sec] = strings.TrimSpace(result.Stdout)
			slog.Info("task_done", "task", sec, "dur", dur)
		}(section)
	}

	wg.Wait()

	// Maintain requested order
	var output []string
	for _, sec := range sections {
		if content, ok := parts[sec]; ok {
			output = append(output, fmt.Sprintf("[%s]\n%s", sec, content))
		}
	}
	output = append(output, errs...)

	if len(output) == 0 {
		return "No sections collected.", nil
	}
	return strings.Join(output, "\n\n"), nil
}

func parseSections(raw string) []string {
	if strings.TrimSpace(raw) == "all" {
		return []string{"os", "cpu", "memory", "disk", "uptime"}
	}
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func buildSystemInfoCommand(section string, platform executor.Platform) string {
	switch platform {
	case executor.PlatformWindows:
		return buildSystemInfoWindows(section)
	default:
		return buildSystemInfoLinux(section)
	}
}

func buildSystemInfoLinux(section string) string {
	switch section {
	case "os":
		return "uname -a && cat /etc/os-release 2>/dev/null || true"
	case "cpu":
		return "lscpu 2>/dev/null || cat /proc/cpuinfo | head -30"
	case "memory":
		return "free -h"
	case "disk":
		return "df -h"
	case "uptime":
		return "uptime"
	default:
		return ""
	}
}

func buildSystemInfoWindows(section string) string {
	switch section {
	case "os":
		return "systeminfo | findstr /B /C:\"OS\""
	case "cpu":
		return "wmic cpu get Name,NumberOfCores,NumberOfLogicalProcessors /format:list"
	case "memory":
		return "wmic OS get TotalVisibleMemorySize,FreePhysicalMemory /format:list"
	case "disk":
		return `Get-Volume | Where-Object {$_.DriveType -eq 'Fixed'} | Select-Object DriveLetter,FileSystemLabel,@{N='TotalGB';E={[math]::Round($_.Size/1GB,2)}},@{N='FreeGB';E={[math]::Round($_.SizeRemaining/1GB,2)}},@{N='UsedPercent';E={if($_.Size -gt 0){[math]::Round(100-($_.SizeRemaining/$_.Size*100),2)}else{0}}} | Format-List`
	case "uptime":
		return "net stats workstation | findstr /B /C:\"Statistics since\""
	default:
		return ""
	}
}
