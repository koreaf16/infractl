// Package agent
// File: compaction.go
// Description: 토큰 기반 컨텍스트 관리 — 히스토리 압축으로 LLM 컨텍스트 초과 방지
// Responsibility: 토큰 상태 추정, circuit breaker, partial compaction, PTL 재시도 오케스트레이션

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/infractl/internal/llm"
)

// TokenState는 현재 히스토리의 토큰 사용 상태를 나타낸다.
type TokenState string

const (
	// TokenOK는 토큰 사용량이 80% 미만인 정상 상태이다.
	TokenOK TokenState = "ok"
	// TokenWarning은 토큰 사용량이 80-95% 구간으로 주의가 필요한 상태이다.
	TokenWarning TokenState = "warning"
	// TokenCritical은 토큰 사용량이 95-100% 구간으로 compaction이 권장되는 상태이다.
	TokenCritical TokenState = "critical"
	// TokenOverflow는 토큰 사용량이 100%를 초과한 상태로 즉시 compaction이 필요하다.
	TokenOverflow TokenState = "overflow"
)

const (
	// defaultMaxContextTokens는 기본 컨텍스트 크기 한계이다.
	defaultMaxContextTokens = 128_000

	// avgCharsPerToken은 한국어/영어 혼합 텍스트의 대략적 chars-per-token 비율이다.
	// 한국어 ~2 chars/token, 영어 ~4 chars/token → 평균 ~3으로 추정한다.
	avgCharsPerToken = 3

	tokenWarningThreshold  = 0.80
	tokenCriticalThreshold = 0.95

	// reservedOutputTokens는 compaction 요약 출력을 위해 예약하는 토큰 수이다.
	// effective window = maxContextTokens - reservedOutputTokens
	reservedOutputTokens = 8_000

	// maxConsecutiveCompactFailures는 circuit breaker 임계값이다.
	// 연속으로 이 횟수만큼 실패하면 compaction을 중단하고 trimHistory만 실행한다.
	maxConsecutiveCompactFailures = 3

	// preserveRecentRounds는 partial compaction 시 원문으로 보존할 최근 라운드 수이다.
	preserveRecentRounds = 3

	// maxPTLRetries는 prompt-too-long 에러 발생 시 재시도 최대 횟수이다.
	maxPTLRetries = 3
)

// checkTokenState는 현재 히스토리 + 캐싱된 시스템 프롬프트의 토큰 상태를 반환한다.
// 시스템 프롬프트 토큰을 포함하여 실제 API 요청 토큰에 더 가깝게 추정한다.
func (a *Agent) checkTokenState() TokenState {
	historyTokens := estimateTokens(a.history)
	systemTokens := a.lastSystemPromptLen / avgCharsPerToken

	maxTokens := a.maxContextTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxContextTokens
	}
	effectiveWindow := maxTokens - reservedOutputTokens
	if effectiveWindow <= 0 {
		effectiveWindow = maxTokens
	}

	estimated := historyTokens + systemTokens
	ratio := float64(estimated) / float64(effectiveWindow)

	slog.Debug("token state check",
		"history_tokens", historyTokens,
		"system_tokens", systemTokens,
		"estimated_total", estimated,
		"effective_window", effectiveWindow,
		"ratio", fmt.Sprintf("%.2f", ratio))

	switch {
	case ratio >= 1.0:
		return TokenOverflow
	case ratio >= tokenCriticalThreshold:
		return TokenCritical
	case ratio >= tokenWarningThreshold:
		return TokenWarning
	default:
		return TokenOK
	}
}

