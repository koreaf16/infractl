package agent

import (
	"context"
	"testing"

	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/store"
)

type sequenceLLMClient struct {
	responses []llm.Response
	calls     [][]llm.Message
}

func (c *sequenceLLMClient) Chat(_ context.Context, _ []llm.Message, _ []llm.ToolDef, _ interface{}, opts ...llm.CallOption) (llm.Response, error) {
	return llm.Response{}, nil
}

func (c *sequenceLLMClient) ChatStream(
	_ context.Context,
	messages []llm.Message,
	_ []llm.ToolDef,
	_ interface{},
	_ func(string),
	_ func(string),
	opts ...llm.CallOption,
) (llm.Response, error) {
	snapshot := append([]llm.Message(nil), messages...)
	c.calls = append(c.calls, snapshot)
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

type captureResponseHandler struct {
	noopAgentEventHandler
	responses []string
}

func (h *captureResponseHandler) OnResponse(content string) {
	h.responses = append(h.responses, content)
}

func TestResolveAmbiguityYOROAutoContinues(t *testing.T) {
	a := &Agent{yoroMode: true}
	servers := []store.Server{{Name: "prod"}}
	classification := ClassifyResult{ToolGroups: []string{"connector"}}

	handled, mode := a.resolveAmbiguity(context.Background(), "prod 서버 접속", classification, servers)
	if !handled {
		t.Fatal("expected ambiguity to be auto-resolved in YORO mode")
	}
	if mode != "full_connect" {
		t.Fatalf("mode = %q, want full_connect", mode)
	}
}

