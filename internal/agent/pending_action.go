// Package agent
// File: pending_action.go
// Description: LLM이 제안한 명령/계획을 추적하여 사용자 확인 응답("예"/"yes")에 즉시 매칭·실행
// Responsibility: 제안 활성화(Activate), 확인 응답 매칭(IsConfirmation), 소비(Consume), 상태 유효성 관리

package agent

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	pendingActionStaleTurns    = 3
	pendingActionStaleDuration = 5 * time.Minute
)

// PendingAction은 LLM이 제안한 명령 하나와 후속 계획을 담는다.
type PendingAction struct {
	ProposedCommand string
	Plan            []string
	Reason          string
	ServerName      string
	CreatedAt       time.Time
	TurnIndex       int
	Source          string // "tool" | "regex_fallback"
}

// ProposeActionMeta는 propose_action 도구가 MetadataJSON에 직렬화하는 페이로드다.
// agent가 이를 읽어 PendingAction을 활성화한다.
type ProposeActionMeta struct {
	Command string   `json:"command"`
	Reason  string   `json:"reason"`
	Plan    []string `json:"plan,omitempty"`
}

// PendingActionTracker는 직전 LLM 제안을 추적하는 인터페이스다.
type PendingActionTracker interface {
	Activate(p *PendingAction, turnIndex int)
	Current() *PendingAction
	Consume() *PendingAction
	Clear(reason string)
	IsStale(currentTurnIndex int) bool
	IsConfirmation(input string) (yes, no bool)
	ExtractFallback(assistantContent string) (*PendingAction, bool)
	ParseFromMeta(metaJSON string) (*PendingAction, bool)
}

// confirmationYesPattern은 사용자가 제안을 승인하는 응답 패턴이다.
var confirmationYesPattern = regexp.MustCompile(
	`(?i)^(예|네|넵|응|좋아|좋습니다|진행|진행해|계속|계속해|해줘|ㄱㄱ|ok|okay|yes|yep|yeah|sure|go|go ahead|proceed|continue|do it|ㅇㅋ)[?!.\s]*$`)

// confirmationNoPattern은 사용자가 제안을 거절하는 응답 패턴이다.
var confirmationNoPattern = regexp.MustCompile(
	`(?i)^(아니|아니요|아니오|취소|중단|그만|stop|no|nope|n|cancel|abort|skip|나중에)[?!.\s]*$`)

// proposalMarkers는 LLM 응답에서 "명령 제안" 의도를 감지하는 키워드다.
var proposalMarkers = []string{
	"해결 방안", "실행하겠습니다", "다음과 같이 실행", "명령을 실행", "아래 명령",
	"다음 명령", "실행할 명령", "이 명령",
	"solution:", "will execute", "will run", "run the following", "execute the following",
}

// codeBlockPattern은 마크다운 코드블록에서 첫 번째 명령줄을 추출한다.
var codeBlockPattern = regexp.MustCompile("(?m)```(?:bash|sh|shell)?\n([^\n`]+)")

// inlineCmdPattern은 코드블록 밖에서 명령줄을 추출한다.
var inlineCmdPattern = regexp.MustCompile(`(?m)^\s*(sudo\s+\S.+|systemctl\s+\S.+|yum\s+\S.+|dnf\s+\S.+|rpm\s+\S.+|unzip\s+\S.+|tar\s+\S.+|chmod\s+\S.+|chown\s+\S.+)`)

type pendingActionTracker struct {
	mu      sync.RWMutex
	current *PendingAction
}

// newPendingActionTracker는 빈 PendingActionTracker 구현체를 반환한다.
func newPendingActionTracker() PendingActionTracker {
	return &pendingActionTracker{}
}

func (t *pendingActionTracker) Activate(p *PendingAction, turnIndex int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p.TurnIndex = turnIndex
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	t.current = p
	slog.Info("pending action activated",
		"command_preview", truncatePendingPreview(p.ProposedCommand, 80),
		"plan_steps", len(p.Plan),
		"source", p.Source)
}

func (t *pendingActionTracker) Current() *PendingAction {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.current
}

func (t *pendingActionTracker) Consume() *PendingAction {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.current
	t.current = nil
	if p != nil {
		slog.Info("pending action consumed", "turn_index", p.TurnIndex)
	}
	return p
}

func (t *pendingActionTracker) Clear(reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		slog.Debug("pending action cleared", "reason", reason)
	}
	t.current = nil
}

func (t *pendingActionTracker) IsStale(currentTurnIndex int) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.current == nil {
		return true
	}
	if currentTurnIndex-t.current.TurnIndex >= pendingActionStaleTurns {
		return true
	}
	if time.Since(t.current.CreatedAt) >= pendingActionStaleDuration {
		return true
	}
	return false
}

func (t *pendingActionTracker) IsConfirmation(input string) (yes, no bool) {
	trimmed := strings.TrimSpace(input)
	if confirmationYesPattern.MatchString(trimmed) {
		return true, false
	}
	if confirmationNoPattern.MatchString(trimmed) {
		return false, true
	}
	return false, false
}

// ParseFromMeta는 propose_action 도구의 MetadataJSON에서 PendingAction을 복원한다.
func (t *pendingActionTracker) ParseFromMeta(metaJSON string) (*PendingAction, bool) {
	if strings.TrimSpace(metaJSON) == "" {
		return nil, false
	}
	var meta ProposeActionMeta
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return nil, false
	}
	if strings.TrimSpace(meta.Command) == "" {
		return nil, false
	}
	return &PendingAction{
		ProposedCommand: strings.TrimSpace(meta.Command),
		Reason:          strings.TrimSpace(meta.Reason),
		Plan:            meta.Plan,
		Source:          "tool",
		CreatedAt:       time.Now(),
	}, true
}

// ExtractFallback는 LLM이 propose_action 도구를 호출하지 않았을 때 자연어 응답에서
// 명령 제안을 정규식으로 추출하는 폴백이다.
func (t *pendingActionTracker) ExtractFallback(assistantContent string) (*PendingAction, bool) {
	hasMarker := false
	lower := strings.ToLower(assistantContent)
	for _, marker := range proposalMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return nil, false
	}

	// 코드블록 우선 시도
	if matches := codeBlockPattern.FindStringSubmatch(assistantContent); len(matches) > 1 {
		cmd := strings.TrimSpace(matches[1])
		if cmd != "" {
			return &PendingAction{
				ProposedCommand: cmd,
				Source:          "regex_fallback",
				CreatedAt:       time.Now(),
			}, true
		}
	}

	// 인라인 명령 시도
	if matches := inlineCmdPattern.FindStringSubmatch(assistantContent); len(matches) > 1 {
		cmd := strings.TrimSpace(matches[1])
		if cmd != "" {
			return &PendingAction{
				ProposedCommand: cmd,
				Source:          "regex_fallback",
				CreatedAt:       time.Now(),
			}, true
		}
	}

	return nil, false
}

func truncatePendingPreview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
