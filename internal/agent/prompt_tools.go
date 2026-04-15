// Package agent
// File: prompt_tools.go
// Description: ?꾧뎄 ?곗꽑?쒖쐞, ?덉쟾 洹쒖튃, RAG ?뱀뀡 ?쒖뒪???꾨＼?꾪듃 援ъ꽦 ?⑥닔
// Responsibility: ?꾩슜 ?꾧뎄 ?곗꽑 ?ъ슜 洹쒖튃, ?덉쟾 洹쒖튃, 吏??寃???곗꽑?쒖쐞瑜??꾨＼?꾪듃??異붽?

package agent

import (
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
)

// appendToolSelectionGuidelines???꾧뎄 ?좏깮 媛?대뱶?쇱씤 ?뱀뀡??異붽??쒕떎.
func appendToolSelectionGuidelines(sb *strings.Builder) {
	sb.WriteString("## Tool Selection Guidelines\n")
	sb.WriteString("Follow this decision priority when choosing tools:\n\n")
	sb.WriteString("**Information Gathering & Parallel Execution**\n")
	sb.WriteString("1. If you already know the answer with high confidence, respond directly.\n")
	sb.WriteString("2. If the user is asking which machine, shell, or default target is in effect, call `session_context` first.\n")
	sb.WriteString("3. **Parallel Execution (Efficiency):** For independent information gathering (e.g., checking multiple servers or multiple metrics), generate multiple tool calls in a single response. These will execute in parallel.\n")
	sb.WriteString("4. **Parallel Execution:** Read-only tools can execute in parallel. Mutation commands execute sequentially to ensure state consistency.\n")
	sb.WriteString("5. If you need server-specific data, use the appropriate dedicated tool on the correct target.\n")
	sb.WriteString("6. For troubleshooting or error resolution, use `rag_search` first.\n")
	sb.WriteString("7. If `rag_search` has no results and internet is available, use `web_search`.\n")
	sb.WriteString("8. If you need detailed content from a specific URL, use `web_fetch` after `web_search`.\n")
	sb.WriteString("9. **Target Ambiguity Check (CRITICAL):** Before any command execution, if the user references a server, DB, or instance name:\n")
	sb.WriteString("   - Check if it matches any entry in the Known Servers Pool or the active server.\n")
	sb.WriteString("   - If NO match is found, call `ask_user_question` immediately ??do NOT guess or execute blindly.\n")
	sb.WriteString("   - If the name partially matches or seems like an alias (e.g. '26AI DB' vs 'oracle-db'), still ask to confirm before proceeding.\n\n")
	sb.WriteString("10. When calling `ask_user_question`, ask exactly ONE concrete question.\n")
	sb.WriteString("   - `options` must be mutually exclusive answer candidates, not a checklist of things to verify.\n")
	sb.WriteString("   - If the answer is open-ended, omit `options` and ask for free-text input instead.\n\n")

	sb.WriteString("**Adaptive Service Learning**\n")
	sb.WriteString("1. First check the learned systems section above. If a matching entry exists, use the stored path directly ??do NOT search again.\n")
	sb.WriteString("2. If not found, use `shell_exec` with `find` to locate binaries ??do this ONCE only.\n")
	sb.WriteString("3. If still not found and internet is available, use `web_search` for setup or commands.\n")
	sb.WriteString("4. **After finding any binary path, call `save_learned_system` IMMEDIATELY** to persist it for future use.\n")
	sb.WriteString("5. When a binary is found but ORACLE_HOME or similar env is not set, also read the user's profile file once to extract the correct env and set it inline before executing.\n\n")

	sb.WriteString("**Multi-Server Operations**\n")
	sb.WriteString("- Read-only queries can execute on multiple targets.\n")
	sb.WriteString("- Mutation commands must execute on one target at a time.\n\n")
}

