// Package agent
// File: tool_exec.go
// Description: 도구 호출 실행 라우터 — LLM 툴콜을 실제 도구로 디스패치
// Responsibility: tool call 파싱, 실행기 선택, 병렬/직렬 실행, 결과 변환

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/hooks"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/pipeline"
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

		isReadOnly := false
		if ok && tool.IsReadOnly() {
			isReadOnly = true
		}

		if isReadOnly {
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

		// Claude CLI의 validateInput() 패턴:
		// 타깃이 서버인지 서비스인지 LLM이 구분할 수 있도록
		// 등록된 서버 목록 + 활성 서비스 목록 + 학습된 서비스 목록을 함께 반환.
		// LLM이 이 정보를 바탕으로 ask_user_question을 호출하게 된다.

		// 1) 등록된 SSH 서버 목록
		var knownServers []string
		if a.store != nil {
			if servers, listErr := a.store.List(ctx); listErr == nil {
				for _, srv := range servers {
					knownServers = append(knownServers, srv.Name)
				}
			}
		}

		// 2) 활성 커넥터(서비스) 목록 — Oracle SID, MySQL DB명 등
		type svcEntry struct{ desc, server string }
		var knownServices []svcEntry
		if a.connectorMgr != nil {
			for _, cs := range a.connectorMgr.States() {
				knownServices = append(knownServices, svcEntry{
					desc:   fmt.Sprintf("%s/%s", cs.Type, cs.ServiceName),
					server: cs.ServerName,
				})
			}
		}

		// 3) 학습된 서비스 목록 (adaptiveLearner)
		if a.adaptiveLearner != nil {
			for _, sys := range a.adaptiveLearner.ListSystems(ctx) {
				knownServices = append(knownServices, svcEntry{
					desc:   sys.ServiceType,
					server: sys.ServerName,
				})
			}
		}

		var sb strings.Builder
		if target == "" {
			sb.WriteString("Error: no target server is set. Use server_focus or specify a target.")
		} else {
			sb.WriteString(fmt.Sprintf("Error: '%s' is not a registered server name.\n", target))
			if len(knownServers) > 0 {
				sb.WriteString(fmt.Sprintf("  Registered servers (SSH targets): %s\n", strings.Join(knownServers, ", ")))
			} else {
				sb.WriteString("  No servers are registered.\n")
			}
			if len(knownServices) > 0 {
				sb.WriteString("  Active/known services on those servers:\n")
				for _, svc := range knownServices {
					sb.WriteString(fmt.Sprintf("    - %s  (on server: %s)\n", svc.desc, svc.server))
				}
				sb.WriteString("  NOTE: If the user mentioned a service/DB instance name (e.g. Oracle SID, MySQL database),\n")
				sb.WriteString("        it is NOT a server — it runs ON a registered server above.\n")
			}
			sb.WriteString("Call ask_user_question to clarify:\n")
			sb.WriteString("  (a) which registered SERVER the user wants to connect to, OR\n")
			sb.WriteString("  (b) which SERVICE/INSTANCE on an existing server they meant.\n")
		}
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    sb.String(),
		}
	}
	if normalizedTarget := exec.Target(); normalizedTarget != "" {
		target = normalizedTarget
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
		a.checkpointMgr.CreateFromArgs(ctx, target, tc.Function.Name, args)
	}
	if a.idleHandler != nil {
		exec = wrapWithIdleDetect(exec, a.idleHandler, tc.Function.Name, target)
	}
	if st, ok := tool.(*tools.ShellExecTool); ok {
		toolID := tc.ID
		st.OutputCb = func(line string) {
			a.handler.OnToolOutput(toolID, line)
		}

		// check for background execution
		if isBackground, ok := args["is_background"].(bool); ok && isBackground && a.bgManager != nil {
			jobID := a.bgManager.Submit(ctx, fmt.Sprintf("shell_exec: %s", tc.Function.Arguments), func(ctx context.Context) (string, error) {
				return tool.Execute(ctx, args, exec)
			})
			return llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Command started in background as task #%d", jobID),
			}
		}
	}
	if ct, ok := tool.(interface{ SetOutputCb(func(string)) }); ok {
		ct.SetOutputCb(func(line string) {
			a.handler.OnToolOutput(tc.ID, line)
		})
	}
	if ft, ok := tool.(*tools.FileTransferTool); ok {
		toolID := tc.ID
		ft.OutputCb = func(line string) {
			a.handler.OnToolOutput(toolID, line)
		}
	}
	if wf, ok := tool.(*tools.WebFetchTool); ok {
		toolID := tc.ID
		wf.OutputCb = func(line string) {
			a.handler.OnToolOutput(toolID, line)
		}
	}
	if aq, ok := tool.(*tools.AskUserQuestionTool); ok && a.questionHandler != nil {
		aq.QuestionCb = func(ctx context.Context, req tools.QuestionRequest) (tools.QuestionResponse, error) {
			return a.questionHandler.RequestQuestion(ctx, req)
		}
		aq.FormCb = func(ctx context.Context, req tools.FormRequest) (tools.FormResponse, error) {
			return a.questionHandler.RequestForm(ctx, req)
		}
	}

	a.handler.OnToolStart(tc.ID, tc.Function.Name, target, args)
	start := time.Now()

	outcome, err := executeToolWithOutcome(ctx, tool, args, exec)
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
		logID := a.recordExecLog(ctx, execContext{
			toolName: tc.Function.Name,
			target:   target,
			args:     args,
			errMsg:   err.Error(),
			exitCode: 1,
			duration: duration,
			success:  false,
		})
		if a.taskMemoryLearner != nil && logID > 0 {
			a.taskMemoryLearner.TriggerRecord(ctx, logID)
		}
		content := fmt.Sprintf("Error: %s", err)
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    content,
		}
	}

	resultStr := outcome.Content
	if !outcome.Success {
		failMsg := outcome.ErrorMessage
		if strings.TrimSpace(failMsg) == "" {
			failMsg = fmt.Sprintf("tool %s reported failure", tc.Function.Name)
		}
		a.handler.OnToolEnd(tc.ID, tc.Function.Name, resultStr, duration, false)
		logID := a.recordExecLog(ctx, execContext{
			toolName: tc.Function.Name,
			target:   target,
			args:     args,
			output:   resultStr,
			errMsg:   failMsg,
			exitCode: outcome.ExitCode,
			duration: duration,
			success:  false,
		})
		if a.taskMemoryLearner != nil && logID > 0 {
			a.taskMemoryLearner.TriggerRecord(ctx, logID)
		}
	} else {
		a.handler.OnToolEnd(tc.ID, tc.Function.Name, resultStr, duration, true)
		logID := a.recordExecLog(ctx, execContext{
			toolName: tc.Function.Name,
			target:   target,
			args:     args,
			output:   resultStr,
			exitCode: outcome.ExitCode,
			duration: duration,
			success:  true,
		})
		if a.knowledgeLearner != nil && logID > 0 {
			a.knowledgeLearner.TriggerLearn(ctx, logID)
		}
		if a.taskMemoryLearner != nil && logID > 0 {
			a.taskMemoryLearner.TriggerRecord(ctx, logID)
		}
	}
	if a.hooksMgr != nil {
		a.hooksMgr.Fire(ctx, hooks.HookContext{
			Event:    hooks.EventAfterExecute,
			ToolName: tc.Function.Name,
			Server:   target,
			Output:   resultStr,
		})
	}
	// ?�트 ?�캔 결과??well-known ?�트 주석 추�? (LLM 추측 기반 ?�별 방�?)
	resultStr = annotatePortOutput(tc.Function.Name, resultStr)
	// ?�구�??�기 ?�한 ?�용: head + tail 보존, CLI ?�이�??�거
	limit := toolResultLimit(tc.Function.Name)
	contentStr := pipeline.NewPreprocessor(pipeline.WithMaxLLMBytes(limit)).Process(resultStr)
	if len(contentStr) > limit {
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

func executeToolWithOutcome(
	ctx context.Context,
	tool tools.Tool,
	args map[string]interface{},
	exec executor.Executor,
) (tools.ToolOutcome, error) {
	if dt, ok := tool.(tools.DetailedTool); ok {
		return dt.ExecuteDetailed(ctx, args, exec)
	}
	content, err := tool.Execute(ctx, args, exec)
	if err != nil {
		return tools.ToolOutcome{}, err
	}
	return tools.ToolOutcome{
		Content:  content,
		Success:  true,
		ExitCode: 0,
	}, nil
}
