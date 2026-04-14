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

type OpenAIClient struct {
	endpoint           string
	model              string
	apiKey             string
	httpClient         *http.Client
	streamClient       *http.Client
	useInlineToolCalls bool // true면 본문 내 <tool_call> 태그를 가로채서 파싱함 (Qwen 27B 등 특정 모델용)
}

func NewOpenAIClient(endpoint, model, apiKey string, timeout time.Duration) *OpenAIClient {
	return &OpenAIClient{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		streamClient: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				ResponseHeaderTimeout: timeout,
			},
		},
	}
}

// SetUseInlineToolCalls는 본문 내 XML 기반 툴 호출 처리 여부를 설정한다.
func (c *OpenAIClient) SetUseInlineToolCalls(v bool) { c.useInlineToolCalls = v }

// transformMessagesForInlineTools는 useInlineToolCalls 모드일 때 
// vLLM의 내부 tool 파서를 우회하기 위해 메시지 이력을 조작한다.
func (c *OpenAIClient) transformMessagesForInlineTools(messages []Message) []Message {
	if !c.useInlineToolCalls {
		return messages
	}
	var transformed []Message
	for _, m := range messages {
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			content := m.Content
			for _, tc := range m.ToolCalls {
				tag := fmt.Sprintf("\n<tool_call>\n{\"name\": %q, \"arguments\": %s}\n</tool_call>\n", tc.Function.Name, tc.Function.Arguments)
				content += tag
			}
			m.Content = strings.TrimSpace(content)
			m.ToolCalls = nil // API 스키마 검증 시 vLLM 파서 개입 방지
		} else if m.Role == RoleTool {
			// RoleTool을 RoleUser로 변경하고 <tool_response> 태그로 감싼다
			m.Role = RoleUser
			m.Content = fmt.Sprintf("<tool_response>\n%s\n</tool_response>", m.Content)
			m.ToolCallID = ""
		}
		transformed = append(transformed, m)
	}
	return transformed
}

