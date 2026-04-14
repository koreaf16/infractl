// Package llm
// File: tool_extract.go
// Description: ??쎈뱜?귐됱빪 delta 獄??紐껋뵬????용뮞?紐꾨퓠??tool call???곕뗄???롫뮉 ???퐣
// Responsibility: JSON夷똛ML ?臾믩뻼 <tool_call> ?됰뗀以????뼓 獄?delta ?袁⑹읅/鈺곌퀬鍮

package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// extractInlineToolCalls????용뮞?紐꾨퓠??<tool_call>...</tool_call> ?됰뗀以?????뼓??뺣뼄.
// JSON ?類ㅻ뻼("{"name":...}") ??Qwen3.5 XML ?類ㅻ뻼("<function=NAME>...") ??筌뤴뫀紐?筌왖?癒곕립??
// ???뼓???源껊궗???됰뗀以?? content?癒?퐣 ??볤탢??랁? ??? ??용뮞?硫? cleanedContent嚥?獄쏆꼹???뺣뼄.
func stripThinking(content string) string {
    const thoughtOpen = "<thought>"
    const thoughtClose = "</thought>"
    for {
        start := strings.Index(content, thoughtOpen)
        if start < 0 { break }
        end := strings.Index(content[start:], thoughtClose)
        if end < 0 { break }
        content = content[:start] + content[start+end+len(thoughtClose):]
    }
    const thinkingOpen = "<thinking>"
    const thinkingClose = "</thinking>"
    for {
        start := strings.Index(content, thinkingOpen)
        if start < 0 { break }
        end := strings.Index(content[start:], thinkingClose)
        if end < 0 { break }
        content = content[:start] + content[start+end+len(thinkingClose):]
    }
    return content
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
		var rawCall struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &rawCall); err == nil && rawCall.Name != "" {
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
			continue
		}

		// 2??뽰맄: Qwen3.5 XML-like ?類ㅻ뻼 <function=NAME><parameter=K>V</parameter>...
		if tc := parseQwenXMLToolCall(jsonStr); tc != nil {
			tc.ID = fmt.Sprintf("inline_%d", idx)
			calls = append(calls, *tc)
			continue
		}

		slog.Debug("inline tool_call parse failed (json+xml)", "content", jsonStr)
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
