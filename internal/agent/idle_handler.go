// Package agent
// File: idle_handler.go
// Description: Idle detection and smart prompt handling for interactive shell commands.
// Responsibility: Detect stdin-waiting commands and auto-respond when safe.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/privilege"
	"github.com/yourorg/infractl/internal/store"
)

const maxLastLines = 5

// IdleDetectExecutor wraps an executor and triggers idle handling when stdout stalls.
type IdleDetectExecutor struct {
	original    executor.Executor
	wrapped     executor.StreamExecutor
	injector    executor.StdinInjector
	idleHandler IdleInputHandler
	toolName    string
	target      string

	mu        sync.Mutex
	lastLines []string
}

func wrapWithIdleDetect(exec executor.Executor, handler IdleInputHandler, toolName, target string) executor.Executor {
	se, isStream := exec.(executor.StreamExecutor)
	inj, isInject := exec.(executor.StdinInjector)
	if !isStream || !isInject {
		return exec
	}
	return &IdleDetectExecutor{
		original:    exec,
		wrapped:     se,
		injector:    inj,
		idleHandler: handler,
		toolName:    toolName,
		target:      target,
	}
}

func (e *IdleDetectExecutor) Execute(ctx context.Context, command string) (executor.ExecResult, error) {
	return e.wrapped.Execute(ctx, command)
}

func (e *IdleDetectExecutor) Target() string {
	return e.wrapped.Target()
}

func (e *IdleDetectExecutor) Platform() executor.Platform {
	if provider, ok := e.wrapped.(executor.PlatformProvider); ok {
		return provider.Platform()
	}
	return executor.PlatformFromExecutor(e.wrapped)
}

func (e *IdleDetectExecutor) ShellName() string {
	if provider, ok := e.wrapped.(executor.PlatformProvider); ok {
		return provider.ShellName()
	}
	return executor.ShellNameForExecutor(e.wrapped)
}

// ExecuteInteractive forwards interactive execution when supported by the original executor.
func (e *IdleDetectExecutor) ExecuteInteractive(ctx context.Context, spec executor.InteractiveSpec, onChunk func(string)) (executor.ExecResult, error) {
	ie, ok := e.original.(executor.InteractiveExecutor)
	if !ok {
		return executor.ExecResult{}, fmt.Errorf("interactive execution is not supported on %s", executor.ExecutionContextLabel(e.original))
	}
	return ie.ExecuteInteractive(ctx, spec, onChunk)
}

// PrivilegeContext returns the attached privilege dependencies if supported by the original executor.
func (e *IdleDetectExecutor) PrivilegeContext() privilege.Context {
	if provider, ok := e.original.(privilege.ContextProvider); ok {
		return provider.PrivilegeContext()
	}
	return privilege.Context{}
}

func (e *IdleDetectExecutor) ExecuteStream(ctx context.Context, command string, onLine func(string)) (executor.ExecResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	watcher := executor.NewIdleWatcher(0)
	watcher.Start(ctx)
	defer watcher.Stop()

	// for-select 루프로 반복 트리거를 처리한다.
	// 비밀번호 프롬프트가 여러 번 나타나는 경우에도 매번 감지한다.
	go func() {
		for {
			select {
			case <-watcher.Triggered():
				e.handleIdle(ctx, cancel, command)
			case <-ctx.Done():
				return
			}
		}
	}()

	wrappedOnLine := func(line string) {
		watcher.Ping()
		e.appendLine(line)
		if onLine != nil {
			onLine(line)
		}
	}

	// PTY 스트리밍을 우선 사용한다.
	// PTY가 있으면 sudo/su/passwd 등의 프롬프트가 /dev/tty 대신 stdout으로 나와
	// idle handler가 감지하고 패스워드를 자동 주입할 수 있다.
	if pty, ok := e.wrapped.(executor.PTYStreamExecutor); ok {
		return pty.ExecuteStreamPTY(ctx, command, wrappedOnLine)
	}
	return e.wrapped.ExecuteStream(ctx, command, wrappedOnLine)
}

func (e *IdleDetectExecutor) InjectStdin(line string) error {
	return e.injector.InjectStdin(line)
}

// Upload forwards file upload to the original executor when it supports FileTransferExecutor.
// File transfers bypass idle detection — SFTP is not an interactive stdin/stdout protocol.
func (e *IdleDetectExecutor) Upload(ctx context.Context, localPath, remotePath string, onProgress func(int64, int64)) error {
	ft, ok := e.original.(executor.FileTransferExecutor)
	if !ok {
		return fmt.Errorf("executor %s does not support file transfer", e.target)
	}
	return ft.Upload(ctx, localPath, remotePath, onProgress)
}

