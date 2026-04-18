// Package agent
// File: loop.go
// Description: 에이전트 메인 루프 — 사용자 입력부터 LLM 호출, 도구 실행, 응답 스트리밍까지 전체 실행 흐름 오케스트레이션
// Responsibility: 분류→도구 선택→컨텍스트 빌드→LLM 호출→도구 실행→히스토리 관리의 단일 진입점
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/agent/compact"
	todoagent "github.com/yourorg/infractl/internal/agent/todo"
	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

const (
	defaultMaxHistory             = 50
	defaultMaxToolLoop            = 50
	defaultMaxContextTokens       = 128_000
	maxConsecutiveCompactFailures = 3
)

// rememberPattern?? "??れ삀?節낆젂???⑤ı?? ????嚥▲꺃彛?癲ル슣?????濚밸Ŧ援욃ㅇ???釉먯뒜?????좊즴????嚥▲꺂痢??嶺????獄????
var rememberPattern = regexp.MustCompile(`(?i)기억해|기억해줘|remember this|please remember|save this`)

// Run?? ?????????곸죷???袁⑸즵?룸돁????????ш낄援θキ???룸Ŧ爾??녿쑟筌?????덈틖??筌먲퐢??
func (a *Agent) Run(ctx context.Context, userInput string) error {
	if a.promptCache == nil {
		a.promptCache = newPromptCache()
	}
	if a.promptInputs == nil {
		a.promptInputs = newPromptInputCache()
	}
	a.lastUserPrompt = userInput
	var servers []store.Server
	if a.store != nil {
		servers = a.promptInputs.GetServers(ctx, func(ctx context.Context) ([]store.Server, error) {
			list, err := a.store.List(ctx)
			if err != nil {
				slog.Warn("list servers for context", "err", err)
			}
			return list, err
		})
	}

	resolvedAlias := ""
	if srv, ok := resolveServerAliasFromInput(userInput, servers); ok {
		a.applyActiveServer(&srv)
		resolvedAlias = srv.Name
	}

	userInput = strings.TrimSpace(userInput)
	const maxUserInputLen = 30000
	if len(userInput) > maxUserInputLen {
		slog.Warn("user input too long, truncating", "original_len", len(userInput))
		userInput = userInput[:maxUserInputLen] + "\n\n[... user input truncated due to extreme length ...]"
	}

	userMsg := llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
	}
	a.history = append(a.history, userMsg)
	a.historyTurnCounter++

	// Proposal Gate: propose_task로 환경이 제안된 상태에서 사용자 응답 처리
	if pp := a.pendingProposal; pp != nil {
		if pp.IsStale(a.historyTurnCounter, time.Now()) {
			a.pendingProposal = nil
			slog.Info("pending proposal expired", "turn", a.historyTurnCounter)
		} else {
			switch {
			case pp.IsConfirmation(userInput):
				proposal := a.pendingProposal
				a.pendingProposal = nil
				return a.executePendingProposal(ctx, proposal, servers)
			case pp.IsCancel(userInput):
				a.pendingProposal = nil
				slog.Info("pending proposal cancelled by user")
			}
			// revision or pass-through: let normal LLM flow handle it
		}
	}

	// Confirmation Gate: 직전 턴에 propose_action으로 명령이 제안된 상태에서
	// 사용자가 '예/yes' 응답 시 전체 분류·intel 파이프라인을 우회해 즉시 실행한다.
	if pending := a.pendingAction.Current(); pending != nil {
		if a.pendingAction.IsStale(a.historyTurnCounter) {
			a.pendingAction.Clear("stale_expired")
		} else {
			yes, no := a.pendingAction.IsConfirmation(userInput)
			if yes {
				consumed := a.pendingAction.Consume()
				return a.executePendingAction(ctx, consumed, servers)
			}
			if no {
				a.pendingAction.Clear("user_rejected")
			}
		}
	}

	// 0단계: 비동기 분류 보조 데이터 프리패치
	activeServerName := ""
	if a.activeServer != nil {
		activeServerName = a.activeServer.Name
	}
	knowledgeCh := prefetchKnowledgeAsync(ctx, a.ragManager, userInput, activeServerName)
	taskMemoryCh := prefetchTaskMemoryAsync(ctx, a.knowledgeStore, a.execLogStore, userInput, activeServerName)

	var savedUserMsg *store.SessionMessage
	if a.sessionStore != nil && a.currentSessionID > 0 {
		sm := sessionMessageFromLLM(userMsg, a.currentSessionID)
		if id, err := a.sessionStore.SaveMessage(ctx, sm); err != nil {
			slog.Warn("save user message", "err", err)
		} else {
			sm.ID = id
			savedUserMsg = &sm
		}
	}

	var connStates []connector.ConnectorState
	if a.connectorMgr != nil {
		states := a.connectorMgr.States()
		if a.activeServer == nil {
			connStates = states
		} else {
			for _, st := range states {
				if strings.EqualFold(st.ServerName, a.activeServer.Name) {
					connStates = append(connStates, st)
				}
			}
		}
	}

	// Phase 6: memory-save intent hint.
	infractlMD := a.promptInputs.GetInfractlMD(loadInfractlMDData)
	if rememberPattern.MatchString(userInput) && a.registry.Has("knowledge_add") {
		infractlMD += "\n\n[INSTRUCTION] The user wants to save something to memory. Use the knowledge_add tool IMMEDIATELY to save the information they described."
	}

	var learnedSystems []store.LearnedSystem
	if a.adaptiveLearner != nil {
		all := a.promptInputs.GetLearnedSystems(ctx, a.adaptiveLearner.ListSystems)
		for _, sys := range all {
			if a.activeServer == nil || strings.EqualFold(sys.ServerName, a.activeServer.Name) {
				learnedSystems = append(learnedSystems, sys)
			}
		}
	}
	var ragSources []store.RAGSource
	var knowledgeStats *rag.KnowledgeStats
	if a.ragManager != nil {
		allSources := a.promptInputs.GetRAGSources(ctx, func(ctx context.Context) ([]store.RAGSource, error) {
			list, err := a.ragManager.ListSources(ctx)
			if err != nil {
				slog.Warn("list rag sources", "err", err)
			}
			return list, err
		})
		for _, src := range allSources {
			if a.activeServer == nil || src.ServerName == "" || strings.EqualFold(src.ServerName, a.activeServer.Name) {
				ragSources = append(ragSources, src)
			}
		}
		knowledgeStats = a.promptInputs.GetKnowledgeStats(ctx, func(ctx context.Context) (*rag.KnowledgeStats, error) {
			stats, err := a.ragManager.Stats(ctx)
			if err != nil {
				slog.Warn("rag knowledge stats", "err", err)
			}
			return stats, err
		})
	}

	// 1??影?됀? Qwen(General LLM)?????怨뺤릇????ш끽維?????ш낄猷?? ?嶺뚮㉡?€쾮? ????덈틖 癲ル슢?꾤땟?????濡ろ뜏????筌먲퐢??
	classification, err := a.runSelfClassification(ctx, userInput, a.history...)
	if err != nil {
		slog.Warn("Self-classification failed, using defaults", "err", err)
		classification = ClassifyResult{
			NeedsTools:     true,
			ToolGroups:     []string{"shell", "system_info", "server_mgmt", "file_ops"},
			PromptSections: []string{"safety", "tool_priority", "tool_selection"},
			Tier:           "general",
		}
	}

	// DB 인스턴스/서비스 직접 접속 키워드가 있으면 connector를 반드시 포함한다.
	// 분류기가 server_mgmt만 선택했더라도 "인스턴스 접속", "PDB 접속", "sqlplus" 등은 connector가 필요하다.
	if classification.NeedsTools && dbInstanceConnectPattern.MatchString(userInput) {
		hasConnector := false
		for _, g := range classification.ToolGroups {
			if g == "connector" {
				hasConnector = true
				break
			}
		}
		if !hasConnector {
			slog.Debug("auto-inject connector: db instance connect pattern detected", "input", userInput)
			classification.ToolGroups = append(classification.ToolGroups, "connector")
		}
	}

	// Ambiguity Gate: 서버 접속 vs DB 접속 모호성 해결
	if isAmbiguous, choice := a.resolveAmbiguity(ctx, userInput, classification, servers); isAmbiguous {
		if choice == "server_only" {
			return a.handleServerOnlyConnect(ctx, userInput, servers)
		}
		// DB/서비스 접속을 선택한 경우 classification을 보정하여 discovery/connector가 확실히 포함되게 함
		hasConnector := false
		for _, g := range classification.ToolGroups {
			if g == "connector" {
				hasConnector = true
				break
			}
		}
		if !hasConnector {
			classification.ToolGroups = append(classification.ToolGroups, "connector", "discovery")
		}
		// LLM이 즉시 서비스 탐색을 실행하도록 히스토리 마지막 user 메시지에 지시 힌트 주입
		if len(a.history) > 0 {
			last := &a.history[len(a.history)-1]
			if last.Role == llm.RoleUser {
				last.Content += "\n[사용자가 DB/서비스 탐색을 선택했습니다. 서버 접속 후 discover_services를 즉시 실행하여 서비스 목록을 확인하세요.]"
			}
		}
	}

	// Plan mode: force reasoning tier.
	if a.IsPlanMode() {
		classification.Tier = "reasoning"
	}

	// 2??影?됀? ?濡ろ뜏?????濡ろ뜏???????ㅻ깹????ш낄猷??? ????????釉뚰????筌먲퐢??
	allowedTools := ResolveToolGroups(classification.ToolGroups, a.registry, a.connectorMgr)
	sections := ResolveSectionsFromList(
		classification.PromptSections,
		classification.NeedsTools,
		a.activeServer != nil,
		len(servers) > 0,
		len(connStates) > 0,
		len(learnedSystems) > 0,
		infractlMD != "",
	)

	var systemPrompt string
	var enabledTools []tools.Tool
	var toolDefs []llm.ToolDef

	// 3??影?됀? Qwen?????ャ뀕??????Β?ш퐨??癲ル슢?꾤땟???紐꾨퓠?????덈틖??筌먲퐢??
	activeClient, activeTier, activeModelName := a.resolveClientForTier(classification.Tier)

	llm.LogSystemEvent("Execution Info", fmt.Sprintf("Final Tier: %s\nModel Name: %s", activeTier, activeModelName))

	// Pre-flight: reasoning-tier 작업 시 인텔 서브에이전트로 환경 수집
	// YORO 모드에서도 환경 파악은 수행한다. 단, 사용자 승인 요청 지침은 생략한다.
	// pending action이 활성 상태이거나 입력이 10자 이하(단문 후속 응답)이면 환경 재조사를 건너뛴다.
	isPendingActive := a.pendingAction.Current() != nil
	isShortFollowUp := len([]rune(userInput)) <= 10
	var intelContext string
	if classification.Tier == "reasoning" && a.subagentRunner != nil &&
		!isPendingActive && !isShortFollowUp {
		// LLM이 분류 단계에서 task_type을 결정했으면 그것을 사용, 없으면 regex 폴백
		taskType := TaskType(classification.TaskType)
		if taskType == "" {
			taskType = classifyTaskType(userInput)
			if taskType != "" {
				classification.TaskType = string(taskType)
			}
		}
		if taskType != "" {
			server := ""
			if a.activeServer != nil {
				server = a.activeServer.Name
			}
			intelReport, err := runIntelSubagent(ctx, a.subagentRunner, taskType, userInput, server)
			if err != nil {
				slog.Warn("pre-flight intel subagent failed, proceeding without intel", "err", err)
			} else if intelReport != "" {
				intelContext = formatIntelPromptSection(intelReport, taskType, a.yoroMode)
			}
		}
	}

	// classification????ш끽維?????筌믨퀣?????ш끽諭욥걡???쒓랜萸??濡ろ뜏???醫듽걫????⑥쥓??棺??짆?れ쒜???⑥?????쒓낯???筌먲퐢??
	// ??ш끽維쀧빊?濚욌꼬裕뼘????逾녜뇡??? ????⒱봼???醫딅뱠 LLM??rag_search ??ш낄猷??쇱뒙?癲ル슣?????濡ろ떟???????????筌먲퐢??
	var prefetchedKnowledge string
	var prefetchedTaskMemory string
	select {
	case k := <-knowledgeCh:
		prefetchedKnowledge = k
	default:
	}
	select {
	case m := <-taskMemoryCh:
		prefetchedTaskMemory = m
	default:
	}

	const maxPrefetchLen = 10000
	if len(prefetchedKnowledge) > maxPrefetchLen {
		prefetchedKnowledge = prefetchedKnowledge[:maxPrefetchLen] + "\n[... knowledge truncated ...]"
	}
	if len(prefetchedTaskMemory) > maxPrefetchLen {
		prefetchedTaskMemory = prefetchedTaskMemory[:maxPrefetchLen] + "\n[... task memory truncated ...]"
	}

	now := time.Now()
	if classification.NeedsTools {
		enabledTools = a.registry.GetEnabledFiltered(allowedTools)
		toolDefs = a.registry.ToToolDefsFiltered(allowedTools)
		layoutKey := contextualPromptCacheKey(
			sections,
			enabledTools,
			infractlMD,
			servers,
			a.activeServer,
			connStates,
			learnedSystems,
			ragSources,
			knowledgeStats,
			activeModelName,
			now,
		)
		layout, ok := a.promptCache.GetContextual(layoutKey)
		if !ok {
			layout = BuildContextualLayoutAt(
				sections,
				enabledTools,
				infractlMD,
				servers,
				a.activeServer,
				connStates,
				learnedSystems,
				ragSources,
				knowledgeStats,
				activeModelName,
				now,
			)
			a.promptCache.PutContextual(layoutKey, layout)
			slog.Debug("prompt cache miss", "type", "contextual", "key", layoutKey)
		} else {
			slog.Debug("prompt cache hit", "type", "contextual", "key", layoutKey)
		}
		systemPrompt = layout.Render(prefetchedTaskMemory, prefetchedKnowledge)
		if a.taskMgr != nil {
			if snap := a.taskMgr.Snapshot(); snap.ID != "" {
				systemPrompt += "\n\n" + snap.RenderMarkdown()
			}
		}
		a.recordPromptProfile(promptProfile{
			Mode:                 "contextual",
			Model:                activeModelName,
			NeedsTools:           true,
			PromptCacheHit:       ok,
			TotalBytes:           len(systemPrompt),
			PrefixBytes:          len(layout.prefix),
			TaskMemoryBytes:      len(prefetchedTaskMemory),
			BeforeKnowledgeBytes: len(layout.beforeKnowledge),
			KnowledgeBytes:       len(prefetchedKnowledge),
			AfterKnowledgeBytes:  len(layout.afterKnowledge),
			ToolCount:            len(enabledTools),
			SectionCount:         countEnabledSections(sections),
			ServerCount:          len(servers),
			ConnectorCount:       len(connStates),
			LearnedSystemCount:   len(learnedSystems),
			RAGSourceCount:       len(ragSources),
		})
	} else {
		minimalKey := minimalPromptCacheKey(infractlMD, now)
		if cached, ok := a.promptCache.GetMinimal(minimalKey); ok {
			systemPrompt = cached
			slog.Debug("prompt cache hit", "type", "minimal", "key", minimalKey)
			a.recordPromptProfile(promptProfile{
				Mode:           "minimal",
				Model:          activeModelName,
				NeedsTools:     false,
				PromptCacheHit: true,
				TotalBytes:     len(systemPrompt),
				PrefixBytes:    len(systemPrompt),
			})
		} else {
			systemPrompt = buildMinimalChatAt(infractlMD, now)
			a.promptCache.PutMinimal(minimalKey, systemPrompt)
			slog.Debug("prompt cache miss", "type", "minimal", "key", minimalKey)
			a.recordPromptProfile(promptProfile{
				Mode:           "minimal",
				Model:          activeModelName,
				NeedsTools:     false,
				PromptCacheHit: false,
				TotalBytes:     len(systemPrompt),
				PrefixBytes:    len(systemPrompt),
			})
		}
	}

	if resolvedAlias != "" {
		resolvedNote := fmt.Sprintf(
			"\n\n[LATEST INPUT RESOLUTION]\nlatest user input resolved to SSH server alias <%s>.\nPrioritize this alias for this turn over stale history references.\n",
			resolvedAlias,
		)
		systemPrompt += resolvedNote
		a.lastPromptProfile.ResolutionBytes = len(resolvedNote)
		a.lastPromptProfile.TotalBytes = len(systemPrompt)
	}

	// Plan mode: inject plan-only instruction.
	if a.IsPlanMode() {
		planText := planModeInstruction()
		systemPrompt += planText
		a.lastPromptProfile.PlanBytes = len(planText)
		a.lastPromptProfile.TotalBytes = len(systemPrompt)
	}

	// TodoWrite enforcer: 다단계 신호어 감지 시 system prompt 에 사전 지침 주입하고
	// enforcer 활성화 여부를 결정한다 (단순 단일 명령은 강제하지 않음).
	todoSignal := todoagent.DetectMultiStep(userInput)
	systemPrompt = todoagent.InjectIfNeeded(userInput, systemPrompt)

	// Pre-flight intel 결과 주입: 환경 정보 + 계획 수립 지침
	if intelContext != "" {
		const maxIntelContextLen = 12000
		if len(intelContext) > maxIntelContextLen {
			intelContext = intelContext[:maxIntelContextLen] + "\n[... intel context truncated ...]"
		}
		systemPrompt += intelContext
		a.lastPromptProfile.TotalBytes = len(systemPrompt)
	}

	// ??筌?痢????ш끽維???ш낄援θキ???ヂ?筌??リ랜??癲?????筌뚯슦肉????ャ뀕????⑤베毓?????筌믨퀡裕??筌먲퐢??
	a.lastSystemPromptLen = len(systemPrompt)
	if a.compactStack != nil {
		cs := &compact.State{
			Messages:      a.history,
			ContextWindow: a.maxContextTokens,
			SystemTokens:  a.lastSystemPromptLen / compact.AvgCharsPerToken,
		}
		preLen := len(a.history)
		a.compactStack.Apply(ctx, cs)
		a.history = cs.Messages
		if len(a.history) < preLen {
			a.injectPostCompactContext(ctx)
		}
	}

	if savedUserMsg != nil && a.ragManager != nil {
		if err := a.ragManager.IndexSessionMessage(ctx, *savedUserMsg); err != nil {
			slog.Warn("index user message", "id", savedUserMsg.ID, "err", err)
		}
	}

	systemMsg := llm.Message{Role: llm.RoleSystem, Content: systemPrompt}

	// Qwen 癲ル슢?꾤땟?????濡ろ뜑???API??癲ル슢?뤸뤃???tools ??ш끽維????????????ш끽維???ш낄援θキ???れ삀??뫢???⑥?????嶺뚮ㅎ???????ル늅筌??筌먲퐢??
	// ?????vLLM ????節녿쨬??쎛 ????덉쉐?域밸Ŧ留⑶뜮?濚????Β????? ???モ닪?????듦뭅??400 ????????????濡ろ뜏????袁⑸젻泳?????꾨탿 ??ш낄援ο쭕?????
	apiTools := toolDefs
	if strings.Contains(strings.ToLower(activeModelName), "qwen") {
		apiTools = nil
	}

	return a.runWithEngine(ctx, systemMsg, activeClient, activeTier, activeModelName, apiTools, todoSignal)
}
