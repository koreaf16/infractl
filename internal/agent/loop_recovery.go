// Package agent
// File: loop_recovery.go
// Description: 에이전트 루프 복구 전략 — 빈 응답 nudge, 최종 응답 강제, 응답 상태 판별
// Responsibility: LLM이 빈 응답/최대 반복 도달 시 루프 재시도를 위한 메시지 생성

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/infractl/internal/llm"
)

const (
	// maxEmptyRetries는 LLM이 빈 응답을 연속 반환할 때 nudge 재시도 최대 횟수이다.
	maxEmptyRetries = 2
)

// isEmptyResponse는 LLM 응답이 빈 컨텐츠이면서 도구 호출도 없는 상태인지 판별한다.
func isEmptyResponse(resp llm.Response) bool {
	return len(resp.ToolCalls) == 0 && strings.TrimSpace(resp.Content) == ""
}

// nudgeMessage는 LLM이 빈 응답을 반환했을 때 재시도를 유도하는 user 메시지를 생성한다.
// attempt가 증가할수록 지시가 강해진다.
//
// Qwen 계열 모델은 직전 user 메시지를 시스템 프롬프트보다 우선 따르므로
// user role로 주입하는 것이 효과적이다.
func nudgeMessage(attempt int) llm.Message {
	var content string
	switch attempt {
	case 1:
		content = "[RETRY] 이전 응답이 비어 있었습니다. " +
			"사용자의 질문에 대해 텍스트로 응답하거나, 필요한 도구를 호출해 주세요."
	default:
		content = "[RETRY] 응답이 다시 비어 있습니다. " +
			"반드시 텍스트로 응답해야 합니다. " +
			"답변할 수 없다면 그 이유를 설명해 주세요. " +
			"도구 호출 없이 직접 텍스트로 응답하세요."
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Content: content,
	}
}

// forceResponseMessage는 최대 반복(maxToolLoop) 도달 시 LLM에게 최종 응답을 강제하는 메시지를 생성한다.
// 도구 호출 없이 현재까지 수집한 정보를 종합하여 사용자에게 답변하도록 지시한다.
func forceResponseMessage(totalToolCalls int) llm.Message {
	return llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf(
			"[FINAL TURN] 지금까지 총 %d회의 도구 호출을 실행했습니다. "+
				"더 이상 도구를 호출할 수 없습니다. "+
				"지금까지 수집한 모든 정보를 종합하여 사용자에게 최종 응답을 제공하세요. "+
				"추가 도구 호출은 절대 하지 마세요.",
			totalToolCalls,
		),
	}
}

// saveFinalResponse는 최종 텍스트 응답을 히스토리에 저장하고 사용자에게 전달한다.
func (a *Agent) saveFinalResponse(ctx context.Context, content string) {
	assistantFinal := llm.Message{
		Role:    llm.RoleAssistant,
		Content: content,
	}
	a.history = append(a.history, assistantFinal)
	a.saveSessionMessage(ctx, assistantFinal)
	a.handler.OnResponse(content)
	a.trimHistory()
}

// saveSessionMessage는 메시지를 세션 스토어에 저장하고 RAG 인덱싱한다.
func (a *Agent) saveSessionMessage(ctx context.Context, msg llm.Message) {
	if a.sessionStore == nil || a.currentSessionID <= 0 {
		return
	}
	sm := sessionMessageFromLLM(msg, a.currentSessionID)
	id, err := a.sessionStore.SaveMessage(ctx, sm)
	if err != nil {
		slog.Warn("save session message", "role", msg.Role, "err", err)
		return
	}
	if a.ragManager != nil {
		sm.ID = id
		if idxErr := a.ragManager.IndexSessionMessage(ctx, sm); idxErr != nil {
			slog.Warn("index session message", "id", id, "err", idxErr)
		}
	}
}

// saveSessionMessages는 복수 메시지를 세션 스토어에 일괄 저장한다.
func (a *Agent) saveSessionMessages(ctx context.Context, msgs []llm.Message) {
	for _, msg := range msgs {
		a.saveSessionMessage(ctx, msg)
	}
}

// forceGracefulExit는 최대 반복 도달 시 마지막 LLM 호출로 최종 응답을 강제한다.
func (a *Agent) forceGracefulExit(
	ctx context.Context,
	systemMsg llm.Message,
	client llm.Client,
	tier, modelName string,
	_ []llm.ToolDef, // apiTools: graceful exit 시에는 도구를 비활성화하므로 사용하지 않음
	totalToolCalls int,
) error {
	messages := a.buildMessages(systemMsg)
	messages = append(messages, forceResponseMessage(totalToolCalls))

	callCtx, cancelCall := context.WithTimeout(ctx, llmCallTimeout)
	a.handler.OnThinking(tier, modelName)
	finalResp, err := client.ChatStream(callCtx, messages, nil, nil,
		a.handler.OnThinkingToken, a.handler.OnToken)
	cancelCall()

	if err != nil || strings.TrimSpace(finalResp.Content) == "" {
		loopErr := fmt.Errorf("max tool loop iterations (%d) exceeded", a.maxToolLoop)
		a.handler.OnError(loopErr)
		return loopErr
	}

	if finalResp.InputTokens > 0 || finalResp.OutputTokens > 0 {
		a.handler.OnUsageUpdate(finalResp.InputTokens, finalResp.OutputTokens, 0, 0)
	}
	a.saveFinalResponse(ctx, finalResp.Content)
	return nil
}
