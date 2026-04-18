package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/yourorg/infractl/internal/llm"
)

func TestRunSelfClassificationIncludesRecentConversationContext(t *testing.T) {
	client := &classifyModeProbeClient{
		state: &classifyProbeState{},
		resp: llm.Response{
			ToolCalls: []llm.ToolCall{
				{
					Function: llm.FunctionCall{
						Name:      "classify_request",
						Arguments: `{"needs_tools":true,"tool_groups":["file_ops"],"prompt_sections":["tool_selection"],"tier":"general","reasoning":"continuation of an upload task"}`,
					},
				},
			},
		},
	}

	agent := &Agent{
		llmClient: client,
		modelName: "general-model",
	}

	history := []llm.Message{
		{Role: llm.RoleUser, Content: "zip파일 있어"},
		{Role: llm.RoleAssistant, Content: "업로드가 필요하면 말씀해주세요."},
	}

	_, err := agent.runSelfClassification(context.Background(), "가능하니깐 해줘", history...)
	if err != nil {
		t.Fatalf("runSelfClassification() error = %v", err)
	}
	if len(client.state.messages) != 2 {
		t.Fatalf("expected 2 messages passed to classifier, got %d", len(client.state.messages))
	}
	if got := client.state.messages[1].Content; !strings.Contains(got, "최근 대화 컨텍스트:") {
		t.Fatalf("expected recent conversation context in classifier prompt, got %q", got)
	}
	if got := client.state.messages[1].Content; !strings.Contains(got, "zip파일 있어") {
		t.Fatalf("expected previous user message in classifier prompt, got %q", got)
	}
}
