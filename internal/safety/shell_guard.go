package safety

import (
	"regexp"

	"github.com/yourorg/infractl/internal/tools"
)

// ShellRiskOverride is the risk correction derived from the raw shell command.
type ShellRiskOverride struct {
	MinLevel    tools.RiskLevel
	NeedsBackup bool
	Reason      string
}

type riskPattern struct {
	re          *regexp.Regexp
	minLevel    tools.RiskLevel
	needsBackup bool
	reason      string
}

var patterns = []riskPattern{
	{
		re:          regexp.MustCompile(`(?i)\brm\s+(-[^\n]*[rR]|--recursive|--force)`),
		minLevel:    tools.RiskHigh,
		needsBackup: true,
		reason:      "recursive or forced deletion",
	},
	{
		re:          regexp.MustCompile(`(?i)\brm\s+\S+`),
		minLevel:    tools.RiskMedium,
		needsBackup: true,
		reason:      "file deletion",
	},
	{
		re:          regexp.MustCompile(`(?i)\b(remove-item|del|erase|rmdir|rd)\b`),
		minLevel:    tools.RiskHigh,
		needsBackup: true,
		reason:      "windows deletion command",
	},
	{
		re:          regexp.MustCompile(`(?i)\btruncate\b`),
		minLevel:    tools.RiskHigh,
		needsBackup: true,
		reason:      "file truncation",
	},
	{
		re:          regexp.MustCompile(`(?i)\bsed\b[^\n]*\s-i(\s|$)`),
		minLevel:    tools.RiskMedium,
		needsBackup: true,
		reason:      "in-place file edit",
	},
	{
		re:          regexp.MustCompile(`(?i)\bperl\b[^\n]*-pi(\s|$)`),
		minLevel:    tools.RiskMedium,
		needsBackup: true,
		reason:      "in-place file edit",
	},
	{
		re:          regexp.MustCompile(`(?i)\bdd\b.*\bof=`),
		minLevel:    tools.RiskHigh,
		needsBackup: false,
		reason:      "block device write",
	},
	{
		re:          regexp.MustCompile(`(?i)\bmkfs\b`),
		minLevel:    tools.RiskHigh,
		needsBackup: false,
		reason:      "filesystem format",
	},
	{
		re:          regexp.MustCompile(`(?i)\b(fdisk|gdisk|parted)\b`),
		minLevel:    tools.RiskHigh,
		needsBackup: false,
		reason:      "disk partition operation",
	},
	{
		re:          regexp.MustCompile(`(?i)\b(shred|wipefs)\b`),
		minLevel:    tools.RiskHigh,
		needsBackup: true,
		reason:      "secure erase",
	},
	{
		re:          regexp.MustCompile(`>\s*/dev/(sd|vd|xvd|nvme)`),
		minLevel:    tools.RiskHigh,
		needsBackup: false,
		reason:      "direct disk write",
	},
	{
		re:          regexp.MustCompile(`(?i)\bkill\s+-9\b`),
		minLevel:    tools.RiskMedium,
		needsBackup: false,
		reason:      "force process termination",
	},
	{
		re:          regexp.MustCompile(`(?i)\bsystemctl\s+(stop|disable|restart)\b`),
		minLevel:    tools.RiskLow,
		needsBackup: false,
		reason:      "service lifecycle change",
	},
}

// EnforceRisk matches the command against destructive patterns and raises the minimum risk level.
func EnforceRisk(command string, llmAssessed tools.RiskLevel) ShellRiskOverride {
	result := ShellRiskOverride{MinLevel: llmAssessed}

	for _, p := range patterns {
		if !p.re.MatchString(command) {
			continue
		}
		if tools.RiskOrd(p.minLevel) > tools.RiskOrd(result.MinLevel) {
			result.MinLevel = p.minLevel
			result.Reason = p.reason
		}
		if p.needsBackup {
			result.NeedsBackup = true
		}
	}

	return result
}
