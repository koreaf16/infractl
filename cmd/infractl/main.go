// Package main
// File: main.go
// Description: infractl CLI ?酉?껆뵳?猷?紐낅뱜 獄???뺥닏?뚣끇????브쑨由?
// Responsibility: ??뤵??鈺곌퀡??composition root)????쎈뻬 筌뤴뫀諭?野껉퀣??(TUI / REPL)

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourorg/infractl/internal/agent"
	"github.com/yourorg/infractl/internal/background"
	"github.com/yourorg/infractl/internal/checkpoint"
	"github.com/yourorg/infractl/internal/cli"
	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/cost"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/hooks"
	infrainit "github.com/yourorg/infractl/internal/infrainit"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/mcp"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/schedule"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/subagent"
	"github.com/yourorg/infractl/internal/tools"
	"github.com/yourorg/infractl/internal/tui"
)

const appVersion = "0.5.0"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	filtered := args[:0]
	useREPL := false
	for _, a := range args {
		switch a {
		case "--tui":
			// default interactive mode
		case "--repl":
			useREPL = true
		default:
			filtered = append(filtered, a)
		}
	}

	if len(filtered) == 0 {
		if useREPL {
			return runREPL()
		}
		return runTUI()
	}

	switch filtered[0] {
	case "init":
		return runInit()
	case "version", "--version", "-v":
		fmt.Printf("infractl v%s\n", appVersion)
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	case "daemon":
		fmt.Println("daemon 筌뤴뫀諭??Phase 7?癒?퐣 ?닌뗭겱??몃빍??")
		return nil
	default:
		fmt.Fprintf(os.Stderr, "??????용뮉 筌뤿굝議? %s\n\n", filtered[0])
		printUsage()
		return nil
	}
}

func runInit() error {
	if err := infrainit.Run(context.Background()); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	return nil
}

// deps??buildDeps?? ?????? ???????????.
type deps struct {
	cfg             *config.Config
	serverStore     store.ServerStore
	discoveryStore  store.DiscoveryStore
	connectorStore  store.ConnectorStore
	sessionStore    store.SessionStore
	execLogStore    store.ExecLogStore
	historyStore    store.HistoryStore
	knowledgeStore  store.KnowledgeStore
	learnedSysStore store.LearnedSystemStore
	userToolStore   store.UserToolStore
	ragSourceStore  store.RAGSourceStore
	costStore       store.CostStore
	checkpointStore store.CheckpointStore
	hookStore       store.HookStore
	scheduleStore   store.ScheduleStore

	execMgr   *executor.Manager
	registry  *tools.Registry
	llmClient llm.Client

	llmRegistry *llm.Registry
	llmRouter   *llm.Router

	externalEmbedder rag.EmbeddingGenerator
	memoryService    *rag.MemoryService
	ragManager       *rag.Manager

	connectorMgr *connector.Manager
	mcpClients   []*mcp.Client

	costTracker   *cost.Tracker
	bgManager     *background.Manager
	checkpointMgr *checkpoint.Manager
	hooksMgr      *hooks.Manager
	scheduler     *schedule.Scheduler
}

