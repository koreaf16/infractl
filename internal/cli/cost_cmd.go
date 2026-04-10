// Package cli
// File: cost_cmd.go
// Description: /cost 슬래시 명령 핸들러
// Responsibility: LLM 비용/사용량 집계 결과를 터미널에 포맷하여 출력

package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/cost"
)

// CostTracker는 비용 조회에 필요한 인터페이스이다.
type CostTracker interface {
	Summary(ctx context.Context, days int) (cost.CostSummary, error)
	DailyCosts(ctx context.Context, days int) ([]cost.DailyCost, error)
}

// SetCostTracker는 비용 추적기를 주입한다.
func (r *REPL) SetCostTracker(t CostTracker) {
	r.costTracker = t
}

// handleCostCommand는 /cost 명령을 처리한다.
// /cost          → 이번 달(30일) 요약
// /cost week     → 최근 7일 요약
// /cost detail   → 최근 30일 일별 상세
func (r *REPL) handleCostCommand(ctx context.Context, parts []string) {
	if r.costTracker == nil {
		fmt.Println("비용 추적이 설정되지 않았습니다.")
		return
	}

	subCmd := ""
	if len(parts) >= 2 {
		subCmd = parts[1]
	}

	switch subCmd {
	case "week":
		printCostSummary(ctx, r.costTracker, 7, "최근 7일")
	case "detail":
		printCostDetail(ctx, r.costTracker, 30)
	default:
		printCostSummary(ctx, r.costTracker, 30, "이번 달 (30일)")
	}
}

func printCostSummary(ctx context.Context, t CostTracker, days int, label string) {
	s, err := t.Summary(ctx, days)
	if err != nil {
		fmt.Printf("비용 조회 실패: %s\n", err)
		return
	}
	fmt.Printf("\n💰 비용 요약 — %s\n", label)
	fmt.Printf("  총 입력 토큰: %s\n", formatTokens(s.TotalInputTokens))
	fmt.Printf("  총 출력 토큰: %s\n", formatTokens(s.TotalOutputTokens))
	fmt.Printf("  총 호출 횟수: %d 회\n", s.CallCount)
	fmt.Printf("  예상 비용:    $%.4f\n", s.TotalCost)
	fmt.Println()
}

func printCostDetail(ctx context.Context, t CostTracker, days int) {
	daily, err := t.DailyCosts(ctx, days)
	if err != nil {
		fmt.Printf("일별 비용 조회 실패: %s\n", err)
		return
	}
	if len(daily) == 0 {
		fmt.Println("기록된 사용량이 없습니다.")
		return
	}
	fmt.Printf("\n💰 일별 비용 상세 — 최근 %d일\n", days)
	fmt.Printf("%-12s  %12s  %12s  %8s  %8s\n", "날짜", "입력 토큰", "출력 토큰", "호출수", "비용($)")
	fmt.Println("  " + repeat("-", 60))
	for _, d := range daily {
		fmt.Printf("%-12s  %12s  %12s  %8d  %8.4f\n",
			d.Date, formatTokens(d.InputTokens), formatTokens(d.OutputTokens),
			d.CallCount, d.EstimatedCost)
	}
	fmt.Println()
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}
