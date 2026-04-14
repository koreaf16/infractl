// Package agent
// File: history.go
// Description: 에이전트 대화 히스토리 관리 유틸리티
// Responsibility: 히스토리 트리밍, API 라운드 그룹화, 메시지 빌딩, 초기화 로직 분리

package agent

import (
	"log/slog"

	"github.com/yourorg/infractl/internal/llm"
)

// apiRound는 원자적으로 보존해야 하는 대화 API 라운드이다.
// user 메시지를 시작으로, 이에 대한 assistant 응답과 tool_result들로 구성된다.
type apiRound struct {
	messages []llm.Message
}

// groupByApiRound는 히스토리를 API 라운드 단위 슬라이스로 분할한다.
// 경계 조건: user 메시지가 시작될 때마다 새 라운드를 시작한다.
// tool_result들은 선행 assistant(with tool_calls)와 같은 라운드에 묶인다.
// 이를 통해 trimHistory/compaction 시 tool_call+tool_result 쌍이 분리되지 않는다.
func groupByApiRound(history []llm.Message) []apiRound {
	var rounds []apiRound
	var current []llm.Message

	for _, msg := range history {
		if msg.Role == llm.RoleUser && len(current) > 0 {
			// 새 user 메시지 = 이전 라운드 확정 후 새 라운드 시작
			rounds = append(rounds, apiRound{messages: current})
			current = []llm.Message{msg}
		} else {
			current = append(current, msg)
		}
	}
	if len(current) > 0 {
		rounds = append(rounds, apiRound{messages: current})
	}
	return rounds
}

// flattenRounds는 apiRound 슬라이스를 단일 메시지 슬라이스로 평탄화한다.
func flattenRounds(rounds []apiRound) []llm.Message {
	total := 0
	for _, r := range rounds {
		total += len(r.messages)
	}
	result := make([]llm.Message, 0, total)
	for _, r := range rounds {
		result = append(result, r.messages...)
	}
	return result
}

// filterHistory는 오래된 대화 라운드에서 단순 도구 호출 내역을 제거하여 토큰 낭비를 줄인다.
func (a *Agent) filterHistory(history []llm.Message) []llm.Message {
	rounds := groupByApiRound(history)
	// preserveRecentRounds는 compaction.go에서 패키지 레벨 상수로 정의한다.
	if len(rounds) <= preserveRecentRounds {
		return history
	}

	var filtered []llm.Message
	for i, r := range rounds {
		if i >= len(rounds)-preserveRecentRounds {
			filtered = append(filtered, r.messages...)
			continue
		}

		for _, msg := range r.messages {
			if msg.Role == llm.RoleUser {
				filtered = append(filtered, msg)
			} else if msg.Role == llm.RoleAssistant {
				// 텍스트 응답이 있는 assistant 메시지만 보존, 도구 호출 내역은 삭제
				if msg.Content != "" {
					newMsg := msg
					newMsg.ToolCalls = nil
					filtered = append(filtered, newMsg)
				}
			}
			// RoleTool 메시지(도구 실행 결과)는 생략
		}
	}
	return filtered
}

// buildMessages는 시스템 프롬프트와 히스토리를 조합하여 LLM 요청용 메시지 슬라이스를 반환한다.
func (a *Agent) buildMessages(systemMsg llm.Message) []llm.Message {
	filteredHistory := a.filterHistory(a.history)
	messages := make([]llm.Message, 0, len(filteredHistory)+1)
	messages = append(messages, systemMsg)
	messages = append(messages, filteredHistory...)
	return messages
}

// ClearHistory는 대화 히스토리를 초기화한다.
func (a *Agent) ClearHistory() {
	a.history = make([]llm.Message, 0)
}

// trimHistory는 히스토리를 maxHistory 이하로 유지한다.
// API 라운드(user+assistant+tool_results) 단위로 원자적으로 제거하여
// tool_call + tool_result 쌍이 분리되는 것을 방지한다.
func (a *Agent) trimHistory() {
	if len(a.history) <= a.maxHistory {
		return
	}

	rounds := groupByApiRound(a.history)

	// 총 메시지 수가 maxHistory 이하가 될 때까지 가장 오래된 라운드부터 제거한다.
	// 단, 마지막 1개 라운드는 항상 보존한다.
	for len(rounds) > 1 {
		total := 0
		for _, r := range rounds {
			total += len(r.messages)
		}
		if total <= a.maxHistory {
			break
		}
		rounds = rounds[1:]
	}

	before := len(a.history)
	a.history = flattenRounds(rounds)

	slog.Debug("trimHistory complete",
		"before", before,
		"after", len(a.history),
		"remaining_rounds", len(rounds))
}
