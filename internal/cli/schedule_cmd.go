// Package cli
// File: schedule_cmd.go
// Description: /schedules 슬래시 명령 처리 핸들러
// Responsibility: 스케줄 목록 조회, 활성화/비활성화, 삭제 CLI 출력

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/yourorg/infractl/internal/store"
)

// ScheduleManager는 CLI에서 사용하는 스케줄 관리 인터페이스이다.
type ScheduleManager interface {
	List(ctx context.Context) ([]store.Schedule, error)
	SetEnabled(ctx context.Context, id int64, enabled bool) error
	Delete(ctx context.Context, id int64) error
}

// SetScheduleManager는 스케줄 매니저를 주입한다.
func (r *REPL) SetScheduleManager(mgr ScheduleManager) {
	r.scheduleMgr = mgr
}

// handleSchedulesCommand는 /schedules 서브커맨드를 처리한다.
// 사용법: /schedules | /schedules enable <id> | /schedules disable <id> | /schedules delete <id>
func (r *REPL) handleSchedulesCommand(ctx context.Context, parts []string) {
	if r.scheduleMgr == nil {
		fmt.Println("스케줄 매니저가 초기화되지 않았습니다.")
		return
	}

	if len(parts) >= 3 {
		idStr := parts[2]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			fmt.Printf("유효하지 않은 스케줄 ID: %s\n", idStr)
			return
		}
		switch parts[1] {
		case "enable":
			if err := r.scheduleMgr.SetEnabled(ctx, id, true); err != nil {
				fmt.Printf("✗ 스케줄 활성화 실패: %s\n", err)
			} else {
				fmt.Printf("✓ 스케줄 #%d 활성화 완료\n", id)
			}
		case "disable":
			if err := r.scheduleMgr.SetEnabled(ctx, id, false); err != nil {
				fmt.Printf("✗ 스케줄 비활성화 실패: %s\n", err)
			} else {
				fmt.Printf("✓ 스케줄 #%d 비활성화 완료\n", id)
			}
		case "delete":
			if err := r.scheduleMgr.Delete(ctx, id); err != nil {
				fmt.Printf("✗ 스케줄 삭제 실패: %s\n", err)
			} else {
				fmt.Printf("✓ 스케줄 #%d 삭제 완료\n", id)
			}
		default:
			fmt.Printf("알 수 없는 /schedules 서브커맨드: %s\n", parts[1])
			fmt.Println("사용법: /schedules | /schedules enable <id> | /schedules disable <id> | /schedules delete <id>")
		}
		return
	}

	r.printSchedules(ctx)
}

func (r *REPL) printSchedules(ctx context.Context) {
	schedules, err := r.scheduleMgr.List(ctx)
	if err != nil {
		fmt.Printf("스케줄 목록 조회 실패: %s\n", err)
		return
	}
	if len(schedules) == 0 {
		fmt.Println("등록된 스케줄 없음. schedule_create 도구를 사용하거나 LLM에게 요청하세요.")
		return
	}

	fmt.Printf("등록된 스케줄 (%d개):\n", len(schedules))
	fmt.Printf("  %-4s %-6s %-20s %-15s %s\n", "ID", "활성", "이름", "cron", "마지막 실행")
	fmt.Println("  " + strings.Repeat("-", 65))
	for _, s := range schedules {
		enabled := "✓"
		if !s.Enabled {
			enabled = "✗"
		}
		lastRun := "없음"
		if s.LastRun != nil {
			lastRun = s.LastRun.Format("01/02 15:04")
		}
		name := s.Name
		if len(name) > 18 {
			name = name[:16] + ".."
		}
		fmt.Printf("  %-4d %-6s %-20s %-15s %s\n",
			s.ID, enabled, name, s.CronExpr, lastRun)
	}
	fmt.Println()
	fmt.Println("스케줄 등록: LLM에게 'schedule_create로 매일 09시 상태 점검해줘' 처럼 요청하세요.")
}
