// Package query
// File: engine_dispatch.go
// Description: query loop 본체 — turn 단위 LLM 호출, 도구 실행, 상태 전이
// Responsibility: runLoop (무한 루프), runTurn (단일 turn), callLLM
//
// Ported from: claude_cli/src/query.ts:241-251 (queryLoop), 307-337 (loop body 시작),
//              653-863 (모델 호출 + 스트리밍), 864-1183 (에러 복구 분기)
// 핵심 패턴: while(true) state machine → continue sites 마다 state.withTransition

package query

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yourorg/infractl/internal/agent/compact"
	"github.com/yourorg/infractl/internal/llm"
)

// runLoop 는 query 메인 루프다. Terminal 이벤트를 방출하고 반환한다.
// claude_cli: queryLoop async function* (query.ts:241)
func (e *Engine) runLoop(ctx context.Context, p Params, out chan<- QueryEvent) {
	s := initialState(p.Messages)
	se := NewStreamingExecutor(0) // MaxToolConcurrency() 기본값 사용

	for turn := 0; turn < p.MaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			sendDirect(out, EventTerminal{Reason: TerminalInterrupted})
			return
		}

		slog.Debug("query engine turn start",
			"turn", s.turnCount,
			"tier", p.Tier,
			"model", p.ModelName,
			"messages", len(s.messages),
		)

		if e.compact != nil {
			cs := &compact.State{
				Messages:                    s.messages,
				ContextWindow:               p.ContextWindow,
				SystemTokens:                p.SystemTokens,
				HasAttemptedReactiveCompact: s.hasAttemptedReactiveCompact,
				MaxOutputTokensRecoveryCount: s.maxOutputTokensRecoveryCount,
				MaxOutputTokensOverride:     s.maxOutputTokensOverride,
			}
			e.compact.Apply(ctx, cs)
			s.messages = cs.Messages
			s.hasAttemptedReactiveCompact = cs.HasAttemptedReactiveCompact
			s.maxOutputTokensRecoveryCount = cs.MaxOutputTokensRecoveryCount
			s.maxOutputTokensOverride = cs.MaxOutputTokensOverride
		}

		done, nextState := e.runTurn(ctx, p, s, se, out)
		if done {
			return
		}
		s = nextState
	}

	slog.Warn("query engine max turns reached", "max_turns", p.MaxTurns)
	sendDirect(out, EventTerminal{Reason: TerminalMaxTurns})
}

// runTurn 은 단일 LLM turn 을 실행한다.
// done=true 이면 Terminal 이벤트가 방출되었고 루프가 종료되어야 한다.
func (e *Engine) runTurn(
	ctx context.Context,
	p Params,
	s state,
	se *StreamingExecutor,
	out chan<- QueryEvent,
) (done bool, next state) {
	if !send(ctx, out, EventStreamStart{Tier: string(p.Tier), Model: p.ModelName}) {
		sendDirect(out, EventTerminal{Reason: TerminalInterrupted})
		return true, s
	}

	resp, err := e.callLLM(ctx, p, s, out)
	if err != nil {
		if ctx.Err() != nil {
			sendDirect(out, EventTerminal{Reason: TerminalInterrupted})
			return true, s
		}

		if e.recovery != nil {
			cs := &compact.State{
				Messages:                    s.messages,
				ContextWindow:               p.ContextWindow,
				SystemTokens:                p.SystemTokens,
				HasAttemptedReactiveCompact: s.hasAttemptedReactiveCompact,
				MaxOutputTokensRecoveryCount: s.maxOutputTokensRecoveryCount,
				MaxOutputTokensOverride:     s.maxOutputTokensOverride,
			}
			action, recErr := e.recovery.Handle(ctx, cs, err)
			if recErr != nil {
				slog.Warn("query engine recovery failed", "err", recErr)
			}
			s.messages = cs.Messages
			s.hasAttemptedReactiveCompact = cs.HasAttemptedReactiveCompact
			s.maxOutputTokensRecoveryCount = cs.MaxOutputTokensRecoveryCount
			s.maxOutputTokensOverride = cs.MaxOutputTokensOverride

			switch action {
			case compact.ActionDrainCollapse:
				return false, s.withTransition(ContinueCollapseDrainRetry)
			case compact.ActionReactiveCompact:
				return false, s.withTransition(ContinueReactiveCompactRetry)
			case compact.ActionRetryMaxTokens:
				return false, s.withTransition(ContinueMaxOutputTokensRecovery)
			case compact.ActionStripMedia:
				return false, s.withTransition(ContinueReactiveCompactRetry)
			case compact.ActionStripSignaturesAndRetry:
				return false, s.withTransition(ContinueReactiveCompactRetry)
			case compact.ActionMultiTurnRecovery:
				return false, s.withTransition(ContinueMaxOutputTokensRecovery)
			case compact.ActionSurfaceError:
				// fallthrough to TerminalModelError
			}
		}

		send(ctx, out, EventError{Err: err, Recoverable: false})
		sendDirect(out, EventTerminal{Reason: TerminalModelError, Err: err})
		return true, s
	}

	// 완전한 응답을 방출한다 — 소비자가 히스토리를 재구성하는 데 사용한다.
	send(ctx, out, EventAssistantResponse{Text: resp.Content, ToolCalls: resp.ToolCalls})

	if len(resp.ToolCalls) == 0 {
		sendDirect(out, EventTerminal{Reason: TerminalCompleted})
		return true, s
	}

	// 도구 배치 분할 후 StreamingExecutor 로 실행
	batches := PartitionToolCalls(resp.ToolCalls, p.Registry)
	toolResultMsgs, aborted := se.Execute(ctx, batches, out, p.RunTool)
	if aborted {
		sendDirect(out, EventTerminal{Reason: TerminalAbortedTools})
		return true, s
	}

	// 메시지 이력 업데이트: assistant + tool results
	assistantMsg := llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: resp.ToolCalls,
	}
	updated := make([]llm.Message, 0, len(s.messages)+1+len(toolResultMsgs))
	updated = append(updated, s.messages...)
	updated = append(updated, assistantMsg)
	updated = append(updated, toolResultMsgs...)

	next = s
	next.messages = updated
	next.turnCount = s.turnCount + 1
	next = next.withTransition(ContinueNextTurn)

	slog.Debug("query engine turn complete",
		"turn", s.turnCount,
		"tool_calls", len(resp.ToolCalls),
		"input_tokens", resp.InputTokens,
		"output_tokens", resp.OutputTokens,
	)

	return false, next
}

// callLLM 은 llm.ChatStream 을 호출하고 스트리밍 청크를 채널로 방출한다.
func (e *Engine) callLLM(
	ctx context.Context,
	p Params,
	s state,
	out chan<- QueryEvent,
) (llm.Response, error) {
	onThinking := func(tok string) {
		send(ctx, out, EventAssistantChunk{Text: tok, Thinking: true})
	}
	onToken := func(tok string) {
		send(ctx, out, EventAssistantChunk{Text: tok, Thinking: false})
	}

	msgs := make([]llm.Message, 0, 1+len(s.messages))
	msgs = append(msgs, p.SystemMsg)
	msgs = append(msgs, s.messages...)

	var opts []llm.CallOption
	if s.maxOutputTokensOverride != nil {
		opts = append(opts, llm.WithMaxTokens(*s.maxOutputTokensOverride))
	}

	resp, err := p.Client.ChatStream(ctx, msgs, p.Tools, nil, onThinking, onToken, opts...)
	if err != nil {
		return llm.Response{}, fmt.Errorf("llm chat stream (turn %d): %w", s.turnCount, err)
	}
	return resp, nil
}
