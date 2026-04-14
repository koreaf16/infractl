package main

import (
	"context"
	"fmt"
	"log"
	"time"
	"github.com/yourorg/infractl/internal/llm"
)

func main() {
	client := llm.NewOpenAIClient(
		"http://192.168.0.3:11434/v1",
		"codgician/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled-GPTQ-int4",
		"",
		60*time.Second,
	)
	
	// Qwen 27B 특수 모드 활성화 (클라이언트 파싱용)
	client.SetUseInlineToolCalls(true)

	fmt.Println("=== Final Validation: Qwen 3.5 27B Streaming + Prompt Tools ===")
	
	onToken := func(t string) {
		fmt.Printf("[%s]", t) // 실시간 스트리밍 확인
	}

	// 서버 에러를 피하기 위해 tools를 nil로 설정하고 프롬프트에 정의를 넣음 (에이전트 로직 재현)
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: `You are a helpful assistant. To use tools, follow this format:
<tool_call>
{"name": "shell_exec", "arguments": {"command": "ls -la"}}
</tool_call>

Available Tool:
- shell_exec: Execute a shell command. Args: {"command": "string"}`},
		{Role: llm.RoleUser, Content: "List files in the current directory using shell_exec."},
	}

	// 명시적 tools 필드는 nil로 보냄
	resp, err := client.ChatStream(context.Background(), messages, nil, nil, onToken)
	if err != nil {
		log.Fatalf("ChatStream failed: %v", err)
	}

	fmt.Printf("\n\n=== Final Response ===\n")
	fmt.Printf("Content: %s\n", resp.Content)
	fmt.Printf("ToolCalls: %+v\n", resp.ToolCalls)
}
