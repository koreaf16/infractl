// Package agent
// File: loop.go
// Description: 에이전트 루프 코어 — LLM 호출, 도구 실행, 히스토리 관리
// Responsibility: 사용자 입력부터 최종 응답까지 전체 에이전트 루프 오케스트레이션

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/yourorg/infractl/internal/background"
	"github.com/yourorg/infractl/internal/checkpoint"
	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/cost"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/hooks"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

const (
	defaultMaxHistory  = 50
	defaultMaxToolLoop = 50
)

// rememberPattern은 "기억해줘" 등 수동 지식 등록 요청을 감지하는 정규식이다.
var rememberPattern = regexp.MustCompile(`(?i)기억해줘|기억해|remember this|please remember|save this`)

// Agent는 LLM, 도구, ExecutorManager를 조합하여 에이전트 루프를 실행한다.
type Agent struct {
	llmClient           llm.Client
	llmRegistry         *llm.Registry // 멀티 LLM 티어 레지스트리 (nil이면 단일 모델)
	registry            *tools.Registry
	manager             *executor.Manager
	store               store.ServerStore
	handler             EventHandler
	sessionStore        store.SessionStore
	execLogStore        store.ExecLogStore
	history             []llm.Message
	activeServer        *store.Server
	activeServerNotify  func(*store.Server)
	connectorMgr        *connector.Manager
	knowledgeLearner    *KnowledgeLearner   // Phase 6: 에러 패턴 자동 학습
	adaptiveLearner     *AdaptiveLearner    // Phase 6: 적응형 시스템 학습
	ragManager          *rag.Manager        // Phase 7: RAG 검색 오케스트레이터
	costTracker         *cost.Tracker       // Phase 8: 비용/사용량 추적
	bgManager           *background.Manager // Phase 8: 백그라운드 작업 관리
	checkpointMgr       *checkpoint.Manager // Phase 8: 체크포인트/롤백
	hooksMgr            *hooks.Manager      // Phase 8: 라이프사이클 훅
	modelName           string              // Phase 8: 비용 기록용 모델명
	sessionHookFired    bool                // Phase 8: on_session_start 훅 중복 방지
	maxHistory          int
	maxToolLoop         int
	maxContextTokens    int
	currentSessionID    int64
	lastUserPrompt      string
	lastFailedLogID     int64
	lastSystemPromptLen int              // 마지막 시스템 프롬프트 문자 수 — 토큰 추정에 사용
	compactFailures     int              // circuit breaker: 연속 compaction 실패 횟수
	yoroMode            bool             // YORO 모드: 확인 대화 없이 바로 실행 (백업/체크포인트는 유지)
	planMode            bool             // Plan 모드: reasoning tier 강제 + 계획 작성 프롬프트 주입
	idleHandler         IdleInputHandler // 명령 실행 중 인터랙티브 프롬프트 감지 처리
	questionHandler     QuestionHandler  // Phase 8: 다중 선택형 질의응답 처리
}

// New는 에이전트를 생성한다.
func New(client llm.Client, registry *tools.Registry, mgr *executor.Manager, handler EventHandler, st store.ServerStore) *Agent {
	return &Agent{
		llmClient:        client,
		registry:         registry,
		manager:          mgr,
		store:            st,
		handler:          handler,
		history:          make([]llm.Message, 0),
		maxHistory:       defaultMaxHistory,
		maxToolLoop:      defaultMaxToolLoop,
		maxContextTokens: defaultMaxContextTokens,
	}
}

// SetConnectorManager는 커넥터 매니저를 주입한다.
func (a *Agent) SetConnectorManager(mgr *connector.Manager) {
	a.connectorMgr = mgr
}

// SetHandler는 이벤트 핸들러를 주입한다.
func (a *Agent) SetHandler(h EventHandler) {
	a.handler = h
}

// SetIdleInputHandler는 명령 실행 중 인터랙티브 프롬프트 감지 핸들러를 주입한다.
func (a *Agent) SetIdleInputHandler(h IdleInputHandler) {
	a.idleHandler = h
}

// SetQuestionHandler는 다중 선택 질의응답 핸들러를 주입한다.
func (a *Agent) SetQuestionHandler(h QuestionHandler) {
	a.questionHandler = h
}

// NewSmartIdleInputHandler는 에이전트의 LLM 클라이언트를 사용하는 SmartIdleInputHandler를 생성한다.
// LLM이 모든 인터랙티브 프롬프트를 자율 판단하며 사용자 입력 폴백은 없다.
func (a *Agent) NewSmartIdleInputHandler() *SmartIdleInputHandler {
	h := NewSmartIdleInputHandler(a.llmClient)
	h.SetServerStore(a.store)
	return h
}

