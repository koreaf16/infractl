// Package cli
// File: hook_cmd.go
// Description: /hooks 슬래시 명령 처리 핸들러
// Responsibility: 훅 목록 조회, 활성화/비활성화, 삭제 CLI 출력

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/yourorg/infractl/internal/store"
)

// HooksManager는 CLI에서 사용하는 훅 관리 인터페이스이다.
type HooksManager interface {
	List(ctx context.Context) ([]store.Hook, error)
	SetEnabled(ctx context.Context, id int64, enabled bool) error
	Delete(ctx context.Context, id int64) error
}

// SetHooksManager는 훅 매니저를 주입한다.
func (r *REPL) SetHooksManager(mgr HooksManager) {
	r.hooksManager = mgr
}

// handleHooksCommand는 /hooks 서브커맨드를 처리한다.
// 사용법: /hooks | /hooks enable <id> | /hooks disable <id> | /hooks delete <id>
func (r *REPL) handleHooksCommand(ctx context.Context, parts []string) {
	if r.hooksManager == nil {
		fmt.Println("훅 매니저가 초기화되지 않았습니다.")
		return
	}

	if len(parts) >= 3 {
		idStr := parts[2]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			fmt.Printf("유효하지 않은 훅 ID: %s\n", idStr)
			return
		}
		switch parts[1] {
		case "enable":
			if err := r.hooksManager.SetEnabled(ctx, id, true); err != nil {
				fmt.Printf("✗ 훅 활성화 실패: %s\n", err)
			} else {
				fmt.Printf("✓ 훅 #%d 활성화 완료\n", id)
			}
		case "disable":
			if err := r.hooksManager.SetEnabled(ctx, id, false); err != nil {
				fmt.Printf("✗ 훅 비활성화 실패: %s\n", err)
			} else {
				fmt.Printf("✓ 훅 #%d 비활성화 완료\n", id)
			}
		case "delete":
			if err := r.hooksManager.Delete(ctx, id); err != nil {
				fmt.Printf("✗ 훅 삭제 실패: %s\n", err)
			} else {
				fmt.Printf("✓ 훅 #%d 삭제 완료\n", id)
			}
		default:
			fmt.Printf("알 수 없는 /hooks 서브커맨드: %s\n", parts[1])
			printHooksHelp()
		}
		return
	}

	r.printHooks(ctx)
}

func (r *REPL) printHooks(ctx context.Context) {
	hooks, err := r.hooksManager.List(ctx)
	if err != nil {
		fmt.Printf("훅 목록 조회 실패: %s\n", err)
		return
	}
	if len(hooks) == 0 {
		fmt.Println("등록된 훅 없음. LLM에게 'hook_register 도구로 훅 등록해줘' 처럼 요청하세요.")
		return
	}

	fmt.Printf("등록된 훅 (%d개):\n", len(hooks))
	fmt.Printf("  %-4s %-18s %-6s %-25s %s\n", "ID", "이벤트", "활성", "조건", "스크립트")
	fmt.Println("  " + strings.Repeat("-", 72))
	for _, h := range hooks {
		enabled := "✓"
		if !h.Enabled {
			enabled = "✗"
		}
		cond := h.Condition
		if cond == "" {
			cond = "(항상)"
		}
		script := h.ScriptPath
		if len(script) > 30 {
			script = "..." + script[len(script)-27:]
		}
		fmt.Printf("  %-4d %-18s %-6s %-25s %s\n",
			h.ID, h.Event, enabled, cond, script)
	}
	fmt.Println()
	fmt.Println("비활성화: /hooks disable <id>  활성화: /hooks enable <id>  삭제: /hooks delete <id>")
}

func printHooksHelp() {
	fmt.Println("사용법: /hooks | /hooks enable <id> | /hooks disable <id> | /hooks delete <id>")
}
