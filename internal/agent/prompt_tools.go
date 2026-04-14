// Package agent
// File: prompt_tools.go
// Description: 도구 우선순위, 안전 규칙, RAG 섹션 시스템 프롬프트 구성 함수
// Responsibility: 전용 도구 우선 사용 규칙, 안전 규칙, 지식 검색 우선순위를 프롬프트에 추가

package agent

import (
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
)

// appendToolSelectionGuidelines는 도구 선택 가이드라인 섹션을 추가한다.
func appendToolSelectionGuidelines(sb *strings.Builder) {
	sb.WriteString("## Tool Selection Guidelines\n")
	sb.WriteString("Follow this decision priority when choosing tools:\n\n")
	sb.WriteString("**Information Gathering**\n")
	sb.WriteString("1. If you already know the answer with high confidence, respond directly.\n")
	sb.WriteString("2. If the user is asking which machine, shell, or default target is in effect, call `session_context` first.\n")
	sb.WriteString("3. If you need server-specific data, use the appropriate dedicated tool on the correct target.\n")
	sb.WriteString("4. For troubleshooting or error resolution, use `rag_search` first.\n")
	sb.WriteString("5. If `rag_search` has no results and internet is available, use `web_search`.\n")
	sb.WriteString("6. If you need detailed content from a specific URL, use `web_fetch` after `web_search`.\n")
	sb.WriteString("7. If user intent is ambiguous or you must choose between multiple valid solutions, call `ask_user_question` to present choices to the user.\n\n")

	sb.WriteString("**Adaptive Service Learning**\n")
	sb.WriteString("1. First check the learned systems section above.\n")
	sb.WriteString("2. If not found, use `shell_exec` to locate binaries.\n")
	sb.WriteString("3. If still not found and internet is available, use `web_search` for setup or commands.\n")
	sb.WriteString("4. After discovery, call `save_learned_system` to persist the result.\n\n")

	sb.WriteString("**Multi-Server Operations**\n")
	sb.WriteString("- Read-only queries can execute on multiple targets.\n")
	sb.WriteString("- Mutation commands must execute on one target at a time, with confirmation when required.\n\n")
}

// appendDiscoveryFlow는 서비스 디스커버리 플로우 섹션을 추가한다.
func appendDiscoveryFlow(sb *strings.Builder) {
	sb.WriteString("**Service Discovery Flow**\n")
	sb.WriteString("1. Call `discover_services` to scan processes, ports, and config files.\n")
	sb.WriteString("2. If a known database service is found, call `connector_probe_os_auth` first.\n")
	sb.WriteString("3. If confidence is only medium, ask the user to confirm service type before activating.\n")
	sb.WriteString("4. Once a connector is active, use connector-specific tools instead of raw shell_exec.\n\n")
}

func appendDedicatedToolPriority(sb *strings.Builder) {
	sb.WriteString("## Dedicated Tool Priority (IMPORTANT)\n")
	sb.WriteString("When a dedicated tool exists, you MUST use it instead of shell_exec.\n")
	sb.WriteString("Dedicated tools are cross-platform, produce structured output, and can run in parallel.\n\n")
	sb.WriteString("| Dedicated Tool | Replaces shell_exec with |\n")
	sb.WriteString("|---|---|\n")
	sb.WriteString("| `system_info` | uname, hostname, uptime, free, df, lscpu, /proc/meminfo |\n")
	sb.WriteString("| `service_status` | systemctl status/list, sc query, service --status-all |\n")
	sb.WriteString("| `log_tail` | journalctl, tail -f/n /var/log/*, Get-EventLog |\n")
	sb.WriteString("| `k8s_query` | kubectl get/describe/logs/top |\n")
	sb.WriteString("| `disk_usage` | du -sh, du --max-depth, df -h |\n")
	sb.WriteString("| `process_list` | ps aux, tasklist, pgrep |\n")
	sb.WriteString("| `network_info` | ss -tlnp, netstat, Get-NetTCPConnection |\n")
	sb.WriteString("| `file_read` | cat, head, tail, Get-Content |\n")
	sb.WriteString("| `file_transfer` | scp, sftp — file upload/download between local and remote server |\n\n")
	sb.WriteString("Use `shell_exec` ONLY when no dedicated tool covers the operation.\n\n")
}

