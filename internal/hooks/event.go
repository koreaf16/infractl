// Package hooks
// File: event.go
// Description: Hook event input/output schema (claude_cli 정합 — Phase D).
// Responsibility: HookInput/HookOutput + Decision 타입 정의. 모든 backend 가 공유.

// Ported from: claude_cli/src/utils/hooks/hookHelpers.ts:hookResponseSchema,
//              claude_cli/src/services/tools/toolHooks.ts:permissionBehavior
// 핵심 패턴: Decision(allow|deny|ask) + systemMessage + newInput. Reason 은 내부 로그용.

package hooks

// HookEvent is the top-level hook event name in hooks.yaml.
type HookEvent string

const (
	HookEventPreToolUse   HookEvent = "PreToolUse"
	HookEventPostToolUse  HookEvent = "PostToolUse"
	HookEventSessionStart HookEvent = "SessionStart"
	HookEventSessionEnd   HookEvent = "SessionEnd"
)

// Decision 은 PreToolUse hook 의 허용/거부/질문 결정이다.
// 빈 문자열("")은 DecisionAllow 와 동일하게 취급된다.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionAsk   Decision = "ask" // Phase D 에서는 deny 로 축약, Phase G Plan Mode 에서 실체화
)

// HookInput is passed to hook backends.
type HookInput struct {
	Event    HookEvent              `json:"event"`
	Tool     string                 `json:"tool"`
	Input    map[string]interface{} `json:"input,omitempty"`
	Output   map[string]interface{} `json:"output,omitempty"`
	Session  HookSession            `json:"session"`
	Metadata HookMetadata           `json:"metadata,omitempty"`
}

// HookSession describes the active session.
type HookSession struct {
	ID   string `json:"id"`
	User string `json:"user"`
	CWD  string `json:"cwd"`
}

// HookMetadata carries cheap analysis for hook matchers.
type HookMetadata struct {
	DiskModifying bool   `json:"disk_modifying,omitempty"`
	NetworkAccess bool   `json:"network_access,omitempty"`
	ReadOnly      bool   `json:"read_only,omitempty"`
	DangerScore   string `json:"danger_score,omitempty"`
}

// HookOutput is returned by a hook backend.
// 과거 Approved bool 필드는 Phase D 에서 Decision 으로 교체되었다.
type HookOutput struct {
	Decision      Decision               `json:"decision,omitempty"`      // 없으면 allow
	Reason        string                 `json:"reason,omitempty"`        // 내부 로그/감사용
	SystemMessage string                 `json:"systemMessage,omitempty"` // 사용자/LLM 에게 노출
	NewInput      map[string]interface{} `json:"newInput,omitempty"`
}

// IsDeny 는 결정이 deny 인 경우 true 를 반환한다.
// Phase D: ask 는 deny 로 축약 (Phase G 에서 분리).
func (o HookOutput) IsDeny() bool {
	return o.Decision == DecisionDeny || o.Decision == DecisionAsk
}

// IsAllow 는 결정이 allow (또는 빈 문자열) 인 경우 true 를 반환한다.
func (o HookOutput) IsAllow() bool {
	return o.Decision == "" || o.Decision == DecisionAllow
}
