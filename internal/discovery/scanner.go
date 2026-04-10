package discovery

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/executor"
)

// Scanner performs process, port, and config-file discovery on a target host.
type Scanner struct {
	patterns []ServicePattern
}

// NewScanner creates a scanner with builtin service patterns.
func NewScanner() *Scanner {
	return &Scanner{patterns: BuiltinPatterns}
}

// Scan runs discovery on the supplied executor target.
func (s *Scanner) Scan(ctx context.Context, exec executor.Executor) (*ScanResult, error) {
	serverName := exec.Target()
	slog.Info("starting service scan", "server", serverName)

	processes, err := s.scanProcesses(ctx, exec)
	if err != nil {
		slog.Warn("process scan failed", "server", serverName, "err", err)
		processes = nil
	}

	ports, err := s.scanPorts(ctx, exec)
	if err != nil {
		slog.Warn("port scan failed", "server", serverName, "err", err)
		ports = nil
	}

	result := &ScanResult{
		ServerName: serverName,
		ScannedAt:  time.Now(),
	}

	for _, pattern := range s.patterns {
		svc := s.matchPattern(ctx, exec, pattern, processes, ports)
		if svc == nil {
			continue
		}
		svc.ServerName = serverName
		result.Services = append(result.Services, *svc)
	}

	unknowns := s.findUnknown(processes, result.Services)
	result.Services = append(result.Services, unknowns...)

	slog.Info("scan complete", "server", serverName, "found", len(result.Services))
	return result, nil
}

type processInfo struct {
	PID  int
	User string
	Comm string
	Args string
}

type portInfo struct {
	Port    int
	Process string
}

func (s *Scanner) scanProcesses(ctx context.Context, exec executor.Executor) ([]processInfo, error) {
	switch executor.CommandPlatform(exec) {
	case executor.PlatformWindows:
		return s.scanWindowsProcesses(ctx, exec)
	default:
		return s.scanUnixProcesses(ctx, exec)
	}
}

func (s *Scanner) scanUnixProcesses(ctx context.Context, exec executor.Executor) ([]processInfo, error) {
	res, err := exec.Execute(ctx, "ps -eo pid,user,comm,args --no-headers 2>/dev/null || ps -eo pid,user,comm,args 2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("execute ps: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("ps exit %d: %s", res.ExitCode, res.Stderr)
	}

	var procs []processInfo
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := parseProcessLine(line)
		if p != nil {
			procs = append(procs, *p)
		}
	}
	return procs, nil
}

func (s *Scanner) scanWindowsProcesses(ctx context.Context, exec executor.Executor) ([]processInfo, error) {
	cmd := executor.PowerShellCommand(exec, "Get-CimInstance Win32_Process | Select-Object ProcessId,Name,CommandLine | ConvertTo-Csv -NoTypeInformation")
	res, err := exec.Execute(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("execute windows process scan: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("windows process scan exit %d: %s", res.ExitCode, res.Stderr)
	}
	return parseWindowsProcessCSV(res.Stdout), nil
}

func parseProcessLine(line string) *processInfo {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return nil
	}
	return &processInfo{
		PID:  pid,
		User: fields[1],
		Comm: fields[2],
		Args: strings.Join(fields[3:], " "),
	}
}

func parseWindowsProcessCSV(output string) []processInfo {
	reader := csv.NewReader(strings.NewReader(strings.TrimSpace(output)))
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil || len(records) <= 1 {
		return nil
	}

	var procs []processInfo
	for _, row := range records[1:] {
		if len(row) < 3 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(row[0]))
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(row[1])
		args := strings.TrimSpace(row[2])
		if args == "" {
			args = comm
		}
		procs = append(procs, processInfo{
			PID:  pid,
			Comm: comm,
			Args: args,
		})
	}
	return procs
}

func (s *Scanner) scanPorts(ctx context.Context, exec executor.Executor) ([]portInfo, error) {
	switch executor.CommandPlatform(exec) {
	case executor.PlatformWindows:
		return s.scanWindowsPorts(ctx, exec)
	default:
		return s.scanUnixPorts(ctx, exec)
	}
}

