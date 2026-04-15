// Package tools
// File: web_fetch.go
// Description: URL 내용을 가져와 Markdown으로 변환하는 도구
// Responsibility: HTTP GET + HTML→Markdown 변환, fast-tier LLM 3단계 압축 처리

package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
	OutputCb    func(string)  // 단계별 진행 상황을 실시간으로 전달하는 콜백 (nil 허용)
}

// emit은 OutputCb가 설정된 경우 진행 상황 메시지를 전달한다.
func (t *WebFetchTool) emit(msg string) {
	if t.OutputCb != nil {
		t.OutputCb(msg)
	}
}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) IsReadOnly() bool     { return true }
// IsEnabled는 항상 true를 반환한다. 인터넷 가용성은 Execute() 시점에서 판단한다.
func (t *WebFetchTool) IsEnabled() bool { return true }

func (t *WebFetchTool) Description() string {
	return "Fetch the content of a URL and convert it to Markdown. Optionally provide a prompt to extract specific information from the page using LLM. Large pages (>15KB) are automatically summarized. Use after web_search to read the full content of a result."
}

func (t *WebFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch content from (legacy, for single URL)",
			},
			"urls": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of URLs to fetch in parallel.",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Optional: specific question or extraction request to apply to the fetched content using LLM.",
			},
			"max_length": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters to return (default: 100000)",
				"default":     100000,
			},
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	var urls []string
	if u, _ := argString(args, "url", false); u != "" {
		urls = append(urls, u)
	}
	if uArr, ok := args["urls"].([]interface{}); ok {
		for _, u := range uArr {
			if s, ok := u.(string); ok {
				urls = append(urls, s)
			}
		}
	}

	if len(urls) == 0 {
		return "Error: no URL provided", nil
	}

	prompt, _ := argString(args, "prompt", false)
	fastClient := t.resolveFastClient()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make(map[string]string)
		errs    []string
	)

	slog.Info("web_fetch_start", "count", len(urls))

	for _, rawURL := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			slog.Info("task_start", "task", u)
			start := time.Now()

			md, err := t.Fetcher.Fetch(ctx, u)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("fetch %s: %v", u, err))
				mu.Unlock()
				slog.Info("task_fail", "task", u, "dur", time.Since(start))
				return
			}

			content := md
			// apply prompt or summarize if needed
			if prompt != "" && fastClient != nil {
				if processed, pErr := t.applyPrompt(ctx, fastClient, u, md, prompt); pErr == nil {
					content = processed
				}
			} else if len(md) > autoSummarizeThreshold && fastClient != nil {
				if summarized, sErr := t.autoSummarize(ctx, fastClient, u, md); sErr == nil {
					content = summarized
				}
			}

			mu.Lock()
			results[u] = content
			mu.Unlock()
			slog.Info("task_done", "task", u, "dur", time.Since(start))
		}(rawURL)
	}

	wg.Wait()

	var output []string
	for _, u := range urls {
		if content, ok := results[u]; ok {
			output = append(output, content)
		}
	}
	output = append(output, errs...)

	return strings.Join(output, "\n\n---\n\n"), nil
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
	resp, err := client.Chat(ctx, []llm.Message{userMsg}, nil, nil)
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
	resp, err := client.Chat(ctx, []llm.Message{userMsg}, nil, nil)
	if err != nil {
		return "", fmt.Errorf("llm auto-summarize: %w", err)
	}
	return fmt.Sprintf("Content from %s (auto-summarized):\n\n%s", rawURL, resp.Content), nil
}
