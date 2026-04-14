// Package agent
// File: safety.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/infractl/internal/safety"
	"github.com/yourorg/infractl/internal/tools"
)

type safetyDecision struct {
	RiskLevel tools.RiskLevel
}

// evaluateToolSafety derives the effective risk level from tool arguments.
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
			return decision
		}

		decision.RiskLevel = llmLevel
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

// runConfirmationFlow performs confirmation steps for medium/high-risk actions using QuestionHandler.
func runConfirmationFlow(ctx context.Context, handler QuestionHandler, tool tools.Tool, target string, args map[string]interface{}, riskLevel tools.RiskLevel) (bool, error) {
	if handler == nil {
		// 핸들러 없음(테스트·임베디드 사용) — 위험도와 무관하게 실행 허용
		slog.Warn("no confirmation handler, allowing action", "tool", tool.Name(), "risk", riskLevel)
		return true, nil
	}

	desc := buildDescription(tool, target, args)
	question := fmt.Sprintf("[%s] 위험 작업을 실행하시겠습니까?", riskLevel)
	header := "⚠  Security Confirm"
	if riskLevel == tools.RiskHigh {
		question = fmt.Sprintf("⚠️ [HIGH RISK] %s", question)
		header = "⚠  HIGH RISK — Security Confirm"
	}

	resp, err := handler.RequestQuestion(ctx, tools.QuestionRequest{
		Question: question,
		Header:   header,
		Options: []tools.QuestionOption{
			{
				Label:       "실행 (Execute)",
				Description: fmt.Sprintf("%s: %s", tool.Name(), desc),
			},
			{
				Label:       "취소 (Abort)",
				Description: "작업을 중단하고 다음으로 넘어갑니다.",
			},
		},
	})
	if err != nil {
		return false, err
	}

	// 첫 번째 선택지(Index 0)가 '실행'임
	return !resp.IsOther && resp.SelectedIndex == 0, nil
}

func buildDescription(tool tools.Tool, target string, args map[string]interface{}) string {
	if target == "" || target == "localhost" {
		target = "localhost"
	}
	if desc, ok := args["description"].(string); ok && strings.TrimSpace(desc) != "" {
		return fmt.Sprintf("[%s] %s", target, strings.TrimSpace(desc))
	}
	if cmd, ok := args["command"].(string); ok && cmd != "" {
		return fmt.Sprintf("[%s] %s", target, cmd)
	}
	return fmt.Sprintf("[%s] %s", target, tool.Name())
}
