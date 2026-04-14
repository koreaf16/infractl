// Package agent
// File: rag_inject.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/infractl/internal/rag"
)

// buildKnowledgeContext injects scoped internal memory hits before the tool loop.
// Returns the formatted context string and the number of results injected.
// Only knowledge and learned_system entries are searched — never past session messages.
func buildKnowledgeContext(ctx context.Context, ragMgr *rag.Manager, userInput, activeServerName string) (string, int) {
	if ragMgr == nil || strings.TrimSpace(userInput) == "" {
		return "", 0
	}

	opts := rag.SearchOptions{
		TopK:        3,
		MinScore:    0.05,
		LocalOnly:   true,
		ServerName:  strings.TrimSpace(activeServerName),
		SourceTypes: []string{"knowledge", "learned_system"},
	}

	results, err := ragMgr.SearchWithOptions(ctx, userInput, opts)
	if err != nil {
		slog.Debug("pre-tool knowledge search failed", "err", err)
		return "", 0
	}
	if len(results) == 0 {
		return "", 0
	}

	var sb strings.Builder
	sb.WriteString("## Relevant Internal Memory\n")
	sb.WriteString("Use the following scoped internal memory before issuing commands.\n")
	sb.WriteString("Prefer concrete prior fixes only when the current target and session context still match.\n\n")

	for i, r := range results {
		sourceType := r.LocalSourceType
		if sourceType == "" {
			sourceType = "memory"
		}
		fmt.Fprintf(&sb, "**%d. [%s] %s** (score %.2f)\n", i+1, sourceType, r.Title, r.Score)
		sb.WriteString(truncateKnowledgeSnippet(r.Content, 600))
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String()), len(results)
}

// prefetchKnowledgeAsync는 사용자 입력에 대한 관련 내부 지식을 비동기로 검색한다.
// classification과 병렬로 실행하여 임베딩 검색 지연을 숨긴다.
// ragMgr가 nil이거나 입력이 비어 있으면 즉시 빈 문자열을 채널에 보낸다.
func prefetchKnowledgeAsync(ctx context.Context, ragMgr *rag.Manager, userInput, serverName string) <-chan string {
	ch := make(chan string, 1)
	if ragMgr == nil || strings.TrimSpace(userInput) == "" {
		ch <- ""
		return ch
	}
	go func() {
		snippet, _ := buildKnowledgeContext(ctx, ragMgr, userInput, serverName)
		ch <- snippet
	}()
	return ch
}

func truncateKnowledgeSnippet(content string, limit int) string {
	content = strings.TrimSpace(content)
	if limit <= 0 || len(content) <= limit {
		return content
	}
	return content[:limit] + "..."
}
