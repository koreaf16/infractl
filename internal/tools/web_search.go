// Package tools
// File: web_search.go
// Description: DuckDuckGo Lite 웹 검색 도구
// Responsibility: 인터넷 연결 시 웹 검색, 폐쇄망 자동 감지 후 비활성화

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
	internetCheckHost    = "lite.duckduckgo.com"
	internetCheckTTL     = 60 * time.Second
	internetCheckTimeout = 5 * time.Second
)

var (
	internetCheckMu      sync.Mutex
	internetLastCheckAt  time.Time
	internetLastCheckRes bool
)

// IsInternetAvailable는 인터넷 연결 여부를 캐시하여 반환한다.
func IsInternetAvailable() bool {
	internetCheckMu.Lock()
	defer internetCheckMu.Unlock()

	if time.Since(internetLastCheckAt) < internetCheckTTL {
		return internetLastCheckRes
	}

	// DNS 확인으로 인터넷 연결 체크
	conn, err := net.DialTimeout("tcp", internetCheckHost+":443", internetCheckTimeout)
	if err == nil {
		conn.Close()
		internetLastCheckRes = true
	} else {
		internetLastCheckRes = false
	}
	internetLastCheckAt = time.Now()
	return internetLastCheckRes
}

// WebSearchTool은 DuckDuckGo Lite를 통해 웹 검색을 수행하는 도구이다.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) RiskLevel() RiskLevel { return RiskNone }
func (t *WebSearchTool) IsReadOnly() bool     { return true }

func (t *WebSearchTool) Description() string {
	return "Search the web using DuckDuckGo. Returns URLs and snippets for the query. Use this to find solutions for unknown errors, documentation for unfamiliar systems, or any information not available locally."
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

// IsEnabled는 인터넷 연결 여부를 캐시하여 반환한다. 폐쇄망이면 false.
func (t *WebSearchTool) IsEnabled() bool {
	return IsInternetAvailable()
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	query, err := argString(args, "query", true)
	if err != nil {
		return "", err
	}
	limit := argInt(args, "max_results", 5)
	if limit > 10 {
		limit = 10
	}

	results, err := web.SearchDDG(ctx, query, limit)
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