func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, tools []ToolDef, toolChoice interface{}) (Response, error) {
	reqMessages := messages
	reqTools := tools

	if c.useInlineToolCalls {
		reqMessages = c.transformMessagesForInlineTools(messages)
		reqTools = nil // Tool API 비활성화
	}

	reqBody := chatRequest{
		Model:      c.model,
		Messages:   reqMessages,
		Tools:      reqTools,
		ToolChoice: toolChoice,
		Stream:     false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	logRequestJSON(c.model, data)

	resp, err := c.doRequest(ctx, data)
	if err != nil {
		logToFile(c.model, "ERROR", err.Error())
		return Response{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	// vLLM --reasoning-parser는 reasoning 필드를 별도로 반환하므로 인라인 구조체로 파싱한다.
	var raw struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
				Reasoning string     `json:"reasoning"` // vLLM --reasoning-parser 전용
			} `json:"message"`
		} `json:"choices"`
		Usage *usageInfo `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}
	if len(raw.Choices) == 0 {
		return Response{}, fmt.Errorf("empty choices in response")
	}

	msg := raw.Choices[0].Message
	content := stripThinking(msg.Content)
	toolCalls := msg.ToolCalls

	// Fallback: qwen3_xml parser가 스트리밍에서 tool call을 유실하는 vLLM 버그(#39056) 우회.
	// 비스트리밍 응답에서는 <tool_call> 텍스트가 content에 그대로 남으므로 직접 추출한다.
	if len(toolCalls) == 0 && strings.Contains(content, "<tool_call>") {
		if parsed, cleaned := extractInlineToolCalls(content); len(parsed) > 0 {
			toolCalls = parsed
			content = cleaned
		}
	}

	result := Response{
		Content:   content,
		Thinking:  msg.Reasoning,
		ToolCalls: toolCalls,
	}
	if raw.Usage != nil {
		result.InputTokens = raw.Usage.PromptTokens
		result.OutputTokens = raw.Usage.CompletionTokens
	}

	logToFile(c.model, "RESPONSE", result.Content)
	if len(result.ToolCalls) > 0 {
		logToolCalls(c.model, result.ToolCalls, "")
	}
	return result, nil
}

func (c *OpenAIClient) ChatStream(ctx context.Context, messages []Message, tools []ToolDef, onThinkingToken func(string), onToken func(string)) (Response, error) {
	reqMessages := messages
	reqTools := tools

	if c.useInlineToolCalls {
		reqMessages = c.transformMessagesForInlineTools(messages)
		reqTools = nil // Tool API 비활성화
	}

	reqBody := chatRequest{
		Model:    c.model,
		Messages: reqMessages,
		Tools:    reqTools,
		Stream:   true,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	logRequestJSON(c.model, data)

	resp, err := c.doRequestWith(ctx, data, c.streamClient)
	if err != nil {
		logToFile(c.model, "ERROR", err.Error())
		return Response{}, err
	}
	defer resp.Body.Close()

	return c.parseStream(c.model, resp.Body, onThinkingToken, onToken)
}

func (c *OpenAIClient) parseStream(model string, body io.Reader, onThinkingToken func(string), onToken func(string)) (Response, error) {
	var thinkBuf strings.Builder
	var contentBuf strings.Builder
	var inlineToolBuf strings.Builder // <tool_call> 태그 내용을 담을 버퍼
	toolCallBuf := make(map[int]*ToolCall)
	var inputTokens, outputTokens int

	const (
		tagThinkStart = "<think>"
		tagThinkEnd   = "</think>"
		tagToolStart  = "<tool_call>"
		tagToolEnd    = "</tool_call>"
	)

	// 상태 정의
	const (
		stateContent = iota
		stateThinking
		stateToolCalling
	)

	state := stateContent
	pendingBuf := ""

	flushPending := func(text string) {
		if text == "" {
			return
		}
		switch state {
		case stateThinking:
			thinkBuf.WriteString(text)
			if onThinkingToken != nil {
				onThinkingToken(text)
			}
		case stateToolCalling:
			inlineToolBuf.WriteString(text)
		default:
			contentBuf.WriteString(text)
			if onToken != nil {
				onToken(text)
			}
		}
	}

	processContent := func(incoming string) {
		pendingBuf += incoming
		for {
			switch state {
			case stateThinking:
				idx := strings.Index(pendingBuf, tagThinkEnd)
				if idx >= 0 {
					flushPending(pendingBuf[:idx])
					state = stateContent
					pendingBuf = strings.TrimLeft(pendingBuf[idx+len(tagThinkEnd):], "\n")
				} else {
					safeLen := len(pendingBuf) - (len(tagThinkEnd) - 1)
					if safeLen > 0 {
						flushPending(pendingBuf[:safeLen])
						pendingBuf = pendingBuf[safeLen:]
					}
					return
				}

			case stateToolCalling:
				idx := strings.Index(pendingBuf, tagToolEnd)
				if idx >= 0 {
					flushPending(pendingBuf[:idx])
					inlineToolBuf.WriteString(tagToolEnd)
					state = stateContent
					pendingBuf = pendingBuf[idx+len(tagToolEnd):]
				} else {
					safeLen := len(pendingBuf) - (len(tagToolEnd) - 1)
					if safeLen > 0 {
						flushPending(pendingBuf[:safeLen])
						pendingBuf = pendingBuf[safeLen:]
					}
					return
				}

			default: // stateContent
				thinkIdx := strings.Index(pendingBuf, tagThinkStart)
				toolIdx := -1
				if c.useInlineToolCalls {
					toolIdx = strings.Index(pendingBuf, tagToolStart)
				}

				if thinkIdx >= 0 && (toolIdx < 0 || thinkIdx < toolIdx) {
					flushPending(pendingBuf[:thinkIdx])
					state = stateThinking
					pendingBuf = pendingBuf[thinkIdx+len(tagThinkStart):]
				} else if toolIdx >= 0 {
					flushPending(pendingBuf[:toolIdx])
					state = stateToolCalling
					inlineToolBuf.WriteString(tagToolStart)
					pendingBuf = pendingBuf[toolIdx+len(tagToolStart):]
				} else {
					maxTagLen := 8 // len("<tool_call>") or len("<think>")
					safeLen := len(pendingBuf) - (maxTagLen - 1)
					if safeLen > 0 {
						flushPending(pendingBuf[:safeLen])
						pendingBuf = pendingBuf[safeLen:]
					}
					return
				}
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
		if delta.Reasoning != "" {
			thinkBuf.WriteString(delta.Reasoning)
			if onThinkingToken != nil {
				onThinkingToken(delta.Reasoning)
			}
		}
		if delta.Content != "" {
			processContent(delta.Content)
		}
		for _, tc := range delta.ToolCalls {
			accumulateToolCall(toolCallBuf, tc)
		}
	}

	flushPending(pendingBuf)
	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read stream: %w", err)
	}

	toolCalls := assembleToolCalls(toolCallBuf)
	finalContent := contentBuf.String()

	// 1. API(tool_calls)로부터 받은 것 필터링
	filtered := toolCalls[:0]
	for _, tc := range toolCalls {
		if tc.Function.Name != "" {
			filtered = append(filtered, tc)
		}
	}
	toolCalls = filtered

	// 2. 본문 내 인라인 추출 병합
	if inlineStr := inlineToolBuf.String(); inlineStr != "" {
		slog.Debug("parseStream: inlineToolBuf found", "content", inlineStr)
		if parsed, _ := extractInlineToolCalls(inlineStr); len(parsed) > 0 {
			slog.Debug("parseStream: inline tool calls extracted", "count", len(parsed))
			toolCalls = append(toolCalls, parsed...)
		}
	}

	// 3. Fallback: 상태 머신이 놓친 경우 대비
	if len(toolCalls) == 0 {
		if strings.Contains(finalContent, tagToolStart) {
			slog.Debug("parseStream: fallback content check", "found", true)
			if parsed, cleaned := extractInlineToolCalls(finalContent); len(parsed) > 0 {
				toolCalls = parsed
				finalContent = cleaned
			}
		}
		// thinking 영역에 포함된 경우도 체크 (일부 모델은 thought 내에 툴 호출을 생성하기도 함)
		thinkContent := thinkBuf.String()
		if len(toolCalls) == 0 && strings.Contains(thinkContent, tagToolStart) {
			slog.Debug("parseStream: fallback think check", "found", true)
			if parsed, _ := extractInlineToolCalls(thinkContent); len(parsed) > 0 {
				toolCalls = parsed
			}
		}
	}

	if len(toolCalls) > 0 {
		slog.Info("parseStream: tool calls detected", "count", len(toolCalls))
	}

	logToFile(model, "RESPONSE", finalContent)
	logToolCalls(model, toolCalls, inlineToolBuf.String())
	return Response{
		Content:      finalContent,
		ToolCalls:    toolCalls,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

func (c *OpenAIClient) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	return c.doRequestWith(ctx, body, c.httpClient)
}

func (c *OpenAIClient) doRequestWith(ctx context.Context, body []byte, client *http.Client) (*http.Response, error) {
	url := c.endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return client.Do(req)
}
