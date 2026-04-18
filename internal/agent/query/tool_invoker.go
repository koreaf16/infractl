// Package query
// File: tool_invoker.go
// Description: PreToolUse / PostToolUse hook 통합 도구 실행 래퍼
// Responsibility: hook 자리 마련 (PreToolUse deny/newInput 반영, PostToolUse fire-and-forget)
//                 실제 hook 활성화는 Phase D
//
// Ported from: claude_cli/src/services/tools/toolExecution.ts (PreToolUse/PostToolUse 통합부)
// 핵심 패턴: RunPreToolUse → deny/modify input → base run → RunPostToolUse

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/yourorg/infractl/internal/agent/planmode"
	todoagent "github.com/yourorg/infractl/internal/agent/todo"
	"github.com/yourorg/infractl/internal/hooks"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/tools"
)

// BlockedCallback 은 도구가 enforcer 또는 plan gate 에 의해 차단될 때 호출된다.
type BlockedCallback func(tc llm.ToolCall, reason string)

// ToolInvoker 는 PreToolUse / PostToolUse hook 호출을 기존 도구 실행기에 씌우는 래퍼다.
// Phase D: RunPreToolUse 가 활성화되어 실제 deny/newInput 이 동작한다. Session/Metadata 를 주입한다.
// Phase G: planState/filter 가 주입되면 Plan Mode 활성 시 mutation 도구를 하드 차단하고 PendingQueue 에 적재한다.
type ToolInvoker struct {
	hookRunner      *hooks.Runner
	base            ToolRunner
	session         hooks.HookSession
	registry        *tools.Registry
	planState       *planmode.State
	planFilter      *planmode.ReadOnlyFilter
	todoEnforcer    *todoagent.Enforcer
	blockedCallback BlockedCallback
}

// NewToolInvoker 는 ToolInvoker 를 생성한다.
// hookRunner 가 nil 이면 모든 hook 호출을 skip 한다.
// base 는 hook 없이 도구를 실행하는 원본 함수다.
func NewToolInvoker(hookRunner *hooks.Runner, base ToolRunner) *ToolInvoker {
	return &ToolInvoker{hookRunner: hookRunner, base: base}
}

// SetPlanGate 는 Plan Mode 상태와 필터를 주입한다.
// nil 을 전달하면 Plan Mode 차단이 비활성화된다.
func (ti *ToolInvoker) SetPlanGate(state *planmode.State, filter *planmode.ReadOnlyFilter) {
	ti.planState = state
	ti.planFilter = filter
}

// SetTodoEnforcer 는 TodoWrite enforcer 를 주입한다.
// 주입되면 mutation 도구 호출 시 todo 가 비어있으면 차단한다.
func (ti *ToolInvoker) SetTodoEnforcer(e *todoagent.Enforcer) {
	ti.todoEnforcer = e
}

// SetBlockedCallback 은 도구가 차단될 때 호출할 콜백을 등록한다.
// TUI 에서 차단된 도구도 박스로 표시하기 위해 사용한다.
func (ti *ToolInvoker) SetBlockedCallback(cb BlockedCallback) {
	ti.blockedCallback = cb
}

// SetSession 은 hook 에 함께 전달될 세션 정보(ID/User/CWD)를 설정한다.
// 호출하지 않으면 빈 HookSession 이 전달된다.
func (ti *ToolInvoker) SetSession(s hooks.HookSession) {
	ti.session = s
}

// SetRegistry injects the tool registry used for read-only classification.
func (ti *ToolInvoker) SetRegistry(reg *tools.Registry) {
	ti.registry = reg
}

