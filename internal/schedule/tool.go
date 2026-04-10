// Package schedule
// File: tool.go
// Description: 스케줄 생성 및 목록 조회 LLM 도구 구현
// Responsibility: LLM이 자연어로 정기 실행 스케줄을 등록하고 조회하는 도구 제공

package schedule

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

// CreateTool은 스케줄을 등록하는 LLM 도구이다.
type CreateTool struct {
	Scheduler *Scheduler
}

func (t *CreateTool) Name() string { return "schedule_create" }
func (t *CreateTool) Description() string {
	return "정기 실행 스케줄을 등록한다. cron 표현식으로 실행 주기를 지정하고 에이전트 프롬프트를 저장한다."
}
func (t *CreateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "스케줄 이름 (고유해야 함)",
			},
			"cron_expr": map[string]interface{}{
				"type":        "string",
				"description": "cron 표현식 (예: '0 9 * * *' = 매일 09:00)",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "스케줄 실행 시 에이전트에게 전달할 프롬프트",
			},
		},
		"required": []string{"name", "cron_expr", "prompt"},
	}
}
func (t *CreateTool) RiskLevel() tools.RiskLevel { return tools.RiskLow }
func (t *CreateTool) IsReadOnly() bool            { return false }
func (t *CreateTool) IsEnabled() bool             { return true }

func (t *CreateTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	name, _ := args["name"].(string)
	cronExpr, _ := args["cron_expr"].(string)
	prompt, _ := args["prompt"].(string)

	if name == "" || cronExpr == "" || prompt == "" {
		return "", fmt.Errorf("name, cron_expr, prompt는 필수입니다")
	}

	id, err := t.Scheduler.Add(ctx, store.Schedule{
		Name:     name,
		CronExpr: cronExpr,
		Prompt:   prompt,
		Enabled:  true,
	})
	if err != nil {
		return "", fmt.Errorf("스케줄 등록: %w", err)
	}
	return fmt.Sprintf("✓ 스케줄 #%d '%s' 등록 완료\n주기: %s\n프롬프트: %s",
		id, name, cronExpr, prompt), nil
}

// ListTool은 스케줄 목록을 조회하는 LLM 도구이다.
type ListTool struct {
	Scheduler *Scheduler
}

func (t *ListTool) Name() string { return "schedule_list" }
func (t *ListTool) Description() string {
	return "등록된 정기 실행 스케줄 목록을 반환한다."
}
func (t *ListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (t *ListTool) RiskLevel() tools.RiskLevel { return tools.RiskNone }
func (t *ListTool) IsReadOnly() bool            { return true }
func (t *ListTool) IsEnabled() bool             { return true }

func (t *ListTool) Execute(ctx context.Context, _ map[string]interface{}, _ executor.Executor) (string, error) {
	schedules, err := t.Scheduler.List(ctx)
	if err != nil {
		return "", fmt.Errorf("스케줄 목록 조회: %w", err)
	}
	if len(schedules) == 0 {
		return "등록된 스케줄 없음.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("스케줄 목록 (%d개):\n", len(schedules)))
	for _, s := range schedules {
		status := "✓"
		if !s.Enabled {
			status = "✗"
		}
		lastRun := "없음"
		if s.LastRun != nil {
			lastRun = s.LastRun.Format("2006-01-02 15:04:05")
		}
		sb.WriteString(fmt.Sprintf("  %s #%d [%s] %s — 마지막: %s\n",
			status, s.ID, s.CronExpr, s.Name, lastRun))
	}
	return sb.String(), nil
}