// buildDeps???⑤벏????뤵?源놁뱽 鈺곌퀡???뺣뼄.
func buildDeps(ctx context.Context) (*deps, error) {
	if !config.Exists() {
		return nil, fmt.Errorf("config file not found; run 'infractl init' first")
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	configDir, err := config.DefaultConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config dir: %w", err)
	}
	dbPath := filepath.Join(configDir, "infractl.db")
	sqliteStore, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open server store: %w", err)
	}

	generalCfg := cfg.GeneralLLM()
	localExec := executor.NewLocalExecutor(time.Duration(generalCfg.Timeout) * time.Second)
	execMgr := executor.NewManager(localExec)

	if err := loadSavedServers(ctx, sqliteStore, execMgr); err != nil {
		slog.Warn("failed to load some saved servers", "err", err)
	}

	// LLM Registry 鈺곌퀡?????怨쀫선癰??????곷섧???源낆쨯
	llmReg := llm.NewRegistry()

	generalClient := llm.NewOpenAIClient(
		generalCfg.Endpoint,
		generalCfg.Model,
		generalCfg.APIKey,
		time.Duration(generalCfg.Timeout)*time.Second,
	)
	llmReg.Register(llm.TierGeneral, generalClient, generalCfg.Model)

	if cfg.Models.Reasoning != nil {
		rc := cfg.Models.Reasoning
		llmReg.Register(llm.TierReasoning,
			llm.NewOpenAIClient(rc.Endpoint, rc.Model, rc.APIKey, time.Duration(rc.Timeout)*time.Second),
			rc.Model,
		)
	}
	if cfg.Models.Fast != nil {
		fc := cfg.Models.Fast
		llmReg.Register(llm.TierFast,
			llm.NewOpenAIClient(fc.Endpoint, fc.Model, fc.APIKey, time.Duration(fc.Timeout)*time.Second),
			fc.Model,
		)
	}

	llmRouter := llm.NewRouter(llmReg)

	// general ?????곷섧?紐? llmClient嚥?????(??륁맄?紐낆넎 ??疫꿸퀣???꾨뗀諭?癰궰野?筌ㅼ뮇???
	llmClient := generalClient

	registry := tools.NewRegistry()
	connectorMgr := connector.NewManager(registry, sqliteStore)
	registerConnectorFactories(connectorMgr)

	// Phase 6: ??????袁㏓럡 筌띲끇??? (??쎄쾿?깆????遺얠젂?醫듼봺: ~/.infractl/scripts)
	scriptsDir := filepath.Join(configDir, "scripts")
	userToolMgr := tools.NewUserToolManager(sqliteStore, registry, scriptsDir)
	bgMgr := background.NewManager()

	// Phase 7: ??? CPU 筌롫뗀?덄뵳?+ ?紐? RAG 野꺜???┛ ??밴쉐
	externalEmbedder := rag.NewEmbeddingGenerator(cfg.Embedding)
	memoryService := rag.NewMemoryService(sqliteStore, sqliteStore, sqliteStore, sqliteStore, externalEmbedder, bgMgr)
	externalSearcher := rag.NewExternalSearcher(execMgr)
	reranker := rag.NewReranker(cfg.Reranker)
	ragMgr := rag.NewManager(memoryService, externalSearcher, sqliteStore, sqliteStore, sqliteStore, externalEmbedder, reranker)

	// Phase 8: 筌ｋ똾寃?????+ ??+ ??뺥닏?癒?뵠?袁る뱜 + ???餓?筌띲끇??? ??밴쉐 (defaultTools??雅뚯눘???袁⑹뒄)
	cpMgr := checkpoint.NewManager(sqliteStore)
	hooksMgr := hooks.NewManager(sqliteStore)
	subRunner := subagent.NewRunner(llmClient, registry, execMgr, nil) // costTracker???袁⑸퓠 雅뚯눘??
	subRunner.SetLLMRegistry(llmReg)                                   // fast ?怨쀫선 ?怨쀪퐨 ????
	subOrchestrator := subagent.NewOrchestrator(subRunner)
	scheduler := schedule.NewScheduler(sqliteStore, func(ctx context.Context, prompt string) (string, error) {
		return "(???餓???쎈뻬?? daemon 筌뤴뫀諭?癒?퐣筌?筌왖?癒?쭢??덈뼄)", nil
	})

	for _, t := range defaultTools(sqliteStore, sqliteStore, sqliteStore, sqliteStore, userToolMgr, execMgr, connectorMgr, llmClient, llmReg, ragMgr, sqliteStore, memoryService, cpMgr, hooksMgr, subOrchestrator, scheduler) {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("register tool: %w", err)
		}
	}

	// Phase 6: ???貫留???????袁㏓럡 嚥≪뮆諭?(?????쎈뱜?귐딅퓠 ??슦????袁㏓럡揶쎛 ?源낆쨯????
	if err := userToolMgr.LoadAll(ctx); err != nil {
		slog.Warn("load user tools", "err", err)
	}

	// ?怨대럡 ???貫留??뚣끇苑??嚥≪뮆諭?
	if err := connectorMgr.LoadSaved(ctx); err != nil {
		slog.Warn("load saved connectors", "err", err)
	}

	// MCP ??뺤쒔 ?怨뚭퍙
	mcpClients := connectMCPServers(ctx, cfg, registry)

	// Phase 8: ??쑴???곕뗄?삥묾???밴쉐
	pricing := make(map[string]cost.ModelPricing)
	for model, p := range cfg.CostPerMTokens {
		pricing[model] = cost.ModelPricing{Input: p.Input, Output: p.Output}
	}
	costTracker := cost.NewTracker(sqliteStore, generalCfg.Model, pricing)

	// Phase 8: 獄쏄퉫???깆뒲???臾믩씜 筌띲끇??? ??밴쉐

	// Phase 8: BackgroundTool ?源낆쨯
	if err := registry.Register(&background.ManageTool{Manager: bgMgr}); err != nil {
		slog.Warn("register background tool", "err", err)
	}

	// Phase 8: ??뺥닏?癒?뵠?袁る뱜 ??쑴???곕뗄?삥묾?雅뚯눘??(costTracker ??밴쉐 ??
	subRunner.SetCostTracker(costTracker)

	// Phase 8: on_connect ???怨뚭퍙
	connectorMgr.SetConnectHook(func(ctx context.Context, server, serviceType string) {
		hooksMgr.Fire(ctx, hooks.HookContext{
			Event:       hooks.EventOnConnect,
			Server:      server,
			ServiceType: serviceType,
		})
	})

	return &deps{
		cfg:              cfg,
		serverStore:      sqliteStore,
		discoveryStore:   sqliteStore,
		connectorStore:   sqliteStore,
		sessionStore:     sqliteStore,
		execLogStore:     sqliteStore,
		historyStore:     sqliteStore,
		knowledgeStore:   sqliteStore,
		learnedSysStore:  sqliteStore,
		userToolStore:    sqliteStore,
		ragSourceStore:   sqliteStore,
		costStore:        sqliteStore,
		checkpointStore:  sqliteStore,
		hookStore:        sqliteStore,
		execMgr:          execMgr,
		registry:         registry,
		llmClient:        llmClient,
		llmRegistry:      llmReg,
		llmRouter:        llmRouter,
		externalEmbedder: externalEmbedder,
		memoryService:    memoryService,
		ragManager:       ragMgr,
		connectorMgr:     connectorMgr,
		mcpClients:       mcpClients,
		costTracker:      costTracker,
		bgManager:        bgMgr,
		checkpointMgr:    cpMgr,
		hooksMgr:         hooksMgr,
		scheduleStore:    sqliteStore,
		scheduler:        scheduler,
	}, nil
}

