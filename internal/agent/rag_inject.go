package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/infractl/internal/rag"
)

// buildKnowledgeContext injects scoped internal memory hits before the tool loop.
func buildKnowledgeContext(ctx context.Context, ragMgr *rag.Manager, userInput, activeServerName string, conversationID int64) string {
	if ragMgr == nil || strings.TrimSpace(userInput) == "" {
		return ""
	}

	opts := rag.SearchOptions{
		TopK:       3,
		MinScore:   0.05,
		LocalOnly:  true,
		ServerName: strings.TrimSpace(activeServerName),
	}
	if conversationID > 0 {
		opts.ConversationID = &conversationID
	} else {
		opts.SourceTypes = []string{"knowledge", "learned_system"}
	}

	results, err := ragMgr.SearchWithOptions(ctx, userInput, opts)
	if err != nil {
		slog.Debug("pre-tool knowledge search failed", "err", err)
		return ""
	}
	if len(results) == 0 {
		return ""
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
		sb.WriteString(fmt.Sprintf("**%d. [%s] %s** (score %.2f)\n", i+1, sourceType, r.Title, r.Score))
		sb.WriteString(truncateKnowledgeSnippet(r.Content, 600))
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}

func truncateKnowledgeSnippet(content string, limit int) string {
	content = strings.TrimSpace(content)
	if limit <= 0 || len(content) <= limit {
		return content
	}
	return content[:limit] + "..."
}
