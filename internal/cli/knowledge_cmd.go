// Package cli
// File: knowledge_cmd.go
// Description: /knowledge 슬래시 명령 핸들러
// Responsibility: 지식 베이스 목록 조회, FTS5 검색, 항목 삭제 처리

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/yourorg/infractl/internal/store"
)

// handleKnowledgeCommand는 /knowledge 슬래시 명령을 처리한다.
func (r *REPL) handleKnowledgeCommand(ctx context.Context, parts []string) {
	if r.knowledgeStore == nil {
		fmt.Println("지식 저장소가 초기화되지 않았습니다.")
		return
	}

	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "search":
		if len(parts) < 3 {
			fmt.Println("사용법: /knowledge search <검색어>")
			return
		}
		query := strings.Join(parts[2:], " ")
		r.printKnowledgeSearch(ctx, query)
	case "delete":
		if len(parts) < 3 {
			fmt.Println("사용법: /knowledge delete <ID>")
			return
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			fmt.Printf("유효하지 않은 ID: %s\n", parts[2])
			return
		}
		if err := r.knowledgeStore.DeleteKnowledge(ctx, id); err != nil {
			fmt.Printf("✗ 삭제 실패: %s\n", err)
			return
		}
		fmt.Printf("✓ 지식 항목 #%d 삭제 완료\n", id)
	default:
		r.printKnowledgeList(ctx)
	}
}

// printKnowledgeList는 최근 지식 목록 20개를 출력한다.
func (r *REPL) printKnowledgeList(ctx context.Context) {
	entries, err := r.knowledgeStore.ListKnowledge(ctx, "", 20)
	if err != nil {
		fmt.Printf("지식 목록 조회 실패: %s\n", err)
		return
	}
	if len(entries) == 0 {
		fmt.Println("저장된 지식 없음. knowledge_add 도구 또는 '기억해줘' 명령으로 추가할 수 있습니다.")
		return
	}
	fmt.Printf("저장된 지식 (%d개):\n", len(entries))
	fmt.Printf("  %-6s %-16s %-35s %-8s\n", "ID", "카테고리", "제목", "사용횟수")
	fmt.Println("  " + strings.Repeat("-", 70))
	for _, e := range entries {
		title := e.Title
		if len(title) > 33 {
			title = title[:33] + ".."
		}
		fmt.Printf("  %-6d %-16s %-35s %d회\n", e.ID, e.Category, title, e.UseCount)
	}
	fmt.Println()
	fmt.Println("상세 보기: /knowledge search <키워드> | 삭제: /knowledge delete <ID>")
}

// printKnowledgeSearch는 FTS5 검색 결과를 출력한다.
func (r *REPL) printKnowledgeSearch(ctx context.Context, query string) {
	entries, err := r.knowledgeStore.SearchKnowledge(ctx, query, 10)
	if err != nil {
		fmt.Printf("검색 실패: %s\n", err)
		return
	}
	if len(entries) == 0 {
		fmt.Printf("'%s' 검색 결과 없음\n", query)
		return
	}
	fmt.Printf("'%s' 검색 결과 (%d개):\n\n", query, len(entries))
	printKnowledgeEntries(entries)
}

// printKnowledgeEntries는 지식 항목 목록을 상세 형식으로 출력한다.
func printKnowledgeEntries(entries []store.KnowledgeEntry) {
	for _, e := range entries {
		fmt.Printf("  [#%d] %s (%s)\n", e.ID, e.Title, e.Category)
		if e.Situation != "" {
			fmt.Printf("  상황: %s\n", e.Situation)
		}
		fmt.Printf("  해결: %s\n", e.Resolution)
		if e.ErrorPattern != "" {
			fmt.Printf("  패턴: %s\n", e.ErrorPattern)
		}
		if e.SuccessCommand != "" {
			fmt.Printf("  명령: %s\n", e.SuccessCommand)
		}
		fmt.Println()
	}
}
