package agent

import (
	"strings"
	"testing"

	"github.com/yourorg/infractl/internal/llm"
)

func TestIsEmptyResponse(t *testing.T) {
	tests := []struct {
		name string
		resp llm.Response
		want bool
	}{
		{
			name: "truly empty",
			resp: llm.Response{},
			want: true,
		},
		{
			name: "whitespace only",
			resp: llm.Response{Content: "   \n\t  "},
			want: true,
		},
		{
			name: "has content",
			resp: llm.Response{Content: "hello"},
			want: false,
		},
		{
			name: "has tool calls",
			resp: llm.Response{ToolCalls: []llm.ToolCall{{ID: "1"}}},
			want: false,
		},
		{
			name: "has both",
			resp: llm.Response{Content: "text", ToolCalls: []llm.ToolCall{{ID: "1"}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyResponse(tt.resp); got != tt.want {
				t.Errorf("isEmptyResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNudgeMessage(t *testing.T) {
	// attempt 1: 첫 번째 nudge
	msg1 := nudgeMessage(1)
	if msg1.Role != llm.RoleUser {
		t.Errorf("expected role user, got %s", msg1.Role)
	}
	if !strings.Contains(msg1.Content, "[RETRY]") {
		t.Error("nudge message should contain [RETRY] tag")
	}

	// attempt 2: 더 강한 nudge
	msg2 := nudgeMessage(2)
	if !strings.Contains(msg2.Content, "반드시") {
		t.Error("second nudge should be more assertive")
	}

	// 메시지가 다른지 확인
	if msg1.Content == msg2.Content {
		t.Error("nudge messages for different attempts should differ")
	}
}

func TestForceResponseMessage(t *testing.T) {
	msg := forceResponseMessage(42)

	if msg.Role != llm.RoleUser {
		t.Errorf("expected role user, got %s", msg.Role)
	}
	if !strings.Contains(msg.Content, "[FINAL TURN]") {
		t.Error("force response message should contain [FINAL TURN] tag")
	}
	if !strings.Contains(msg.Content, "42") {
		t.Error("force response message should contain the tool call count")
	}
}