// SetSessionStore는 세션 저장소를 주입한다.
func (a *Agent) SetSessionStore(s store.SessionStore) {
	a.sessionStore = s
}

// SetExecLogStore는 실행 이력 저장소를 주입한다.
func (a *Agent) SetExecLogStore(s store.ExecLogStore) {
	a.execLogStore = s
}

// SetSessionID는 현재 대화 세션 ID를 설정한다.
func (a *Agent) SetSessionID(id int64) {
	a.currentSessionID = id
}

// SetMaxContextTokens는 컨텍스트 토큰 한계를 설정한다.
func (a *Agent) SetMaxContextTokens(n int) {
	a.maxContextTokens = n
}

// SetKnowledgeLearner는 Phase 6 에러 패턴 학습기를 주입한다.
func (a *Agent) SetKnowledgeLearner(kl *KnowledgeLearner) {
	a.knowledgeLearner = kl
}

// SetAdaptiveLearner는 Phase 6 적응형 학습 매니저를 주입한다.
func (a *Agent) SetAdaptiveLearner(al *AdaptiveLearner) {
	a.adaptiveLearner = al
}

// SetRAGManager는 Phase 7 RAG 오케스트레이터를 주입한다.
func (a *Agent) SetRAGManager(rm *rag.Manager) {
	a.ragManager = rm
}

// SetYoroMode는 YORO 모드 활성화 여부를 설정한다.
func (a *Agent) SetYoroMode(enabled bool) {
	a.yoroMode = enabled
}

// ToggleYoroMode는 YORO 모드를 토글하고 변경된 상태를 반환한다.
func (a *Agent) ToggleYoroMode() bool {
	a.yoroMode = !a.yoroMode
	return a.yoroMode
}

