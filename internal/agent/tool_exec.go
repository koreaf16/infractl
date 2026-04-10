package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/infractl/internal/hooks"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/tools"
)

type indexedToolCall struct {
	call          llm.ToolCall
	originalIndex int
}

func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []llm.ToolCall) []llm.Message {
	results := make([]llm.Message, len(toolCalls))

	readOnly, mutation := a.partitionToolCalls(toolCalls)
	if len(readOnly) > 0 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, item := range readOnly {
			item := item
			wg.Add(1)
			go func() {
				defer wg.Done()
				result := a.executeSingleTool(ctx, item.call)
				mu.Lock()
				results[item.originalIndex] = result
				mu.Unlock()
			}()
		}
		wg.Wait()
	}
	for _, item := range mutation {
		results[item.originalIndex] = a.executeSingleTool(ctx, item.call)
	}

	return results
}
func (a *Agent) partitionToolCalls(toolCalls []llm.ToolCall) (readOnly []indexedToolCall, mutation []indexedToolCall) {
	for i, tc := range toolCalls {
		item := indexedToolCall{call: tc, originalIndex: i}
		tool, ok := a.registry.Get(tc.Function.Name)
		if ok && tool.IsReadOnly() {
			readOnly = append(readOnly, item)
		} else {
			mutation = append(mutation, item)
		}
	}
	return
}
func (a *Agent) executeSingleTool(ctx context.Context, tc llm.ToolCall) llm.Message {
	tool, ok := a.registry.Get(tc.Function.Name)
	if !ok {
		slog.Warn("unknown tool called", "name", tc.Function.Name)
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("Error: unknown tool '%s'", tc.Function.Name),
		}
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		slog.Warn("tool argument parse error", "tool", tc.Function.Name, "err", err)
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("Error: failed to parse arguments: %s", err),
		}
	}

	target := tools.ExtractTarget(args)
	if tc.Function.Name == "checkpoint_rollback" {
		if rollbackServer, ok := args["server"].(string); ok && strings.TrimSpace(rollbackServer) != "" {
			target = rollbackServer
		}
	}
	if target == "" && a.connectorMgr != nil {
		target = a.connectorMgr.DefaultTargetForTool(tc.Function.Name)
	}
	if target == "" && a.activeServer != nil {
		target = a.activeServer.Name
	}

	exec, err := a.manager.Get(target)
	if err != nil {
		slog.Warn("executor not found", "target", target, "err", err)
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("Error: %s", err),
		}
	}
	if normalizedTarget := exec.Target(); normalizedTarget != "" {
		target = normalizedTarget
	}
	safetyDecision := evaluateToolSafety(tool, args)
	riskLevel := safetyDecision.RiskLevel
	if safetyDecision.BackupRequired {
		preBackup, _ := args["pre_backup_command"].(string)
		if strings.TrimSpace(preBackup) == "" {
			reason := safetyDecision.BackupReason
			if reason == "" {
				reason = "destructive shell command"
			}
			return llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Error: pre_backup_command is required before executing this command (%s).", reason),
			}
		}
	}
	if riskLevel != tools.RiskNone {
		if a.yoroMode {
			slog.Warn("YORO mode: skipping confirmation",
				"tool", tc.Function.Name, "risk", riskLevel, "target", target)
		} else {
			confirmed, confirmErr := runConfirmationFlow(ctx, a.confirmHandler, tool, target, args, riskLevel)
			if confirmErr != nil {
				slog.Warn("confirmation flow error", "tool", tc.Function.Name, "err", confirmErr)
			}
			if !confirmed {
				return llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: tc.ID,
					Content:    "?ъ슜?먭? ?묒뾽??痍⑥냼?덉뒿?덈떎.",
				}
			}
		}
	}
	if a.hooksMgr != nil {
		a.hooksMgr.Fire(ctx, hooks.HookContext{
			Event:    hooks.EventBeforeExecute,
			ToolName: tc.Function.Name,
			Server:   target,
			Args:     argsToStrings(args),
		})
	}
	if a.checkpointMgr != nil && !tool.IsReadOnly() {
		if tools.RiskOrd(riskLevel) >= tools.RiskOrd(tools.RiskMedium) {
			a.checkpointMgr.CreateMandatory(ctx, target, tc.Function.Name, args, riskLevel)
		} else {
			a.checkpointMgr.CreateFromArgs(ctx, target, tc.Function.Name, args)
		}
	}
	if a.idleHandler != nil {
		exec = wrapWithIdleDetect(exec, a.idleHandler, tc.Function.Name, target)
	}
	if st, ok := tool.(*tools.ShellExecTool); ok {
		toolID := tc.ID
		st.OutputCb = func(line string) {
			a.handler.OnToolOutput(toolID, line)
		}
	}

	a.handler.OnToolStart(tc.ID, tc.Function.Name, target, args)
	start := time.Now()

	resultStr, err := tool.Execute(ctx, args, exec)
	duration := time.Since(start)

	if err != nil {
		if a.hooksMgr != nil {
			a.hooksMgr.Fire(ctx, hooks.HookContext{
				Event:    hooks.EventOnError,
				ToolName: tc.Function.Name,
				Server:   target,
				Error:    err.Error(),
			})
		}
		a.handler.OnToolEnd(tc.ID, tc.Function.Name, err.Error(), duration, false)
		a.recordExecLog(ctx, execContext{
			toolName:  tc.Function.Name,
			target:    target,
			args:      args,
			errMsg:    err.Error(),
			duration:  duration,
			riskLevel: riskLevel,
			success:   false,
		})
		content := fmt.Sprintf("Error: %s", err)
		content += a.buildErrorHints()
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    content,
		}
	}

	a.handler.OnToolEnd(tc.ID, tc.Function.Name, resultStr, duration, true)
	logID := a.recordExecLog(ctx, execContext{
		toolName:  tc.Function.Name,
		target:    target,
		args:      args,
		output:    resultStr,
		duration:  duration,
		riskLevel: riskLevel,
		success:   true,
	})
	if a.knowledgeLearner != nil && logID > 0 {
		a.knowledgeLearner.TriggerLearn(ctx, logID)
	}
	if a.hooksMgr != nil {
		a.hooksMgr.Fire(ctx, hooks.HookContext{
			Event:    hooks.EventAfterExecute,
			ToolName: tc.Function.Name,
			Server:   target,
			Output:   resultStr,
		})
	}
	contentStr := resultStr
	if len(contentStr) > 4000 {
		contentStr = SaveLargeOutput(contentStr)
	}

	return llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: tc.ID,
		Content:    contentStr,
	}
}
func argsToStrings(args map[string]interface{}) map[string]string {
	result := make(map[string]string, len(args))
	for k, v := range args {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}
func (a *Agent) buildErrorHints() string {
	var hints []string
	if a.registry.Has("knowledge_search") {
		hints = append(hints, "Use knowledge_search to check if this error has been resolved before.")
	}
	if a.registry.Has("web_search") {
		hints = append(hints, "Use web_search to find solutions for this error online.")
	}
	if len(hints) == 0 {
		return ""
	}
	result := "\n\n[Suggested next steps]"
	for _, h := range hints {
		result += "\n- " + h
	}
	return result
}
