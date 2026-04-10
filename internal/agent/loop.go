// Package agent
// File: loop.go
// Description: 에이전트 루프 코어 — LLM 호출, 도구 실행, 히스토리 관리
// Responsibility: 사용자 입력부터 최종 응답까지 전체 에이전트 루프 오케스트레이션

package agent

import (
	"context"
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
	defaultMaxToolLoop = 20
)

// rememberPattern은 "기억해줘" 등 수동 지식 등록 요청을 감지하는 정규식이다.
var rememberPattern = regexp.MustCompile(`(?i)기억해줘|기억해|remember this|please remember|save this`)

// Agent는 LLM, 도구, ExecutorManager를 조합하여 에이전트 루프를 실행한다.
type Agent struct {
	llmClient           llm.Client
	llmRegistry         *llm.Registry // 멀티 LLM 티어 레지스트리 (nil이면 단일 모델)
	llmRouter           *llm.Router   // 티어 라우터 (nil이면 라우팅 비활성)
	registry            *tools.Registry
	manager             *executor.Manager
	store               store.ServerStore
	handler             EventHandler
	confirmHandler      ConfirmationHandler
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
	idleHandler         IdleInputHandler // 명령 실행 중 인터랙티브 프롬프트 감지 처리
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

// SetConfirmationHandler는 위험 작업 확인 핸들러를 주입한다.
func (a *Agent) SetConfirmationHandler(h ConfirmationHandler) {
	a.confirmHandler = h
}

// SetIdleInputHandler는 명령 실행 중 인터랙티브 프롬프트 감지 핸들러를 주입한다.
func (a *Agent) SetIdleInputHandler(h IdleInputHandler) {
	a.idleHandler = h
}

// NewSmartIdleInputHandler는 에이전트의 LLM 클라이언트를 사용하는 SmartIdleInputHandler를 생성한다.
// 에이전트 내부 llmClient를 외부에 노출하지 않고 핸들러를 생성할 수 있다.
func (a *Agent) NewSmartIdleInputHandler(tuiHandler IdleInputHandler) *SmartIdleInputHandler {
	return NewSmartIdleInputHandler(a.llmClient, tuiHandler)
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
		learnedSystems = a.adaptiveLearner.ListSystems(ctx)
	}
	var ragSources []store.RAGSource
	var knowledgeStats *rag.KnowledgeStats
	if a.ragManager != nil {
		ragSources, _ = a.ragManager.ListSources(ctx)
		knowledgeStats, _ = a.ragManager.Stats(ctx)
	}
	systemPrompt := BuildContextual(a.registry.GetEnabled(), infractlMD, servers, a.activeServer, connStates, learnedSystems, ragSources, knowledgeStats)
	if resolvedAlias != "" {
		systemPrompt += fmt.Sprintf(
			"\n\n[LATEST INPUT RESOLUTION]\nlatest user input resolved to SSH server alias <%s>.\nPrioritize this alias for this turn over stale history references.\n",
			resolvedAlias,
		)
	}

	// Phase 7: 도구 루프 진입 전 로컬 벡터 검색 결과를 시스템 프롬프트에 사전 주입
	activeServerName := ""
	if a.activeServer != nil {
		activeServerName = a.activeServer.Name
	}
	if knowledgeCtx := buildKnowledgeContext(ctx, a.ragManager, userInput, activeServerName, a.currentSessionID); knowledgeCtx != "" {
		systemPrompt += "\n\n" + knowledgeCtx
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

	toolDefs := a.registry.ToToolDefs()

	for i := 0; i < a.maxToolLoop; i++ {
		messages := a.buildMessages(systemMsg)

		a.handler.OnThinking()
		resp, err := a.llmClient.ChatStream(ctx, messages, toolDefs, a.handler.OnThinkingToken, a.handler.OnToken)
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
				a.costTracker.Record(ctx, a.modelName, resp.InputTokens, resp.OutputTokens,
					cost.SourceUser, a.currentSessionID)
			}
		}

		if len(resp.ToolCalls) == 0 {
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

	err := fmt.Errorf("max tool loop iterations (%d) exceeded", a.maxToolLoop)
	a.handler.OnError(err)
	return err
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
