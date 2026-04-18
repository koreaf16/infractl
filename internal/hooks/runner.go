// Package hooks
// File: runner.go
// Description: Hook 실행 오케스트레이터 — PreToolUse / PostToolUse 진입점
// Responsibility: Config 스냅샷 조회 → 매처 평가 → timeout context → backend 선택 및 실행

// Ported from: claude_cli/src/utils/hooks/hookHelpers.ts, hookEvents.ts:56-150
// 핵심 패턴: 이벤트 → 매처 목록 → HookDef 별 backend 결정 → timeout context → Run().

package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/yourorg/infractl/internal/llm"
)

// Runner 는 hook 실행 오케스트레이터이다.
type Runner struct {
	llmRegistry *llm.Registry
	http        *httpBackend
	agentRunner AgentRunner // nil 허용 — SetAgentRunner로 주입
}

// NewRunner 는 Runner 를 생성한다.
// llmRegistry 는 prompt backend 에서 사용된다 (nil 이면 prompt backend 비활성).
func NewRunner(llmRegistry *llm.Registry) *Runner {
	return &Runner{
		llmRegistry: llmRegistry,
		http:        newHTTPBackend(),
	}
}

// SetAgentRunner 는 agent hook backend 용 AgentRunner 를 주입한다.
// Phase F.8: subagent.HookAgentRunner 어댑터를 통해 mini-agent 실행.
func (r *Runner) SetAgentRunner(runner AgentRunner) {
	r.agentRunner = runner
}

// RunPostToolUse 는 PostToolUse 훅을 실행한다.
// Phase A: 관측(감사 로그)만 — 도구 실행 차단 없음, 실패 시 WARN 로그 후 계속 진행.
func (r *Runner) RunPostToolUse(ctx context.Context, input HookInput) {
	input.Event = HookEventPostToolUse
	r.runEvent(ctx, HookEventPostToolUse, input, false)
}

// RunPreToolUse 는 PreToolUse 훅을 실행하고 결과를 반환한다.
// Phase D: fail-closed (backend 오류 시 deny 로 합성). first-deny-wins + NewInput 전달.
// Ported from: claude_cli/src/utils/hooks/toolHooks.ts:resolveHookPermissionDecision
func (r *Runner) RunPreToolUse(ctx context.Context, input HookInput) HookOutput {
	input.Event = HookEventPreToolUse
	return r.runEvent(ctx, HookEventPreToolUse, input, true)
}

// runEvent 는 이벤트에 해당하는 모든 매처를 평가하고 hook 을 실행한다.
// failClosed=true 이면 backend 오류(timeout/exception) 시 deny 로 합성해 반환한다.
// 매칭된 hook 이 없거나 전부 allow 면 allow 를 반환한다.
// 여러 hook 의 NewInput 은 마지막 값이 우선한다 (hook 체인은 현재 이전 결과를 보지 않음).
func (r *Runner) runEvent(ctx context.Context, event HookEvent, input HookInput, failClosed bool) HookOutput {
	cfg := GetSnapshot()
	matchers := cfg.MatchersFor(event)
	if len(matchers) == 0 {
		return HookOutput{Decision: DecisionAllow}
	}

	defs, matched := MatchInput(matchers, input)
	if !matched {
		return HookOutput{Decision: DecisionAllow}
	}

	merged := HookOutput{Decision: DecisionAllow}
	for _, def := range defs {
		out, err := r.runSingle(ctx, def, input)
		if err != nil {
			slog.Warn("hook execution failed",
				"event", event,
				"tool", input.Tool,
				"backend", def.Type,
				"err", err,
			)
			if failClosed {
				return HookOutput{
					Decision:      DecisionDeny,
					Reason:        fmt.Sprintf("hook error: %s", err),
					SystemMessage: fmt.Sprintf("Hook 실행 실패로 도구가 차단되었습니다: %s", err),
				}
			}
			continue
		}
		slog.Debug("hook executed",
			"event", event,
			"tool", input.Tool,
			"backend", def.Type,
			"decision", string(out.Decision),
		)
		if out.IsDeny() {
			// first-deny-wins — 이전 allow 의 NewInput 은 파기.
			return out
		}
		// allow: NewInput/SystemMessage 를 누적 (뒤의 hook 이 앞을 덮어씀).
		if len(out.NewInput) > 0 {
			merged.NewInput = out.NewInput
		}
		if out.SystemMessage != "" {
			merged.SystemMessage = out.SystemMessage
		}
	}
	return merged
}

// runSingle 은 단일 HookDef 를 timeout context 로 실행한다.
func (r *Runner) runSingle(ctx context.Context, def HookDef, input HookInput) (HookOutput, error) {
	timeout := HookTimeout(def)
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	b, err := r.selectBackend(def)
	if err != nil {
		return HookOutput{}, err
	}
	return b.Run(tctx, def, input)
}

// selectBackend 는 HookDef.Type 에 따라 Backend 를 반환한다.
func (r *Runner) selectBackend(def HookDef) (Backend, error) {
	switch def.Type {
	case BackendCommand:
		return &commandBackend{}, nil
	case BackendPrompt:
		if r.llmRegistry == nil {
			return nil, errors.New("prompt backend: llm registry not configured")
		}
		return &promptBackend{registry: r.llmRegistry}, nil
	case BackendHTTP:
		return r.http, nil
	case BackendAgent:
		return &agentBackend{runner: r.agentRunner}, nil
	default:
		return nil, fmt.Errorf("unknown backend type: %q", def.Type)
	}
}
