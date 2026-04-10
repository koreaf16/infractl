// Package cli
// File: rag_cmd.go
// Description: /rag 슬래시 명령 핸들러
// Responsibility: RAG 소스 목록 조회, 삭제, 우선순위 변경

package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/yourorg/infractl/internal/store"
)

// handleRAGCommand는 /rag 슬래시 명령을 처리한다.
func (r *REPL) handleRAGCommand(ctx context.Context, parts []string) {
	if r.ragSourceStore == nil {
		fmt.Println("RAG 소스 저장소가 초기화되지 않았습니다.")
		return
	}

	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "delete":
		if len(parts) < 3 {
			fmt.Println("사용법: /rag delete <ID>")
			return
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			fmt.Printf("잘못된 ID: %s\n", parts[2])
			return
		}
		if err := r.ragSourceStore.DeleteRAGSource(ctx, id); err != nil {
			fmt.Printf("삭제 실패: %s\n", err)
			return
		}
		fmt.Printf("✓ RAG 소스 %d 삭제 완료\n", id)

	case "priority":
		if len(parts) < 4 {
			fmt.Println("사용법: /rag priority <ID> <우선순위>")
			return
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			fmt.Printf("잘못된 ID: %s\n", parts[2])
			return
		}
		priority, err := strconv.Atoi(parts[3])
		if err != nil || priority < 1 {
			fmt.Printf("잘못된 우선순위: %s (1 이상의 숫자)\n", parts[3])
			return
		}
		if err := r.ragSourceStore.UpdateRAGSourcePriority(ctx, id, priority); err != nil {
			fmt.Printf("우선순위 변경 실패: %s\n", err)
			return
		}
		fmt.Printf("✓ RAG 소스 %d 우선순위 → %d\n", id, priority)

	default:
		r.printRAGSources(ctx)
	}
}

// printRAGSources는 등록된 RAG 소스 목록을 출력한다.
func (r *REPL) printRAGSources(ctx context.Context) {
	sources, err := r.ragSourceStore.ListRAGSources(ctx)
	if err != nil {
		fmt.Printf("RAG 소스 조회 실패: %s\n", err)
		return
	}
	if len(sources) == 0 {
		fmt.Println("등록된 RAG 소스가 없습니다.")
		fmt.Println("LLM에게 'RAG 소스 등록해줘'라고 요청하면 대화로 등록할 수 있습니다.")
		return
	}

	fmt.Printf("\n== RAG 소스 목록 (%d건) ==\n\n", len(sources))
	fmt.Printf("%-4s %-20s %-10s %-15s %-15s %-4s\n",
		"ID", "이름", "DB타입", "서버", "테이블", "우선순위")
	fmt.Println("────────────────────────────────────────────────────────────────")
	for _, src := range sources {
		fmt.Printf("%-4d %-20s %-10s %-15s %-15s %d\n",
			src.ID, src.Name, src.DBType, src.ServerName, src.TableName, src.Priority)
		if src.Description != "" {
			fmt.Printf("     설명: %s\n", src.Description)
		}
	}
	fmt.Println()
	fmt.Println("명령어: /rag delete <ID> | /rag priority <ID> <숫자>")
}

// SetRAGSourceStore는 RAG 소스 저장소를 주입한다.
func (r *REPL) SetRAGSourceStore(s store.RAGSourceStore) {
	r.ragSourceStore = s
}
