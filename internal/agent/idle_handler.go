// Package agent
// File: idle_handler.go
// Description: 쉘 명령 유휴 상태 감지 executor 래퍼 및 LLM 자동 응답 핸들러
// Responsibility: 인터랙티브 프롬프트 감지 시 LLM 판단 → 자동 응답 또는 TUI 위임

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/llm"
)

const maxLastLines = 5

// ─────────────────────────── IdleDetectExecutor ────────────────────────────

// IdleDetectExecutor는 StreamExecutor + StdinInjector를 구현하는 executor를 래핑하여
// 실행 중 출력이 defaultIdleThreshold 동안 없으면 IdleInputHandler를 호출한다.
type IdleDetectExecutor struct {
	wrapped     executor.StreamExecutor
	injector    executor.StdinInjector
	idleHandler IdleInputHandler
	toolName    string
	target      string

	mu        sync.Mutex
	lastLines []string // 유휴 컨텍스트 제공용 최근 라인
}

// wrapWithIdleDetect는 exec이 StreamExecutor + StdinInjector를 모두 구현하면
// IdleDetectExecutor로 래핑한다. 그렇지 않으면 원본 exec을 반환한다.
func wrapWithIdleDetect(exec executor.Executor, handler IdleInputHandler, toolName, target string) executor.Executor {
	se, isStream := exec.(executor.StreamExecutor)
	inj, isInject := exec.(executor.StdinInjector)
	if !isStream || !isInject {
		return exec
	}
	return &IdleDetectExecutor{
		wrapped:     se,
		injector:    inj,
		idleHandler: handler,
		toolName:    toolName,
		target:      target,
	}
}

// Execute는 executor.Executor를 구현한다. idle 감지 없이 바로 위임한다.
func (e *IdleDetectExecutor) Execute(ctx context.Context, command string) (executor.ExecResult, error) {
	return e.wrapped.Execute(ctx, command)
}

// Target은 executor.Executor를 구현한다.
func (e *IdleDetectExecutor) Target() string {
	return e.wrapped.Target()
}

// Platform returns the wrapped executor platform when available.
func (e *IdleDetectExecutor) Platform() executor.Platform {
	if provider, ok := e.wrapped.(executor.PlatformProvider); ok {
		return provider.Platform()
	}
	return executor.PlatformFromExecutor(e.wrapped)
}

// ShellName returns the wrapped executor shell label when available.
func (e *IdleDetectExecutor) ShellName() string {
	if provider, ok := e.wrapped.(executor.PlatformProvider); ok {
		return provider.ShellName()
	}
	return executor.ShellNameForExecutor(e.wrapped)
}

// ExecuteStream은 executor.StreamExecutor를 구현하며, 유휴 감지 로직을 추가한다.
// 출력이 defaultIdleThreshold(10초) 동안 없으면 idleHandler를 호출해 응답을 결정한다.
func (e *IdleDetectExecutor) ExecuteStream(ctx context.Context, command string, onLine func(string)) (executor.ExecResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	watcher := executor.NewIdleWatcher(0) // 0 = 기본 10초
	watcher.Start(ctx)
	defer watcher.Stop()

	go func() {
		select {
		case <-watcher.Triggered():
			e.handleIdle(ctx, cancel, command)
		case <-ctx.Done():
		}
	}()

	wrappedOnLine := func(line string) {
		watcher.Ping()
		e.appendLine(line)
		if onLine != nil {
			onLine(line)
		}
	}

	return e.wrapped.ExecuteStream(ctx, command, wrappedOnLine)
}

// InjectStdin은 executor.StdinInjector를 구현한다.
func (e *IdleDetectExecutor) InjectStdin(line string) error {
	return e.injector.InjectStdin(line)
}

