package llm

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeLLMLogTextExtractsSSEContent(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"\uc548\ub155"}}]}`,
		`data: {"choices":[{"delta":{"content":"\n\uc138\uacc4"}}]}`,
		`data: [DONE]`,
	}, "\n")

	got := normalizeLLMLogText(raw)
	want := "\uc548\ub155\n\uc138\uacc4"
	if got != want {
		t.Fatalf("normalizeLLMLogText() = %q, want %q", got, want)
	}
}

func TestNormalizeLLMLogTextDecodesEscapes(t *testing.T) {
	got := normalizeLLMLogText(`\uc548\ub155\n\ud14c\uc2a4\ud2b8`)
	want := "\uc548\ub155\n\ud14c\uc2a4\ud2b8"
	if got != want {
		t.Fatalf("normalizeLLMLogText() = %q, want %q", got, want)
	}
}

func TestNormalizeLLMLogTextExtractsChatResponseContent(t *testing.T) {
	raw := `{"choices":[{"message":{"content":"\uc548\ub155\n\uc138\uacc4"}}]}`

	got := normalizeLLMLogText(raw)
	want := "\uc548\ub155\n\uc138\uacc4"
	if got != want {
		t.Fatalf("normalizeLLMLogText() = %q, want %q", got, want)
	}
}

func TestLogRequestJSONWritesAllSentContentToLLMLog(t *testing.T) {
	tmp := chdirTemp(t)

	raw := []byte(`{"messages":[` +
		`{"role":"system","content":"system\nprompt"},` +
		`{"role":"user","content":"\uc548\ub155\n\ud14c\uc2a4\ud2b8"},` +
		`{"role":"assistant","content":"previous answer"},` +
		`{"role":"tool","content":"tool result"}` +
		`]}`)
	logRequestJSON("test-model", raw)

	logText := readLLMLog(t, tmp)
	for _, want := range []string{
		"system\nprompt",
		"\uc548\ub155\n\ud14c\uc2a4\ud2b8",
		"previous answer",
		"tool result",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("llm.log = %q, want it to contain %q", logText, want)
		}
	}
	if strings.Contains(logText, `"content"`) || strings.Contains(logText, `\n`) {
		t.Fatalf("llm.log still contains JSON or escaped newlines: %q", logText)
	}
}

func TestLogResponseJSONWritesOnlyResponseContentToLLMLog(t *testing.T) {
	tmp := chdirTemp(t)

	raw := []byte(`{"choices":[{"message":{"content":"\uc548\ub155\n\uc138\uacc4"}}]}`)
	logResponseJSON("test-model", raw)

	logText := readLLMLog(t, tmp)
	want := "\uc548\ub155\n\uc138\uacc4"
	if !strings.Contains(logText, want) {
		t.Fatalf("llm.log = %q, want it to contain %q", logText, want)
	}
	if strings.Contains(logText, `"choices"`) || strings.Contains(logText, `"content"`) {
		t.Fatalf("llm.log still contains JSON: %q", logText)
	}
}

func TestParseStreamWithoutThinkTagReturnsContentAndLogsReadableText(t *testing.T) {
	tmp := chdirTemp(t)

	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"\uc548\ub155"}}]}`,
		`data: {"choices":[{"delta":{"content":"\n\uc138\uacc4"}}]}`,
		`data: [DONE]`,
	}, "\n")

	c := &OpenAIClient{}
	resp, err := c.parseStream("test-model", strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "\uc548\ub155\n\uc138\uacc4"
	if resp.Content != want {
		t.Fatalf("Content = %q, want %q", resp.Content, want)
	}

	logText := readLLMLog(t, tmp)
	if strings.Contains(logText, "data:") || strings.Contains(logText, `"delta"`) {
		t.Fatalf("llm.log contains raw SSE/JSON: %q", logText)
	}
	if !strings.Contains(logText, want) {
		t.Fatalf("llm.log = %q, want it to contain %q", logText, want)
	}
}

func TestExtractInlineToolCallsParsesJSONFormat(t *testing.T) {
	input := `text before<tool_call>{"name":"server_command","arguments":{"server":"test","command":"ls"}}</tool_call>text after`
	calls, cleaned := extractInlineToolCalls(input)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "server_command" {
		t.Errorf("Name = %q, want server_command", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"server":"test"`) {
		t.Errorf("Arguments = %q, want it to contain server:test", calls[0].Function.Arguments)
	}
	if strings.Contains(cleaned, "<tool_call>") {
		t.Errorf("cleaned content still has <tool_call> tags: %q", cleaned)
	}
}

func TestExtractInlineToolCallsParsesQwenXMLFormat(t *testing.T) {
	input := "<tool_call>\n<function=server_command>\n<parameter=server>\ntest\n</parameter>\n<parameter=command>\ncat /etc/os-release\n</parameter>\n</function>\n</tool_call>"
	calls, cleaned := extractInlineToolCalls(input)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1; cleaned=%q", len(calls), cleaned)
	}
	if calls[0].Function.Name != "server_command" {
		t.Errorf("Name = %q, want server_command", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"server"`) {
		t.Errorf("Arguments = %q, want it to contain server key", calls[0].Function.Arguments)
	}
	if !strings.Contains(calls[0].Function.Arguments, "test") {
		t.Errorf("Arguments = %q, want it to contain 'test' value", calls[0].Function.Arguments)
	}
	if strings.Contains(cleaned, "<tool_call>") {
		t.Errorf("cleaned content still has <tool_call> tags: %q", cleaned)
	}
}

func TestExtractInlineToolCallsMultipleQwenXMLBlocks(t *testing.T) {
	block1 := "<tool_call>\n<function=cmd_a>\n<parameter=x>1</parameter>\n</function>\n</tool_call>"
	block2 := "<tool_call>\n<function=cmd_b>\n<parameter=y>2</parameter>\n</function>\n</tool_call>"
	calls, _ := extractInlineToolCalls(block1 + "\n" + block2)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "cmd_a" || calls[1].Function.Name != "cmd_b" {
		t.Errorf("names = %q %q, want cmd_a cmd_b", calls[0].Function.Name, calls[1].Function.Name)
	}
}

func chdirTemp(t *testing.T) string {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	return tmp
}

func readLLMLog(t *testing.T, dir string) string {
	t.Helper()

	logBytes, err := os.ReadFile(filepath.Join(dir, llmLogPath))
	if err != nil {
		t.Fatal(err)
	}
	bom := []byte{0xEF, 0xBB, 0xBF}
	if !bytes.HasPrefix(logBytes, bom) {
		t.Fatalf("llm.log missing UTF-8 BOM: % x", logBytes[:min(len(logBytes), 3)])
	}
	return string(bytes.TrimPrefix(logBytes, bom))
}
