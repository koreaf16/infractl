// Package tools
// File: web_fetch.go
// Description: URL 내용을 가져와 Markdown으로 변환하는 도구
// Responsibility: HTTP GET + HTML→Markdown 변환, fast-tier LLM 3단계 압축 처리

package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/web"
)

const (
	// autoSummarizeThreshold: 이 크기를 초과하면 prompt 없이도 자동 요약을 수행한다.
	autoSummarizeThreshold = 15 * 1024 // 15KB
)

// WebFetchTool은 URL의 HTML 내용을 Markdown으로 변환하여 반환하는 도구이다.
// prompt 파라미터가 있으면 fast-tier LLM으로 요약한다.
// prompt 없이도 15KB 초과 시 fast-tier LLM이 자동으로 핵심만 요약한다.
// Reranker가 설정되어 있으면 여러 URL에서 추출한 결과를 재순위화할 수 있다 (향후 확장).
type WebFetchTool struct {
	Fetcher     *web.Fetcher
	LLMClient   llm.Client    // 기본 general-tier 클라이언트 (폴백용)
	LLMRegistry *llm.Registry // fast-tier 자동 요약에 사용 (nil이면 LLMClient 폴백)
	Reranker    rag.Reranker  // nil 허용 — 향후 다중 URL 결과 리랭킹에 사용
}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) RiskLevel() RiskLevel { return RiskNone }
func (t *WebFetchTool) IsReadOnly() bool     { return true }
func (t *WebFetchTool) IsEnabled() bool      { return IsInternetAvailable() }

func (t *WebFetchTool) Description() string {
	return "Fetch the content of a URL and convert it to Markdown. Optionally provide a prompt to extract specific information from the page using LLM. Large pages (>15KB) are automatically summarized. Use after web_search to read the full content of a result."
}

func (t *WebFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch content from",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Optional: specific question or extraction request to apply to the fetched content using LLM. If omitted and page is large, auto-summarized.",
			},
			"max_length": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters to return (default: 100000)",
				"default":     100000,
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	rawURL, err := argString(args, "url", true)
	if err != nil {
		return "", err
	}
	prompt, _ := argString(args, "prompt", false)

	md, err := t.Fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}

	fastClient := t.resolveFastClient()

	// prompt 있음 → fast-tier LLM으로 질문에 대한 답 추출
	if prompt != "" && fastClient != nil {
		result, err := t.applyPrompt(ctx, fastClient, rawURL, md, prompt)
		if err != nil {
			slog.Warn("web fetch prompt failed, returning raw markdown", "url", rawURL, "err", err)
			return fmt.Sprintf("Content from %s:\n\n%s", rawURL, md), nil
		}
		return result, nil
	}

	// prompt 없음 + 대용량 → fast-tier LLM 자동 요약
	if len(md) > autoSummarizeThreshold && fastClient != nil {
		slog.Debug("web fetch auto-summarizing large content", "url", rawURL, "len", len(md))
		result, err := t.autoSummarize(ctx, fastClient, rawURL, md)
		if err != nil {
			slog.Warn("web fetch auto-summarize failed, returning raw markdown", "url", rawURL, "err", err)
			return fmt.Sprintf("Content from %s:\n\n%s", rawURL, md), nil
		}
		return result, nil
	}

	// 소용량 or LLM 없음 → Markdown 그대로 반환
	return fmt.Sprintf("Content from %s:\n\n%s", rawURL, md), nil
}

// resolveFastClient는 fast-tier 클라이언트를 반환한다.
// Registry에 fast 티어가 있으면 그것을 사용하고, 없으면 LLMClient로 폴백한다.
func (t *WebFetchTool) resolveFastClient() llm.Client {
	if t.LLMRegistry != nil {
		if c, _, err := t.LLMRegistry.Resolve(llm.TierFast); err == nil {
			return c
		}
	}
	return t.LLMClient
}

// applyPrompt는 변환된 Markdown에 LLM 프롬프트를 적용하여 결과를 반환한다.
func (t *WebFetchTool) applyPrompt(ctx context.Context, client llm.Client, rawURL, markdown, prompt string) (string, error) {
	userMsg := llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf("URL: %s\n\nPage content:\n%s\n\n---\nQuestion: %s",
			rawURL, markdown, prompt),
	}
	resp, err := client.Chat(ctx, []llm.Message{userMsg}, nil)
	if err != nil {
		return "", fmt.Errorf("llm apply prompt: %w", err)
	}
	return fmt.Sprintf("Content from %s (processed):\n\n%s", rawURL, resp.Content), nil
}

// autoSummarize는 대용량 마크다운을 fast-tier LLM으로 핵심 내용만 추출한다.
func (t *WebFetchTool) autoSummarize(ctx context.Context, client llm.Client, rawURL, markdown string) (string, error) {
	userMsg := llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf(
			"URL: %s\n\nPage content (long, needs summarization):\n%s\n\n---\n"+
				"Please provide a comprehensive summary of this page's key information. "+
				"Preserve important technical details, numbers, and structured data.",
			rawURL, markdown),
	}
	resp, err := client.Chat(ctx, []llm.Message{userMsg}, nil)
	if err != nil {
		return "", fmt.Errorf("llm auto-summarize: %w", err)
	}
	return fmt.Sprintf("Content from %s (auto-summarized):\n\n%s", rawURL, resp.Content), nil
}