// Download forwards file download to the original executor when it supports FileTransferExecutor.
func (e *IdleDetectExecutor) Download(ctx context.Context, remotePath, localPath string, onProgress func(int64, int64)) error {
	ft, ok := e.original.(executor.FileTransferExecutor)
	if !ok {
		return fmt.Errorf("executor %s does not support file transfer", e.target)
	}
	return ft.Download(ctx, remotePath, localPath, onProgress)
}

// SessionExecute forwards persistent session execution to the original executor.
// The idle wrapper must preserve this capability; otherwise session_id/root reuse
// silently falls back to one-shot SSH execution.
func (e *IdleDetectExecutor) SessionExecute(
	ctx context.Context,
	sessionID, command string,
	timeout time.Duration,
	onIdle func([]string) (string, bool),
) (executor.ShellRunResult, error) {
	pse, ok := e.original.(executor.PersistentSessionExecutor)
	if !ok {
		return executor.ShellRunResult{}, fmt.Errorf("persistent sessions are not supported on %s", executor.ExecutionContextLabel(e.original))
	}
	return pse.SessionExecute(ctx, sessionID, command, timeout, onIdle)
}

// SessionElevate forwards persistent session elevation to the original executor.
func (e *IdleDetectExecutor) SessionElevate(
	ctx context.Context,
	sessionID, elevationCmd string,
	timeout time.Duration,
	onIdle func([]string) (string, bool),
) (executor.ShellRunResult, error) {
	pse, ok := e.original.(executor.PersistentSessionExecutor)
	if !ok {
		return executor.ShellRunResult{}, fmt.Errorf("persistent sessions are not supported on %s", executor.ExecutionContextLabel(e.original))
	}
	return pse.SessionElevate(ctx, sessionID, elevationCmd, timeout, onIdle)
}

// SessionClose forwards persistent session close to the original executor.
func (e *IdleDetectExecutor) SessionClose(ctx context.Context, sessionID string) error {
	pse, ok := e.original.(executor.PersistentSessionExecutor)
	if !ok {
		return fmt.Errorf("persistent sessions are not supported on %s", executor.ExecutionContextLabel(e.original))
	}
	return pse.SessionClose(ctx, sessionID)
}

