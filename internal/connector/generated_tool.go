// Package connector
// File: generated_tool.go
// Description: 구조화된 결과를 반환하는 synthesized tool 구현
// Responsibility: profile synthesis 결과를 ToolResult까지 보존

package connector

import (
	"context"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/tools"
)

// GeneratedTool은 profile synthesis가 만든 tool이다.
type GeneratedTool struct {
	name        string
	description string
	params      map[string]interface{}
	riskLevel   tools.RiskLevel
	readOnly    bool
	status      *ConnectorStatus
	executeFn   func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (tools.ToolResult, error)
}

// NewGeneratedTool은 structured result를 반환하는 tool을 생성한다.
func NewGeneratedTool(
	name, description string,
	params map[string]interface{},
	riskLevel tools.RiskLevel,
	readOnly bool,
	status *ConnectorStatus,
	executeFn func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (tools.ToolResult, error),
) *GeneratedTool {
	return &GeneratedTool{
		name:        name,
		description: description,
		params:      params,
		riskLevel:   riskLevel,
		readOnly:    readOnly,
		status:      status,
		executeFn:   executeFn,
	}
}

func (t *GeneratedTool) Name() string                       { return t.name }
func (t *GeneratedTool) Description() string                { return t.description }
func (t *GeneratedTool) Parameters() map[string]interface{} { return t.params }
func (t *GeneratedTool) RiskLevel() tools.RiskLevel         { return t.riskLevel }
func (t *GeneratedTool) IsReadOnly() bool                   { return t.readOnly }

func (t *GeneratedTool) IsEnabled() bool {
	if t.status == nil {
		return false
	}
	return *t.status == StatusConnected
}

func (t *GeneratedTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	result, err := t.ExecuteResult(ctx, args, exec)
	if err != nil {
		return "", err
	}
	return result.DisplayString(), nil
}

func (t *GeneratedTool) ExecuteResult(ctx context.Context, args map[string]interface{}, exec executor.Executor) (tools.ToolResult, error) {
	return t.executeFn(ctx, args, exec)
}