func (e *IdleDetectExecutor) handleIdle(ctx context.Context, cancel context.CancelFunc, command string) {
	e.mu.Lock()
	lines := make([]string, len(e.lastLines))
	copy(lines, e.lastLines)
	e.mu.Unlock()

	req := IdleInputRequest{
		ToolName:  e.toolName,
		Target:    e.target,
		Command:   command,
		LastLines: lines,
	}

	resp, err := e.idleHandler.RequestIdleInput(ctx, req)
	if err != nil {
		slog.Warn("idle input handler error, aborting command", "err", err)
		cancel()
		return
	}
	if resp.Abort {
		cancel()
		return
	}
	if injectErr := e.injector.InjectStdin(resp.Input); injectErr != nil {
		slog.Warn("idle stdin inject failed", "err", injectErr)
		cancel()
	}
}

func (e *IdleDetectExecutor) appendLine(line string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastLines = append(e.lastLines, line)
	if len(e.lastLines) > maxLastLines {
		e.lastLines = e.lastLines[len(e.lastLines)-maxLastLines:]
	}
}

// ─────────────────────────── SmartIdleInputHandler ─────────────────────────

// SmartIdleInputHandler는 LLM으로 인터랙티브 프롬프트를 분류하고
// 자동 응답하거나 tuiHandler에 위임한다.
type SmartIdleInputHandler struct {
	llmClient  llm.Client
	tuiHandler IdleInputHandler // LLM이 "ask_user"를 반환하거나 실패 시 폴백
}

// NewSmartIdleInputHandler는 SmartIdleInputHandler를 생성한다.
// tuiHandler가 nil이면 LLM 실패/ask_user 시 abort로 처리한다.
func NewSmartIdleInputHandler(client llm.Client, tuiHandler IdleInputHandler) *SmartIdleInputHandler {
	return &SmartIdleInputHandler{llmClient: client, tuiHandler: tuiHandler}
}

const idleClassifySystemPrompt = `You are helping an AI agent respond to interactive shell prompts.
The shell command appears to be waiting for user input. Analyze the last lines of output
and respond with a JSON object ONLY (no other text):
{"action": "y"|"n"|"enter"|"ask_user"}
- "y": Send "y" — for [Y/n] prompts where yes is appropriate to continue
- "n": Send "n" — for [y/N] prompts where n/default is safer to send
- "enter": Send empty line — for "Press Enter to continue" style prompts
- "ask_user": Cannot auto-respond — password, confirmation name, or complex input required`

// idleAction은 LLM 판단 결과 JSON 구조이다.
type idleAction struct {
	Action string `json:"action"`
}

// RequestIdleInput은 IdleInputHandler를 구현한다.
func (h *SmartIdleInputHandler) RequestIdleInput(ctx context.Context, req IdleInputRequest) (IdleInputResponse, error) {
	userContent := fmt.Sprintf("Command: %s\nTarget: %s\nLast output lines:\n%s",
		req.Command, req.Target, strings.Join(req.LastLines, "\n"))

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: idleClassifySystemPrompt},
		{Role: llm.RoleUser, Content: userContent},
	}

	resp, err := h.llmClient.Chat(ctx, messages, nil)
	if err != nil {
		slog.Warn("idle llm classify failed, falling back", "err", err)
		return h.fallback(ctx, req)
	}

	var action idleAction
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &action); jsonErr != nil {
		slog.Warn("idle llm response parse failed, falling back", "raw", resp.Content, "err", jsonErr)
		return h.fallback(ctx, req)
	}

	slog.Info("idle auto-respond", "action", action.Action, "command", req.Command, "target", req.Target)

	switch action.Action {
	case "y":
		return IdleInputResponse{Input: "y"}, nil
	case "n":
		return IdleInputResponse{Input: "n"}, nil
	case "enter":
		return IdleInputResponse{Input: ""}, nil
	default: // "ask_user" 또는 알 수 없는 응답
		return h.fallback(ctx, req)
	}
}

func (h *SmartIdleInputHandler) fallback(ctx context.Context, req IdleInputRequest) (IdleInputResponse, error) {
	if h.tuiHandler == nil {
		slog.Warn("no tui handler for idle input, aborting", "command", req.Command)
		return IdleInputResponse{Abort: true}, nil
	}
	return h.tuiHandler.RequestIdleInput(ctx, req)
}
