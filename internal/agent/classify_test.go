package agent

import (
	"context"
	"testing"

	"github.com/yourorg/infractl/internal/llm"
)

type classifyModeProbeClient struct {
	withInlineArgs []bool
	chatCalled     bool
	resp           llm.Response
}

func (c *classifyModeProbeClient) Chat(_ context.Context, _ []llm.Message, _ []llm.ToolDef, _ interface{}) (llm.Response, error) {
	c.chatCalled = true
	return c.resp, nil
}

func (c *classifyModeProbeClient) ChatStream(_ context.Context, _ []llm.Message, _ []llm.ToolDef, _ interface{}, _ func(string), _ func(string)) (llm.Response, error) {
	c.chatCalled = true
	return c.resp, nil
}

func (c *classifyModeProbeClient) WithInlineToolCalls(enabled bool) llm.Client {
	c.withInlineArgs = append(c.withInlineArgs, enabled)
	return &classifyModeProbeClient{
		withInlineArgs: c.withInlineArgs,
		resp:           c.resp,
	}
}

func TestLLMClassifyDisablesInlineToolCallsForClassification(t *testing.T) {
	client := &classifyModeProbeClient{
		resp: llm.Response{
			ToolCalls: []llm.ToolCall{
				{
					Function: llm.FunctionCall{
						Name:      "classify_request",
						Arguments: `{"needs_tools":true,"tool_groups":["shell"],"prompt_sections":["safety"],"tier":"reasoning","reasoning":"needs analysis"}`,
					},
				},
			},
		},
	}

	agent := &Agent{
		llmClient: client,
		modelName: "general-model",
	}

	result, err := agent.llmClassify(context.Background(), "왜 서비스가 죽었는지 분석해줘")
	if err != nil {
		t.Fatalf("llmClassify() error = %v", err)
	}
	if len(client.withInlineArgs) != 1 {
		t.Fatalf("expected WithInlineToolCalls to be called once, got %d", len(client.withInlineArgs))
	}
	if client.withInlineArgs[0] {
		t.Fatal("expected classification client to disable inline tool calls")
	}
	if client.chatCalled {
		t.Fatal("expected shared base client not to execute Chat directly")
	}
	if result.Tier != "reasoning" {
		t.Fatalf("expected reasoning tier, got %q", result.Tier)
	}
	if !result.NeedsTools {
		t.Fatal("expected needs_tools=true")
	}
}
