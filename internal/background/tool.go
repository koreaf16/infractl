// Package background
// File: tool.go
// Description: 백그라운드 작업 관리 LLM 도구 구현
// Responsibility: 작업 목록/결과/취소를 LLM이 자연어로 처리할 수 있도록 도구 제공

package background

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/tools"
)

// ManageTool은 백그라운드 작업을 관리하는 LLM 도구이다.
// action 파라미터로 list/result/cancel 서브커맨드를 처리한다.
type ManageTool struct {
	Manager *Manager
}

func (t *ManageTool) Name() string { return "background_jobs" }

func (t *ManageTool) Description() string {
	return "백그라운드 작업을 관리한다. action=list: 전체 목록, action=result: 특정 작업 결과, action=cancel: 작업 취소."
}

func (t *ManageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "result", "cancel"},
				"description": "수행할 작업 (list: 목록, result: 결과 조회, cancel: 취소)",
			},
			"job_id": map[string]interface{}{
				"type":        "integer",
				"description": "대상 작업 ID (result, cancel에서 필요)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ManageTool) RiskLevel() tools.RiskLevel { return tools.RiskNone }
func (t *ManageTool) IsReadOnly() bool            { return true }
func (t *ManageTool) IsEnabled() bool             { return true }

func (t *ManageTool) Execute(_ context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	action, _ := args["action"].(string)

	switch action {
	case "list":
		return t.handleList()
	case "result":
		id, err := extractID(args)
		if err != nil {
			return "", err
		}
		return t.handleResult(id)
	case "cancel":
		id, err := extractID(args)
		if err != nil {
			return "", err
		}
		return t.handleCancel(id)
	default:
		return "", fmt.Errorf("알 수 없는 action: %s", action)
	}
}

func (t *ManageTool) handleList() (string, error) {
	jobs := t.Manager.List()
	if len(jobs) == 0 {
		return "실행 중이거나 완료된 백그라운드 작업이 없습니다.", nil
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })

	var sb strings.Builder
	sb.WriteString("백그라운드 작업 목록:\n")
	for _, j := range jobs {
		icon := "⏳"
		switch j.Status {
		case StatusCompleted:
			icon = "✓"
		case StatusFailed:
			icon = "✗"
		case StatusCancelled:
			icon = "⊘"
		}
		sb.WriteString(fmt.Sprintf("  #%d %s [%s] %s\n", j.ID, icon, j.Status, j.Description))
	}
	return sb.String(), nil
}

func (t *ManageTool) handleResult(id int) (string, error) {
	j, err := t.Manager.Get(id)
	if err != nil {
		return "", err
	}
	if j.Status == StatusRunning {
		return fmt.Sprintf("작업 #%d '%s' 는 아직 실행 중입니다.", id, j.Description), nil
	}
	if j.Error != "" {
		return fmt.Sprintf("작업 #%d 실패:\n%s", id, j.Error), nil
	}
	if j.Result == "" {
		return fmt.Sprintf("작업 #%d '%s' 완료 (결과 없음)", id, j.Description), nil
	}
	return fmt.Sprintf("작업 #%d '%s' 결과:\n%s", id, j.Description, j.Result), nil
}

func (t *ManageTool) handleCancel(id int) (string, error) {
	if err := t.Manager.Cancel(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("작업 #%d 취소 완료.", id), nil
}

func extractID(args map[string]interface{}) (int, error) {
	v, ok := args["job_id"]
	if !ok {
		return 0, fmt.Errorf("job_id 파라미터가 필요합니다")
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	}
	return 0, fmt.Errorf("job_id는 정수여야 합니다")
}
