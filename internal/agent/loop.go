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

	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/hooks"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

const (
	defaultMaxHistory  = 50
	defaultMaxToolLoop = 50

	// llmCallTimeout?? ???쒒?LLM ChatStream ?嶺뚮ㅎ????癲ル슔?됭짆? ???源낅츛 ??癰???????
	// ????癰??????⑤；諭?????덉쉐?繹먮끏?????ш끽維???? ???怨룔걬癲????爾?????덉쉐??좊읈? ???爾???筌뚯슦苑????????袁⑸즵????筌먲퐢??
	// openai.go??idleReader??좊읈? 癲??????쒙쭕?idle timeout?????????寃뗏?
	// ????좊즴?? ??ш끽維???嶺뚮ㅎ???????ㅺ강????モ섌??
	llmCallTimeout = 5 * time.Minute

	// maxToolsPerTurn?? ?????????Run ?嶺뚮ㅎ???1?? ???⑤；諭?????源낅츛??嚥▲꺂痢?癲ル슔?됭짆? ??ш끽維????ш낄猷???嶺뚮ㅎ?????嚥▲꺂??
	// ????? ?縕????嚥??????源낆쓱 LLM ?嶺뚮ㅎ?????"癲ル슣鍮뽳쭕??????몄릇?嶺뚮ㅎ?붷ㅇ????쑩?젆??嚥???????좊즴甕??癲ル슣????? ??낆뒩????筌먲퐢??
	// ???????룸Ŧ爾??ls ??ls subdirectory ??ls deeper ???????爾?????덉쉐??좊읈? ?????嚥▲꺂痢??濡ろ뜏???癲ル슢??쭕???
	maxToolsPerTurn = 4
)

// rememberPattern?? "??れ삀?節낆젂???⑤ı?? ????嚥▲꺃彛?癲ル슣?????濚밸Ŧ援욃ㅇ???釉먯뒜?????좊즴????嚥▲꺂痢??嶺????獄????
var rememberPattern = regexp.MustCompile(`(?i)기억해|기억해줘|remember this|please remember|save this`)

// Run?? ?????????곸죷???袁⑸즵?룸돁????????ш낄援θキ???룸Ŧ爾??녿쑟筌?????덈틖??筌먲퐢??
func (a *Agent) Run(ctx context.Context, userInput string) error {
	a.lastUserPrompt = userInput
	var servers []store.Server
	if a.store != nil {
		var listErr error
		servers, listErr = a.store.List(ctx)
		if listErr != nil {
			slog.Warn("list servers for context", "err", listErr)
		}
	}

	resolvedAlias := ""
	if srv, ok := resolveServerAliasFromInput(userInput, servers); ok {
		a.applyActiveServer(&srv)
		resolvedAlias = srv.Name
	}

	// Phase 8: ?嶺뚮ㅎ???癲????⑤；諭??on_session_start ??????덈틖
	if a.hooksMgr != nil && !a.sessionHookFired {
		a.sessionHookFired = true
		server := ""
		if a.activeServer != nil {
			server = a.activeServer.Name
		}
		a.hooksMgr.Fire(ctx, hooks.HookContext{
			Event:  hooks.EventOnSessionStart,
			Server: server,
		})
	}

	userMsg := llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
	}
	a.history = append(a.history, userMsg)

	// 0??影?됀? 癲ル슣??????ш끽諭욥걡???쒓랜萸??classification???怨뚮옖筌?獄쎻뫗肉?癲ル슣鍮뽳쭕????筌믨퀣援??筌먲퐢??
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
	infractlMD := LoadInfractlMD()
	if rememberPattern.MatchString(userInput) && a.registry.Has("knowledge_add") {
		infractlMD += "\n\n[INSTRUCTION] The user wants to save something to memory. Use the knowledge_add tool IMMEDIATELY to save the information they described."
	}

	var learnedSystems []store.LearnedSystem
	if a.adaptiveLearner != nil {
		all := a.adaptiveLearner.ListSystems(ctx)
		for _, sys := range all {
			if a.activeServer == nil || strings.EqualFold(sys.ServerName, a.activeServer.Name) {
				learnedSystems = append(learnedSystems, sys)
			}
		}
	}
	var ragSources []store.RAGSource
	var knowledgeStats *rag.KnowledgeStats
	if a.ragManager != nil {
		allSources, listSrcErr := a.ragManager.ListSources(ctx)
		if listSrcErr != nil {
			slog.Warn("list rag sources", "err", listSrcErr)
		}
		for _, src := range allSources {
			if a.activeServer == nil || src.ServerName == "" || strings.EqualFold(src.ServerName, a.activeServer.Name) {
				ragSources = append(ragSources, src)
			}
		}
		var statsErr error
		knowledgeStats, statsErr = a.ragManager.Stats(ctx)
		if statsErr != nil {
			slog.Warn("rag knowledge stats", "err", statsErr)
		}
	}

	// 1??影?됀? Qwen(General LLM)?????怨뺤릇????ш끽維?????ш낄猷?? ?嶺뚮㉡?€쾮? ????덈틖 癲ル슢?꾤땟?????濡ろ뜏????筌먲퐢??
	classification, err := a.runSelfClassification(ctx, userInput)
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
	}

	// Plan mode: force reasoning tier.
	if a.planMode {
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

	if classification.NeedsTools {
		enabledTools = a.registry.GetEnabledFiltered(allowedTools)
		toolDefs = a.registry.ToToolDefsFiltered(allowedTools)
		systemPrompt = BuildContextual(
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
			prefetchedKnowledge,
			prefetchedTaskMemory,
		)
	} else {
		systemPrompt = BuildMinimalChat(infractlMD)
	}

	if resolvedAlias != "" {
		systemPrompt += fmt.Sprintf(
			"\n\n[LATEST INPUT RESOLUTION]\nlatest user input resolved to SSH server alias <%s>.\nPrioritize this alias for this turn over stale history references.\n",
			resolvedAlias,
		)
	}

	// Plan mode: inject plan-only instruction.
	if a.planMode {
		systemPrompt += planModeInstruction()
	}

	// ??筌?痢????ш끽維???ш낄援θキ???ヂ?筌??リ랜??癲?????筌뚯슦肉????ャ뀕????⑤베毓?????筌믨퀡裕??筌먲퐢??
	a.lastSystemPromptLen = len(systemPrompt)
	a.compactIfNeeded(ctx)

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


	return a.runLLMLoop(ctx, systemMsg, activeClient, activeTier, activeModelName, apiTools)
}
