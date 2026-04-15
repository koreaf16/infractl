// Package tools
// File: rag_search.go
// Description: rag_search 도구 — 로컬 + 외부 RAG 소스 통합 벡터 검색
// Responsibility: LLM이 호출하는 RAG 검색 도구, 검색 우선순위 0→1 결과를 포맷하여 반환

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/rag"
)

// RAGSearchTool은 로컬 + 외부 RAG 소스를 통합 검색하는 도구이다.
type RAGSearchTool struct {
	Manager *rag.Manager
}

var _ Tool = (*RAGSearchTool)(nil)

func (t *RAGSearchTool) Name() string         { return "rag_search" }
func (t *RAGSearchTool) IsReadOnly() bool     { return true }
func (t *RAGSearchTool) IsEnabled() bool      { return true }

func (t *RAGSearchTool) Description() string {
	return "Search knowledge in priority order: local learned patterns (0) → registered external RAG databases (1). " +
		"ALWAYS try this before web_search for troubleshooting, error resolution, procedures, and incident history."
}

func (t *RAGSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "검색 질문 또는 에러 메시지",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "최대 반환 건수 (기본값: 5)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *RAGSearchTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "query 파라미터가 필요합니다", nil
	}

	topK := 5
	if v, ok := args["max_results"].(float64); ok && int(v) > 0 {
		topK = int(v)
	}

	results, err := t.Manager.Search(ctx, query, topK)
	if err != nil {
		return fmt.Sprintf("RAG 검색 오류: %s", err), nil
	}

	if len(results) == 0 {
		return "RAG 검색 결과 없음. web_search를 사용하여 인터넷에서 검색하세요.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## RAG 검색 결과 (%d건)\n\n", len(results)))

	for i, r := range results {
		sourceLabel := formatSourceLabel(r)
		sb.WriteString(fmt.Sprintf("### %d. %s [%s]\n", i+1, r.Title, sourceLabel))
		if r.Score > 0 {
			sb.WriteString(fmt.Sprintf("유사도: %.2f\n", r.Score))
		}
		sb.WriteString(r.Content)
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}

func formatSourceLabel(r rag.SearchResult) string {
	if r.Source == "local" {
		return "로컬 학습 지식"
	}
	return fmt.Sprintf("RAG: %s", r.Source)
}
