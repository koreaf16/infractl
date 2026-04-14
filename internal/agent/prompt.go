// Package agent
// File: prompt.go
// Description: 에이전트 시스템 프롬프트 최상위 조립 함수와 INFRACTL.md 로더
// Responsibility: BuildContextual로 SectionSet 기반 동적 시스템 프롬프트를 조립하고 LoadInfractlMD로 사용자 지시를 로드

package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

// BuildContextual은 SectionSet에 포함된 섹션만 골라 시스템 프롬프트를 조립한다.
func BuildContextual(
	sections SectionSet,
	toolList []tools.Tool,
	infractlMD string,
	servers []store.Server,
	activeServer *store.Server,
	connectorStates []connector.ConnectorState,
	learnedSystems []store.LearnedSystem,
	ragSources []store.RAGSource,
	knowledgeStats *rag.KnowledgeStats,
	modelName string,
	knowledgeContext string, // 입력 기반 관련 지식 (비동기 프리페치 결과)
) string {
	var sb strings.Builder

	isQwen := strings.Contains(strings.ToLower(modelName), "qwen")

	// --- SectionCore: 항상 포함 ---
	sb.WriteString("You are infractl, an AI infrastructure management agent.\n")
	sb.WriteString("You help operators manage servers and infrastructure by executing shell commands and analyzing results.\n\n")

	// Qwen 전용 가이드라인 주입
	if isQwen {
		appendQwenToolGuideline(&sb)
	}

	now := time.Now()
	sb.WriteString(fmt.Sprintf("## Current Date & Time\n**%s** (UTC%s)\n\n",
		now.Format("2006-01-02 15:04:05 (Mon)"),
		now.Format("-07:00"),
	))

	// --- SectionEnvironment: 항상 포함 ---
	if activeServer != nil {
		appendActiveServerContext(&sb, activeServer)
	} else {
		hostname, _ := os.Hostname()
		cwd, _ := os.Getwd()
		appendLocalControllerContext(&sb, runtime.GOOS, runtime.GOARCH, hostname, cwd)
	}

	// --- SectionTools ---
	if sections.Has(SectionTools) && len(toolList) > 0 {
		sb.WriteString("## Available Tools\n")
		sb.WriteString("You can use the following tools by following the Tool Calling Format above:\n\n")
		for _, t := range toolList {
			risk := string(t.RiskLevel())
			if risk != "" && risk != "none" {
				sb.WriteString(fmt.Sprintf("- **%s** [risk:%s]: %s\n", t.Name(), risk, t.Description()))
			} else {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.Name(), t.Description()))
			}
			// Qwen 모드일 경우 툴의 상세 스키마를 프롬프트에 직접 노출
			if isQwen {
				schemaBytes, _ := json.Marshal(t.Parameters())
				sb.WriteString(fmt.Sprintf("  Args JSON Schema: %s\n", string(schemaBytes)))
			}
		}
		sb.WriteString("\n")
	}

	// --- SectionServers ---
	if sections.Has(SectionServers) && len(servers) > 0 {
		sb.WriteString("## Known Servers Pool\n")
		for _, srv := range servers {
			sb.WriteString(fmt.Sprintf("- **%s** (%s@%s:%d)\n", srv.Name, srv.User, srv.Host, srv.Port))
		}
		sb.WriteString("\n")

		if activeServer == nil {
			sb.WriteString("When executing commands on a specific server, include `\"target\": \"<server_name>\"` in tool arguments.\n")
			sb.WriteString("When no server is specified, omit target or use \"localhost\".\n")
			sb.WriteString("Do not invent remote-only constraints when target is omitted.\n\n")
		}
	}

	// --- SectionGuardrails ---
	if sections.Has(SectionGuardrails) {
		appendContextGuardrails(&sb, activeServer == nil)
	}

	// --- SectionServerFocus ---
	if sections.Has(SectionServerFocus) {
		appendServerFocusSection(&sb, activeServer, servers)
	}

	// --- SectionRAG ---
	if sections.Has(SectionRAG) {
		// 입력과 관련된 지식이 프리페치된 경우 먼저 인라인으로 주입한다.
		// LLM이 rag_search를 호출하기 전에 이미 관련 내용을 파악할 수 있게 된다.
		if knowledgeContext != "" {
			sb.WriteString(knowledgeContext)
			sb.WriteString("\n\n")
		}
		appendRAGSection(&sb, ragSources, knowledgeStats)
	}

	// --- SectionLearnedSystems ---
	if sections.Has(SectionLearnedSystems) && len(learnedSystems) > 0 {
		appendLearnedSystemsSection(&sb, learnedSystems)
	}

	// --- SectionConnectors ---
	if sections.Has(SectionConnectors) && len(connectorStates) > 0 {
		appendConnectorsSection(&sb, connectorStates)
	}

	// --- 의도 기반 섹션들 ---
	if sections.Has(SectionToolPriority) {
		appendDedicatedToolPriority(&sb)
	}

	if sections.Has(SectionToolSelection) {
		appendToolSelectionGuidelines(&sb)
	}

	if sections.Has(SectionErrorRecovery) {
		appendErrorRecoveryProtocol(&sb)
	}

	if sections.Has(SectionDiscovery) {
		appendDiscoveryFlow(&sb)
	}

	if sections.Has(SectionTaskCompletion) {
		appendTaskCompletionRules(&sb)
	}

	if sections.Has(SectionSafety) {
		appendSafetyRules(&sb)
	}

	if sections.Has(SectionGrounding) {
		appendGroundingRules(&sb)
	}

	// --- SectionBehavior ---
	if sections.Has(SectionBehavior) {
		appendBehaviorRules(&sb)
	}

	if sections.Has(SectionPhasePlanning) {
		appendPhasePlanning(&sb)
	}

	// --- SectionInfractlMD ---
	if sections.Has(SectionInfractlMD) && infractlMD != "" {
		sb.WriteString("## Project-Specific Instructions\n")
		sb.WriteString(infractlMD)
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}