// appendDiscoveryFlow???쒕퉬???붿뒪而ㅻ쾭由??뚮줈???뱀뀡??異붽??쒕떎.
func appendDiscoveryFlow(sb *strings.Builder) {
	sb.WriteString("**Service Discovery Flow**\n")
	sb.WriteString("1. Call `discover_services` to scan processes, ports, and config files.\n")
	sb.WriteString("2. If a known database service is found, call `connector_probe_os_auth` first.\n")
	sb.WriteString("3. Once a connector is active, use connector-specific tools instead of raw shell_exec.\n\n")

	sb.WriteString("**Oracle PDB Connection (CRITICAL)**\n")
	sb.WriteString("To connect to an Oracle PDB (Pluggable Database), you MUST use `connector_activate` with `sub_instance=<PDB_NAME>`.\n")
	sb.WriteString("- CORRECT: call `connector_activate` with `service_name=<CDB_SID>` and `sub_instance=<PDB_NAME>`.\n")
	sb.WriteString("  This creates a dedicated connector (e.g., `oracle_ai26_ai_db.query`) that connects directly to the PDB service.\n")
	sb.WriteString("- WRONG: running `ALTER SESSION SET CONTAINER = <PDB_NAME>` via query tool.\n")
	sb.WriteString("  Each query tool call spawns a new sqlplus process — session state does NOT persist between calls.\n")
	sb.WriteString("  `ALTER SESSION SET CONTAINER` is lost the moment that sqlplus process exits.\n")
	sb.WriteString("- After `connector_activate` with sub_instance succeeds, use the new PDB-specific tools (e.g., `oracle_<sid>_<pdb>.query`).\n\n")
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
	sb.WriteString("| `file_transfer` | scp, sftp ??file upload/download between local and remote server |\n\n")
	sb.WriteString("Use `shell_exec` ONLY when no dedicated tool covers the operation.\n\n")
}

func appendSafetyRules(sb *strings.Builder) {
	sb.WriteString("## Safety Rules\n")
	sb.WriteString("1. Avoid heredoc patterns like `<<EOF` for non-interactive edits.\n")
	sb.WriteString("2. Prefer `printf` or `tee` one-liners for deterministic file writes.\n")
	sb.WriteString("3. When writing shell exports, escape `$` so variable references are written literally.\n")
	sb.WriteString("4. Prefer non-interactive privilege patterns such as `sudo -n`, `runuser -l`, and `bash -lc`.\n\n")
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
	sb.WriteString("## ?먮윭 蹂듦뎄 ?꾨줈?좎퐳 (?꾩닔)\n")
	sb.WriteString("?꾧뎄 ?몄텧???ㅽ뙣?섍굅??shell_exec媛 鍮꾩젙??醫낅즺 肄붾뱶瑜?諛섑솚?섎㈃ **利됱떆 ?ъ떆??湲덉?**.\n")
	sb.WriteString("???뺣낫 ?녿뒗 ?ъ떆?꾨뒗 ?댁쓣 ??퉬?쒕떎. 諛섎뱶???꾨옒 ?쒖꽌瑜??곕? 寃?\n\n")
	sb.WriteString("**[?듭떖 ?먯튃] ?숈씪???묎렐 諛⑹떇?쇰줈 3???댁긽 ?ㅽ뙣?섎㈃ 利됱떆 以묐떒?섍퀬 ?ъ슜?먯뿉寃??곹솴???ㅻ챸?섎씪.**\n\n")
	sb.WriteString("1. **`rag_search` ?몄텧** ???먮윭 硫붿떆吏 ?먮뒗 ?듭떖 臾멸뎄瑜?荑쇰━濡??ъ슜\n")
	sb.WriteString("   - ?먯닔 ??0.3 寃곌낵 ?덉쑝硫? ?닿껐梨??곸슜 ???ъ떆??n")
	sb.WriteString("   - 寃곌낵 ?놁쑝硫? 2踰덉쑝濡?吏꾪뻾\n")
	sb.WriteString("2. **`web_search` ?몄텧** ??rag_search 寃곌낵 ?놁쓣 ?뚮쭔\n")
	sb.WriteString("   - ?먮윭 肄붾뱶/硫붿떆吏 + ?꾧뎄紐?+ OS 而⑦뀓?ㅽ듃濡?寃??n")
	sb.WriteString("   - `web_fetch`濡?媛??愿?⑥꽦 ?믪? URL???쎌뼱 ?닿껐梨??뺤씤\n")
	sb.WriteString("3. 李얠? ?닿껐梨낆쓣 ?곸슜?섍퀬 ?먮옒 ?꾧뎄瑜??ъ떆??n")
	sb.WriteString("4. **?ъ떆???깃났 ??* `knowledge_add` ?몄텧 ???ㅼ쓬 踰덉뿉 利됱떆 李얠쓣 ???덈룄濡????n")
	sb.WriteString("5. ?닿껐梨낆쓣 李얠? 紐삵뻽嫄곕굹 3???쒕룄 ?꾩뿉???숈씪 ?먮윭 諛섎났 ?? ?ъ슜?먯뿉寃??꾩옱源뚯???吏꾨떒 寃곌낵? ?꾩슂???뺣낫瑜?紐낇솗???ㅻ챸?섍퀬 以묐떒\n\n")
	sb.WriteString("紐⑤뱺 ?꾧뎄???곸슜 (shell_exec, oracle.query, mysql.query, file_write ??.\n")
	sb.WriteString("?뱀닔 耳?댁뒪:\n")
	sb.WriteString("- `privilege authentication failed` / `sorry, try again`: ?몄슜 諛⑹떇(?⑥씪/?댁쨷 ?곗샂??, `become_method` ??? sudoers ?ㅼ젙 寃??n")
	sb.WriteString("- `command not found` / sqlplus 誘몃컻寃? `find` 濡?諛붿씠?덈━ ?꾩튂 ?뺤씤 1?뚮쭔 ?섑뻾. 諛쒓껄 ??ORACLE_HOME???ㅼ젙?섏뿬 ?ъ떆?? 誘몃컻寃????ъ슜?먯뿉寃?蹂닿퀬\n")
	sb.WriteString("- `ORACLE_HOME not set` / `SP2-0750`: `cat ~/.bash_profile ~/.bashrc ~/.profile` 濡??섍꼍蹂???ㅼ젙 ?뚯씪 1???뺤씤 ??寃쎈줈瑜?吏곸젒 吏?뺥븯???ъ떆??n")
	sb.WriteString("- `ORA-XXXXX` / `MySQL error XXXX`: ?먮윭 肄붾뱶 吏곸젒 寃??n\n")
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
	sb.WriteString("- Output your thought process (Thinking) first, then the tool call.\n")
	sb.WriteString("- After the system executes your tool, it will reply with a <tool_response> block.\n")
	sb.WriteString("- You MUST analyze the <tool_response> and provide a final text response to the user in Korean.\n")
	sb.WriteString("- Do NOT output empty responses.\n\n")
}