// Invoke 는 Plan Mode 차단 → PreToolUse hook → 도구 실행 → PostToolUse hook 순으로 실행한다.
// ToolRunner 시그니처를 구현하므로 Params.RunTool 에 ti.Invoke 를 직접 할당할 수 있다.
func (ti *ToolInvoker) Invoke(ctx context.Context, tc llm.ToolCall) (string, bool) {
	// --- Plan Mode 하드 차단 (PreToolUse hook 보다 먼저) ---
	if ti.planState != nil && ti.planState.IsActive() && ti.planFilter != nil {
		args := parseArgsForHook(tc.Function.Arguments)
		md := ComputeMetadata(tc.Function.Name, args)
		if allowed, reason := ti.planFilter.Allow(tc.Function.Name, md); !allowed {
			id := ti.planState.Queue().Add(tc.Function.Name, args)
			slog.Info("plan mode blocked tool call",
				"tool", tc.Function.Name,
				"pending_id", id,
				"reason", reason,
			)
			msg := fmt.Sprintf("Error: %s (pending_id=%s)", reason, id)
			if ti.blockedCallback != nil {
				ti.blockedCallback(tc, msg)
			}
			return msg, true
		}
	}

	// --- TodoWrite enforcer (Plan Mode 외 일반 mutation 차단) ---
	if ti.todoEnforcer != nil && (ti.planState == nil || !ti.planState.IsActive()) {
		args := parseArgsForHook(tc.Function.Arguments)
		md := ComputeMetadata(tc.Function.Name, args)
		isReadOnly := md.ReadOnly
		if !isReadOnly && ti.registry != nil {
			if tool, ok := ti.registry.Get(tc.Function.Name); ok && tool.IsReadOnly() {
				isReadOnly = true
			}
		}
		if ok, reason := ti.todoEnforcer.Enforce(tc.Function.Name, isReadOnly); !ok {
			slog.Info("todo enforcer blocked tool call", "tool", tc.Function.Name, "reason", reason)
			msg := fmt.Sprintf("Error: %s", reason)
			if ti.blockedCallback != nil {
				ti.blockedCallback(tc, msg)
			}
			return msg, true
		}
	}

	// --- PreToolUse hook ---
	if ti.hookRunner != nil {
		args := parseArgsForHook(tc.Function.Arguments)
		hookIn := hooks.HookInput{
			Tool:     tc.Function.Name,
			Input:    args,
			Session:  ti.session,
			Metadata: ComputeMetadata(tc.Function.Name, args),
		}
		hookOut := ti.hookRunner.RunPreToolUse(ctx, hookIn)
		if hookOut.IsDeny() {
			// claude_cli 패턴: SystemMessage 가 있으면 사용자/LLM 노출용으로 우선,
			// 없으면 Reason 사용. 둘 다 없으면 일반 메시지.
			msg := hookOut.SystemMessage
			if msg == "" {
				msg = hookOut.Reason
			}
			if msg == "" {
				msg = "denied by hook"
			}
			slog.Info("tool denied by PreToolUse hook",
				"tool", tc.Function.Name,
				"decision", string(hookOut.Decision),
				"reason", hookOut.Reason,
			)
			return fmt.Sprintf("Error: tool denied by PreToolUse hook: %s", msg), true
		}
		// hook 이 입력 수정을 요청한 경우 인자를 재직렬화한다.
		if len(hookOut.NewInput) > 0 {
			if newArgs, err := json.Marshal(hookOut.NewInput); err == nil {
				tc.Function.Arguments = string(newArgs)
				slog.Debug("PreToolUse hook modified tool input", "tool", tc.Function.Name)
			}
		}
	}

	// --- 실제 도구 실행 ---
	output, isError := ti.base(ctx, tc)

	// --- PostToolUse hook (fire-and-forget, 실패 무시) ---
	if ti.hookRunner != nil {
		args := parseArgsForHook(tc.Function.Arguments)
		hookIn := hooks.HookInput{
			Tool:     tc.Function.Name,
			Input:    args,
			Session:  ti.session,
			Metadata: ComputeMetadata(tc.Function.Name, args),
			Output: map[string]any{
				"output":   output,
				"is_error": isError,
			},
		}
		ti.hookRunner.RunPostToolUse(ctx, hookIn)
	}

	return output, isError
}

// AsToolRunner 는 ti.Invoke 를 ToolRunner 타입으로 반환한다.
func (ti *ToolInvoker) AsToolRunner() ToolRunner {
	return ti.Invoke
}

// parseArgsForHook 은 JSON 인자 문자열을 hook input map 으로 파싱한다.
// 파싱 실패 시 빈 map 을 반환한다 (hook 에서 에러로 처리하지 않도록).
func parseArgsForHook(arguments string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(arguments), &m); err != nil {
		return map[string]any{}
	}
	return m
}

// ParseArgsForCallback 은 BlockedCallback 등 패키지 외부에서 인자를 파싱할 때 사용한다.
func ParseArgsForCallback(arguments string) map[string]any {
	return parseArgsForHook(arguments)
}
