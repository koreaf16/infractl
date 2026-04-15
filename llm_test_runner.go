//go:build tools
// +build tools

package main

import (
	"context"
	"fmt"
	"github.com/yourorg/infractl/internal/llm"
	"log"
	"time"
)

func main() {
	client := llm.NewOpenAIClient(
		"http://192.168.0.3:11434/v1",
		"codgician/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled-GPTQ-int4",
		"",
		60*time.Second,
	)

	// Qwen 27B ?뱀닔 紐⑤뱶 ?쒖꽦??(?대씪?댁뼵???뚯떛??
	client.SetUseInlineToolCalls(true)

	fmt.Println("=== Final Validation: Qwen 3.5 27B Streaming + Prompt Tools ===")

	onToken := func(t string) {
		fmt.Printf("[%s]", t) // ?ㅼ떆媛??ㅽ듃由щ컢 ?뺤씤
	}

	// ?쒕쾭 ?먮윭瑜??쇳븯湲??꾪븯??tools瑜?nil濡??ㅼ젙?섍퀬 ?꾨＼?꾪듃???뺤쓽瑜??ｌ쓬 (?먯씠?꾪듃 濡쒖쭅 ?ы쁽)
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: `You are a helpful assistant. To use tools, follow this format:
<tool_call>
{"name": "shell_exec", "arguments": {"command": "ls -la"}}
</tool_call>

Available Tool:
- shell_exec: Execute a shell command. Args: {"command": "string"}`},
		{Role: llm.RoleUser, Content: "List files in the current directory using shell_exec."},
	}

	// 紐낆떆??tools ?꾨뱶??nil濡?蹂대깂
	resp, err := client.ChatStream(context.Background(), messages, nil, nil, nil, onToken)
	if err != nil {
		log.Fatalf("ChatStream failed: %v", err)
	}

	fmt.Printf("\n\n=== Final Response ===\n")
	fmt.Printf("Content: %s\n", resp.Content)
	fmt.Printf("ToolCalls: %+v\n", resp.ToolCalls)
}


