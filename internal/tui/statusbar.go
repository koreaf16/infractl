// Package tui
// File: statusbar.go
// Description: TUI 하단 정보바 컴포넌트 — Claude CLI StatusLine 스타일
// Responsibility: 모델명, 비용, 토큰, 컨텍스트 사용률, 서버 수를 하단 바로 표시

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/store"
)

const tuiVersion = "0.3.0"

// statusBar는 화면 하단 고정 정보바이다.
type statusBar struct {
	model        string
	serverCount  int
	width        int
	busy         bool
	yoloMode     bool
	activeServer *store.Server // 현재 활성 서버 (nil이면 표시 안 함)

	// 토큰/비용 추적 (UsageUpdateMsg로 갱신)
	totalCostUSD   float64
	inputTokens    int
	outputTokens   int
	contextPercent float64
	turnDurationMs int64
}

// newStatusBar는 상태바를 생성한다.
func newStatusBar(model string, serverCount int) statusBar {
	return statusBar{model: model, serverCount: serverCount}
}

func (s statusBar) Init() tea.Cmd { return nil }

func (s statusBar) Update(msg tea.Msg) (statusBar, tea.Cmd) {
	return s, nil
}

// View는 하단 정보바를 렌더링한다.
// 형식: ▸▸ model · $0.12 · 24.5k tokens · 67% ctx     ● oracle (192.168.0.120) · 3 servers · v0.3.0
func (s statusBar) View() string {
	arrow := StyleInfoBarArrow.Render("▸▸")
	sep := StyleFooterDim.Render(" · ")

	// 왼쪽: 핵심 정보
	leftParts := []string{arrow + " " + s.model}

	// 비용 (0이 아닐 때만)
	if s.totalCostUSD > 0 {
		leftParts = append(leftParts, formatCost(s.totalCostUSD))
	}

	// 토큰 합계 (0이 아닐 때만)
	totalTokens := s.inputTokens + s.outputTokens
	if totalTokens > 0 {
		leftParts = append(leftParts, formatTokens(totalTokens)+" tokens")
	}

	// 컨텍스트 사용률 (0이 아닐 때만)
	if s.contextPercent > 0 {
		leftParts = append(leftParts, formatContext(s.contextPercent)+" ctx")
	}

	left := strings.Join(leftParts, sep)

	// 오른쪽: YOLO 표시 + 활성 서버 + 서버 수 + 버전
	rightParts := []string{}
	if s.yoloMode {
		rightParts = append(rightParts, StyleWarning.Render("⚡YOLO"))
	}
	if s.activeServer != nil {
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
		name := lipgloss.NewStyle().Bold(true).Render(s.activeServer.Name)
		rightParts = append(rightParts, dot+" "+name+" ("+s.activeServer.Host+")")
	}
	if s.serverCount > 0 {
		rightParts = append(rightParts, fmt.Sprintf("%d servers", s.serverCount))
	}
	rightParts = append(rightParts, "v"+tuiVersion)
	right := StyleInfoBarDim.Render(strings.Join(rightParts, sep))

	if s.width <= 0 {
		return left + "  " + right
	}

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	gap := s.width - leftWidth - rightWidth
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}

func (s *statusBar) setWidth(w int)                    { s.width = w }
func (s *statusBar) setServerCount(n int)               { s.serverCount = n }
func (s *statusBar) setBusy(b bool)                     { s.busy = b }
func (s *statusBar) setYoloMode(v bool)                 { s.yoloMode = v }
func (s *statusBar) setActiveServer(srv *store.Server)  { s.activeServer = srv }

// updateUsage는 UsageUpdateMsg를 반영한다.
func (s *statusBar) updateUsage(msg UsageUpdateMsg) {
	s.totalCostUSD += msg.CostUSD
	s.inputTokens += msg.InputTokens
	s.outputTokens += msg.OutputTokens
	s.turnDurationMs = msg.DurationMs
}

// formatCost는 USD 비용을 "$0.12" 형식으로 포맷한다.
func formatCost(usd float64) string {
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// formatTokens는 토큰 수를 "24.5k", "1.2M" 형식으로 포맷한다.
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatContext는 컨텍스트 사용률을 색상 적용된 "%d%%" 형식으로 반환한다.
// 80% 미만 green, 80-95% yellow, 95% 이상 red.
func formatContext(pct float64) string {
	text := fmt.Sprintf("%.0f%%", pct)
	switch {
	case pct >= 95:
		return StyleError.Render(text)
	case pct >= 80:
		return StyleWarning.Render(text)
	default:
		return StyleSuccess.Render(text)
	}
}
