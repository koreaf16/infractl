// Package tui
// File: shimmer.go
// Description: Claude CLI 스타일 Thinking shimmer 애니메이션 렌더링
// Responsibility: LLM 응답 대기 중 shimmer 효과와 경과시간을 표시

package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	shimmerInterval  = 50 * time.Millisecond // 고주파 50ms (20fps)
	shimmerWidth     = 4 // 하이라이트 너비 확장
	shimmerPause     = 30 // 간격이 짧아졌으므로 pause도 조정
	stalledThreshold = 3 * time.Second
)

// shimmerState는 shimmer 애니메이션 상태를 관리한다.
type shimmerState struct {
	text             string
	startTime        time.Time
	lastActivityTime time.Time
	pos              int
	width            int
	bgCount          int // "N in background" 표시용
	active           bool
}

// newShimmer는 새 shimmerState를 생성한다.
func newShimmer() shimmerState {
	return shimmerState{}
}

// Start는 shimmer 애니메이션을 시작한다.
func (s *shimmerState) Start(text string) tea.Cmd {
	s.text = text
	now := time.Now()
	s.startTime = now
	s.lastActivityTime = now
	s.pos = 0
	s.active = true
	return shimmerTickCmd()
}

// Stop은 shimmer 애니메이션을 정지한다.
func (s *shimmerState) Stop() {
	s.active = false
	s.pos = 0
}

// RecordActivity는 마지막 활동 시각을 갱신한다.
func (s *shimmerState) RecordActivity() {
	s.lastActivityTime = time.Now()
}

// SetText는 shimmer 텍스트를 변경한다 (도구명 등).
func (s *shimmerState) SetText(text string) {
	s.text = text
	s.RecordActivity()
}

// Tick은 shimmer 위치를 전진시킨다.
func (s *shimmerState) Tick() tea.Cmd {
	if !s.active {
		return nil
	}
	s.pos++
	return shimmerTickCmd()
}

// View는 shimmer 애니메이션을 렌더링한다.
// 형식: "  ● thinking... (3s)          2 in background"
func (s *shimmerState) View() string {
	if !s.active {
		return ""
	}

	text := s.text
	if text == "" {
		text = "thinking..."
	}

	// 지연 감지
	stalledDur := time.Since(s.lastActivityTime)
	stalled := stalledDur > stalledThreshold
	isVeryStalled := stalledDur > 5*time.Second

	// shimmer 색상 적용
	colored := s.colorize(text, stalled)

	// 경과시간
	elapsed := formatElapsed(time.Since(s.startTime))
	elapsedStr := StyleInfoBarDim.Render("(" + elapsed + ")")

	// 좌측: dot + shimmer text + elapsed
	bullet := "●"
	bulletStyle := StyleClaude()
	if isVeryStalled {
		bulletStyle = StyleStalled
	} else if stalled {
		bulletStyle = lipgloss.NewStyle().Foreground(ColorStalled)
	}

	left := "  " + bulletStyle.Render(bullet) + " " + colored + " " + elapsedStr

	// 우측: background count
	if s.bgCount > 0 && s.width > 0 {
		right := StyleInfoBarDim.Render(fmt.Sprintf("%d in background", s.bgCount))
		leftW := lipgloss.Width(left)
		rightW := lipgloss.Width(right)
		gap := s.width - leftW - rightW
		if gap < 2 {
			return left
		}
		return left + strings.Repeat(" ", gap) + right
	}

	return left
}

// colorize는 텍스트에 shimmer 하이라이트를 적용한다.
func (s *shimmerState) colorize(text string, stalled bool) string {
	runes := []rune(text)
	textLen := len(runes)
	cycleLen := textLen + shimmerPause
	peakPos := s.pos % cycleLen

	// 지연 정도에 따른 강도 조절
	stalledDur := time.Since(s.lastActivityTime)
	isVeryStalled := stalledDur > 5*time.Second

	var buf strings.Builder
	for i, r := range runes {
		dist := peakPos - i
		if dist < 0 {
			dist = -dist
		}

		var style lipgloss.Style
		if isVeryStalled {
			// 5초 이상: 강력한 경고 (Bold)
			style = StyleStalled
		} else if stalled {
			// 3초 이상: 가벼운 지연 예고 (Italic + Dim)
			style = lipgloss.NewStyle().Foreground(ColorStalled).Italic(true)
		} else {
			// 정상: 부드러운 Shimmer
			switch {
			case dist == 0:
				style = lipgloss.NewStyle().Foreground(ColorShimmerPeak).Bold(true)
			case dist <= 1:
				style = lipgloss.NewStyle().Foreground(ColorShimmerMid)
			case dist <= shimmerWidth/2:
				style = lipgloss.NewStyle().Foreground(ColorShimmerBase)
			default:
				style = StyleInfoBarDim // 기본은 매우 희미하게 (Visual Diet)
			}
		}
		buf.WriteString(style.Render(string(r)))
	}

	// 텍스트가 비어있으면 빈 문자열
	if utf8.RuneCountInString(text) == 0 {
		return ""
	}

	return buf.String()
}

// StyleClaude는 Claude orange bold 스타일을 반환한다.
func StyleClaude() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorClaude).Bold(true)
}

// shimmerTickCmd는 shimmer 간격으로 틱을 생성하는 Cmd를 반환한다.
func shimmerTickCmd() tea.Cmd {
	return tea.Tick(shimmerInterval, func(time.Time) tea.Msg {
		return ShimmerTickMsg{}
	})
}

// formatElapsed는 Duration을 간결한 문자열로 변환한다.
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	s = s % 60
	return fmt.Sprintf("%dm %ds", m, s)
}
