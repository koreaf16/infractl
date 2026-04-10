// Package cli
// File: streamer.go
// Description: REPL 내 하이브리드 스트리밍 마크다운 렌더링 모델
// Responsibility: agent 이벤트 채널을 수신받아 실시간으로 마크다운을 터미널에 렌더링

package cli

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type TokenMsg string
type DoneMsg struct{}
type ThinkingMsg struct{}
type ThinkingTokenMsg string
type ToolMsg string
type ErrorMsg struct{ Err error }

// StreamModel은 REPL에서 텍스트 수신 중 일시적으로 실행되는 inline BubbleTea 모델이다.
type StreamModel struct {
	spinner    spinner.Model
	tokens     string
	isThinking bool
	isDone     bool
	err        error
	renderer      *glamour.TermRenderer
	renderedValue string
	lastRenderTime time.Time
	width         int
	tools         []string
}

func NewStreamModel() StreamModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return StreamModel{
		spinner: s,
		width:   80, // 초기 기본값
	}
}

// updateRenderer는 현재 너비에 맞춰 glamour 렌더러를 재생성합니다.
func (m *StreamModel) updateRenderer() {
	if m.width < 10 {
		m.width = 80
	}
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(m.width - 2), // 여백 고려
	)
	m.renderer = r
	m.render() // 너비가 바뀌었으므로 즉시 재렌더링
}

func (m StreamModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m StreamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.updateRenderer()
		return m, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	case TokenMsg:
		m.isThinking = false
		m.tokens += string(msg)
		// 스로틀링: 150ms 간격으로만 렌더링 수행
		if time.Since(m.lastRenderTime) > 150*time.Millisecond {
			m.render()
		}
		return m, nil
	case ThinkingMsg:
		m.isThinking = true
		return m, nil
	case ThinkingTokenMsg:
		return m, nil
	case ToolMsg:
		m.tools = append(m.tools, string(msg))
		return m, nil
	case ErrorMsg:
		m.err = msg.Err
		m.render() // 에러 시 최종 렌더링
		m.isDone = true
		return m, tea.Quit
	case DoneMsg:
		m.render() // 완료 시 최종 렌더링
		m.isDone = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// render는 현재까지의 토큰을 마크다운으로 변환하여 캐시에 저장합니다.
func (m *StreamModel) render() {
	raw := m.tokens
	if raw == "" {
		return
	}
	if m.renderer != nil {
		safe := closeDanglingMarkdown(raw)
		out, err := m.renderer.Render(safe)
		if err == nil {
			m.renderedValue = out
		} else {
			m.renderedValue = raw
		}
	} else {
		m.renderedValue = raw
	}
	m.lastRenderTime = time.Now()
}

func (m StreamModel) View() string {
	var sb strings.Builder
	sb.WriteString("\n")

	// 도구 실행 내역 렌더링
	for _, t := range m.tools {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#999999")).Render("  ▶ " + t) + "\n")
	}

	// Thinking 스피너 렌더링
	if m.isThinking && !m.isDone {
		sb.WriteString(m.spinner.View() + lipgloss.NewStyle().Foreground(lipgloss.Color("#D77757")).Italic(true).Render(" Thinking...") + "\n")
	}

	// 토큰 마크다운 렌더링 (캐시된 값 사용)
	if m.renderedValue != "" {
		sb.WriteString(m.renderedValue)
	} else if m.tokens != "" {
		// 아직 렌더링 전이라면 생 텍스트라도 출력
		sb.WriteString(m.tokens)
	}

	if m.err != nil {
		sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B80")).Render("Error: "+m.err.Error()) + "\n")
	}

	// BubbleTea inline 렌더링 시 마지막 개행 제거
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// closeDanglingMarkdown은 렌더링 깨짐을 막기 위해 짝이 안 맞는 마크다운 기호를 닫아준다.
func closeDanglingMarkdown(content string) string {
	count := strings.Count(content, "```")
	if count%2 != 0 {
		content += "\n```"
	}

	backtickCount := 0
	for _, r := range content {
		if r == '`' {
			backtickCount++
		}
	}
	backtickCount -= (strings.Count(content, "```")) * 3
	if backtickCount < 0 {
		backtickCount = 0
	}
	if backtickCount%2 != 0 {
		content += "`"
	}
	return content
}
