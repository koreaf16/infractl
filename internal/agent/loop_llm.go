// Package agent
// File: loop_llm.go
// Description: LLM 호출 루프 — 재시도, 빈 응답 복구, 최대 반복 graceful exit 포함
// Responsibility: 메인 for 루프 실행: LLM 호출 → 도구 실행 → 히스토리 갱신 사이클 관리

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/cost"
	"github.com/yourorg/infractl/internal/llm"
)

// runLLMLoop는 LLM 호출-도구 실행 루프를 수행한다.
// 일시적 에러 재시도, 빈 응답 nudge 복구, 최대 반복 시 graceful exit를 포함한다.
func (a *Agent) runLLMLoop(
	ctx context.Context,
	systemMsg llm.Message,
	activeClient llm.Client,
	activeTier llm.Tier,
	activeModelName string,
	apiTools []llm.ToolDef,
) error {
	// toolCallsThisTurn?? ?????????????⑤；諭????ш끽維?????ш낄猷???嶺뚮ㅎ?????嚥▲꺂??
	// maxToolsPerTurn???縕????嚥???LLM?????癲ル슣鍮뽳쭕??????몄릇?????쑩?젆????좊즴甕???筌먲퐢??
	toolCallsThisTurn := 0
	totalToolCalls := 0
	emptyRetries := 0
	ptlRetries := 0
	const maxPTLRetries = 2
	retryCfg := llm.DefaultRetryConfig()

	for i := 0; i < a.maxToolLoop; i++ {
		// 루프 시작부 context 취소 체크: 이미 취소된 context로 비싼 LLM 호출을 방지한다
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("agent loop cancelled: %w", err)
		}

		// ??ш낄猷???嶺뚮ㅎ?????ш끽維????? ???ㅺ강??????쒓낮?뺧┼? ?????LLM ?嶺뚮ㅎ?????????뤅 ??좊즴甕?????쑩?젆?癲ル슣????? ??낆뒩????筌먲퐢??
		// systemMsg????됰씭?????⑥????????寃뗏?currentSystemMsg癲????쒓낯???筌뚯슦肉???딅텑???????????⑤８彛??
		forceResponse := toolCallsThisTurn >= maxToolsPerTurn
		messages := a.buildMessages(systemMsg)
		if forceResponse {
			// FORCE RESPONSE를 시스템 프롬프트가 아닌 대화 맨 끝 user 메시지로 주입한다.
			// Qwen 계열 모델은 긴 시스템 프롬프트 내 지시보다 직전 user 메시지를 우선 따르기 때문이다.
			slog.Warn("per-turn tool limit reached, injecting force-response directive",
				"tool_calls_this_turn", toolCallsThisTurn, "limit", maxToolsPerTurn)
			messages = append(messages, llm.Message{
				Role: llm.RoleUser,
				Content: fmt.Sprintf(
					"[FORCE RESPONSE] You have already called %d tools in this turn. "+
						"You MUST respond to the user with text RIGHT NOW. "+
						"Do NOT call any more tools. Summarize what you found and present it to the user.",
					toolCallsThisTurn,
				),
			})
		}

		// per-call timeout: llmCallTimeout ???⑤；諭????쑩?젆????? ???怨룔걬癲?????濡μ뒙?癲ル슪?ｇ몭???筌먲퐢??
		// LLM 호출 + 일시적 에러 재시도 서브루프 (429, 500, 502, 503, 504 등)
		// Circuit breaker: 연속 실패가 누적되면 재시도 없이 즉시 실패 처리
		if a.llmBreaker.IsOpen() {
			a.handler.OnError(fmt.Errorf("llm circuit breaker open: too many consecutive failures"))
			return fmt.Errorf("llm circuit breaker open after %d failures", a.llmBreaker.Failures())
		}
		var resp llm.Response
		var llmErr error
		for attempt := 1; attempt <= retryCfg.MaxRetries+1; attempt++ {
			if retryCtxErr := ctx.Err(); retryCtxErr != nil {
				return fmt.Errorf("llm call cancelled: %w", retryCtxErr)
			}
			callCtx, cancelCall := context.WithTimeout(ctx, llmCallTimeout)
			a.handler.OnThinking(string(activeTier), activeModelName)
			resp, llmErr = activeClient.ChatStream(callCtx, messages, apiTools, nil,
				a.handler.OnThinkingToken, a.handler.OnToken)
			cancelCall()

			if llmErr == nil {
				a.llmBreaker.RecordSuccess()
				break
			}
			// PTL 에러: compaction으로 컨텍스트를 줄인 후 재시도
			if llm.IsPTLError(llmErr) {
				ptlRetries++
				if ptlRetries > maxPTLRetries {
					slog.Warn("PTL retries exhausted, giving up",
						"ptl_retries", ptlRetries, "err", llmErr)
					a.handler.OnError(fmt.Errorf("prompt too long after %d compaction attempts: %w",
						ptlRetries-1, llmErr))
					return fmt.Errorf("prompt too long: compaction exhausted: %w", llmErr)
				}
				slog.Warn("prompt too long detected, attempting compaction before retry",
					"ptl_retry", ptlRetries, "max", maxPTLRetries, "err", llmErr)
				a.compactIfNeeded(ctx)
				if ptlRetries >= 2 {
					// 2번째 이후: aggressive trim — 최근 라운드만 남기고 전부 삭제
					slog.Warn("aggressive trim: keeping only recent history",
						"before", len(a.history))
					a.trimHistory()
					a.trimHistory() // 2회 연속 trim으로 히스토리를 최소 수준까지 축소
				} else {
					a.trimHistory()
				}
				break // 외부 for 루프에서 메시지 재구성 후 재시도
			}
			// 비일시적 에러(400, 401 등)는 재시도 없이 즉시 종료
			if !llm.IsTransient(llmErr) {
				a.handler.OnError(fmt.Errorf("llm call failed: %w", llmErr))
				return fmt.Errorf("llm call: %w", llmErr)
			}
			if attempt > retryCfg.MaxRetries {
				a.llmBreaker.RecordFailure()
				a.handler.OnError(fmt.Errorf("llm call failed after %d retries: %w",
					retryCfg.MaxRetries, llmErr))
				return fmt.Errorf("llm call retries exhausted: %w", llmErr)
			}

			delay := llm.RetryDelay(attempt, retryCfg, llmErr)
			slog.Warn("llm call transient error, retrying",
				"attempt", attempt, "max_retries", retryCfg.MaxRetries,
				"delay", delay, "err", llmErr)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("llm retry cancelled: %w", ctx.Err())
			}
		}
		// PTL compaction 후 break된 경우: 메시지 재구성을 위해 외부 루프 재진입
		if llmErr != nil && llm.IsPTLError(llmErr) {
			continue
		}
		// retry 소진 후 에러가 남아있는 경우 (위의 return에서 처리되지 않은 경우)
		if llmErr != nil {
			a.handler.OnError(fmt.Errorf("llm call failed: %w", llmErr))
			return fmt.Errorf("llm call: %w", llmErr)
		}

		slog.Debug("llm response", "has_tool_calls", len(resp.ToolCalls) > 0,
			"input_tokens", resp.InputTokens, "output_tokens", resp.OutputTokens)

		// Token usage tracking.
		if resp.InputTokens > 0 || resp.OutputTokens > 0 {
			a.handler.OnUsageUpdate(resp.InputTokens, resp.OutputTokens, 0, 0)
			// Phase 8: ???????れ삀??쎈뭄?(??????袁ⓓ? ????됰꽡???????룸Ŧ爾????節뚮쳮??
			if a.costTracker != nil {
				a.costTracker.Record(ctx, activeModelName, resp.InputTokens, resp.OutputTokens,
					cost.SourceUser, a.currentSessionID)
			}
		}

		// forceResponse 상태에서 모델이 텍스트를 반환했으면 툴콜은 무시하고 즉시 종료한다.
		// Qwen 계열 모델은 텍스트 응답과 툴콜을 같은 출력에 섞는 경우가 있는데,
		// 이 경우 텍스트가 실질적인 최종 답변이므로 루프를 계속해선 안 된다.
		// forceResponse 상태에서 모델이 텍스트를 반환했으면 툴콜은 무시하고 즉시 종료한다
		if forceResponse && strings.TrimSpace(resp.Content) != "" {
			slog.Info("force-response: model returned text content, treating as final response",
				"tool_calls_also_present", len(resp.ToolCalls))
			a.saveFinalResponse(ctx, resp.Content)
			return nil
		}


		if len(resp.ToolCalls) == 0 {
			// 빈 응답 복구: nudge 메시지를 주입하여 재시도한다
			if isEmptyResponse(resp) {
				emptyRetries++
				if emptyRetries > maxEmptyRetries {
					slog.Warn("empty response retries exhausted", "attempts", emptyRetries)
					a.handler.OnResponse("응답을 생성하지 못했습니다. 잠시 후 다시 시도해 주세요.")
					a.trimHistory()
					return nil
				}
				slog.Warn("llm returned empty response, injecting nudge",
					"attempt", emptyRetries, "iteration", i)
				a.history = append(a.history, nudgeMessage(emptyRetries))
				continue
			}
			emptyRetries = 0

			// 정상 최종 응답: 텍스트 컨텐츠를 사용자에게 반환
			a.saveFinalResponse(ctx, resp.Content)
			return nil
		}


		emptyRetries = 0

		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		a.history = append(a.history, assistantMsg)
		a.saveSessionMessage(ctx, assistantMsg)

		toolCallsThisTurn += len(resp.ToolCalls)
		totalToolCalls += len(resp.ToolCalls)
		toolResults := a.executeToolCalls(ctx, resp.ToolCalls)
		for j, tc := range resp.ToolCalls {
			if j < len(toolResults) {
				llm.LogToolResult(tc.Function.Name, toolResults[j].Content)
			}
		}
		a.history = append(a.history, toolResults...)

		// 툴 결과 기반 사후 RAG 주입
		a.InjectPostToolKnowledge(ctx, resp.ToolCalls, toolResults)
		a.saveSessionMessages(ctx, toolResults)
	}

	// 최대 반복 도달: 마지막으로 한 번 더 LLM을 호출하여 최종 응답을 강제한다
	slog.Warn("max tool loop reached, forcing final response",
		"iterations", a.maxToolLoop, "total_tool_calls", totalToolCalls)
	return a.forceGracefulExit(ctx, systemMsg, activeClient, string(activeTier), activeModelName,
		apiTools, totalToolCalls)

}
