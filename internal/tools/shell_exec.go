package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/privilege"
)

// elevationPrefixes lists command prefixes that permanently change the shell user context.
// These commands must be sent via SessionElevate (no delimiter) rather than SessionExecute.
var elevationPrefixes = []string{
	"sudo -i", "sudo -s", "sudo su", "sudo bash", "sudo sh",
	"su -", "su -l", "exec sudo",
}

// ShellExecTool executes arbitrary shell commands locally or via SSH.
type ShellExecTool struct {
	OutputCb       func(string)
	PrivilegeCache *privilege.Cache
	PromptHandler  privilege.PromptHandler
}

func (t *ShellExecTool) Name() string { return "shell_exec" }

func (t *ShellExecTool) Description() string {
	return "Execute a shell command on a target server via SSH or locally.\n" +
		"IMPORTANT: Use dedicated tools (system_info, service_status, log_tail, file_read, etc.) instead of shell_exec whenever possible.\n" +
		"Use shell_exec ONLY when no dedicated tool covers the operation.\n" +
		"For installation or first-time setup tasks, read the install document first, then confirm install paths and environment choices with the user before running mutation commands.\n" +
		"Prefer non-interactive forms such as `sudo -n`, `runuser -l`, and `bash -lc` whenever possible.\n" +
		"For privilege escalation, see Safety Rules for proper `become_method` usage. Never use inline `echo 'PASSWORD' | sudo -S`.\n" +
		"When writing shell profile entries, escape $ as \\$ so variables are stored literally in the target file.\n" +
		"For database-specific queries, prefer connector tools (oracle.query, mysql.query) if available.\n" +
		"Always include 'target' field when targeting a specific server.\n" +
		"Always include 'description' field with a brief Korean explanation of what this command does."
}

func (t *ShellExecTool) IsReadOnly() bool { return false }
func (t *ShellExecTool) IsEnabled() bool  { return true }

func (t *ShellExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute. Direct sudo/su is allowed; become_method is preferred for structured privilege handling.",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Command timeout in seconds (default: 30)",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
			"become_method": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"sudo", "su"},
				"description": "Optional privilege escalation method. Use instead of raw sudo/su in the command.",
			},
			"become_user": map[string]interface{}{
				"type":        "string",
				"description": "Optional target account for privilege escalation. Defaults to 'root'.",
			},
			"risk_assessment": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"none", "low", "medium", "high"},
				"description": "Risk level of this command. none=read-only, low=modification, medium=deletion/config change, high=destructive (DROP/rm -rf/mkfs). Omit for read-only commands.",
			},
			"pre_backup_command": map[string]interface{}{
				"type":        "string",
				"description": "Command to run BEFORE the main command as a backup step. For high-risk commands (rm -rf, truncate), this is REQUIRED.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Brief Korean description of what this command does. Shown to the user in the TUI.",
			},
			"session_id": map[string]interface{}{
				"type": "string",
				"description": "Persistent session ID. When provided, the command runs in a long-lived bash session " +
					"where shell state (working directory, environment variables, su/sudo user context) is preserved " +
					"across calls. Use this when:\n" +
					"- Working directory must persist: cd /app then ls\n" +
					"- Environment variables must persist: export VAR=val then use $VAR\n" +
					"- Elevated user context must persist: sudo -i or su - oracle once, then run multiple commands\n" +
					"- Multi-step workflows require consistent shell state\n" +
					"Omit (or leave empty) for single isolated commands — this is safer and uses the original behavior.\n" +
					"Use meaningful names: 'root' for a root session, 'oracle' for an oracle session, 'default' for admin.\n" +
					"If root has already been acquired, use session_id='root' for permission-denied recovery; root can switch to any local account without another password.\n" +
					"Elevation commands (sudo -i, su - user) automatically persist the new user context within the session.\n" +
					"Check current user/dir from the [Session:] header in the response.",
			},
			"is_background": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, the command runs as a background task. Useful for long-running processes (e.g., --watch, -f).",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellExecTool) RiskLevel() RiskLevel { return RiskNone }

