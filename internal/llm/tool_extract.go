// Package llm
// File: tool_extract.go
// Description: ??쎈뱜?귐됱빪 delta 獄??紐껋뵬????용뮞?紐꾨퓠??tool call???곕뗄???롫뮉 ???퐣
// Responsibility: JSON夷똛ML ?臾믩뻼 <tool_call> ?됰뗀以????뼓 獄?delta ?袁⑹읅/鈺곌퀬鍮

package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

var (
	// reLLMTrailingBS: 문자열 종료 직전의 미이스케이프 백슬래시-따옴표.
	// 케이스: "C:\", → "C:\\",  (Windows 경로 trailing backslash)
	reLLMTrailingBS = regexp.MustCompile(`\\"([,}\]"])`)

	// reLLMInvalidEsc: 유효하지 않은 JSON 이스케이프 시퀀스.
	// 케이스: \U, \p 등 JSON 표준 외 문자 → \\U, \\p
	reLLMInvalidEsc = regexp.MustCompile(`\\([^"\\/bfnrtu])`)

	// reShorthandNoArgs: {"tool_name"} 단축 형식 (인자 없음).
	// 케이스: 모델이 인자 없는 툴 호출 시 {"tool_name"} 형태로 출력하는 경우.
	reShorthandNoArgs = regexp.MustCompile(`^\{\s*"([a-zA-Z_][a-zA-Z0-9_]*)"\s*\}$`)
)

// extractInlineToolCalls????용뮞?紐꾨퓠??<tool_call>...</tool_call> ?됰뗀以?????뼓??뺣뼄.
// JSON ?類ㅻ뻼("{"name":...}") ??Qwen3.5 XML ?類ㅻ뻼("<function=NAME>...") ??筌뤴뫀紐?筌왖?癒곕립??
// ???뼓???源껊궗???됰뗀以?? content?癒?퐣 ??볤탢??랁? ??? ??용뮞?硫? cleanedContent嚥?獄쏆꼹???뺣뼄.
func stripThinking(content string) string {
	for _, pair := range [3][2]string{
		{"<think>", "</think>"},
		{"<thought>", "</thought>"},
		{"<thinking>", "</thinking>"},
	} {
		open, close := pair[0], pair[1]
		for {
			start := strings.Index(content, open)
			if start < 0 {
				break
			}
			end := strings.Index(content[start:], close)
			if end < 0 {
				break
			}
			content = content[:start] + content[start+end+len(close):]
		}
	}
	return content
}

func sanitizeAssistantContent(content string) string {
	content = stripThinking(content)
	content = stripOrphanThinkingClosers(content)
	content = stripLeakedReasoningPrelude(content)
	return strings.TrimSpace(content)
}

func SanitizeAssistantContent(content string) string {
	return sanitizeAssistantContent(content)
}

func stripLeakedReasoningPrelude(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	trimmed := strings.TrimSpace(normalized)
	if trimmed == "" {
		return ""
	}

	paragraphs := splitParagraphs(trimmed)
	if len(paragraphs) == 0 || !looksLikeReasoningLeak(paragraphs[0]) {
		return trimmed
	}

	lastLeakIdx := -1
	for i := len(paragraphs) - 1; i >= 0; i-- {
		if looksLikeReasoningLeak(paragraphs[i]) {
			lastLeakIdx = i
			if i+1 < len(paragraphs) {
				return strings.TrimSpace(strings.Join(paragraphs[i+1:], "\n\n"))
			}
			break
		}
	}

	if lastLeakIdx >= 0 {
		return ""
	}
	return trimmed
}

func stripOrphanThinkingClosers(content string) string {
	lower := strings.ToLower(content)
	closers := []string{"</think>", "</thinking>", "</thought>"}
	lastIdx := -1
	lastLen := 0
	for _, closer := range closers {
		if idx := strings.LastIndex(lower, closer); idx >= 0 && idx > lastIdx {
			lastIdx = idx
			lastLen = len(closer)
		}
	}
	if lastIdx >= 0 {
		tail := strings.TrimSpace(content[lastIdx+lastLen:])
		if tail != "" {
			return tail
		}
	}
	replacer := strings.NewReplacer(
		"</think>", "",
		"</thinking>", "",
		"</thought>", "",
	)
	return replacer.Replace(content)
}

func splitParagraphs(content string) []string {
	var paragraphs []string
	var current []string
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, strings.TrimSpace(strings.Join(current, "\n")))
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, strings.TrimSpace(strings.Join(current, "\n")))
	}
	return paragraphs
}

