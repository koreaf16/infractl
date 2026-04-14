// Package tools
// File: web_search.go
// Description: SearXNG 웹 검색 도구
// Responsibility: SearXNG 인스턴스 가용 시 웹 검색, 미가용 시 비활성화

package tools

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/web"
)

const (
	internetCheckHost    = "192.168.0.3:30080"
	internetCheckTTL     = 60 * time.Second
	internetCheckTimeout = 5 * time.Second
)

var (
	internetCheckMu      sync.Mutex
	internetLastCheckAt  time.Time
	internetLastCheckRes bool
)

// IsInternetAvailable는 SearXNG 인스턴스 가용 여부를 캐시하여 반환한다.
func IsInternetAvailable() bool {
	internetCheckMu.Lock()
	defer internetCheckMu.Unlock()

	if time.Since(internetLastCheckAt) < internetCheckTTL {
		return internetLastCheckRes
	}

	// SearXNG 가용 여부 체크 (TCP 연결)
	conn, err := net.DialTimeout("tcp", internetCheckHost, internetCheckTimeout)
	if err == nil {
		conn.Close()
		internetLastCheckRes = true
	} else {
		internetLastCheckRes = false
	}
	internetLastCheckAt = time.Now()
	return internetLastCheckRes
}

// WebSearchTool은 SearXNG를 통해 웹 검색을 수행하는 도구이다.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string         { return "web_search" }
func (t *WebSearchTool) RiskLevel() RiskLevel { return RiskNone }
func (t *WebSearchTool) IsReadOnly() bool     { return true }

func (t *WebSearchTool) Description() string {
	return "Search the web using SearXNG. Returns URLs and snippets for the query. Use this to find solutions for unknown errors, documentation for unfamiliar systems, or any information not available locally."
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query (English preferred for better results)",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5, max: 10)",
				"default":     5,
			},
		},
		"required": []string{"query"},
	}
}

// IsEnabled는 항상 true를 반환한다.
// 인터넷 가용성 체크는 Execute() 시점에서 수행되므로, toolDefs에는 항상 포함된다.
// 이전 설계에서 IsEnabled()에서 체크했을 때 toolDefs에서 도구가 사라지면
// LLM이 web_search를 호출하지 못하고 텍스트 응답만 생성하여 루프가 종료되는 문제가 있었다.
func (t *WebSearchTool) IsEnabled() bool { return true }

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	query, err := argString(args, "query", true)
	if err != nil {
		return "", err
	}
	limit := argInt(args, "max_results", 5)
	if limit > 10 {
		limit = 10
	}

	results, err := web.Search(ctx, query, limit)
	if err != nil {
		return "", fmt.Errorf("web search %q: %w", query, err)
	}
	if len(results) == 0 {
		return fmt.Sprintf("No results found for: %q\n(Internet may be unavailable or no matching pages found)", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %q\n\n", query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n   URL: %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", strings.TrimSpace(r.Snippet)))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Use web_fetch with a URL above to get the full content of a page.")
	return sb.String(), nil
}