// Run은 사용자 입력을 받아 에이전트 루프를 실행한다.
func (a *Agent) Run(ctx context.Context, userInput string) error {
	a.lastUserPrompt = userInput
	var servers []store.Server
	if a.store != nil {
		servers, _ = a.store.List(ctx)
	}

	resolvedAlias := ""
	if srv, ok := resolveServerAliasFromInput(userInput, servers); ok {
		a.applyActiveServer(&srv)
		resolvedAlias = srv.Name
	}

	// Phase 8: 세션 첫 턴에서 on_session_start 훅 실행
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

	// 0단계: 지식 프리페치를 classification과 병렬로 즉시 시작한다.
	activeServerName := ""
	if a.activeServer != nil {
		activeServerName = a.activeServer.Name
	}
	knowledgeCh := prefetchKnowledgeAsync(ctx, a.ragManager, userInput, activeServerName)

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

	// Phase 6: "기억해줘" 패턴 감지 → 시스템 프롬프트 힌트 주입
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
		allSources, _ := a.ragManager.ListSources(ctx)
		for _, src := range allSources {
			if a.activeServer == nil || src.ServerName == "" || strings.EqualFold(src.ServerName, a.activeServer.Name) {
				ragSources = append(ragSources, src)
			}
		}
		knowledgeStats, _ = a.ragManager.Stats(ctx)
	}

	// 1단계: Qwen(General LLM)이 스스로 필요한 도구, 정보, 실행 모델을 결정한다.
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

	// Plan 모드: 최고 추론 티어 강제 적용
	if a.planMode {
		classification.Tier = "reasoning"
	}

	// 2단계: 결정된 결과에 따라 도구와 섹션을 조립한다.
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

	// 3단계: Qwen이 선택한 티어의 모델로 실행한다.
	activeClient, activeTier, activeModelName := a.resolveClientForTier(classification.Tier)
	
	llm.LogSystemEvent("Execution Info", fmt.Sprintf("Final Tier: %s\nModel Name: %s", activeTier, activeModelName))

	// classification이 완료된 시점에 프리페치 결과를 논블로킹으로 수집한다.
	// 아직 준비되지 않았다면 LLM이 rag_search 도구로 직접 검색하게 된다.
	var prefetchedKnowledge string
	select {
	case k := <-knowledgeCh:
		prefetchedKnowledge = k
	default:
	}

	if classification.NeedsTools {
		enabledTools = a.registry.GetEnabledFiltered(allowedTools)
		toolDefs = a.registry.ToToolDefsFiltered(allowedTools)
		systemPrompt = BuildContextual(sections, enabledTools, infractlMD, servers, a.activeServer, connStates, learnedSystems, ragSources, knowledgeStats, activeModelName, prefetchedKnowledge)
	} else {
		systemPrompt = BuildMinimalChat(infractlMD)
	}

	if resolvedAlias != "" {
		systemPrompt += fmt.Sprintf(
			"\n\n[LATEST INPUT RESOLUTION]\nlatest user input resolved to SSH server alias <%s>.\nPrioritize this alias for this turn over stale history references.\n",
			resolvedAlias,
		)
	}

	// Plan 모드: 계획 작성 전용 프롬프트 섹션 주입
	if a.planMode {
		systemPrompt += planModeInstruction()
	}

	// 시스템 프롬프트 길이를 캐싱하여 토큰 추정에 활용한다.
	a.lastSystemPromptLen = len(systemPrompt)
	a.compactIfNeeded(ctx)

	if savedUserMsg != nil && a.ragManager != nil {
		if err := a.ragManager.IndexSessionMessage(ctx, *savedUserMsg); err != nil {
			slog.Warn("index user message", "id", savedUserMsg.ID, "err", err)
		}
	}

	systemMsg := llm.Message{Role: llm.RoleSystem, Content: systemPrompt}

	// Qwen 모델일 경우 API의 명시적 tools 필드를 비우고 프롬프트 기반으로 툴 호출을 유도한다.
	// 이는 vLLM 파서가 스트리밍 중 데이터를 유실하거나 400 에러를 내는 것을 방지하기 위함이다.
	apiTools := toolDefs
	if strings.Contains(strings.ToLower(activeModelName), "qwen") {
		apiTools = nil
	}

	for i := 0; i < a.maxToolLoop; i++ {
		messages := a.buildMessages(systemMsg)

		a.handler.OnThinking(string(activeTier), activeModelName)
		resp, err := activeClient.ChatStream(ctx, messages, apiTools, a.handler.OnThinkingToken, a.handler.OnToken)
		if err != nil {
			a.handler.OnError(fmt.Errorf("llm call failed: %w", err))
			return fmt.Errorf("llm call: %w", err)
		}

		slog.Debug("llm response", "has_tool_calls", len(resp.ToolCalls) > 0,
			"input_tokens", resp.InputTokens, "output_tokens", resp.OutputTokens)

		// 토큰/비용 정보 전달 및 기록
		if resp.InputTokens > 0 || resp.OutputTokens > 0 {
			a.handler.OnUsageUpdate(resp.InputTokens, resp.OutputTokens, 0, 0)
			// Phase 8: 비용 기록 (비동기, 실패해도 루프 계속)
			if a.costTracker != nil {
				a.costTracker.Record(ctx, activeModelName, resp.InputTokens, resp.OutputTokens,
					cost.SourceUser, a.currentSessionID)
			}
		}

		if len(resp.ToolCalls) == 0 {
			// Loop Guard: 상태 변경 작업이 있는데 verify_complete를 호출하지 않은 경우 강제 중단 방지
			if a.isMutationPerformedWithoutVerification() {
				slog.Info("Loop guard: mutation detected without verify_complete, injecting system hint", "session", a.currentSessionID)
				
				// 이미 스트리밍된 응답이 있다면, 사용자에게 루프 가드 상황임을 알림
				if resp.Content != "" {
					a.handler.OnResponse("[SYSTEM] 상태 변경 내역이 감지되어 최종 확인 절차를 진행합니다...")
				}

				a.history = append(a.history, llm.Message{
					Role:    llm.RoleUser,
					Content: "[SYSTEM] 당신은 시스템 상태를 변경하는 명령을 수행했지만, 아직 `verify_complete` 도구를 호출하지 않았습니다. 반드시 결과를 명령어로 직접 확인(Verify)한 뒤, `verify_complete` 도구를 호출하여 최종 보고를 완료하십시오. 이 도구 호출 전에는 작업을 마칠 수 없습니다.",
				})
				continue
			}

			assistantFinal := llm.Message{
				Role:    llm.RoleAssistant,
				Content: resp.Content,
			}
			a.history = append(a.history, assistantFinal)
			if a.sessionStore != nil && a.currentSessionID > 0 {
				sm := sessionMessageFromLLM(assistantFinal, a.currentSessionID)
				if id, err := a.sessionStore.SaveMessage(ctx, sm); err != nil {
					slog.Warn("save assistant message", "err", err)
				} else if a.ragManager != nil {
					sm.ID = id
					if err := a.ragManager.IndexSessionMessage(ctx, sm); err != nil {
						slog.Warn("index assistant message", "id", id, "err", err)
					}
				}
			}
			a.handler.OnResponse(resp.Content)
			a.trimHistory()
			return nil
		}

		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		a.history = append(a.history, assistantMsg)
		if a.sessionStore != nil && a.currentSessionID > 0 {
			sm := sessionMessageFromLLM(assistantMsg, a.currentSessionID)
			if id, err := a.sessionStore.SaveMessage(ctx, sm); err != nil {
				slog.Warn("save assistant tool_calls message", "err", err)
			} else if a.ragManager != nil {
				sm.ID = id
				if err := a.ragManager.IndexSessionMessage(ctx, sm); err != nil {
					slog.Warn("index assistant tool_calls message", "id", id, "err", err)
				}
			}
		}

		toolResults := a.executeToolCalls(ctx, resp.ToolCalls)
		for i, tc := range resp.ToolCalls {
			if i < len(toolResults) {
				llm.LogToolResult(tc.Function.Name, toolResults[i].Content)
			}
		}
		a.history = append(a.history, toolResults...)
		if a.sessionStore != nil && a.currentSessionID > 0 {
			for _, tr := range toolResults {
				sm := sessionMessageFromLLM(tr, a.currentSessionID)
				if id, err := a.sessionStore.SaveMessage(ctx, sm); err != nil {
					slog.Warn("save tool result message", "err", err)
				} else if a.ragManager != nil {
					sm.ID = id
					if err := a.ragManager.IndexSessionMessage(ctx, sm); err != nil {
						slog.Warn("index tool result message", "id", id, "err", err)
					}
				}
			}
		}
	}

	loopErr := fmt.Errorf("max tool loop iterations (%d) exceeded", a.maxToolLoop)
	a.handler.OnError(loopErr)
	return loopErr
}