func runTUI() error {
	ctx := context.Background()

	// TUI 筌뤴뫀諭?癒?퐣??slog??嚥≪뮄?????뵬嚥??귐됰뼄????紐낅립??
	// stderr嚥?筌욊낯???怨뺛늺 bubbletea ?遺얇늺??bleeding??獄쏆뮇源??뺣뼄.
	if logFile, err := openLogFile(); err == nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	d, err := buildDeps(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer d.serverStore.Close()
	defer d.execMgr.Close()
	defer closeMCPClients(d.mcpClients)

	handler := tui.NewTUIHandler()
	ag := agent.New(d.llmClient, d.registry, d.execMgr, handler, d.serverStore)
	ag.SetConnectorManager(d.connectorMgr)
	ag.SetSessionStore(d.sessionStore)
	ag.SetExecLogStore(d.execLogStore)
	// Phase 6: ?癒???덈뮸 ?뚮똾猷??곕뱜 雅뚯눘??
	ag.SetKnowledgeLearner(agent.NewKnowledgeLearner(d.knowledgeStore, d.execLogStore, d.llmClient, d.memoryService))
	ag.SetAdaptiveLearner(agent.NewAdaptiveLearner(d.learnedSysStore))
	// Phase 7: RAG 筌띲끇??? 雅뚯눘??
	ag.SetRAGManager(d.ragManager)
	// 筌렺??LLM ?????쎈뱜??+ ??깆뒭??雅뚯눘??
	ag.SetLLMRegistry(d.llmRegistry)
	ag.SetLLMRouter(d.llmRouter)
	ag.SetBackgroundManager(d.bgManager)
	d.memoryService.SubmitBackfill(ctx)
	if ct, ok := d.registry.Get("session_context"); ok {
		if tool, ok2 := ct.(*tools.SessionContextTool); ok2 {
			tool.ActiveServer = ag.ActiveServerSnapshot
		}
	}
	if rt, ok := d.registry.Get("server_remove"); ok {
		if tool, ok2 := rt.(*tools.ServerRemoveTool); ok2 {
			tool.ActiveServer = ag.ActiveServerSnapshot
			tool.OnActiveServerClear = ag.ClearActiveServer
		}
	}

	if d.sessionStore != nil {
		if id, err := ag.NewSession(ctx); err == nil && id > 0 {
			slog.Debug("tui session created", "id", id)
		}
	}

	// ?紐껋뵬??筌뤴뫀諭? ?뚣끉苑???곌때 writer嚥?IME 鈺곌퀬鍮 ?袁⑺뒄 ?⑥쥙??
	parker := tui.NewCursorParkWriter(os.Stdout)
	// program 筌〓챷???뚢뫂???瑗???tea.NewProgram() ??곸읈??AppModel??雅뚯눘??
	box := tui.NewProgramBox()

	// /server ?щ옒??紐낅졊???몃뱾?? p ?앹꽦 ?꾩뿉 AppOptions???꾨떖?섍퀬 p ?앹꽦 ??SetProgram?쇰줈 珥덇린??
	slashSelectHandler := &tui.TUISelectHandler{}
	// ?뺤씤 ?몃뱾?? p ?앹꽦 ??鍮?援ъ“泥대줈 ?앹꽦, p ?앹꽦 ??SetProgram?쇰줈 珥덇린??(YOLO ?곹깭 怨듭쑀)
	confirmHandler := &tui.TUIConfirmHandler{}
	app := tui.NewAppWithOptions(ag, d.cfg, d.serverStore, d.execMgr, tui.AppOptions{
		InitialSessionID: ag.CurrentSessionID(),
		HistoryStore:     d.historyStore,
		ConnectorMgr:     d.connectorMgr,
		MCPClients:       d.mcpClients,
		SessionStore:     d.sessionStore,
		ExecLogStore:     d.execLogStore,
		CursorParker:     parker,
		ProgramBox:       box,
		SelectHandler:    slashSelectHandler,
		ConfirmHandler:   confirmHandler,
	})
	// WithAltScreen() ??볤탢 ???紐껋뵬??筌뤴뫀諭? tea.WithOutput??곗쨮 ?뚣끉苑???곌때 writer 雅뚯눘??
	p := tea.NewProgram(app, tea.WithOutput(parker))
	box.Set(p) // p.Run() ?袁⑸퓠 ??쇱젟 ??Send()?? ?????怨뺣굡????곸벉
	handler.SetProgram(p)
	ag.SetActiveServerNotifier(func(srv *store.Server) {
		p.Send(tui.ActiveServerMsg{Server: srv})
	})
	// shell_exec OutputCb??tool_exec.go?먯꽌 ?ㅽ뻾 ?쒕쭏??toolID? ?④퍡 二쇱엯??
	// connector ?꾧뎄???대쫫 異⑸룎 disambiguation UI ?곌껐
	selectHandler := tui.NewTUISelectHandler(p)
	slashSelectHandler.SetProgram(p) // 吏??珥덇린?? p ?앹꽦 ??/server ?щ옒??紐낅졊 ?몃뱾?ъ뿉 p 二쇱엯
	disambig := &tuiDisambiguateAdapter{h: selectHandler}
	if at, ok := d.registry.Get("connector_activate"); ok {
		if tool, ok2 := at.(*connector.ActivateTool); ok2 {
			tool.Disambiguate = disambig
		}
	}
	if oat, ok := d.registry.Get("connector_probe_os_auth"); ok {
		if tool, ok2 := oat.(*connector.OSAuthProbeTool); ok2 {
			tool.Disambiguate = disambig
		}
	}
	// server_focus ?꾧뎄???쒖꽦 ?쒕쾭 肄쒕갚 + ?좏깮 UI ?곌껐
	if ft, ok := d.registry.Get("server_focus"); ok {
		if tool, ok2 := ft.(*tools.ServerFocusTool); ok2 {
			tool.OnChange = func(srv *store.Server) {
				if srv == nil {
					ag.ClearActiveServer()
				} else {
					ag.SetActiveServer(*srv)
				}
			}
			tool.SelectFn = tuiFocusSelectAdapter(selectHandler)
		}
	}
	// subagent_analyze ?꾧뎄???ㅼ떆媛??대깽??肄쒕갚 ?곌껐
	if at, ok := d.registry.Get("subagent_analyze"); ok {
		if analyzeTool, ok2 := at.(*subagent.AnalyzeTool); ok2 {
			analyzeTool.EventCb = handler.SubagentEventCallback()
		}
	}
	// Phase 5: TUI ?뺤씤 ?몃뱾??program ?ㅼ젙 ???먯씠?꾪듃???깅줉
	confirmHandler.SetProgram(p)
	confirmHandler.SetSelectHandler(selectHandler)
	ag.SetConfirmationHandler(confirmHandler)
	ag.SetIdleInputHandler(agent.NewSmartIdleInputHandler(d.llmClient, tui.NewTUIIdleInputHandler(p)))

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui run: %w", err)
	}
	return nil
}