func looksLikeReasoningLeak(paragraph string) bool {
	lower := strings.ToLower(strings.TrimSpace(paragraph))
	if lower == "" {
		return false
	}

	markers := []string{
		"thinking process:",
		"the user is saying",
		"this is a casual greeting",
		"according to my rules",
		"i should not call any tools",
		"i'll respond in",
		"i will respond in",
		"analyze the input:",
		"determine the appropriate response:",
		"draft the response:",
		"refine the response",
		"final polish:",
		"check against constraints:",
		"final output generation",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return regexp.MustCompile(`(?m)^\s*\d+\.\s*(analyze|determine|draft|refine|check|final)\b`).MatchString(lower)
}

func hasPossibleReasoningLeakPrefix(content string) bool {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	trimmed := strings.TrimSpace(normalized)
	if trimmed == "" {
		return false
	}

	firstParagraph := trimmed
	if idx := strings.Index(firstParagraph, "\n\n"); idx >= 0 {
		firstParagraph = strings.TrimSpace(firstParagraph[:idx])
	}
	lower := strings.ToLower(firstParagraph)
	if looksLikeReasoningLeak(lower) {
		return true
	}

	markers := []string{
		"thinking process:",
		"the user is saying",
		"this is a casual greeting",
		"according to my rules",
		"i should not call any tools",
		"i'll respond in",
		"i will respond in",
		"analyze the input:",
		"determine the appropriate response:",
		"draft the response:",
		"refine the response",
		"final polish:",
		"check against constraints:",
		"final output generation",
	}
	for _, marker := range markers {
		if strings.HasPrefix(marker, lower) || strings.HasPrefix(lower, marker) {
			return true
		}
	}

	return regexp.MustCompile(`(?m)^\s*\d+\.?\s*[a-z]*$`).MatchString(lower)
}

func hasUnclosedThinkingTag(content string) bool {
	lower := strings.ToLower(content)
	pairs := [][2]string{
		{"<think>", "</think>"},
		{"<thinking>", "</thinking>"},
		{"<thought>", "</thought>"},
	}
	for _, pair := range pairs {
		open, close := pair[0], pair[1]
		lastOpen := strings.LastIndex(lower, open)
		lastClose := strings.LastIndex(lower, close)
		if lastOpen >= 0 && lastClose < lastOpen {
			return true
		}
	}
	return false
}

func hasDanglingThinkingTagSuffix(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	fragments := []string{
		"<", "</", "<t", "</t", "<th", "</th", "<thi", "</thi",
		"<thin", "</thin", "<think", "</think",
		"<thinki", "</thinki", "<thinkin", "</thinkin",
		"<thought", "</thought",
	}
	for _, frag := range fragments {
		if strings.HasSuffix(lower, frag) {
			return true
		}
	}
	return false
}

func extractInlineToolCalls(content string) (calls []ToolCall, cleanedContent string) {
	content = stripThinking(content)
	const openTag = "<tool_call>"
	const closeTag = "</tool_call>"

	var sb strings.Builder
	remaining := content

	for idx := 0; ; idx++ {
		start := strings.Index(remaining, openTag)
		if start < 0 {
			sb.WriteString(remaining)
			break
		}
		// ??볥젃 ??롮벥 ??용뮞?紐껊뮉 域밸챶?嚥?癰귣똻??
		sb.WriteString(remaining[:start])
		remaining = remaining[start+len(openTag):]

		end := strings.Index(remaining, closeTag)
		if end < 0 {
			// ??ル뮉 ??볥젃 ??곸벉 ???癒??癰귣벊?????ル굝利?
			sb.WriteString(openTag)
			sb.WriteString(remaining)
			break
		}
		jsonStr := strings.TrimSpace(remaining[:end])
		remaining = remaining[end+len(closeTag):]

		// 1??뽰맄: JSON ?類ㅻ뻼 {"name":"...", "arguments":{...}}
		// 1단계: JSON 포맷 {"name":"...", "arguments":{...}}
		// 원본 파싱 실패 시 LLM JSON repair(백슬래시 이스케이프 보정)를 한 번 더 시도.
		// 주 케이스: Windows 경로 미이스케이프 (예: "path":"C:\")
		candidates := []string{jsonStr}
		if repaired := repairLLMJSON(jsonStr); repaired != jsonStr {
			candidates = append(candidates, repaired)
		}
		var matched bool
		for _, candidate := range candidates {
			var rawCall struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(candidate), &rawCall); err == nil && rawCall.Name != "" {
				argStr := string(rawCall.Arguments)
				if argStr == "" || argStr == "null" {
					argStr = "{}"
				}
				calls = append(calls, ToolCall{
					ID:   fmt.Sprintf("inline_%d", idx),
					Type: "function",
					Function: FunctionCall{
						Name:      rawCall.Name,
						Arguments: argStr,
					},
				})
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// 2??뽰맄: Qwen3.5 XML-like ?類ㅻ뻼 <function=NAME><parameter=K>V</parameter>...
		if tc := parseQwenXMLToolCall(jsonStr); tc != nil {
			tc.ID = fmt.Sprintf("inline_%d", idx)
			calls = append(calls, *tc)
			continue
		}

		// 3단계: 단축 형식 파싱 — {"tool_name"} 또는 {"tool_name": {...args}}
		if tc := parseShorthandToolCall(jsonStr); tc != nil {
			tc.ID = fmt.Sprintf("inline_%d", idx)
			calls = append(calls, *tc)
			continue
		}

		slog.Debug("inline tool_call parse failed (json+xml+shorthand)", "content", jsonStr)
	}
	return calls, strings.TrimSpace(sb.String())
}

// parseQwenXMLToolCall?? Qwen3.5 27B揶쎛 ?곗뮆???롫뮉 XML-like tool call ????????뼓??뺣뼄.
//
// ??낆젾 ??됰뻻:
//
//	<function=server_command>
//	<parameter=server>
//	test
//	</parameter>
//	<parameter=command>
//	cat /etc/os-release
//	</parameter>
//	</function>
// parseShorthandToolCall은 모델이 출력하는 비표준 단축 형식을 처리한다.
//
// 지원 형식:
//   - {"tool_name"}            — 인자 없는 호출 (유효하지 않은 JSON)
//   - {"tool_name": {...args}} — 단일 키 오브젝트로 인자 전달
func parseShorthandToolCall(content string) *ToolCall {
	content = strings.TrimSpace(content)

	// 형식 1: {"tool_name"} — 유효하지 않은 JSON, 정규식으로 처리
	if m := reShorthandNoArgs.FindStringSubmatch(content); m != nil {
		return &ToolCall{
			Type: "function",
			Function: FunctionCall{
				Name:      m[1],
				Arguments: "{}",
			},
		}
	}

	// 형식 2: {"tool_name": {...}} — 단일 키 JSON 오브젝트
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err == nil && len(raw) == 1 {
		for name, argsRaw := range raw {
			// tool_name 형식(snake_case 식별자)인지 확인
			if !reShorthandNoArgs.MatchString(`{"` + name + `"}`) {
				continue
			}
			argStr := string(argsRaw)
			if argStr == "" || argStr == "null" {
				argStr = "{}"
			}
			return &ToolCall{
				Type: "function",
				Function: FunctionCall{
					Name:      name,
					Arguments: argStr,
				},
			}
		}
	}

	return nil
}

func parseQwenXMLToolCall(content string) *ToolCall {
	const funcPrefix = "<function="
	const paramPrefix = "<parameter="
	const paramClose = "</parameter>"

	// <function=NAME> ???뼓
	funcStart := strings.Index(content, funcPrefix)
	if funcStart < 0 {
		return nil
	}
	rest := content[funcStart+len(funcPrefix):]
	funcTagEnd := strings.Index(rest, ">")
	if funcTagEnd < 0 {
		return nil
	}
	funcName := strings.TrimSpace(rest[:funcTagEnd])
	if funcName == "" {
		return nil
	}
	rest = rest[funcTagEnd+1:]

	// <parameter=KEY>VALUE</parameter> 獄쏆꼶?????뼓
	args := make(map[string]string)
	for {
		paramStart := strings.Index(rest, paramPrefix)
		if paramStart < 0 {
			break
		}
		rest = rest[paramStart+len(paramPrefix):]
		paramTagEnd := strings.Index(rest, ">")
		if paramTagEnd < 0 {
			break
		}
		paramName := strings.TrimSpace(rest[:paramTagEnd])
		rest = rest[paramTagEnd+1:]

		valueEnd := strings.Index(rest, paramClose)
		if valueEnd < 0 {
			break
		}
		args[paramName] = strings.TrimSpace(rest[:valueEnd])
		rest = rest[valueEnd+len(paramClose):]
	}

	argBytes, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	return &ToolCall{
		Type: "function",
		Function: FunctionCall{
			Name:      funcName,
			Arguments: string(argBytes),
		},
	}
}

// accumulateToolCall?? ??쎈뱜?귐됱빪 delta???紐껊쑔??삵?甕곌쑵????袁⑹읅??뺣뼄.
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

// assembleToolCalls??甕곌쑵???tool_calls???紐껊쑔????뽰몵嚥??類ｌ졊???????곷뮞嚥?獄쏆꼹???뺣뼄.
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

// repairLLMJSON는 LLM이 생성한 JSON에서 흔히 발생하는 백슬래시 인코딩 오류를 보정한다.
//
// 보정 케이스:
//  1. Windows 경로 trailing backslash: "C:\", → "C:\\",
//  2. 유효하지 않은 이스케이프: \U, \p 등 → \\U, \\p
func repairLLMJSON(s string) string {
	// 1단계: 문자열 종료 직전의 \": "C:\", → "C:\\",
	s = reLLMTrailingBS.ReplaceAllStringFunc(s, func(m string) string {
		// m = `\"` + terminator (e.g., `\",`)
		// → `\\"` + terminator  (escaped backslash + closing quote)
		return `\\"` + m[2:]
	})
	// 2단계: 나머지 유효하지 않은 이스케이프 시퀀스.
	// \U, \p, \( 등 JSON 표준에 없는 → \\U, \\p, \\(
	s = reLLMInvalidEsc.ReplaceAllString(s, `\\$1`)
	return s
}
