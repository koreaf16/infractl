package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/safety"
	"github.com/yourorg/infractl/internal/tools"
)

type safetyDecision struct {
	RiskLevel      tools.RiskLevel
	BackupRequired bool
	BackupReason   string
}

// evaluateToolSafety derives effective risk and backup requirements from tool arguments.
func evaluateToolSafety(tool tools.Tool, args map[string]interface{}) safetyDecision {
	decision := safetyDecision{RiskLevel: tool.RiskLevel()}

	if tool.Name() == "shell_exec" {
		var llmLevel tools.RiskLevel
		if raw, ok := args["risk_assessment"].(string); ok {
			switch tools.RiskLevel(strings.TrimSpace(raw)) {
			case tools.RiskNone, tools.RiskLow, tools.RiskMedium, tools.RiskHigh:
				llmLevel = tools.RiskLevel(strings.TrimSpace(raw))
			}
		}

		if cmd, ok := args["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			override := safety.EnforceRisk(cmd, llmLevel)
			decision.RiskLevel = override.MinLevel
			if override.NeedsBackup || decision.RiskLevel == tools.RiskHigh {
				decision.BackupRequired = true
				decision.BackupReason = override.Reason
			}
			return decision
		}

		decision.RiskLevel = llmLevel
		if decision.RiskLevel == tools.RiskHigh {
			decision.BackupRequired = true
		}
		return decision
	}

	if strings.HasSuffix(tool.Name(), ".query") {
		if sql, ok := args["sql"].(string); ok && strings.TrimSpace(sql) != "" {
			decision.RiskLevel = safety.ClassifySQL(sql).Level
			return decision
		}
		decision.RiskLevel = tools.RiskLow
		return decision
	}

	if tool.Name() == "file_write" {
		if path, ok := args["path"].(string); ok && strings.TrimSpace(path) != "" {
			if isProtectedPath(path) {
				decision.RiskLevel = tools.RiskMedium
			}
		}
	}

	return decision
}

func isProtectedPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	systemPrefixes := []string{
		"/etc/", "/usr/", "/opt/", "/boot/", "/var/", "/bin/", "/sbin/", "/lib/",
		"c:\\windows\\", "c:\\program files\\", "c:\\program files (x86)\\", "c:\\programdata\\",
	}
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// runConfirmationFlow performs confirmation steps for medium/high-risk actions.
func runConfirmationFlow(ctx context.Context, handler ConfirmationHandler, tool tools.Tool, target string, args map[string]interface{}, riskLevel tools.RiskLevel) (bool, error) {
	if handler == nil {
		if tools.RiskOrd(riskLevel) >= tools.RiskOrd(tools.RiskMedium) {
			return false, nil
		}
		return true, nil
	}

	desc := buildDescription(tool, target, args)

	switch riskLevel {
	case tools.RiskNone, tools.RiskLow:
		return true, nil
	case tools.RiskMedium, tools.RiskHigh:
		resp, err := handler.RequestConfirm(ctx, ConfirmRequest{
			RiskLevel:   riskLevel,
			ToolName:    tool.Name(),
			Target:      target,
			Description: desc,
			Step:        1,
			TotalSteps:  1,
		})
		if err != nil {
			return false, err
		}
		return resp.Confirmed, nil
	default:
		return true, nil
	}
}

func buildDescription(tool tools.Tool, target string, args map[string]interface{}) string {
	if target == "" || target == "localhost" {
		target = "localhost"
	}
	if cmd, ok := args["command"].(string); ok && cmd != "" {
		return fmt.Sprintf("[%s] %s", target, cmd)
	}
	return fmt.Sprintf("[%s] %s", target, tool.Name())
}