func (s *Scanner) scanUnixPorts(ctx context.Context, exec executor.Executor) ([]portInfo, error) {
	res, err := exec.Execute(ctx, "ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("execute port scan: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("port scan exit %d: %s", res.ExitCode, res.Stderr)
	}
	return parsePortOutput(res.Stdout), nil
}

func (s *Scanner) scanWindowsPorts(ctx context.Context, exec executor.Executor) ([]portInfo, error) {
	res, err := exec.Execute(ctx, "netstat -ano -p tcp")
	if err != nil {
		return nil, fmt.Errorf("execute windows port scan: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("windows port scan exit %d: %s", res.ExitCode, res.Stderr)
	}
	return parsePortOutput(res.Stdout), nil
}

func parsePortOutput(output string) []portInfo {
	var ports []portInfo
	portRe := regexp.MustCompile(`:(\d+)\s`)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Active") {
			continue
		}
		upper := strings.ToUpper(line)
		if !strings.Contains(upper, "LISTEN") {
			continue
		}
		matches := portRe.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}
		port, err := strconv.Atoi(matches[1])
		if err != nil || port <= 0 {
			continue
		}
		ports = append(ports, portInfo{Port: port})
	}
	return ports
}

func (s *Scanner) matchPattern(ctx context.Context, exec executor.Executor, pattern ServicePattern, procs []processInfo, ports []portInfo) *DiscoveredService {
	re, err := regexp.Compile(pattern.ProcessRegex)
	if err != nil {
		slog.Warn("invalid pattern regex", "type", pattern.Type, "err", err)
		return nil
	}

	var confidence float64
	svc := &DiscoveredService{
		Type:    pattern.Type,
		Details: make(map[string]string),
	}

	for _, p := range procs {
		if m := re.FindStringSubmatch(p.Args); m != nil {
			confidence += WeightProcess
			svc.PID = p.PID
			svc.User = p.User
			if len(m) > 1 && m[1] != "" {
				svc.Name = m[1]
			}
			svc.Details["comm"] = p.Comm
			break
		}
	}

	for _, port := range ports {
		for _, defaultPort := range pattern.DefaultPorts {
			if port.Port == defaultPort {
				confidence += WeightPort
				svc.Port = port.Port
				break
			}
		}
	}

	for _, cfgFile := range pattern.ConfigFiles {
		res, err := exec.Execute(ctx, buildConfigFileCheckCommand(exec, cfgFile))
		if err == nil && strings.Contains(res.Stdout, "exists") {
			confidence += WeightConfigFile
			svc.Details["config_file"] = cfgFile
			break
		}
	}

	if confidence == 0 {
		return nil
	}

	svc.Confidence = confidence
	svc.Level = CalcLevel(confidence)
	return svc
}

func buildConfigFileCheckCommand(exec executor.Executor, path string) string {
	switch executor.CommandPlatform(exec) {
	case executor.PlatformWindows:
		return executor.PowerShellCommand(exec, fmt.Sprintf("if (Test-Path -LiteralPath %s) { 'exists' }", executor.QuotePowerShell(path)))
	default:
		return fmt.Sprintf("test -f %s && echo exists", executor.QuotePOSIX(path))
	}
}

func (s *Scanner) findUnknown(procs []processInfo, known []DiscoveredService) []DiscoveredService {
	knownPIDs := make(map[int]bool, len(known))
	for _, k := range known {
		knownPIDs[k.PID] = true
	}

	highResRe := regexp.MustCompile(`(?i)^(java(\.exe)?|python[23]?(\.exe)?|node(\.exe)?|ruby(\.exe)?|perl(\.exe)?)$`)
	var unknowns []DiscoveredService

	for _, p := range procs {
		if knownPIDs[p.PID] {
			continue
		}
		if !highResRe.MatchString(p.Comm) {
			continue
		}
		unknowns = append(unknowns, DiscoveredService{
			Type:       ServiceUnknown,
			PID:        p.PID,
			User:       p.User,
			Confidence: 0.1,
			Level:      ConfidenceLow,
			Details: map[string]string{
				"comm": p.Comm,
				"args": truncate(p.Args, 120),
			},
		})
	}
	return unknowns
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