func runREPL() error {
	if !config.Exists() {
		fmt.Fprintln(os.Stderr, "??쇱젟 ???뵬????곷뮸??덈뼄. ?믪눘? 'infractl init'????쎈뻬??뤾쉭??")
		os.Exit(1)
	}

	ctx := context.Background()
	d, err := buildDeps(ctx)
	if err != nil {
		return err
	}
	defer d.serverStore.Close()
	defer d.execMgr.Close()
	defer closeMCPClients(d.mcpClients)

	// ?귐딇뒄 ???쐭筌? RichHandler揶쎛 EventHandler ??釉?
	servers, _ := d.serverStore.List(ctx)
	state := tui.NewSessionState(d.cfg.GeneralLLM().Model, len(servers))
	richHandler := tui.NewRichHandler(state)

	handler := &cli.REPLHandler{} // ??媛???醫?
	ag := agent.New(d.llmClient, d.registry, d.execMgr, richHandler, d.serverStore)
	ag.SetConnectorManager(d.connectorMgr)
	// RichHandler揶쎛 ConfirmationHandler???닌뗭겱
	ag.SetConfirmationHandler(richHandler)
	ag.SetIdleInputHandler(agent.NewSmartIdleInputHandler(d.llmClient, nil))
	ag.SetSessionStore(d.sessionStore)
	ag.SetExecLogStore(d.execLogStore)
	// Phase 6: ?癒???덈뮸 ?뚮똾猷??곕뱜 雅뚯눘??
	ag.SetKnowledgeLearner(agent.NewKnowledgeLearner(d.knowledgeStore, d.execLogStore, d.llmClient, d.memoryService))
	ag.SetAdaptiveLearner(agent.NewAdaptiveLearner(d.learnedSysStore))
	// Phase 7: RAG 筌띲끇??? 雅뚯눘??
	ag.SetRAGManager(d.ragManager)
	// 筌렺??LLM ?????쎈뱜??+ ??깆뒭??雅뚯눘??
	ag.SetLLMRegistry(d.llmRegistry)
	ag.SetLLMRouter(d.llmRouter)
	// Phase 8: ??쑴???곕뗄?삥묾?+ 獄쏄퉫???깆뒲??筌띲끇??? + 筌ｋ똾寃?????+ ??雅뚯눘??
	ag.SetCostTracker(d.costTracker)
	ag.SetModelName(d.cfg.GeneralLLM().Model)
	ag.SetBackgroundManager(d.bgManager)
	ag.SetCheckpointManager(d.checkpointMgr)
	ag.SetHooksManager(d.hooksMgr)
	if ct, ok := d.registry.Get("session_context"); ok {
		if tool, ok2 := ct.(*tools.SessionContextTool); ok2 {
			tool.ActiveServer = ag.ActiveServerSnapshot
		}
	}
	if rt, ok := d.registry.Get("server_remove"); ok {
		if tool, ok2 := rt.(*tools.ServerRemoveTool); ok2 {
			tool.ActiveServer = ag.ActiveServerSnapshot
			tool.OnActiveServerClear = ag.ClearActiveServer
		}
	}
	d.bgManager.SetNotifyFunc(richHandler.OnJobComplete)
	ag.SetActiveServerNotifier(func(srv *store.Server) {
		if srv == nil {
			fmt.Println("[active server] cleared")
			return
		}
		fmt.Printf("[active server] %s (%s:%d)\n", srv.Name, srv.Host, srv.Port)
	})
	if ft, ok := d.registry.Get("server_focus"); ok {
		if tool, ok2 := ft.(*tools.ServerFocusTool); ok2 {
			tool.OnChange = func(srv *store.Server) {
				if srv == nil {
					ag.ClearActiveServer()
					return
				}
				ag.SetActiveServer(*srv)
			}
		}
	}

	repl := cli.NewPromptREPL(ag, handler, d.cfg, d.serverStore, d.execMgr)
	repl.SetConnectorManager(d.connectorMgr)
	repl.SetMCPClients(d.mcpClients)
	repl.SetSessionStore(d.sessionStore)
	repl.SetExecLogStore(d.execLogStore)
	repl.SetHistoryStore(d.historyStore)
	repl.SetKnowledgeStore(d.knowledgeStore)
	repl.SetRAGSourceStore(d.ragSourceStore)
	repl.SetCostTracker(d.costTracker)
	repl.SetCheckpointManager(d.checkpointMgr)
	repl.SetHooksManager(d.hooksMgr)
	repl.SetScheduleManager(d.scheduler)
	d.memoryService.SubmitBackfill(ctx)
	repl.SetRichHandler(richHandler, state)
	return repl.Run(ctx)
}

func printUsage() {
	fmt.Printf("infractl v%s - AI infrastructure management CLI\n\n", appVersion)
	fmt.Println("Usage:")
	fmt.Println("  infractl           Run TUI mode")
	fmt.Println("  infractl --repl    Run REPL mode")
	fmt.Println("  infractl init      Initialize configuration")
	fmt.Println("  infractl version   Show version")
	fmt.Println("  infractl help      Show help")
	fmt.Println("  infractl daemon    Run daemon mode (Phase 7)")
}
