// Package tools
// File: shell_exec_become.go
// Description: sudo/su 권한 승격 실행 경로
// Responsibility: become_method + become_user 파라미터 기반의 대화형/비대화형 실행

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/privilege"
)

// executeWithBecome은 method/user에 맞춰 sudo 또는 su로 명령을 실행한다.
// 비대화형 경로(NOPASSWD sudo, root→runuser)를 먼저 시도하고,
// 실패 시 PTY 기반 대화형 실행으로 비밀번호 프롬프트를 처리한다.
func executeWithBecome(
	ctx context.Context,
	exec executor.Executor,
	command string,
	rawMethod string,
	user string,
	cache *privilege.Cache,
	prompter privilege.PromptHandler,
) (executor.ExecResult, bool, error) {
	method, ok := privilege.ParseMethod(rawMethod)
	if !ok || method == privilege.MethodNone {
		return executor.ExecResult{}, false, nil
	}
	if executor.CommandPlatform(exec) == executor.PlatformWindows {
		return executor.ExecResult{}, false, fmt.Errorf("privilege escalation is not supported on Windows targets")
	}

	plan, err := privilege.BuildPlan(method, user, command)
	if err != nil {
		return executor.ExecResult{}, true, err
	}

	// 1) su: try runuser(1) first — works without a password when running as root.
	if method == privilege.MethodSU && plan.NonInteractiveRun != "" {
		run, runErr := exec.Execute(ctx, plan.NonInteractiveRun)
		if runErr == nil && run.ExitCode == 0 {
			return run, true, nil
		}
	}

	// 2) sudo: validate non-interactive access, then run without a password.
	if method == privilege.MethodSudo && plan.ValidateCommand != "" {
		validate, _ := exec.Execute(ctx, plan.ValidateCommand)
		if privilege.ClassifyValidateResult(validate) == privilege.ValidateAllowed {
			run, runErr := exec.Execute(ctx, plan.NonInteractiveRun)
			return run, true, runErr
		}
	}

	ie, ok := exec.(executor.InteractiveExecutor)
	if !ok {
		if plan.ProfileFallbackRun != "" {
			run, runErr := exec.Execute(ctx, plan.ProfileFallbackRun)
			if runErr == nil && run.ExitCode == 0 {
				run.Stdout = "[Profile Fallback: ran as current user with target user's environment]\n" + run.Stdout
				return run, true, nil
			}
		}
		if method == privilege.MethodSudo && plan.NonInteractiveRun != "" {
			run, runErr := exec.Execute(ctx, plan.NonInteractiveRun)
			return run, true, runErr
		}
		return executor.ExecResult{}, true, fmt.Errorf("interactive execution is required for %s but executor does not support it", method)
	}

	target := exec.Target()
	if strings.TrimSpace(target) == "" {
		target = "localhost"
	}
	pw, _ := cache.Get(target, method, plan.User)
	if strings.TrimSpace(pw) == "" {
		if prompter == nil {
			return executor.ExecResult{}, true, fmt.Errorf("no privilege prompt handler available")
		}
		resp, err := prompter.RequestPassword(ctx, privilege.PromptRequest{Target: target, Method: method, User: plan.User})
		if err != nil {
			return executor.ExecResult{}, true, err
		}
		if resp.Abort {
			return executor.ExecResult{}, true, fmt.Errorf("privilege prompt aborted")
		}
		pw = resp.Password
	}

	router := newStdinRouter(exec)
	var chunks []string
	// 파일 작업 관련 명령어일 경우 PTY를 비활성화하여 대화형 프롬프트 멈춤 방지
	requirePTY := plan.RequirePTY
	if isFileTransferCommand(plan.InteractiveRun) {
		requirePTY = false
	}

	session, err := ie.ExecuteInteractive(ctx, executor.InteractiveSpec{Command: plan.InteractiveRun, RequirePTY: requirePTY}, func(chunk string) {
		chunks = append(chunks, chunk)
		if trimmed := strings.TrimSpace(chunk); trimmed != "" && trimmed != plan.PromptToken {
			EmitOutput(ctx, trimmed)
		}
		if plan.PromptToken != "" && strings.Contains(chunk, plan.PromptToken) {
			_ = router.Inject(pw)
		}
	})
	if err != nil {
		return executor.ExecResult{}, true, err
	}
	if err := router.BindSession(session); err != nil {
		return executor.ExecResult{}, true, err
	}

	run, waitErr := session.Wait()
	if waitErr != nil {
		return executor.ExecResult{}, true, waitErr
	}

	combined := strings.Join(chunks, "")
	run.Stdout = privilege.SanitizeOutput(method, combined, plan.PromptToken)
	if privilege.IsAuthFailure(combined, privilege.CountPromptMatches(method, combined, plan.PromptToken)) {
		cache.Delete(target, method, plan.User)
	}
	if run.ExitCode == 0 {
		cache.Set(target, method, plan.User, pw)
	}
	return run, true, nil
}