// Tools는 등록된 도구 목록을 반환한다.
func (a *Agent) Tools() []tools.Tool {
	return a.registry.List()
}

// Manager는 executor 매니저를 반환한다.
func (a *Agent) Manager() *executor.Manager {
	return a.manager
}

// SetActiveServer는 전용 접속 세션 대상을 지정한다.
func (a *Agent) SetActiveServer(srv store.Server) {
	a.applyActiveServer(&srv)
}

// ClearActiveServer는 다중 서버 관리 상태(로컬 기본)로 복귀한다.
func (a *Agent) ClearActiveServer() {
	a.applyActiveServer(nil)
}

// SetActiveServerNotifier registers a callback fired when active server changes.
func (a *Agent) SetActiveServerNotifier(fn func(*store.Server)) {
	a.activeServerNotify = fn
}

// ToggleYOROMode는 YORO 모드를 토글하고 변경 후 상태를 반환한다.
// YORO(You Only Run Once) 모드: 확인 대화를 건너뛰고 바로 실행한다.
// 백업과 체크포인트는 YORO 모드에서도 그대로 동작한다.
func (a *Agent) ToggleYOROMode() bool {
	a.yoroMode = !a.yoroMode
	return a.yoroMode
}

// IsYOROMode는 YORO 모드 활성 여부를 반환한다.
func (a *Agent) IsYOROMode() bool { return a.yoroMode }

// isMutationPerformedWithoutVerification은 현재 턴에서 상태 변경 작업이 있었으나
// verify_complete 도구가 호출되지 않았는지 확인한다.
func (a *Agent) isMutationPerformedWithoutVerification() bool {
	hasMutation := false
	hasVerification := false

	// 현재 턴의 시작(가장 최근의 실제 사용자 입력)부터 탐색
	for i := len(a.history) - 1; i >= 0; i-- {
		msg := a.history[i]

		// 루프 가드에 의해 주입된 시스템 메시지는 유저 역할이지만 Skip
		if msg.Role == llm.RoleUser && !strings.Contains(msg.Content, "[SYSTEM]") {
			break
		}

		if msg.Role == llm.RoleAssistant {
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					if tc.Function.Name == "verify_complete" {
						hasVerification = true
						continue
					}
					// 도구 레지스트리에서 읽기 전용 여부 확인
					if tool, ok := a.registry.Get(tc.Function.Name); ok {
						if !tool.IsReadOnly() {
							// 인자를 파싱하여 실제 위험도를 평가 (RiskNone이면 읽기 전용으로 취급)
							var args map[string]interface{}
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
								if evaluateToolSafety(tool, args).RiskLevel != tools.RiskNone {
									hasMutation = true
								}
							} else {
								// 파싱 실패 시 보수적으로 상태 변경으로 간주
								hasMutation = true
							}
						}
					}
				}
			}
		}
	}

	// 상태 변경은 있었는데 검증 도구 호출이 없었다면 true
	return hasMutation && !hasVerification
}