// SessionList forwards persistent session listing to the original executor.
func (e *IdleDetectExecutor) SessionList(ctx context.Context) ([]executor.SessionInfo, error) {
	pse, ok := e.original.(executor.PersistentSessionExecutor)
	if !ok {
		return nil, fmt.Errorf("persistent sessions are not supported on %s", executor.ExecutionContextLabel(e.original))
	}
	return pse.SessionList(ctx)
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

// SmartIdleInputHandler uses LLM to autonomously respond to interactive shell prompts.
// There is no user-facing fallback — the LLM decides or aborts.
type SmartIdleInputHandler struct {
	llmClient   llm.Client
	serverStore store.ServerStore
}

func NewSmartIdleInputHandler(client llm.Client) *SmartIdleInputHandler {
	return &SmartIdleInputHandler{llmClient: client}
}

func (h *SmartIdleInputHandler) SetServerStore(st store.ServerStore) {
	h.serverStore = st
}

const idleClassifySystemPrompt = `You are helping an AI agent respond to interactive shell prompts autonomously.
The shell command is waiting for stdin. Analyze the last output lines and respond with JSON ONLY (no other text):

{"input": "<string to send>", "abort": false}

If you cannot safely determine a response (e.g., an unknown password is required), respond:
{"input": "", "abort": true}

Decision rules:
- SSH host key "(yes/no/[fingerprint])?" or "(yes/no)?": {"input": "yes", "abort": false}
- Simple [Y/n] prompt where continuing is appropriate: {"input": "y", "abort": false}
- Simple [y/N] prompt where the safe default is no: {"input": "n", "abort": false}
- "Press Enter to continue" / "-- More --": {"input": "", "abort": false}
- unzip "replace FILE? [y]es, [n]o, [A]ll, [N]one, [r]ename:": {"input": "A", "abort": false}
- Any multi-choice prompt with bracketed options: pick the most appropriate option letter/word
- Password / passphrase field when no stored credential is available: {"input": "", "abort": true}
- NEVER abort for simple yes/no/choice prompts — always pick the best answer`

type idleAction struct {
	Input string `json:"input"`
	Abort bool   `json:"abort"`
}

// passwordPromptRegex detects common password prompts in English and Korean.
// Korean: "암호:" (su/sudo), Japanese: "パスワード:", common sudo "[sudo] password for user:"
var passwordPromptRegex = regexp.MustCompile(`(?i)(password:|passphrase for .*:|password for .*:|password for [^ ]+|[A-Za-z0-9_.-]+@[A-Za-z0-9_.:-]+'s password:|암호:|パスワード:)`)
var passwordPromptUserHostRegex = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]+)@([^']+)'s password:`)
var passwordPromptForRegex = regexp.MustCompile(`(?i)password for ([^:]+):`)

// Korean sudo prompt: "[sudo] <user>의 암호:"
var passwordPromptKoreanSudoRegex = regexp.MustCompile(`\[sudo\]\s+([A-Za-z0-9_.-]+)의 암호:`)

// ansiEscapeRegex strips ANSI color/style codes from PTY output before regex matching.
var ansiEscapeRegex = regexp.MustCompile(`\033\[[0-9;]*[mKHFABCDsuJn]`)

type passwordPromptMatcher struct {
	host string
	user string
}

func resolveStoredPassword(ctx context.Context, st store.ServerStore, req IdleInputRequest) (string, bool) {
	if st == nil {
		return "", false
	}

	combined := ansiEscapeRegex.ReplaceAllString(strings.Join(req.LastLines, "\n"), "")
	if !passwordPromptRegex.MatchString(combined) {
		return "", false
	}

	match := parsePasswordPrompt(combined)

	servers, err := st.List(ctx)
	if err != nil {
		slog.Warn("idle password lookup failed", "err", err, "target", req.Target)
		return "", false
	}

	target := strings.TrimSpace(req.Target)
	for _, srv := range servers {
		if srv.AuthType != store.AuthTypePassword || srv.Credential == "" {
			continue
		}

		// 프롬프트가 서버 SSH 접속 유저와 다른 계정(예: root)을 요구하면
		// 저장된 SSH 크리덴셜은 해당 계정의 비밀번호가 아니므로 주입하지 않는다.
		// su - root 는 root 비밀번호를 요구하며, SSH 크리덴셜은 틀린 비밀번호다.
		if match.user != "" && !strings.EqualFold(match.user, srv.User) {
			slog.Debug("idle password: prompt user differs from SSH user, skipping",
				"prompt_user", match.user, "ssh_user", srv.User, "target", target)
			continue
		}

		if target != "" && (strings.EqualFold(srv.Name, target) || strings.EqualFold(srv.Host, target)) {
			return srv.Credential, true
		}
		if match.host != "" && strings.EqualFold(srv.Host, match.host) {
			return srv.Credential, true
		}
		if match.user != "" && strings.EqualFold(srv.User, match.user) && target == "" {
			return srv.Credential, true
		}
	}
	return "", false
}

func parsePasswordPrompt(text string) passwordPromptMatcher {
	matcher := passwordPromptMatcher{}

	if m := passwordPromptUserHostRegex.FindStringSubmatch(text); len(m) == 3 {
		matcher.user = strings.TrimSpace(m[1])
		matcher.host = strings.TrimSpace(m[2])
		return matcher
	}

	if m := passwordPromptForRegex.FindStringSubmatch(text); len(m) == 2 {
		matcher.user = strings.TrimSpace(m[1])
		return matcher
	}

	// Korean sudo: "[sudo] <user>의 암호:"
	if m := passwordPromptKoreanSudoRegex.FindStringSubmatch(text); len(m) == 2 {
		matcher.user = strings.TrimSpace(m[1])
	}

	return matcher
}

func (h *SmartIdleInputHandler) RequestIdleInput(ctx context.Context, req IdleInputRequest) (IdleInputResponse, error) {
	if pw, ok := resolveStoredPassword(ctx, h.serverStore, req); ok {
		slog.Info("idle auto-respond password", "target", req.Target, "tool", req.ToolName)
		return IdleInputResponse{Input: pw}, nil
	}

	userContent := fmt.Sprintf("Command: %s\nTarget: %s\nLast output lines:\n%s",
		req.Command, req.Target, strings.Join(req.LastLines, "\n"))

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: idleClassifySystemPrompt},
		{Role: llm.RoleUser, Content: userContent},
	}

	resp, err := h.llmClient.Chat(ctx, messages, nil, nil)
	if err != nil {
		slog.Warn("idle llm classify failed, aborting command", "err", err)
		return IdleInputResponse{Abort: true}, nil
	}

	var action idleAction
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &action); jsonErr != nil {
		slog.Warn("idle llm response parse failed, aborting command", "raw", resp.Content, "err", jsonErr)
		return IdleInputResponse{Abort: true}, nil
	}

	if action.Abort {
		slog.Warn("idle llm chose to abort", "command", req.Command, "target", req.Target)
		return IdleInputResponse{Abort: true}, nil
	}

	slog.Info("idle auto-respond", "input", action.Input, "command", req.Command, "target", req.Target)
	return IdleInputResponse{Input: action.Input}, nil
}
