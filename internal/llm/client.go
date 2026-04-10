// Package llm
// File: client.go
// Description: LLM API 클라이언트 인터페이스 정의
// Responsibility: LLM 통신 추상화 인터페이스 제공

package llm

import "context"

// Client는 LLM API와 통신하는 인터페이스이다.
// 구현체: OpenAIClient (OpenAI 호환 API용)
type Client interface {
	// Chat는 동기 방식으로 LLM에 메시지를 전송하고 응답을 받는다.
	Chat(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)

	// ChatStream은 스트리밍 방식으로 LLM에 메시지를 전송한다.
	// onThinkingToken은 </think> 이전의 추론 토큰에 호출된다 (nil 허용).
	// onToken은 </think> 이후의 최종 응답 토큰에 호출된다.
	// tool_calls가 포함된 경우 최종 Response에 조합하여 반환한다.
	ChatStream(ctx context.Context, messages []Message, tools []ToolDef, onThinkingToken func(string), onToken func(string)) (Response, error)
}
