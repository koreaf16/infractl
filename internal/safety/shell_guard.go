// Package safety
// File: shell_guard.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package safety

import (
	"regexp"

	"github.com/yourorg/infractl/internal/tools"
)

// ShellRiskOverride is the risk correction derived from the raw shell command.
type ShellRiskOverride struct {
	MinLevel tools.RiskLevel
	Reason   string
}

type riskPattern struct {
	re       *regexp.Regexp
	minLevel tools.RiskLevel
	reason   string
}

var patterns = []riskPattern{
	{
		re:       regexp.MustCompile(`(?i)\brm\s+(-[^\n]*[rR]|--recursive|--force)`),
		minLevel: tools.RiskHigh,
		reason:   "recursive or forced deletion",
	},
	{
		re:       regexp.MustCompile(`(?i)\brm\s+\S+`),
		minLevel: tools.RiskMedium,
		reason:   "file deletion",
	},
	{
		re:       regexp.MustCompile(`(?i)\b(remove-item|del|erase|rmdir|rd)\b`),
		minLevel: tools.RiskHigh,
		reason:   "windows deletion command",
	},
	{
		re:       regexp.MustCompile(`(?i)\btruncate\b`),
		minLevel: tools.RiskHigh,
		reason:   "file truncation",
	},
	{
		re:       regexp.MustCompile(`(?i)\bsed\b[^\n]*\s-i(\s|$)`),
		minLevel: tools.RiskMedium,
		reason:   "in-place file edit",
	},
	{
		re:       regexp.MustCompile(`(?i)\bperl\b[^\n]*-pi(\s|$)`),
		minLevel: tools.RiskMedium,
		reason:   "in-place file edit",
	},
	{
		re:       regexp.MustCompile(`(?i)\bdd\b.*\bof=`),
		minLevel: tools.RiskHigh,
		reason:   "block device write",
	},
	{
		re:       regexp.MustCompile(`(?i)\bmkfs\b`),
		minLevel: tools.RiskHigh,
		reason:   "filesystem format",
	},
	{
		re:       regexp.MustCompile(`(?i)\b(fdisk|gdisk|parted)\b`),
		minLevel: tools.RiskHigh,
		reason:   "disk partition operation",
	},
	{
		re:       regexp.MustCompile(`(?i)\b(shred|wipefs)\b`),
		minLevel: tools.RiskHigh,
		reason:   "secure erase",
	},
	{
		re:       regexp.MustCompile(`>\s*/dev/(sd|vd|xvd|nvme)`),
		minLevel: tools.RiskHigh,
		reason:   "direct disk write",
	},
	{
		re:       regexp.MustCompile(`(?i)\bkill\s+-9\b`),
		minLevel: tools.RiskMedium,
		reason:   "force process termination",
	},
	{
		re:       regexp.MustCompile(`(?i)\bsystemctl\s+(stop|disable|restart)\b`),
		minLevel: tools.RiskLow,
		reason:   "service lifecycle change",
	},
}

var safePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(nvidia-smi|ls|grep|cat|head|tail|ps|top|htop|df|du|uptime|uname|hostname|id|who|which|whereis|file|stat|find|awk|sed)\b`),
	regexp.MustCompile(`(?i)^[a-zA-Z0-9_\-./]+\s+--version$`),
	regexp.MustCompile(`(?i)^[a-zA-Z0-9_\-./]+\s+--help$`),
	regexp.MustCompile(`(?i)^[a-zA-Z0-9_\-./]+\s+-h$`),
}

// EnforceRisk matches the command against destructive patterns and raises the minimum risk level.
func EnforceRisk(command string, llmAssessed tools.RiskLevel) ShellRiskOverride {
	result := ShellRiskOverride{MinLevel: llmAssessed}

	// 1단계: 명시적인 안전 패턴 확인 (이미 정교한 패턴으로 분류된 경우)
	if llmAssessed == tools.RiskNone {
		for _, re := range safePatterns {
			if re.MatchString(command) {
				return result // RiskNone 유지
			}
		}
	}

	// 2단계: 위험 패턴 확인 및 최소 위험도 선언
	for _, p := range patterns {
		if !p.re.MatchString(command) {
			continue
		}
		if tools.RiskOrd(p.minLevel) > tools.RiskOrd(result.MinLevel) {
			result.MinLevel = p.minLevel
			result.Reason = p.reason
		}
	}
	if reason := InstallContextReason(command); reason != "" {
		if tools.RiskOrd(tools.RiskMedium) > tools.RiskOrd(result.MinLevel) {
			result.MinLevel = tools.RiskMedium
			if result.Reason == "" {
				result.Reason = reason
			}
		}
	}

	return result
}