func appendSafetyRules(sb *strings.Builder) {
	sb.WriteString("## Safety Rules\n")
	sb.WriteString("**CRITICAL - Follow these rules at all times:**\n\n")
	sb.WriteString("1. When using `shell_exec`, include a short Korean `description` argument.\n")
	sb.WriteString("2. When using `shell_exec`, include `risk_assessment` for non-read-only commands.\n")
	sb.WriteString("3. If the request is ambiguous, ask for clarification before executing anything.\n")
	sb.WriteString("4. High-risk operations require explicit confirmation.\n")
	sb.WriteString("5. High-risk shell commands must include `pre_backup_command`.\n")
	sb.WriteString("6. Mutation tool calls should include rollback and checkpoint metadata when supported.\n")
	sb.WriteString("7. Before restarting a service, capture current status and prefer dedicated restart tools.\n")
	sb.WriteString("8. Learned mutation commands must carry backup instructions.\n")
	sb.WriteString("9. For file uploads/downloads, ALWAYS use `file_transfer` instead of `shell_exec` + scp/sftp. `file_transfer` reuses the authenticated SSH connection and requires no password prompt.\n")
	sb.WriteString("10. Avoid heredoc commands like `<<EOF` for file edits or config writes; prefer non-interactive `printf` or `tee` one-liners so the command does not block on stdin.\n")
	sb.WriteString("11. When appending shell environment exports, escape `$` so variable references are written literally to the target file.\n")
	sb.WriteString("12. Avoid commands that wait for stdin or a password prompt. Prefer non-interactive forms such as `sudo -n`, `runuser -l`, `bash -lc`, or explicit env exports.\n")
	sb.WriteString("13. **CRITICAL — Privilege escalation and session management:**\n" +
		"    - ALWAYS use `become_method`/`become_user` (PTY stdin injection) for privilege escalation.\n" +
		"    - NEVER use inline `echo 'PASSWORD' | sudo -S` or `printf | sudo -S`.\n" +
		"    - NEVER use `su - root` (use `become_method: sudo` instead).\n" +
		"    - Reuse acquired sessions (e.g. `session_id: \"root\"` or `session_id: \"oracle\"`) instead of elevating repeatedly.\n" +
		"    - For file transfers, ALWAYS use `file_transfer` tool (reuses SSH connection, no password needed).\n\n")
}



