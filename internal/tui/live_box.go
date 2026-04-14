// Package tui
// File: live_box.go
// Description: direct renderer가 사용할 하단 고정 라이브 박스를 렌더링한다.
// Responsibility: 도구 출력, shimmer, LLM 스트리밍 미리보기를 고정 영역에 표시한다.
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	liveAreaHeight  = 10
	liveAreaContent = liveAreaHeight - 2 - 1
)

// liveArea는 화면 하단에 고정된 라이브 상태 박스다.
type liveArea struct {
	height int
	width  int
	title  string
	header string
	lines  []string
	drawn  int
}

func newLiveArea(width int) *liveArea {
	return &liveArea{height: liveAreaHeight, width: width}
}

func (la *liveArea) SetTitle(s string)  { la.title = s }
func (la *liveArea) SetHeader(s string) { la.header = s }

func (la *liveArea) Append(line string) {
	la.lines = append(la.lines, truncateLiveLine(line, la.contentWidth()-2))
	if len(la.lines) > liveAreaContent {
		la.lines = la.lines[len(la.lines)-liveAreaContent:]
	}
}

func (la *liveArea) SetContent(lines []string) {
	maxW := la.contentWidth() - 2
	if len(lines) > liveAreaContent {
		lines = lines[len(lines)-liveAreaContent:]
	}
	la.lines = make([]string, len(lines))
	for i, l := range lines {
		la.lines[i] = truncateLiveLine(l, maxW)
	}
}

func (la *liveArea) Reset() {
	la.title = ""
	la.header = ""
	la.lines = nil
}

func (la *liveArea) Redraw() {
	la.clearDrawn()
	la.draw()
}

func (la *liveArea) PrintPermanent(text string) {
	la.clearDrawn()
	if text != "" {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Fprint(os.Stdout, text)
	}
	la.draw()
}

// PrintPermanentBatch는 여러 텍스트를 한 번의 clear/draw 사이클로 영구 출력한다.
// 연속 PrintPermanent 호출의 깜빡임을 방지한다.
func (la *liveArea) PrintPermanentBatch(texts ...string) {
	la.clearDrawn()
	for _, text := range texts {
		if text == "" {
			continue
		}
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Fprint(os.Stdout, text)
	}
	la.draw()
}

func (la *liveArea) Clear() {
	la.clearDrawn()
}

func (la *liveArea) SetWidth(w int) {
	la.width = w
}

func (la *liveArea) contentWidth() int {
	return max(20, la.width-4)
}

func (la *liveArea) clearDrawn() {
	if la.drawn > 0 {
		fmt.Fprintf(os.Stdout, ansiCursorUp, la.drawn)
		fmt.Fprint(os.Stdout, ansiClearToEnd)
		la.drawn = 0
	}
}

func (la *liveArea) draw() {
	rendered := la.render()
	if rendered == "" {
		la.drawn = 0
		return
	}
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	fmt.Fprint(os.Stdout, rendered)
	la.drawn = strings.Count(rendered, "\n")
}

func (la *liveArea) render() string {
	// 콘텐츠 라인이 있으면 full bordered box 렌더링 (스트리밍 미리보기)
	if len(la.lines) > 0 {
		boxW := la.contentWidth()
		numContent := la.height - 2
		parts := make([]string, numContent)
		parts[0] = la.header
		for i := 1; i < numContent; i++ {
			if i-1 < len(la.lines) {
				parts[i] = la.lines[i-1]
			}
		}

		content := strings.Join(parts, "\n")
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSubtle).
			Width(boxW).
			Render(content)

		if la.title != "" {
			lines := strings.Split(box, "\n")
			if len(lines) > 0 {
				lines[0] = buildRoundedBorderTitleLine(lipgloss.Width(lines[0]), la.title)
				box = strings.Join(lines, "\n")
			}
		}
		return box
	}

	// 콘텐츠 없이 shimmer(header)만 있으면 1줄만 출력 — box 없이
	if la.header != "" {
		return la.header
	}

	return ""
}

func truncateLiveLine(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := 1
		if r > 0x2E7F {
			rw = 2
		}
		if w+rw > maxW {
			return s[:i] + "..."
		}
		w += rw
	}
	return s
}