func (t *ShellExecTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	command, err := argString(args, "command", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	method, user, err := parseBecomeArgs(args)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}

	timeoutSec := argInt(args, "timeout", 30)
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	timeout := time.Duration(timeoutSec) * time.Second

	sessionID, _ := argString(args, "session_id", false)

	// ── persistent session path ──────────────────────────────────────────────
	if sessionID != "" {
		if pse, ok := exec.(executor.PersistentSessionExecutor); ok {
			return t.executeInSession(ctx, pse, exec, command, method, user, sessionID, timeout)
		}
		slog.Warn("shell_exec: session_id requested but executor does not support persistent sessions; falling back to one-shot", "target", exec.Target())
	}

	// ── one-shot path (original behavior) ────────────────────────────────────
	contextLine := "Execution Context: " + executor.ExecutionContextLabel(exec)

	if preBackup, _ := argString(args, "pre_backup_command", false); preBackup != "" {
		backupCtx, backupCancel := context.WithTimeout(ctx, 5*time.Minute)
		bakRes, bakErr := t.executeCommand(backupCtx, exec, preBackup, method, user)
		backupCancel()
		if bakErr != nil || bakRes.ExitCode != 0 {
			errMsg := preBackup + " failed"
			if bakErr != nil {
				errMsg = bakErr.Error()
			} else if bakRes.Stderr != "" {
				errMsg = bakRes.Stderr
			} else if bakRes.Stdout != "" {
				errMsg = bakRes.Stdout
			}
			return fmt.Sprintf("%s\npre_backup_command failed; main command aborted:\n%s", contextLine, errMsg), nil
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := t.executeCommand(cmdCtx, exec, command, method, user)
	if err != nil {
		return fmt.Sprintf("%s\nExecution failed: %s", contextLine, err), nil
	}

	// Reuse an acquired root session once when a plain command fails on permissions.
	if result.ExitCode != 0 && method == privilege.MethodNone && isPermissionFailure(result, nil) {
		if retryResult, ok := executePlainViaAcquiredRoot(cmdCtx, exec, command); ok && retryResult.ExitCode == 0 {
			result = retryResult
		}
	}

	privilegeNote := ""
	reusedPrivilege := strings.Contains(result.Stdout, "[Privilege Reused:")
	if result.ExitCode == 0 && !reusedPrivilege && method != privilege.MethodNone {
		if pse, ok := exec.(executor.PersistentSessionExecutor); ok {
			if t.autoAcquireSession(exec.Target(), pse, method, user) {
				privilegeNote = fmt.Sprintf("[Privilege Acquired: session %q is available as %s]", sessionIDForUser(user), privilege.NormalizeUser(user))
			}
		}
	} else if result.ExitCode == 0 && !reusedPrivilege && method == privilege.MethodNone {
		if sudoUser, ok := inferSuccessfulSudoUser(command); ok {
			if pse, ok := exec.(executor.PersistentSessionExecutor); ok {
				if t.autoAcquireSession(exec.Target(), pse, privilege.MethodSudo, sudoUser) {
					privilegeNote = fmt.Sprintf("[Privilege Acquired: session %q is available as %s]", sessionIDForUser(sudoUser), privilege.NormalizeUser(sudoUser))
				}
			}
		}
	}

	output := fmt.Sprintf("%s\n[Exit Code: %d]\n%s", contextLine, result.ExitCode, result.Stdout)
	if result.Stderr != "" {
		output += fmt.Sprintf("\n[Stderr]\n%s", result.Stderr)
	}
	if privilegeNote != "" {
		output += "\n" + privilegeNote
	}
	return output, nil
}

// executeInSession routes the command through a persistent shell session.
// Elevation commands (sudo -i, su - user) use SessionElevate to permanently change context.
func (t *ShellExecTool) executeInSession(
	ctx context.Context,
	pse executor.PersistentSessionExecutor,
	exec executor.Executor,
	command string,
	method privilege.Method,
	user string,
	sessionID string,
	timeout time.Duration,
) (string, error) {
	// Build the effective command (wrap with become if requested)
	effectiveCmd := command
	if method != privilege.MethodNone {
		plan, err := privilege.BuildPlan(method, user, command)
		if err != nil {
			return fmt.Sprintf("Error: build privilege plan: %s", err), nil
		}
		effectiveCmd = plan.NonInteractiveRun // try non-interactive first within session
	}

	onIdle := t.buildSessionIdleHandler(ctx, exec.Target(), method, user)

	var result executor.ShellRunResult
	var execErr error

	if isElevationCommand(effectiveCmd) {
		// Route to the target user's own session so the current session is not mutated.
		// Example: root session + "su - oracle" → elevate oracle session, root stays intact.
		targetUser := extractElevationTargetUser(effectiveCmd)
		targetSessionID := sessionIDForUser(targetUser)
		result, execErr = pse.SessionElevate(ctx, targetSessionID, effectiveCmd, timeout, onIdle)
	} else {
		result, execErr = pse.SessionExecute(ctx, sessionID, effectiveCmd, timeout, onIdle)
	}

	if execErr != nil {
		return fmt.Sprintf("Execution Context: %s\nSession %q execution failed: %s",
			executor.ExecutionContextLabel(exec), sessionID, execErr), nil
	}

	sessionHeader := fmt.Sprintf("Execution Context: %s\n[Session: %s | User: %s | CWD: %s]",
		executor.ExecutionContextLabel(exec), sessionID, result.CurrentUser, result.CurrentDir)
	output := fmt.Sprintf("%s\n[Exit Code: %d]\n%s", sessionHeader, result.ExitCode, result.Stdout)
	return output, nil
}

// buildSessionIdleHandler returns an onIdle callback that resolves passwords
// from privilege.Cache or PromptHandler when a session command stalls on a prompt.
func (t *ShellExecTool) buildSessionIdleHandler(
	ctx context.Context,
	target string,
	method privilege.Method,
	user string,
) func([]string) (string, bool) {
	return func(recentLines []string) (string, bool) {
		// Try cache first
		if t.PrivilegeCache != nil {
			if pw, ok := t.PrivilegeCache.Get(target, method, user); ok {
				return pw, false
			}
		}
		// Fallback to prompt handler
		if t.PromptHandler == nil {
			slog.Warn("session idle: password required but no prompt handler configured", "target", target)
			return "", true // abort
		}
		resp, err := t.PromptHandler.RequestPassword(ctx, privilege.PromptRequest{
			Target: target, Method: method, User: user,
		})
		if err != nil || resp.Abort || strings.TrimSpace(resp.Password) == "" {
			return "", true // abort
		}
		// Cache the supplied password for future use
		if t.PrivilegeCache != nil {
			t.PrivilegeCache.Set(target, method, user, resp.Password)
		}
		return resp.Password, false
	}
}

// autoAcquireSession creates and elevates a persistent session after a successful
// one-shot privilege escalation. This makes the elevated context available for
// subsequent commands without re-authentication.
func (t *ShellExecTool) autoAcquireSession(
	target string,
	pse executor.PersistentSessionExecutor,
	method privilege.Method,
	user string,
) bool {
	sessionID := sessionIDForUser(user)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var elevationCmd string
	switch method {
	case privilege.MethodSudo:
		normalizedUser := privilege.NormalizeUser(user)
		elevationCmd = fmt.Sprintf("sudo -i -u %s", normalizedUser)
		if normalizedUser == "root" {
			elevationCmd = "sudo -i"
		}
	case privilege.MethodSU:
		elevationCmd = fmt.Sprintf("su - %s", privilege.NormalizeUser(user))
	default:
		return false
	}

	onIdle := func(lines []string) (string, bool) {
		if t.PrivilegeCache != nil {
			if pw, ok := t.PrivilegeCache.Get(target, method, user); ok {
				return pw, false
			}
		}
		return "", true // no cached password — skip acquisition silently
	}

	// Check if session already exists and is alive
	if sessions, err := pse.SessionList(ctx); err == nil {
		for _, s := range sessions {
			if s.SessionID == sessionID && s.Alive {
				return true // already have a live session
			}
		}
	}

	if _, err := pse.SessionElevate(ctx, sessionID, elevationCmd, 30*time.Second, onIdle); err != nil {
		slog.Debug("auto-acquire session failed (non-critical)", "session", sessionID, "target", target, "err", err)
		return false
	}
	slog.Info("auto-acquired persistent session", "session", sessionID, "target", target)
	return true
}

func sessionIDForUser(user string) string {
	user = privilege.NormalizeUser(user)
	if user == "" || user == "root" {
		return "root"
	}
	return user
}

// isElevationCommand reports whether cmd is a shell-context-changing elevation command.
// These must be sent via SessionElevate (no delimiter wrapping) to persist the new shell.
func isElevationCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, prefix := range elevationPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

// HasBecomeArgs reports whether shell_exec requested structured privilege escalation.
func HasBecomeArgs(args map[string]interface{}) bool {
	raw, _ := args["become_method"].(string)
	return strings.TrimSpace(raw) != ""
}

func parseBecomeArgs(args map[string]interface{}) (privilege.Method, string, error) {
	rawMethod, _ := args["become_method"].(string)
	method, ok := privilege.ParseMethod(rawMethod)
	if !ok {
		return privilege.MethodNone, "", fmt.Errorf("unsupported become_method %q", rawMethod)
	}
	rawUser, _ := args["become_user"].(string)
	return method, privilege.NormalizeUser(rawUser), nil
}

func (t *ShellExecTool) executeCommand(
	ctx context.Context,
	exec executor.Executor,
	command string,
	method privilege.Method,
	user string,
) (executor.ExecResult, error) {
	if method == privilege.MethodNone {
		return t.executePlain(ctx, exec, command)
	}

	platform := executor.CommandPlatform(exec)
	if platform == executor.PlatformWindows {
		if executor.IsLocalTarget(exec.Target()) {
			elevated, err := t.isLocalWindowsElevated(ctx, exec)
			if err != nil {
				return executor.ExecResult{}, err
			}
			if !elevated {
				return executor.ExecResult{}, fmt.Errorf("Windows privilege escalation is not automated. Re-run infractl as Administrator and retry")
			}
			return t.executePlain(ctx, exec, command)
		}
		return executor.ExecResult{}, fmt.Errorf("Windows privilege escalation is not automated for remote targets. Use an already elevated account or run from an Administrator session")
	}

	if result, ok := executeWithAcquiredPrivilege(ctx, exec, command, user); ok {
		return result, nil
	}

	result, err := t.tryPrivilegeMethod(ctx, exec, command, method, user)

	// sudo 실패 → su fallback
	if method == privilege.MethodSudo && isSudoDenied(result, err) {
		slog.Info("sudo failed, falling back to su", "user", user)
		return t.tryPrivilegeMethod(ctx, exec, command, privilege.MethodSU, user)
	}

	return result, err
}

// tryPrivilegeMethod는 지정된 권한 상승 방법으로 명령을 실행한다.
func (t *ShellExecTool) tryPrivilegeMethod(
	ctx context.Context,
	exec executor.Executor,
	command string,
	method privilege.Method,
	user string,
) (executor.ExecResult, error) {
	plan, err := privilege.BuildPlan(method, user, command)
	if err != nil {
		return executor.ExecResult{}, err
	}

	if method == privilege.MethodSudo {
		validateRes, validateErr := exec.Execute(ctx, plan.ValidateCommand)
		if validateErr != nil {
			return executor.ExecResult{}, validateErr
		}
		switch privilege.ClassifyValidateResult(validateRes) {
		case privilege.ValidateAllowed:
			return t.executePlain(ctx, exec, plan.NonInteractiveRun)
		case privilege.ValidateDenied:
			return validateRes, privilege.ErrPermissionDenied
		}
		// ValidateNeedsPassword: 아래 executeInteractivePrivilege로 진행
	}

	return t.executeInteractivePrivilege(ctx, exec, plan)
}

// isSudoDenied는 sudo 실패로 su fallback이 필요한지 판단한다.
func isSudoDenied(_ executor.ExecResult, err error) bool {
	return errors.Is(err, privilege.ErrPermissionDenied) || errors.Is(err, privilege.ErrAuthFailed)
}

func (t *ShellExecTool) executePlain(ctx context.Context, exec executor.Executor, command string) (executor.ExecResult, error) {
	// stdin을 /dev/null로 리다이렉트해 인터랙티브 블로킹 방지.
	// Windows 대상이거나 이미 stdin 리다이렉트(<, <<)가 있으면 제외.
	if executor.CommandPlatform(exec) != executor.PlatformWindows && !hasStdinRedirect(command) {
		command += " </dev/null"
	}
	if se, ok := exec.(executor.StreamExecutor); ok && t.OutputCb != nil {
		return se.ExecuteStream(ctx, command, t.OutputCb)
	}
	return exec.Execute(ctx, command)
}

// hasStdinRedirect은 명령에 이미 stdin 리다이렉트(<, <<)가 있거나
// 파이프(|)로 stdin을 공급하는 패턴이 있으면 true를 반환한다.
// 파이프가 있을 때 </dev/null을 추가하면 파이프 stdin이 오버라이드되어
// echo 'PASSWORD' | sudo -S 같은 패턴이 동작하지 않으므로 반드시 제외한다.
func hasStdinRedirect(command string) bool {
	return strings.Contains(command, "<") || strings.Contains(command, "|")
}

func (t *ShellExecTool) executeInteractivePrivilege(ctx context.Context, exec executor.Executor, plan privilege.Plan) (executor.ExecResult, error) {
	target := exec.Target()
	if target == "" {
		target = "localhost"
	}

	privCtx := privilege.Context{
		Cache:         t.PrivilegeCache,
		PromptHandler: t.PromptHandler,
	}
	if provider, ok := exec.(privilege.ContextProvider); ok {
		// Only override when the executor provides a non-empty context.
		// IdleDetectExecutor wraps SSHExecutor which doesn't implement ContextProvider,
		// so it returns a zero-value Context — using it would discard the tool's
		// configured Cache and PromptHandler.
		if override := provider.PrivilegeContext(); override.Cache != nil || override.PromptHandler != nil {
			privCtx = override
		}
	}

	var lastResult executor.ExecResult
	var lastErr error
	useCache := true
	for attempt := 0; attempt < 2; attempt++ {
		password, fromCache, err := resolvePrivilegePassword(ctx, privCtx, target, plan.Method, plan.User, useCache)
		if err != nil {
			return executor.ExecResult{}, err
		}

		lastResult, lastErr = t.runInteractiveWithPassword(ctx, exec, plan, password)
		if lastErr == nil {
			if privCtx.Cache != nil {
				privCtx.Cache.Set(target, plan.Method, plan.User, password)
			}
			return lastResult, nil
		}
		if !errors.Is(lastErr, privilege.ErrAuthFailed) {
			return lastResult, lastErr
		}

		if privCtx.Cache != nil {
			privCtx.Cache.Delete(target, plan.Method, plan.User)
		}
		useCache = false
		if !fromCache && attempt > 0 {
			break
		}
	}

	return lastResult, lastErr
}

func resolvePrivilegePassword(
	ctx context.Context,
	privCtx privilege.Context,
	target string,
	method privilege.Method,
	user string,
	allowCache bool,
) (string, bool, error) {
	if allowCache && privCtx.Cache != nil {
		if password, ok := privCtx.Cache.Get(target, method, user); ok {
			return password, true, nil
		}
	}
	if privCtx.PromptHandler == nil {
		return "", false, fmt.Errorf("privilege password is required but no prompt handler is configured")
	}
	resp, err := privCtx.PromptHandler.RequestPassword(ctx, privilege.PromptRequest{
		Target: target,
		Method: method,
		User:   user,
	})
	if err != nil {
		return "", false, err
	}
	if resp.Abort || strings.TrimSpace(resp.Password) == "" {
		return "", false, fmt.Errorf("privilege password prompt cancelled")
	}
	return resp.Password, false, nil
}

func (t *ShellExecTool) runInteractiveWithPassword(
	ctx context.Context,
	exec executor.Executor,
	plan privilege.Plan,
	password string,
) (executor.ExecResult, error) {
	interactive, ok := exec.(executor.InteractiveExecutor)
	if !ok {
		return executor.ExecResult{}, fmt.Errorf("interactive privilege execution is not supported on %s", executor.ExecutionContextLabel(exec))
	}

	var rawOutput bytes.Buffer
	lineSink := newChunkLineSink(plan.Method, plan.PromptToken, t.OutputCb)
	promptCount := 0
	passwordSent := false
	var callbackErr error

	result, err := interactive.ExecuteInteractive(ctx, executor.InteractiveSpec{
		Command:    plan.InteractiveRun,
		RequirePTY: plan.RequirePTY,
	}, func(chunk string) {
		if callbackErr != nil {
			return
		}
		rawOutput.WriteString(chunk)
		current := rawOutput.String()
		count := privilege.CountPromptMatches(plan.Method, current, plan.PromptToken)
		if count > promptCount {
			promptCount = count
			if !passwordSent {
				if injectErr := interactive.InjectStdin(password); injectErr != nil {
					callbackErr = injectErr
					return
				}
				passwordSent = true
			}
		}
		lineSink.Append(chunk)
	})
	if err != nil {
		return result, err
	}
	if callbackErr != nil {
		return result, callbackErr
	}
	lineSink.Flush()

	raw := rawOutput.String()
	if raw == "" {
		raw = result.Stdout
	}
	result.Stdout = privilege.SanitizeOutput(plan.Method, raw, plan.PromptToken)
	result.Stderr = ""
	if privilege.IsAuthFailure(raw, promptCount) {
		return result, privilege.ErrAuthFailed
	}
	return result, nil
}

func (t *ShellExecTool) isLocalWindowsElevated(ctx context.Context, exec executor.Executor) (bool, error) {
	script := "if (([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { 'true' } else { 'false' }"
	result, err := exec.Execute(ctx, executor.PowerShellCommand(exec, script))
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(result.Stdout), "true"), nil
}

type chunkLineSink struct {
	method      privilege.Method
	promptToken string
	onLine      func(string)
	pending     string
}

func newChunkLineSink(method privilege.Method, promptToken string, onLine func(string)) *chunkLineSink {
	return &chunkLineSink{
		method:      method,
		promptToken: promptToken,
		onLine:      onLine,
	}
}

func (s *chunkLineSink) Append(chunk string) {
	if s.onLine == nil {
		return
	}
	s.pending += chunk
	for {
		idx := strings.IndexByte(s.pending, '\n')
		if idx < 0 {
			return
		}
		line := s.pending[:idx]
		s.pending = s.pending[idx+1:]
		s.emit(line)
	}
}

func (s *chunkLineSink) Flush() {
	if s.onLine == nil || s.pending == "" {
		return
	}
	s.emit(s.pending)
	s.pending = ""
}

func (s *chunkLineSink) emit(raw string) {
	line := privilege.SanitizeOutput(s.method, raw, s.promptToken)
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	s.onLine(line)
}