func appendRAGSection(sb *strings.Builder, ragSources []store.RAGSource, stats *rag.KnowledgeStats) {
	sb.WriteString("## Knowledge Search Priority\n")
	sb.WriteString("0. Local knowledge via `rag_search`\n")
	sb.WriteString("1. External RAG via `rag_search`\n")
	sb.WriteString("2. `web_search`\n")
	sb.WriteString("3. Your own knowledge\n\n")

	if len(ragSources) > 0 {
		sb.WriteString("Registered RAG sources:\n")
		for _, src := range ragSources {
			sb.WriteString(fmt.Sprintf("- **%s** (%s) @ %s/%s [priority: %d]\n",
				src.Name, src.Description, src.ServerName, src.DBType, src.Priority))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("Registered RAG sources: none (use `rag_register` to add external knowledge sources)\n\n")
	}

	if stats != nil {
		sb.WriteString(fmt.Sprintf("Internal Memory Stats: %d docs total (%d with vector embeddings)\n", stats.TotalDocs, stats.TotalWithVec))
		for source, cnt := range stats.CountBySource {
			sb.WriteString(fmt.Sprintf("  - %s: %d\n", source, cnt))
		}
		sb.WriteString("Knowledge categories:\n")
		for cat, cnt := range stats.CountByCategory {
			sb.WriteString(fmt.Sprintf("  - %s: %d\n", cat, cnt))
		}
		sb.WriteString("\n")
	}
}

func connectorIcon(status string) string {
	switch status {
	case "connected":
		return "[+]"
	case "connecting":
		return "[~]"
	case "error":
		return "[!]"
	default:
		return "[ ]"
	}
}

func appendLocalControllerContext(sb *strings.Builder, goos, goarch, hostname, cwd string) {
	sb.WriteString("## Current Environment (Local Controller)\n")
	sb.WriteString(fmt.Sprintf("- OS: %s (%s)\n", goos, goarch))
	sb.WriteString(fmt.Sprintf("- Hostname: %s\n", hostname))
	sb.WriteString(fmt.Sprintf("- Working Directory: %s\n", cwd))
	switch goos {
	case "windows":
		sb.WriteString("- Local Shell: PowerShell\n")
		sb.WriteString("- Command Guidance: Use PowerShell cmdlets and Windows paths by default.\n\n")
	default:
		sb.WriteString("- Local Shell: bash\n")
		sb.WriteString("- Command Guidance: Use standard POSIX shell utilities by default.\n\n")
	}
}

func appendContextGuardrails(sb *strings.Builder, noActiveServer bool) {
	sb.WriteString("## Local vs Remote Guardrails\n")
	sb.WriteString("- **Local execution is ALWAYS available.** You can run commands on this local machine at any time by using `\"target\": \"localhost\"` in tool arguments.\n")
	sb.WriteString("- **NEVER say local execution is impossible or unsupported.** If the user asks you to run a local command, do it via `shell_exec` with `target: \"localhost\"`.\n")
	sb.WriteString("- Treat `this PC`, `local`, `the PC running infractl`, and Windows drive paths like `C:\\` as the local controller unless the user explicitly names a server.\n")
	sb.WriteString("- Do not assume a remote SSH session when there is no active server and no explicit target.\n")
	sb.WriteString("- Before claiming access is impossible or before asserting local vs remote context, call `session_context` or run a read-only probe first.\n")
	sb.WriteString("- When the user is asking about execution context, machine identity, or shell choice, prefer `session_context` before `shell_exec`.\n")
	sb.WriteString("- Commands like `scp`, `ssh`, local file operations, and PowerShell cmdlets that reference local paths should run with `target: \"localhost\"`.\n")
	if noActiveServer {
		sb.WriteString("- If the request stays ambiguous in a multi-server setup, use `server_focus` instead of guessing.\n")
	}
	sb.WriteString("\n")
}

func appendErrorRecoveryProtocol(sb *strings.Builder) {
	sb.WriteString("## 에러 복구 프로토콜 (필수)\n")
	sb.WriteString("도구 호출이 실패하거나 shell_exec가 비정상 종료 코드를 반환하면 **즉시 재시도 금지**.\n")
	sb.WriteString("새 정보 없는 재시도는 턴을 낭비한다. 반드시 아래 순서를 따를 것:\n\n")
	sb.WriteString("1. **`rag_search` 호출** — 에러 메시지 또는 핵심 문구를 쿼리로 사용\n")
	sb.WriteString("   - 점수 ≥ 0.3 결과 있으면: 해결책 적용 후 재시도\n")
	sb.WriteString("   - 결과 없으면: 2번으로 진행\n")
	sb.WriteString("2. **`web_search` 호출** — rag_search 결과 없을 때만\n")
	sb.WriteString("   - 에러 코드/메시지 + 도구명 + OS 컨텍스트로 검색\n")
	sb.WriteString("   - `web_fetch`로 가장 관련성 높은 URL을 읽어 해결책 확인\n")
	sb.WriteString("3. 찾은 해결책을 적용하고 원래 도구를 재시도\n")
	sb.WriteString("4. **재시도 성공 후** `knowledge_add` 호출 — 다음 번에 즉시 찾을 수 있도록 저장\n\n")
	sb.WriteString("모든 도구에 적용 (shell_exec, oracle.query, mysql.query, file_write 등).\n")
	sb.WriteString("특수 케이스:\n")
	sb.WriteString("- `privilege authentication failed` / `sorry, try again`: 인용 방식(단일/이중 따옴표), `become_method` 대안, sudoers 설정 검색\n")
	sb.WriteString("- `command not found`: 대상 OS용 올바른 패키지명 / 설치 명령 검색\n")
	sb.WriteString("- `ORA-XXXXX` / `MySQL error XXXX`: 에러 코드 직접 검색\n\n")
}

func appendQwenToolGuideline(sb *strings.Builder) {
	sb.WriteString("## Tool Calling Format (Qwen Distilled Mode)\n")
	sb.WriteString("You are running in a special distilled mode. To use tools, you MUST output them in the following XML format directly in your response body:\n\n")
	sb.WriteString("<tool_call>\n")
	sb.WriteString("{\"name\": \"tool_name\", \"arguments\": {\"arg1\": \"value1\"}}\n")
	sb.WriteString("</tool_call>\n\n")
	sb.WriteString("Points to remember:\n")
	sb.WriteString("- Do not use standard markdown code blocks for tools.\n")
	sb.WriteString("- You can call multiple tools by repeating the <tool_call> block.\n")
	sb.WriteString("- Output your thought process (Thinking) first, then the tool call.\n\n")
}

// appendTaskCompletionRules는 작업 완료 루프 규칙을 프롬프트에 추가한다.
func appendTaskCompletionRules(sb *strings.Builder) {
	sb.WriteString("## Task Completion Rules (CRITICAL — 반드시 준수)\n")
	sb.WriteString("**작업이 완전히 끝나기 전에는 절대 최종 응답을 생성하지 않는다.**\n\n")

	sb.WriteString("### 1. 작업 완료 선언 프로토콜 (Mandatory)\n")
	sb.WriteString("- 모든 변경 작업(파일 수정, 서비스 재시작, 패키지 설치 등)을 마친 후에는 반드시 `verify_complete` 도구를 호출해야 한다.\n")
	sb.WriteString("- `verify_complete`를 호출하기 전에는 어떤 경우에도 \"작업을 완료했습니다\"라고 사용자에게 말하지 마시오.\n")
	sb.WriteString("- 시스템은 `verify_complete` 호출 시 제출된 증거(evidence)를 바탕으로 최종 종료 여부를 판단한다.\n\n")

	sb.WriteString("### 2. 파일 및 상태 확인 (중복 작업 방지)\n")
	sb.WriteString("- 작업을 시작하기 전, 그리고 각 단계 직후에 반드시 `shell_exec`로 현재 상태를 확인한다.\n")
	sb.WriteString("- 파일이 이미 존재하거나 서비스가 이미 원하는 상태이면 해당 단계를 건너뛴다.\n")
	sb.WriteString("- 히스토리만 믿지 말고 실제 시스템 상태를 명령어로 조회하여 확인하라.\n\n")

	sb.WriteString("### 3. 오류 발생 시 자율 복구\n")
	sb.WriteString("- 복구 가능한 오류(디렉토리 없음, 파일 없음, 명령 미설치 등) 발생 시 사용자에게 묻지 말고 즉시 수정 도구를 호출하여 해결한 뒤 작업을 계속하라.\n")
	sb.WriteString("- **절대로 중간에 멈추지 마시오.** 오류를 고치는 도구를 즉시 호출하라.\n\n")

	sb.WriteString("### 4. 다단계 작업의 완전성 (End-to-End)\n")
	sb.WriteString("사용자의 최종 목표가 달성되었음을 증명할 수 있을 때까지 루프를 멈추지 마시오.\n")
	sb.WriteString("예: 설치 작업 -> 서비스 시작 -> 포트 리스닝 확인 -> 웹 응답 확인(`curl`) 순서로 진행.\n")
	sb.WriteString("  Step 1) 사전 상태 확인\n")
	sb.WriteString("  Step 2) 변경 작업 실행\n")
	sb.WriteString("  Step 3) 결과 검증 명령어 실행 (필수)\n")
	sb.WriteString("  Step 4) `verify_complete` 호출\n")
	sb.WriteString("  Step 5) 최종 응답 생성\n\n")

	sb.WriteString("### 5. 자율 실행 및 일괄 보고\n")
	sb.WriteString("- 중간 단계마다 \"~를 했습니다. 계속할까요?\"라고 묻지 마시오. 목표 달성을 위해 필요한 일련의 과정을 멈춤 없이 수행하라.\n")
	sb.WriteString("- 같은 방법으로 3회 이상 실패하거나, 해결 불가능한 권한/설정 문제에 직면했을 때만 사용자에게 도움을 요청하라.\n\n")
}

func appendPhasePlanning(sb *strings.Builder) {
	sb.WriteString("## Phase Planning (Complex Tasks)\n")
	sb.WriteString("When a task requires 4 or more tool calls or involves multiple distinct steps:\n")
	sb.WriteString("1. First, outline a numbered Phase plan in your response before executing.\n")
	sb.WriteString("2. Format each phase as: \"Phase A: <short description>\", \"Phase B: <description>\", etc.\n")
	sb.WriteString("3. In each tool call's `description` field, prefix with the phase identifier.\n")
	sb.WriteString("   Example: description: \"Phase A: K8s Pod 상태 확인\"\n")
	sb.WriteString("4. This helps the user track progress on complex multi-step operations.\n")
	sb.WriteString("5. For simple tasks (1-3 tool calls), skip phase planning and execute directly.\n\n")
}