// appendTaskCompletionRules???묒뾽 ?꾨즺 猷⑦봽 洹쒖튃???꾨＼?꾪듃??異붽??쒕떎.
func appendTaskCompletionRules(sb *strings.Builder) {
	sb.WriteString("## Task Completion Rules (CRITICAL ??諛섎뱶??以??\n")
	sb.WriteString("**?묒뾽???꾩쟾???앸굹湲??꾩뿉???덈? 理쒖쥌 ?묐떟???앹꽦?섏? ?딅뒗??**\n\n")

	sb.WriteString("### 1. ?묒뾽 ?꾨즺 ?좎뼵 ?꾨줈?좎퐳 (Mandatory)\n")
	sb.WriteString("- 紐⑤뱺 蹂寃??묒뾽(?뚯씪 ?섏젙, ?쒕퉬???ъ떆?? ?⑦궎吏 ?ㅼ튂 ????留덉튇 ?꾩뿉??諛섎뱶??`verify_complete` ?꾧뎄瑜??몄텧?댁빞 ?쒕떎.\n")
	sb.WriteString("- `verify_complete`瑜??몄텧?섍린 ?꾩뿉???대뼡 寃쎌슦?먮룄 \"?묒뾽???꾨즺?덉뒿?덈떎\"?쇨퀬 ?ъ슜?먯뿉寃?留먰븯吏 留덉떆??\n")
	sb.WriteString("- ?쒖뒪?쒖? `verify_complete` ?몄텧 ???쒖텧??利앷굅(evidence)瑜?諛뷀깢?쇰줈 理쒖쥌 醫낅즺 ?щ?瑜??먮떒?쒕떎.\n\n")

	sb.WriteString("### 2. ?뚯씪 諛??곹깭 ?뺤씤 (以묐났 ?묒뾽 諛⑹?)\n")
	sb.WriteString("- ?묒뾽???쒖옉?섍린 ?? 洹몃━怨?媛??④퀎 吏곹썑??諛섎뱶??`shell_exec`濡??꾩옱 ?곹깭瑜??뺤씤?쒕떎.\n")
	sb.WriteString("- ?뚯씪???대? 議댁옱?섍굅???쒕퉬?ㅺ? ?대? ?먰븯???곹깭?대㈃ ?대떦 ?④퀎瑜?嫄대꼫?대떎.\n")
	sb.WriteString("- ?덉뒪?좊━留?誘우? 留먭퀬 ?ㅼ젣 ?쒖뒪???곹깭瑜?紐낅졊?대줈 議고쉶?섏뿬 ?뺤씤?섎씪.\n\n")

	sb.WriteString("### 3. ?ㅻ쪟 諛쒖깮 ???먯쑉 蹂듦뎄\n")
	sb.WriteString("- 蹂듦뎄 媛?ν븳 ?ㅻ쪟(?붾젆?좊━ ?놁쓬, ?뚯씪 ?놁쓬, 紐낅졊 誘몄꽕移??? 諛쒖깮 ???ъ슜?먯뿉寃?臾살? 留먭퀬 利됱떆 ?섏젙 ?꾧뎄瑜??몄텧?섏뿬 ?닿껐?????묒뾽??怨꾩냽?섎씪.\n")
	sb.WriteString("- **?? ?숈씪???묎렐 諛⑹떇?쇰줈 3???댁긽 ?ㅽ뙣?섎㈃ 諛섎뱶??硫덉텛怨??ъ슜?먯뿉寃??꾩옱 ?곹솴怨??꾩슂???뺣낫瑜??ㅻ챸?섎씪.**\n")
	sb.WriteString("- ?덈줈???뺣낫 ?놁씠 媛숈? 紐낅졊??蹂?뺣쭔 ?섎뒗 諛섎났 ?쒕룄??利됱떆 以묐떒?쒕떎.\n\n")

	sb.WriteString("### 4. ?ㅻ떒怨??묒뾽???꾩쟾??(End-to-End)\n")
	sb.WriteString("?ъ슜?먯쓽 理쒖쥌 紐⑺몴媛 ?ъ꽦?섏뿀?뚯쓣 利앸챸?????덉쓣 ?뚭퉴吏 猷⑦봽瑜?硫덉텛吏 留덉떆??\n")
	sb.WriteString("?? ?ㅼ튂 ?묒뾽 -> ?쒕퉬???쒖옉 -> ?ы듃 由ъ뒪???뺤씤 -> ???묐떟 ?뺤씤(`curl`) ?쒖꽌濡?吏꾪뻾.\n")
	sb.WriteString("  Step 1) ?ъ쟾 ?곹깭 ?뺤씤\n")
	sb.WriteString("  Step 2) 蹂寃??묒뾽 ?ㅽ뻾\n")
	sb.WriteString("  Step 3) 寃곌낵 寃利?紐낅졊???ㅽ뻾 (?꾩닔)\n")
	sb.WriteString("  Step 4) `verify_complete` ?몄텧\n")
	sb.WriteString("  Step 5) 理쒖쥌 ?묐떟 ?앹꽦\n\n")

	sb.WriteString("### 5. ?먯쑉 ?ㅽ뻾 諛??쇨큵 蹂닿퀬\n")
	sb.WriteString("- 以묎컙 ?④퀎留덈떎 \"~瑜??덉뒿?덈떎. 怨꾩냽?좉퉴??\"?쇨퀬 臾살? 留덉떆?? 紐⑺몴 ?ъ꽦???꾪빐 ?꾩슂???쇰젴??怨쇱젙??硫덉땄 ?놁씠 ?섑뻾?섎씪.\n")
	sb.WriteString("- 媛숈? 諛⑸쾿?쇰줈 3???댁긽 ?ㅽ뙣?섍굅?? ?닿껐 遺덇??ν븳 沅뚰븳/?ㅼ젙 臾몄젣??吏곷㈃?덉쓣 ?뚮쭔 ?ъ슜?먯뿉寃??꾩????붿껌?섎씪.\n\n")
}

func appendPhasePlanning(sb *strings.Builder) {
	sb.WriteString("## Phase Planning (Complex Tasks)\n")
	sb.WriteString("When a task requires 4 or more tool calls or involves multiple distinct steps:\n")
	sb.WriteString("1. First, outline a numbered Phase plan in your response before executing.\n")
	sb.WriteString("2. Format each phase as: \"Phase A: <short description>\", \"Phase B: <description>\", etc.\n")
	sb.WriteString("3. In each tool call's `description` field, prefix with the phase identifier.\n")
	sb.WriteString("   Example: description: \"Phase A: K8s Pod ?곹깭 ?뺤씤\"\n")
	sb.WriteString("4. This helps the user track progress on complex multi-step operations.\n")
	sb.WriteString("5. Even for simple tasks (1-3 tool calls), you MUST provide a Korean `description` for every tool call explaining why you are performing that action.\n\n")
}