// compactIfNeeded는 토큰 상태가 critical 이상이면 히스토리를 압축한다.
// circuit breaker로 연속 실패를 방지하고, partial compaction으로 최근 맥락을 보존한다.
func (a *Agent) compactIfNeeded(ctx context.Context) {
	state := a.checkTokenState()
	if state != TokenCritical && state != TokenOverflow {
		if state == TokenWarning {
			slog.Warn("context token usage above 80%, compaction may be needed soon",
				"history_tokens", estimateTokens(a.history),
				"system_tokens", a.lastSystemPromptLen/avgCharsPerToken)
		}
		return
	}

	// Circuit breaker: 연속 실패가 임계값 이상이면 compaction 중단
	if a.compactFailures >= maxConsecutiveCompactFailures {
		slog.Warn("compaction circuit breaker active: falling back to trimHistory",
			"consecutive_failures", a.compactFailures)
		a.trimHistory()
		return
	}

	slog.Info("compacting conversation history",
		"state", string(state),
		"history_len", len(a.history),
		"consecutive_failures", a.compactFailures)

	toCompact, toKeep := partitionHistoryForCompaction(a.history)

	summary, err := a.requestCompactionSummaryFor(ctx, toCompact)
	if err != nil {
		a.compactFailures++
		slog.Warn("compaction failed",
			"err", err,
			"consecutive_failures", a.compactFailures)
		a.trimHistory()
		return
	}

	// 성공: circuit breaker 리셋
	a.compactFailures = 0

	// [요약 user][ack assistant] + [최근 N라운드 원문]
	summaryMsg := llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf(
			"[Previous conversation summary]\n%s\n\n[Continue from here]",
			summary,
		),
	}
	ackMsg := llm.Message{
		Role:    llm.RoleAssistant,
		Content: "Understood. I have the summarized context from our previous conversation. How can I help you continue?",
	}

	a.history = make([]llm.Message, 0, 2+len(toKeep))
	a.history = append(a.history, summaryMsg, ackMsg)
	a.history = append(a.history, toKeep...)

	slog.Info("compaction complete",
		"kept_rounds", preserveRecentRounds,
		"new_history_len", len(a.history))

	a.injectPostCompactContext(ctx)
}

// partitionHistoryForCompaction은 히스토리를 압축 대상과 보존 대상으로 분할한다.
// 최근 preserveRecentRounds 개 라운드는 원문 보존하고 나머지만 압축한다.
// 라운드 수가 너무 적으면 전체를 압축 대상으로 반환한다.
func partitionHistoryForCompaction(history []llm.Message) (toCompact []llm.Message, toKeep []llm.Message) {
	rounds := groupByApiRound(history)

	if len(rounds) <= preserveRecentRounds+1 {
		return history, nil
	}

	splitAt := len(rounds) - preserveRecentRounds
	return flattenRounds(rounds[:splitAt]), flattenRounds(rounds[splitAt:])
}

// requestCompactionSummaryFor는 주어진 메시지들에 대한 요약을 PTL 재시도 루프로 요청한다.
// prompt-too-long 에러 발생 시 앞부분 라운드를 줄여 최대 maxPTLRetries회 재시도한다.
func (a *Agent) requestCompactionSummaryFor(ctx context.Context, messages []llm.Message) (string, error) {
	current := messages
	for attempt := 0; attempt < maxPTLRetries; attempt++ {
		summary, err := a.doCompactionRequest(ctx, current)
		if err == nil {
			if attempt > 0 {
				slog.Info("compaction PTL retry succeeded",
					"attempt", attempt+1,
					"messages_used", len(current))
			}
			return summary, nil
		}

		if !isPTLError(err) {
			return "", fmt.Errorf("compaction request: %w", err)
		}

		// PTL 에러: 압축 대상 앞부분 라운드를 20% 제거 후 재시도
		rounds := groupByApiRound(current)
		if len(rounds) < 2 {
			return "", fmt.Errorf("compaction: messages too short to retry PTL: %w", err)
		}
		dropCount := max(1, len(rounds)/5)
		dropCount = min(dropCount, len(rounds)-1)
		current = flattenRounds(rounds[dropCount:])

		slog.Warn("compaction PTL retry: dropping oldest rounds",
			"attempt", attempt+1,
			"dropped_rounds", dropCount,
			"remaining_messages", len(current))
	}

	return "", fmt.Errorf("compaction: exhausted PTL retries (%d)", maxPTLRetries)
}

// doCompactionRequest는 단일 compaction API 요청을 실행한다.
func (a *Agent) doCompactionRequest(ctx context.Context, messages []llm.Message) (string, error) {
	summaryMessages := append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: infraCompactPrompt,
	})
	systemMsg := llm.Message{
		Role:    llm.RoleSystem,
		Content: compactionSystemPrompt,
	}
	apiMessages := append([]llm.Message{systemMsg}, summaryMessages...)

	resp, err := a.llmClient.ChatStream(ctx, apiMessages, nil,
		func(string) {}, // thinking 무시
		func(string) {}, // 스트리밍 무시 (최종 결과만 사용)
	)
	if err != nil {
		return "", fmt.Errorf("compaction api call: %w", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		return "", fmt.Errorf("compaction returned empty response")
	}

	return formatCompactSummary(resp.Content), nil
}

// isPTLError는 에러가 prompt-too-long(컨텍스트 길이 초과) 에러인지 판별한다.
func isPTLError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "prompt_too_long") ||
		strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "maximum context length") ||
		strings.Contains(msg, "too many tokens")
}

// estimateTokens는 메시지 목록의 토큰 수를 문자 수 기반으로 추정한다.
func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / avgCharsPerToken
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Arguments) / avgCharsPerToken
		}
	}
	return total
}