// appendActiveServerContext는 액티브 서버 컨텍스트를 프롬프트에 추가한다.
func appendActiveServerContext(sb *strings.Builder, srv *store.Server) {
	sb.WriteString("## [ACTIVE SESSION CONTEXT]\n")
	sb.WriteString(fmt.Sprintf("You are currently focused on a specific target server: **%s**.\n", srv.Name))
	sb.WriteString("Do NOT require the user to explicitly specify the server name. Operate on this server by default unless directed otherwise.\n")
	sb.WriteString("When an active server is set, do not refer to it as \"localhost\" or \"local server\".\n")
	sb.WriteString(fmt.Sprintf("- OS/Distribution: %s\n", srv.OS))
	if srv.EnvProfile != "" {
		sb.WriteString(fmt.Sprintf("- Detected Environment: %s\n", srv.EnvProfile))
	}
	sb.WriteString("\n**Primary Command Guidelines For This System:**\n")
	osLow := strings.ToLower(srv.OS)
	switch {
	case strings.Contains(osLow, "windows"):
		sb.WriteString("- This is a Windows system. You MUST use PowerShell cmdlets or traditional command prompt syntax.\n")
	case strings.Contains(osLow, "ubuntu"), strings.Contains(osLow, "debian"):
		sb.WriteString("- This is a Debian-based Linux system. Prefer `apt`, `systemctl`, `journalctl`, and standard bash utilities.\n")
	case strings.Contains(osLow, "centos"), strings.Contains(osLow, "redhat"), strings.Contains(osLow, "linux"):
		sb.WriteString("- This is a Linux system. Use standard bash utilities, grep, awk, systemctl, etc.\n")
	}
	sb.WriteString("\n")

	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()
	sb.WriteString("## Local Controller (Always Available)\n")
	sb.WriteString(fmt.Sprintf("- Local OS: %s (%s)\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("- Local Hostname: %s\n", hostname))
	sb.WriteString(fmt.Sprintf("- Local Working Directory: %s\n", cwd))
	switch runtime.GOOS {
	case "windows":
		sb.WriteString("- Local Shell: PowerShell\n")
	default:
		sb.WriteString("- Local Shell: bash\n")
	}
	sb.WriteString("**You can ALWAYS execute commands on this local machine by specifying `\"target\": \"localhost\"` in tool arguments.**\n")
	sb.WriteString("Use `target: \"localhost\"` when the user asks about local paths, local files, or commands that must run on this PC.\n")
	sb.WriteString("NEVER tell the user that local execution is impossible. Local execution is always available.\n\n")
}

// appendServerFocusSection은 서버 포커스 규칙 섹션을 추가한다.
func appendServerFocusSection(sb *strings.Builder, activeServer *store.Server, servers []store.Server) {
	if activeServer != nil {
		return // Already covered by [ACTIVE SESSION CONTEXT]
	}
	sb.WriteString("## Active Server Focus\n")
	switch {
	case len(servers) == 1:
		sb.WriteString(fmt.Sprintf("Only one server registered: **%s**. Call `server_focus` to set it as active.\n", servers[0].Name))
	case len(servers) > 1:
		sb.WriteString("Multiple servers registered. Rules:\n")
		sb.WriteString("- If the user explicitly names a server, call `server_focus server=<name>`.\n")
		sb.WriteString("- If you are unsure which server to use, call `server_focus` with no args to let the user choose.\n")
		sb.WriteString("- Once an active server is set, omit the target parameter in tool calls unless the user overrides it.\n")
	default:
		sb.WriteString("No active server is set. Localhost is the default target.\n")
	}
	sb.WriteString("\n")
}

// appendLearnedSystemsSection은 학습된 시스템 목록 섹션을 추가한다.
func appendLearnedSystemsSection(sb *strings.Builder, learnedSystems []store.LearnedSystem) {
	sb.WriteString("## Previously Learned Systems\n")
	sb.WriteString("The following systems were previously discovered via adaptive learning:\n")
	for _, sys := range learnedSystems {
		sb.WriteString(fmt.Sprintf("- **%s** on `%s`", sys.ServiceType, sys.ServerName))
		if sys.CLIPath != "" {
			sb.WriteString(fmt.Sprintf(" | CLI: `%s`", sys.CLIPath))
		}
		if sys.ConfigPath != "" {
			sb.WriteString(fmt.Sprintf(" | Config: `%s`", sys.ConfigPath))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Use these paths directly instead of searching again.\n\n")
}

// appendConnectorsSection은 활성 커넥터 목록 섹션을 추가한다.
func appendConnectorsSection(sb *strings.Builder, connectorStates []connector.ConnectorState) {
	sb.WriteString("## Active Connectors\n")
	for _, cs := range connectorStates {
		icon := connectorIcon(string(cs.Status))
		sb.WriteString(fmt.Sprintf("%s %s/%s/%s -> %s (%d tools)\n",
			icon, cs.ServerName, cs.Type, cs.ServiceName, cs.Status, len(cs.Tools)))
	}
	sb.WriteString("\nConnected connectors have dedicated tools active (for example, oracle_ORCL.tablespace).\n")
	sb.WriteString("Use connector-specific tools when available instead of raw shell_exec.\n\n")
}

// appendBehaviorRules는 기본 동작 규칙 섹션을 추가한다.
func appendBehaviorRules(sb *strings.Builder) {
	sb.WriteString("## Behavior Rules\n")
	sb.WriteString("1. Present results in a clean, readable format.\n")
	sb.WriteString("2. Always explain what you are doing and why.\n")
	sb.WriteString("3. If a tool fails, analyze the error output and try an alternative command.\n")
	sb.WriteString("4. Distinguish \"not found by the current pattern\" from \"does not exist\" in summaries.\n")
	sb.WriteString("5. You must converse in the language the user is speaking.\n")
	sb.WriteString("6. **Execute first, explain after.** Call tool(s) BEFORE writing explanatory text. Keep pre-tool text to one short sentence.\n")
	sb.WriteString("7. After a tool failure, immediately retry with an alternative approach. Write at most one sentence before the next tool call.\n\n")
}

// LoadInfractlMD는 ~/.infractl/INFRACTL.md 파일을 로드한다.
func LoadInfractlMD() string {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("get home dir for INFRACTL.md", "err", err)
		return ""
	}

	path := filepath.Join(home, ".infractl", "INFRACTL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	slog.Debug("loaded INFRACTL.md", "path", path)
	baseDir := filepath.Dir(path)
	return processIncludes(string(data), baseDir, 0)
}

// BuildMinimalChat은 needs_tools=false(순수 대화)일 때 사용하는 최소 시스템 프롬프트를 반환한다.
// 인프라 컨텍스트와 도구 목록을 일절 포함하지 않아 페이로드를 최소화하되,
// 사용자 언어 감지와 에이전트 identity는 유지한다.
func BuildMinimalChat(infractlMD string) string {
	now := time.Now()
	var sb strings.Builder
	sb.WriteString("You are infractl, an AI infrastructure management agent.\n")
	sb.WriteString(fmt.Sprintf("Current time: %s (UTC%s).\n\n",
		now.Format("2006-01-02 15:04:05"),
		now.Format("-07:00"),
	))
	sb.WriteString("Rules:\n")
	sb.WriteString("- You must converse in the language the user is speaking.\n")
	sb.WriteString("- Keep responses concise and friendly.\n")
	sb.WriteString("- If the user needs infrastructure help (server commands, logs, system info), let them know you can assist.\n")
	sb.WriteString("- Do NOT call any tools for greetings, thanks, or casual conversation.\n")
	if infractlMD != "" {
		sb.WriteString("\n## Project-Specific Instructions\n")
		sb.WriteString(infractlMD)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func processIncludes(content string, baseDir string, depth int) string {
	if depth >= 5 {
		return content
	}

	var result strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@include ") {
			includePath := strings.TrimSpace(trimmed[len("@include "):])
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(baseDir, includePath)
			}
			data, err := os.ReadFile(includePath)
			if err != nil {
				slog.Debug("@include file not found", "path", includePath, "err", err)
				result.WriteString(line + "\n")
				continue
			}
			slog.Debug("@include processed", "path", includePath)
			includeBaseDir := filepath.Dir(includePath)
			included := processIncludes(string(data), includeBaseDir, depth+1)
			result.WriteString(included)
			if !strings.HasSuffix(included, "\n") {
				result.WriteString("\n")
			}
			continue
		}
		result.WriteString(line + "\n")
	}
	return strings.TrimSuffix(result.String(), "\n")
}
