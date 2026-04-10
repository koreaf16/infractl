package agent

import (
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

func BuildContextual(
	toolList []tools.Tool,
	infractlMD string,
	servers []store.Server,
	activeServer *store.Server,
	connectorStates []connector.ConnectorState,
	learnedSystems []store.LearnedSystem,
	ragSources []store.RAGSource,
	knowledgeStats *rag.KnowledgeStats,
) string {
	var sb strings.Builder

	sb.WriteString("You are infractl, an AI infrastructure management agent.\n")
	sb.WriteString("You help operators manage servers and infrastructure by executing shell commands and analyzing results.\n\n")

	now := time.Now()
	sb.WriteString(fmt.Sprintf("## Current Date & Time\n**%s** (UTC%s)\n\n",
		now.Format("2006-01-02 15:04:05 (Mon)"),
		now.Format("-07:00"),
	))

	if activeServer != nil {
		sb.WriteString("## [ACTIVE SESSION CONTEXT]\n")
		sb.WriteString(fmt.Sprintf("You are currently focused on a specific target server: **%s**.\n", activeServer.Name))
		sb.WriteString("Do NOT require the user to explicitly specify the server name. Operate on this server by default unless directed otherwise.\n")
		sb.WriteString("When an active server is set, do not refer to it as \"localhost\" or \"local server\".\n")
		sb.WriteString(fmt.Sprintf("- OS/Distribution: %s\n", activeServer.OS))
		if activeServer.EnvProfile != "" {
			sb.WriteString(fmt.Sprintf("- Detected Environment: %s\n", activeServer.EnvProfile))
		}
		sb.WriteString("\n**Primary Command Guidelines For This System:**\n")
		osLow := strings.ToLower(activeServer.OS)
		switch {
		case strings.Contains(osLow, "windows"):
			sb.WriteString("- This is a Windows system. You MUST use PowerShell cmdlets or traditional command prompt syntax.\n")
		case strings.Contains(osLow, "ubuntu"), strings.Contains(osLow, "debian"):
			sb.WriteString("- This is a Debian-based Linux system. Prefer `apt`, `systemctl`, `journalctl`, and standard bash utilities.\n")
		case strings.Contains(osLow, "centos"), strings.Contains(osLow, "redhat"), strings.Contains(osLow, "linux"):
			sb.WriteString("- This is a Linux system. Use standard bash utilities, grep, awk, systemctl, etc.\n")
		}
		sb.WriteString("\n")
	} else {
		hostname, _ := os.Hostname()
		cwd, _ := os.Getwd()
		appendLocalControllerContext(&sb, runtime.GOOS, runtime.GOARCH, hostname, cwd)
	}

	if len(toolList) > 0 {
		sb.WriteString("## Available Tools\n")
		for _, t := range toolList {
			risk := string(t.RiskLevel())
			if risk != "" && risk != "none" {
				sb.WriteString(fmt.Sprintf("- **%s** [risk:%s]: %s\n", t.Name(), risk, t.Description()))
			} else {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.Name(), t.Description()))
			}
		}
		sb.WriteString("\n")
	}

	if len(servers) > 0 {
		sb.WriteString("## Known Servers Pool\n")
		for _, srv := range servers {
			sb.WriteString(fmt.Sprintf("- **%s** (%s@%s:%d)\n", srv.Name, srv.User, srv.Host, srv.Port))
		}
		sb.WriteString("\n")
	}

	if activeServer == nil {
		sb.WriteString("When executing commands on a specific server, include `\"target\": \"<server_name>\"` in tool arguments.\n")
		sb.WriteString("When no server is specified, omit target or use \"localhost\".\n")
		sb.WriteString("Do not invent remote-only constraints when target is omitted.\n\n")
	}

	appendContextGuardrails(&sb, activeServer == nil)

	sb.WriteString("## Active Server Focus\n")
	switch {
	case activeServer != nil:
		sb.WriteString(fmt.Sprintf("Active server is **%s** (%s:%d). All commands default to this server unless user specifies otherwise.\n", activeServer.Name, activeServer.Host, activeServer.Port))
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

	appendRAGSection(&sb, ragSources, knowledgeStats)

	if len(learnedSystems) > 0 {
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

	if len(connectorStates) > 0 {
		sb.WriteString("## Active Connectors\n")
		for _, cs := range connectorStates {
			icon := connectorIcon(string(cs.Status))
			sb.WriteString(fmt.Sprintf("%s %s/%s/%s -> %s (%d tools)\n",
				icon, cs.ServerName, cs.Type, cs.ServiceName, cs.Status, len(cs.Tools)))
		}
		sb.WriteString("\nConnected connectors have dedicated tools active (for example, oracle_ORCL.tablespace).\n")
		sb.WriteString("Use connector-specific tools when available instead of raw shell_exec.\n\n")
	}

	sb.WriteString("## Tool Selection Guidelines\n")
	sb.WriteString("Follow this decision priority when choosing tools:\n\n")
	sb.WriteString("**Information Gathering**\n")
	sb.WriteString("1. If you already know the answer with high confidence, respond directly.\n")
	sb.WriteString("2. If the user is asking which machine, shell, or default target is in effect, call `session_context` first.\n")
	sb.WriteString("3. If you need server-specific data, use `shell_exec` on the correct target.\n")
	sb.WriteString("4. For troubleshooting or error resolution, use `rag_search` first.\n")
	sb.WriteString("5. If `rag_search` has no results and internet is available, use `web_search`.\n")
	sb.WriteString("6. If you need detailed content from a specific URL, use `web_fetch` after `web_search`.\n\n")

	sb.WriteString("**Error Resolution Flow**\n")
	sb.WriteString("1. Analyze the error message yourself first.\n")
	sb.WriteString("2. Use `rag_search` for similar error patterns.\n")
	sb.WriteString("3. If there is no match, use `web_search` for the error code or message.\n")
	sb.WriteString("4. If the fix works, suggest saving it to the knowledge base.\n\n")

	sb.WriteString("**Service Discovery Flow**\n")
	sb.WriteString("1. Call `discover_services` to scan processes, ports, and config files.\n")
	sb.WriteString("2. If a known database service is found, call `connector_probe_os_auth` first.\n")
	sb.WriteString("3. If confidence is only medium, ask the user to confirm service type before activating.\n")
	sb.WriteString("4. Once a connector is active, use connector-specific tools instead of raw shell_exec.\n\n")

	sb.WriteString("**Adaptive Service Learning**\n")
	sb.WriteString("1. First check the learned systems section above.\n")
	sb.WriteString("2. If not found, use `shell_exec` to locate binaries.\n")
	sb.WriteString("3. If still not found and internet is available, use `web_search` for setup or commands.\n")
	sb.WriteString("4. After discovery, call `save_learned_system` to persist the result.\n\n")

	sb.WriteString("**Multi-Server Operations**\n")
	sb.WriteString("- Read-only queries can execute on multiple targets.\n")
	sb.WriteString("- Mutation commands must execute on one target at a time, with confirmation when required.\n\n")

	appendSafetyRules(&sb)

	sb.WriteString("## Behavior Rules\n")
	sb.WriteString("1. Present results in a clean, readable format.\n")
	sb.WriteString("2. Always explain what you are doing and why.\n")
	sb.WriteString("3. If a tool fails, analyze the error output and try an alternative command.\n")
	sb.WriteString("4. Distinguish \"not found by the current pattern\" from \"does not exist\" in summaries.\n")
	sb.WriteString("5. You must converse in the language the user is speaking.\n\n")

	if infractlMD != "" {
		sb.WriteString("## Project-Specific Instructions\n")
		sb.WriteString(infractlMD)
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
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
	sb.WriteString("- Treat `this PC`, `local`, `the PC running infractl`, and Windows drive paths like `C:\\` as the local controller unless the user explicitly names a server.\n")
	sb.WriteString("- Do not assume a remote SSH session when there is no active server and no explicit target.\n")
	sb.WriteString("- Before claiming access is impossible or before asserting local vs remote context, call `session_context` or run a read-only probe first.\n")
	sb.WriteString("- When the user is asking about execution context, machine identity, or shell choice, prefer `session_context` before `shell_exec`.\n")
	if noActiveServer {
		sb.WriteString("- If the request stays ambiguous in a multi-server setup, use `server_focus` instead of guessing.\n")
	}
	sb.WriteString("\n")
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
	sb.WriteString("8. Learned mutation commands must carry backup instructions.\n\n")
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
