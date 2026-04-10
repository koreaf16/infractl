// Package llm
// File: openai.go
// Description: OpenAI 호환 LLM API 클라이언트 구현 (스트리밍 SSE 포함)
// Responsibility: OpenAI 호환 /v1/chat/completions 엔드포인트 HTTP 통신

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient는 OpenAI 호환 API와 통신하는 LLM 클라이언트이다.
// Ollama, Claude, OpenAI 등 OpenAI 호환 엔드포인트를 모두 지원한다.
type OpenAIClient struct {
	endpoint   string
	model      string
	apiKey     string
	httpClient *http.Client
}

// NewOpenAIClient는 OpenAI 호환 클라이언트를 생성한다.
func NewOpenAIClient(endpoint, model, apiKey string, timeout time.Duration) *OpenAIClient {
	return &OpenAIClient{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Chat는 동기 방식으로 LLM API를 호출한다.
func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, data)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return Response{}, fmt.Errorf("empty choices in response")
	}

	result := Response{
		Content:   chatResp.Choices[0].Message.Content,
		ToolCalls: chatResp.Choices[0].Message.ToolCalls,
	}
	if chatResp.Usage != nil {
		result.InputTokens = chatResp.Usage.PromptTokens
		result.OutputTokens = chatResp.Usage.CompletionTokens
	}
	return result, nil
}

// ChatStream은 SSE 스트리밍 방식으로 LLM API를 호출한다.
// onThinkingToken은 </think> 이전 추론 토큰에 호출된다 (nil 허용).
// onToken은 </think> 이후 최종 응답 토큰에 호출된다.
// tool_calls는 delta를 누적하여 최종 Response에 조합한다.
func (c *OpenAIClient) ChatStream(ctx context.Context, messages []Message, tools []ToolDef, onThinkingToken func(string), onToken func(string)) (Response, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, data)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	return c.parseStream(resp.Body, onThinkingToken, onToken)
}

// parseStream은 SSE 스트림을 파싱하여 최종 Response를 조합한다.
// delta.content 내에서 </think> 태그를 경계로 추론/응답 토큰을 분리한다.
func (c *OpenAIClient) parseStream(body io.Reader, onThinkingToken func(string), onToken func(string)) (Response, error) {
	var thinkBuf strings.Builder // 추론 구간 전체
	var contentBuf strings.Builder // 최종 응답 구간 전체
	toolCallBuf := make(map[int]*ToolCall)
	var inputTokens, outputTokens int

	// </think> 탐지를 위한 상태
	const endTag = "</think>"
	isThinking := true     // 첫 토큰부터 추론 구간으로 간주
	pendingBuf := ""       // 경계 탐지를 위한 소형 버퍼 (최대 len(endTag)-1 chars)

	flushPending := func(text string) {
		if text == "" {
			return
		}
		if isThinking {
			thinkBuf.WriteString(text)
			if onThinkingToken != nil {
				onThinkingToken(text)
			}
		} else {
			contentBuf.WriteString(text)
			if onToken != nil {
				onToken(text)
			}
		}
	}

	processContent := func(incoming string) {
		pendingBuf += incoming
		for {
			if !isThinking {
				// 추론 구간 종료 — 이후 모든 내용을 응답으로 플러시
				flushPending(pendingBuf)
				pendingBuf = ""
				break
			}
			idx := strings.Index(pendingBuf, endTag)
			if idx >= 0 {
				// </think> 발견: 앞부분은 추론, 뒷부분은 응답
				flushPending(pendingBuf[:idx])
				isThinking = false
				// </think> 바로 뒤의 공백/줄바꿈 건너뜀
				rest := strings.TrimLeft(pendingBuf[idx+len(endTag):], "\n")
				pendingBuf = rest
			} else {
				// </think> 미발견 — endTag 길이만큼 버퍼 보존, 나머지 플러시
				safeLen := len(pendingBuf) - (len(endTag) - 1)
				if safeLen > 0 {
					flushPending(pendingBuf[:safeLen])
					pendingBuf = pendingBuf[safeLen:]
				}
				break
			}
		}
	}

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		if payload == "[DONE]" {
			break
		}
		if payload == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			slog.Debug("stream chunk parse error", "err", err, "payload", payload)
			continue
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			processContent(delta.Content)
		}

		for _, tc := range delta.ToolCalls {
			accumulateToolCall(toolCallBuf, tc)
		}
	}

	// 남은 버퍼 플러시
	flushPending(pendingBuf)
	pendingBuf = ""

	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read stream: %w", err)
	}

	toolCalls := assembleToolCalls(toolCallBuf)

	return Response{
		Content:      contentBuf.String(),
		ToolCalls:    toolCalls,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// doRequest는 LLM API에 HTTP POST 요청을 전송한다.
func (c *OpenAIClient) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	url := c.endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("llm api status %d: %s", resp.StatusCode, string(snippet))
	}
	return resp, nil
}

// accumulateToolCall은 스트리밍 delta를 인덱스별 버퍼에 누적한다.
func accumulateToolCall(buf map[int]*ToolCall, tc streamToolCall) {
	existing, ok := buf[tc.Index]
	if !ok {
		existing = &ToolCall{Type: "function"}
		buf[tc.Index] = existing
	}
	if tc.ID != "" {
		existing.ID = tc.ID
	}
	if tc.Function.Name != "" {
		existing.Function.Name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		existing.Function.Arguments += tc.Function.Arguments
	}
}

// assembleToolCalls는 버퍼의 tool_calls를 인덱스 순으로 정렬된 슬라이스로 반환한다.
func assembleToolCalls(buf map[int]*ToolCall) []ToolCall {
	if len(buf) == 0 {
		return nil
	}
	result := make([]ToolCall, len(buf))
	for idx, tc := range buf {
		if idx < len(result) {
			result[idx] = *tc
		}
	}
	return result
}
