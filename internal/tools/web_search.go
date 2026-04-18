package tools

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/web"
)

const (
	internetCheckHost    = "192.168.0.3:30080"
	internetCheckTTL     = 60 * time.Second
	internetCheckTimeout = 5 * time.Second
	webSearchMaxUses     = 8
)

var (
	internetCheckMu      sync.Mutex
	internetLastCheckAt  time.Time
	internetLastCheckRes bool
)

// IsInternetAvailable caches SearXNG reachability.
func IsInternetAvailable() bool {
	internetCheckMu.Lock()
	defer internetCheckMu.Unlock()

	if time.Since(internetLastCheckAt) < internetCheckTTL {
		return internetLastCheckRes
	}

	conn, err := net.DialTimeout("tcp", internetCheckHost, internetCheckTimeout)
	if err == nil {
		_ = conn.Close()
		internetLastCheckRes = true
	} else {
		internetLastCheckRes = false
	}
	internetLastCheckAt = time.Now()
	return internetLastCheckRes
}

// WebSearchTool performs multiple SearXNG searches in parallel and returns source links.
type WebSearchTool struct {
	Fetcher     *web.Fetcher
	LLMClient   llm.Client
	LLMRegistry *llm.Registry
	SearchFn    func(ctx context.Context, query string, limit int, opts ...web.SearchOption) ([]web.SearchResult, error)
}

func (t *WebSearchTool) Name() string     { return "web_search" }
func (t *WebSearchTool) IsReadOnly() bool { return true }
func (t *WebSearchTool) IsEnabled() bool  { return true }

func (t *WebSearchTool) IsConcurrencySafe(_ map[string]interface{}) bool {
	return true
}

func (t *WebSearchTool) Description() string {
	return "Search the web and return source links for current facts, docs, and release notes. Supports multiple parallel queries."
}

func (t *WebSearchTool) ModelPrompt() string {
	currentYear := time.Now().Year()
	return fmt.Sprintf(
		"Use web_search for current facts, version-sensitive questions, official docs, release notes, and external verification. " +
		"You can provide multiple queries in the 'queries' array to search in parallel. " +
		"Use web_fetch for deeper inspection of a specific URL from results. " +
		"Always include a Sources section with markdown links from results. " +
		"Use allowed_domains or blocked_domains only when filtering is required. " +
		"Current year: %d.",
		currentYear,
	)
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "A single search query (deprecated, use 'queries' instead).",
			},
			"queries": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Multiple search queries to be executed in parallel.",
			},
			"allowed_domains": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Only include search results from these domains.",
			},
			"blocked_domains": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Never include search results from these domains.",
			},
		},
	}
}

type queryResult struct {
	query    string
	results  []web.SearchResult
	duration time.Duration
	err      error
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (ToolOutcome, error) {
	var queries []string

	// Handle 'queries' array
	if qList, ok := args["queries"].([]interface{}); ok {
		for _, q := range qList {
			if s, ok := q.(string); ok && s != "" {
				queries = append(queries, strings.TrimSpace(s))
			}
		}
	}

	// Handle single 'query' for backward compatibility
	if q, ok := args["query"].(string); ok && q != "" {
		queries = append(queries, strings.TrimSpace(q))
	}

	if len(queries) == 0 {
		msg := "Error: at least one search query is required"
		return ToolOutcome{Content: msg, Success: false, ErrorMessage: msg}, nil
	}

	// Deduplicate queries
	queries = deduplicate(queries)

	allowedDomains := argStringSlice(args, "allowed_domains")
	blockedDomains := argStringSlice(args, "blocked_domains")
	if len(allowedDomains) > 0 && len(blockedDomains) > 0 {
		msg := "Error: cannot specify both allowed_domains and blocked_domains in the same request"
		return ToolOutcome{Content: msg, Success: false, ErrorMessage: msg}, nil
	}

	searchFn := t.SearchFn
	if searchFn == nil {
		searchFn = web.Search
	}

	var wg sync.WaitGroup
	resultsChan := make(chan queryResult, len(queries))

	for _, q := range queries {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()
			start := time.Now()
			res, err := searchFn(
				ctx,
				query,
				webSearchMaxUses,
				web.WithAllowedDomains(allowedDomains...),
				web.WithBlockedDomains(blockedDomains...),
			)
			resultsChan <- queryResult{
				query:    query,
				results:  res,
				duration: time.Since(start),
				err:      err,
			}
		}(q)
	}

	wg.Wait()
	close(resultsChan)

	var sb strings.Builder
	totalResults := 0
	var firstErr error

	for res := range resultsChan {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		totalResults += len(res.results)
		sb.WriteString(formatWebSearchResults(res.query, res.results, res.duration))
		sb.WriteString("\n\n---\n\n")
	}

	if totalResults == 0 && firstErr != nil {
		return ToolOutcome{}, fmt.Errorf("web search failed: %w", firstErr)
	}

	content := strings.TrimSpace(sb.String())
	if content == "" {
		content = "No results found for the provided queries."
	}

	return ToolOutcome{
		Content: content,
		Success: true,
	}, nil
}

func deduplicate(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func formatWebSearchResults(query string, results []web.SearchResult, duration time.Duration) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for query: %q\n", query))
	sb.WriteString(fmt.Sprintf("Found %d results in %s\n\n", len(results), formatSearchDuration(duration)))

	if len(results) > 0 {
		sb.WriteString("Sources:\n")
		for _, r := range results {
			sb.WriteString(fmt.Sprintf("- [%s](%s)\n", markdownSafeText(nonEmpty(r.Title, r.URL)), r.URL))
		}
	}

	return sb.String()
}

func formatSearchDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	}
	return fmt.Sprintf("%dms", int(d.Round(time.Millisecond)/time.Millisecond))
}

func markdownSafeText(in string) string {
	s := strings.ReplaceAll(in, "[", "(")
	s = strings.ReplaceAll(s, "]", ")")
	return strings.TrimSpace(s)
}

func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
