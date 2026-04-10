// Package cli
// File: checkpoint_cmd.go
// Description: /checkpoints 슬래시 명령 처리 핸들러
// Responsibility: 체크포인트 목록 조회 및 CLI 출력

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/yourorg/infractl/internal/store"
)

// CheckpointManager는 CLI에서 사용하는 체크포인트 조회 인터페이스이다.
type CheckpointManager interface {
	List(ctx context.Context, server string, limit int) ([]store.Checkpoint, error)
}

// SetCheckpointManager는 체크포인트 매니저를 주입한다.
func (r *REPL) SetCheckpointManager(mgr CheckpointManager) {
	r.checkpointMgr = mgr
}

// handleCheckpointsCommand는 /checkpoints 서브커맨드를 처리한다.
// 사용법: /checkpoints [서버명]
func (r *REPL) handleCheckpointsCommand(ctx context.Context, parts []string) {
	if r.checkpointMgr == nil {
		fmt.Println("체크포인트 매니저가 초기화되지 않았습니다.")
		return
	}

	server := ""
	if len(parts) >= 2 {
		server = parts[1]
	}

	cps, err := r.checkpointMgr.List(ctx, server, 20)
	if err != nil {
		fmt.Printf("체크포인트 목록 조회 실패: %s\n", err)
		return
	}
	if len(cps) == 0 {
		if server != "" {
			fmt.Printf("서버 '%s'의 체크포인트 없음.\n", server)
		} else {
			fmt.Println("저장된 체크포인트 없음.")
		}
		return
	}

	fmt.Printf("체크포인트 목록 (최근 %d개):\n", len(cps))
	fmt.Printf("  %-4s %-12s %-20s %s\n", "ID", "서버", "시각", "설명")
	fmt.Println("  " + strings.Repeat("-", 60))
	for _, cp := range cps {
		ts := cp.CreatedAt.Format("01/02 15:04:05")
		desc := cp.Description
		if len(desc) > 30 {
			desc = desc[:28] + ".."
		}
		rollback := ""
		if cp.Snapshot.RollbackCommand != "" {
			rollback = " [롤백 가능]"
		}
		fmt.Printf("  %-4s %-12s %-20s %s%s\n",
			strconv.FormatInt(cp.ID, 10), cp.Server, ts, desc, rollback)
	}
	fmt.Println()
	fmt.Println("롤백: LLM에게 '체크포인트 #3으로 롤백해줘' 처럼 자연어로 요청하세요.")
}
