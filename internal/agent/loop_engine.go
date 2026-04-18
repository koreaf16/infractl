// Package agent
// File: loop_engine.go
// Description: query.Engine 어댑터 — 이벤트 스트림을 QueryEventSink + 히스토리 관리로 분기
// Responsibility: runWithEngine (loop.go:472 대체), consumeEngineEvents (이벤트 라우팅)

package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yourorg/infractl/internal/agent/compact"
	"github.com/yourorg/infractl/internal/agent/planmode"
	"github.com/yourorg/infractl/internal/agent/query"
	"github.com/yourorg/infractl/internal/llm"
)

// runWithEngine 는 query.Engine 을 사용한 LLM 루프 실행 진입점이다.
// loop.go 의 기존 LLM 반복 루프를 대체한다.
func (a *Agent) runWithEngine(
	ctx context.Context,
	systemMsg llm.Message,
	activeClient llm.Client,
	activeTier llm.Tier,
	activeModelName string,
	apiTools []llm.ToolDef,
	enableTodoEnforcer bool,
) error {
	baseRunner := query.ToolRunner(func(ctx context.Context, tc llm.ToolCall) (string, bool) {
		msg, ok := a.executeSingleTool(ctx, tc)
		return msg.Content, !ok
	})

	ti := query.NewToolInvoker(a.hookRunner, baseRunner)
	ti.SetRegistry(a.registry)
	ti.SetBlockedCallback(func(tc llm.ToolCall, reason string) {
		args := query.ParseArgsForCallback(tc.Function.Arguments)
		a.handler.OnToolStart(tc.ID, tc.Function.Name, "", args)
		a.handler.OnToolEnd(tc.ID, tc.Function.Name, reason, 0, false, "")
	})
	// Phase G: Plan Mode 차단 gate 주입
	if a.planState != nil {
		filter := planmode.NewReadOnlyFilter(a.registry)
		ti.SetPlanGate(a.planState, filter)
	}
	// Phase G: TodoWrite enforcer 주입 (다단계 신호어 감지 시에만)
	if a.todoEnforcer != nil && enableTodoEnforcer {
		ti.SetTodoEnforcer(a.todoEnforcer)
	}
	runTool := ti.AsToolRunner()

	params := query.Params{
		Messages:      a.history,
		SystemMsg:     systemMsg,
		Tier:          activeTier,
		Client:        activeClient,
		ModelName:     activeModelName,
		Tools:         apiTools,
		MaxTurns:      a.maxToolLoop,
		RunTool:       runTool,
		Registry:      a.registry,
		ContextWindow: a.maxContextTokens,
		SystemTokens:  a.lastSystemPromptLen / compact.AvgCharsPerToken,
	}

	events := a.queryEngine.Run(ctx, params)
	return a.consumeEngineEvents(ctx, events)
}

// consumeEngineEvents 는 query.Engine 이 방출하는 이벤트를 소비한다.
// 스트리밍 이벤트(토큰, thinking)는 a.querySink 로, 히스토리 관리는 직접 처리한다.
// 나머지 콜백(OnError, OnResponse)은 과도기적으로 a.handler 를 사용한다.
func (a *Agent) consumeEngineEvents(ctx context.Context, events <-chan query.QueryEvent) error {
	var lastResponseText string

	for ev := range events {
		// 스트리밍 이벤트 — UI 레이어에 위임 (B.10: a.handler 완전 제거 예정)
		a.querySink.HandleQueryEvent(ev)

		switch e := ev.(type) {

		case query.EventStreamStart:
			lastResponseText = ""

		case query.EventAssistantResponse:
			lastResponseText = e.Text
			assistantMsg := llm.Message{
				Role:      llm.RoleAssistant,
				Content:   e.Text,
				ToolCalls: e.ToolCalls,
			}
			a.history = append(a.history, assistantMsg)
			a.saveSessionMessage(ctx, assistantMsg)

		case query.EventToolResult:
			// handler.OnToolStart / OnToolEnd 는 executeSingleTool 내부에서 이미 호출됨.
			if !e.SiblingSkipped {
				llm.LogToolResult(e.Name, e.Output)
			}
			toolMsg := llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: e.ID,
				Content:    e.Output,
			}
			a.history = append(a.history, toolMsg)
			a.saveSessionMessage(ctx, toolMsg)

		case query.EventError:
			a.handler.OnError(e.Err)

		case query.EventTerminal:
			return a.handleTerminal(ctx, e, lastResponseText)

		case query.EventAssistantChunk, query.EventToolUseStart:
			// 스트리밍/참조 이벤트 — querySink 에서 이미 처리됨.
		}
	}

	return fmt.Errorf("query engine: event channel closed before Terminal event")
}

// handleTerminal 은 EventTerminal 에 따라 루프를 종료한다.
func (a *Agent) handleTerminal(ctx context.Context, e query.EventTerminal, lastText string) error {
	switch e.Reason {
	case query.TerminalCompleted:
		a.handler.OnResponse(lastText)
		a.trimHistory()
		return nil

	case query.TerminalInterrupted:
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("agent loop interrupted: %w", err)
		}
		return fmt.Errorf("agent loop interrupted")

	case query.TerminalModelError:
		if e.Err != nil {
			a.handler.OnError(e.Err)
			return fmt.Errorf("model error: %w", e.Err)
		}
		return fmt.Errorf("model error (no detail)")

	case query.TerminalMaxTurns:
		slog.Warn("query engine max turns reached, no final response generated",
			"max_turns", a.maxToolLoop)
		return fmt.Errorf("max turns (%d) reached without terminal response", a.maxToolLoop)

	case query.TerminalAbortedTools:
		return fmt.Errorf("tool execution aborted by context cancellation")

	default:
		if e.Err != nil {
			return fmt.Errorf("terminal(%s): %w", e.Reason, e.Err)
		}
		return fmt.Errorf("terminal(%s)", e.Reason)
	}
}
