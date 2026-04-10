// Package agent
// File: pattern_idle_handler.go
// Description: 정규식 패턴 기반 인터랙티브 프롬프트 자동 응답 핸들러
// Responsibility: [Y/n], Continue? 등 공통 프롬프트 패턴 감지 후 자동 응답 결정

package agent

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// promptPattern은 프롬프트 감지 패턴과 자동 응답을 정의한다.
type promptPattern struct {
	re       *regexp.Regexp
	response string // 자동으로 전송할 입력 ("y", "n", "", "q" 등)
	label    string // 로그/UI용 설명
}

// defaultPatterns는 기본 감지 패턴 목록이다.
// 우선순위 높은 것부터 순서 배치 — 첫 번째 매치가 승리한다.
var defaultPatterns = []promptPattern{
	// [Y/n] 형태 — 대문자 Y가 기본이므로 y 전송
	{re: regexp.MustCompile(`(?i)\[Y[/|]n\]`), response: "y", label: "[Y/n] → y"},
	// [y/N] 형태 — 대문자 N이 기본이므로 n 전송
	{re: regexp.MustCompile(`(?i)\[y[/|]N\]`), response: "n", label: "[y/N] → n"},
	// [yes/no] 또는 (yes/no)
	{re: regexp.MustCompile(`(?i)\[yes[/|]no\]|\(yes[/|]no\)`), response: "yes", label: "[yes/no] → yes"},
	// Continue? / Do you want to continue?
	{re: regexp.MustCompile(`(?i)(do you want to |do you wish to )?continue\?`), response: "y", label: "Continue? → y"},
	// Press Enter to continue / Press any key
	{re: regexp.MustCompile(`(?i)press (enter|return|any key) to continue`), response: "", label: "Press Enter → ⏎"},
	// Are you sure? / Are you certain?
	{re: regexp.MustCompile(`(?i)are you sure\??`), response: "y", label: "Are you sure? → y"},
	// Overwrite? / Replace?
	{re: regexp.MustCompile(`(?i)(overwrite|replace|rewrite)\?`), response: "y", label: "Overwrite? → y"},
	// Proceed? / Proceed with ...?
	{re: regexp.MustCompile(`(?i)proceed\??`), response: "y", label: "Proceed? → y"},
	// Confirm? 단독
	{re: regexp.MustCompile(`(?i)confirm\?`), response: "y", label: "Confirm? → y"},
	// q or Q to quit
	{re: regexp.MustCompile(`(?i)\bpress q(uit)? to (quit|exit)`), response: "q", label: "Press Q to quit → q"},
}

// PatternIdleInputHandler는 사전 정의된 패턴으로 인터랙티브 프롬프트를 자동 응답한다.
// 패턴과 일치하지 않으면 fallback 핸들러로 위임한다.
type PatternIdleInputHandler struct {
	patterns []promptPattern
	fallback IdleInputHandler // 패턴 불일치 시 폴백 (nil이면 abort)
}

// NewPatternIdleInputHandler는 기본 패턴 목록으로 핸들러를 생성한다.
func NewPatternIdleInputHandler(fallback IdleInputHandler) *PatternIdleInputHandler {
	return &PatternIdleInputHandler{
		patterns: defaultPatterns,
		fallback: fallback,
	}
}

// RequestIdleInput은 마지막 출력 줄에서 패턴을 찾아 자동 응답한다.
func (h *PatternIdleInputHandler) RequestIdleInput(ctx context.Context, req IdleInputRequest) (IdleInputResponse, error) {
	combined := strings.Join(req.LastLines, "\n")

	for _, p := range h.patterns {
		if p.re.MatchString(combined) {
			slog.Info("pattern idle handler: auto-response",
				"pattern", p.label, "tool", req.ToolName, "target", req.Target)
			return IdleInputResponse{Input: p.response}, nil
		}
	}

	// 패턴 불일치 — 폴백으로 위임
	if h.fallback != nil {
		return h.fallback.RequestIdleInput(ctx, req)
	}
	slog.Warn("pattern idle handler: no match, aborting", "tool", req.ToolName)
	return IdleInputResponse{Abort: true}, nil
}

// PatternExamples는 현재 등록된 패턴의 설명 목록을 반환한다 (UI 표시용).
func (h *PatternIdleInputHandler) PatternExamples() []string {
	examples := make([]string, len(h.patterns))
	for i, p := range h.patterns {
		examples[i] = p.label
	}
	return examples
}
