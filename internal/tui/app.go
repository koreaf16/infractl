package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourorg/infractl/internal/agent"
	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/mcp"
	"github.com/yourorg/infractl/internal/privilege"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

// AppModel is the main Bubble Tea application model.
type AppModel struct {
	chat       chatView
	input      inputBar
	statusBar  statusBar
	ag         *agent.Agent
	store      store.ServerStore
	manager    *executor.Manager
	cfg        *config.Config
	ctx        context.Context
	cancel     context.CancelFunc
	width      int
	height     int
	busy       bool
	ctrlCCount int

	shimmer  shimmerState
	progress *progressTree
	sp       spinner.Model

	activeTools activeToolMap
	queue       inputQueue
	selection   selectionState

	selectHandler    *TUISelectHandler
	activeServer     *store.Server
	connectorMgr     *connector.Manager
	mcpClients       []*mcp.Client
	sessionStore     store.SessionStore
	execLogStore     store.ExecLogStore
	currentSessionID int64
	yoloMode         bool
	planMode         bool
	privilege        privilegePromptState

	knowledgeStore store.KnowledgeStore
	ragSourceStore store.RAGSourceStore
	costTracker    CostTracker
	checkpointMgr  CheckpointManager
	hooksMgr       HooksManager
	scheduleMgr    ScheduleManager
	privCache      *privilege.Cache

	thinkingLabel string
	thinkBuf      string // thinking 스트리밍 누적 버퍼 (shimmer hint 업데이트용)
	stats         turnStats
	turnCount     int // 지금까지 시작된 턴 수 (구분선 출력 시점 판단용)

	history      toolHistory
	histOverlay  toolOverlayState

	box          *ProgramBox
	parker       *CursorParkWriter
	mdRend       *mdRenderer
	streamTokens string
	streamLines  []string
	streamCache  stableCache
	lastStreamAt time.Time
}

// AppOptions configures optional integrations for the app.
type AppOptions struct {
	InitialSessionID int64
	HistoryStore     store.HistoryStore
	ConnectorMgr     *connector.Manager
	MCPClients       []*mcp.Client
	SessionStore     store.SessionStore
	ExecLogStore     store.ExecLogStore
	CursorParker     *CursorParkWriter
	ProgramBox       *ProgramBox
	SelectHandler    *TUISelectHandler

	KnowledgeStore store.KnowledgeStore
	RAGSourceStore store.RAGSourceStore
	CostTracker    CostTracker
	CheckpointMgr  CheckpointManager
	HooksMgr       HooksManager
	ScheduleMgr    ScheduleManager
	PrivCache      *privilege.Cache
}

// NewApp constructs the base TUI app.
func NewApp(ag *agent.Agent, cfg *config.Config, st store.ServerStore, mgr *executor.Manager) AppModel {
	servers, _ := st.List(context.Background())
	ctx, cancel := context.WithCancel(context.Background())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = StyleSpinner

	return AppModel{
		chat:      newChatView(80, 20),
		input:     newInputBar(80),
		statusBar: newStatusBar(cfg.LLM.Model, len(servers)),
		shimmer:      newShimmer(),
		progress:     newProgressTree(),
		sp:        sp,
		ag:        ag,
		store:     st,
		manager:   mgr,
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		mdRend:    newMdRenderer(74),
	}
}

// NewAppWithOptions constructs the app with optional integrations enabled.
func NewAppWithOptions(
	ag *agent.Agent,
	cfg *config.Config,
	st store.ServerStore,
	mgr *executor.Manager,
	opts AppOptions,
) AppModel {
	m := NewApp(ag, cfg, st, mgr)
	m.currentSessionID = opts.InitialSessionID
	m.input.SetHistoryStore(opts.HistoryStore, opts.InitialSessionID)
	if opts.HistoryStore != nil {
		if entries, err := opts.HistoryStore.ListPromptHistory(context.Background(), 200); err == nil {
			m.input.LoadHistory(entries)
		}
	}
	m.connectorMgr = opts.ConnectorMgr
	m.mcpClients = opts.MCPClients
	m.sessionStore = opts.SessionStore
	m.execLogStore = opts.ExecLogStore
	m.parker = opts.CursorParker
	m.box = opts.ProgramBox
	m.selectHandler = opts.SelectHandler
	m.knowledgeStore = opts.KnowledgeStore
	m.ragSourceStore = opts.RAGSourceStore
	m.costTracker = opts.CostTracker
	m.checkpointMgr = opts.CheckpointMgr
	m.hooksMgr = opts.HooksMgr
	m.scheduleMgr = opts.ScheduleMgr
	m.privCache = opts.PrivCache

	// Agent에 자신(QuestionHandler 구현체)을 등록합니다.
	m.ag.SetQuestionHandler(m)

	return m
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnableBracketedPaste,
		tea.ShowCursor,
		initialWindowSizeCmd(m.parker),
		m.input.Init(),
	)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case ShimmerTickMsg:
		if m.busy {
			if cmd := m.shimmer.Tick(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case UsageUpdateMsg:
		m.statusBar.updateUsage(msg)
		m.stats.AddTokens(msg.InputTokens, msg.OutputTokens)
		return m, nil

	case tea.WindowSizeMsg:
		firstResize := m.width == 0
		m.width = msg.Width
		m.height = msg.Height
		m.mdRend = newMdRenderer(m.width - 6)
		m.streamCache.Reset()
		if m.streamTokens != "" {
			m.streamLines = renderStreamingPreview(m.streamTokens, m.mdRend, &m.streamCache, 0)
		}
		m = m.resize()
		if firstResize && m.box != nil {
			servers, _ := m.store.List(context.Background())
			m.box.Println(welcomeBanner(m.cfg.LLM.Model, len(servers)))
		}
		return m, nil

	case tea.MouseMsg:
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case SubmitMsg:
		displayInput := msg.DisplayInput
		expandedInput := msg.ExpandedInput
		if IsSlashCommand(displayInput) {
			return m.handleSlashCommand(displayInput)
		}
		if m.busy {
			m.queue.Enqueue(displayInput, expandedInput)
			if m.box != nil {
				m.box.Println(renderSystemLine(
					fmt.Sprintf("queued [%d]: %s", m.queue.Len(), truncateForQueue(displayInput, 60))))
			}
			return m, nil
		}
		if m.sessionStore != nil && m.currentSessionID == 0 {
			cmds = append(cmds, m.createSessionCmd(displayInput))
		} else {
			m.ag.UpdateSessionTitle(m.ctx, displayInput)
		}
		m.busy = true
		m.activeTools.Clear()
		m.progress.Reset()
		m.stats.Start()
		if m.box != nil {
			if m.turnCount > 0 {
				m.box.Println(renderTurnSeparator(m.width))
			}
			m.box.Println(renderUserInputLine(displayInput, m.width))
		}
		m.turnCount++
		label := m.thinkingLabel
		if label == "" {
			label = "thinking..."
		}
		cmds = append(cmds, m.runAgent(expandedInput), m.sp.Tick, m.shimmer.Start(label))
		return m, tea.Batch(cmds...)

	case SystemMsg:
		if m.box != nil {
			m.box.Println(renderSystemLine(string(msg)))
		}
		return m, nil

	case TokenMsg:
		m.shimmer.RecordActivity()
		m.streamTokens += string(msg)
		if time.Since(m.lastStreamAt) > 150*time.Millisecond {
			m.streamLines = renderStreamingPreview(m.streamTokens, m.mdRend, &m.streamCache, 0)
			m.lastStreamAt = time.Now()
		}
		return m, nil

	case ThinkingTokenMsg:
		m.shimmer.RecordActivity()
		m.thinkBuf += string(msg)
		m.shimmer.SetHint(thinkHint(m.thinkBuf, 60))
		return m, nil

	case ThinkingStartMsg:
		m.thinkingLabel = ThinkingLabel(msg.Tier, msg.Model)
		m.shimmer.SetText(m.thinkingLabel)
		// 새 LLM 호출 시 이전 thinking 버퍼/힌트 초기화
		m.thinkBuf = ""
		m.shimmer.SetHint("")
		return m, nil
	}

	if next, cmd, handled := m.handleSystemMsg(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.handleToolMsg(msg); handled {
		return next, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	m = m.resize()
	return m, tea.Batch(cmds...)
}

// RequestQuestion은 QuestionHandler 인터페이스를 구현합니다.
func (m AppModel) RequestQuestion(ctx context.Context, req tools.QuestionRequest) (tools.QuestionResponse, error) {
	if m.selectHandler == nil {
		return tools.QuestionResponse{SelectedIndex: -1}, fmt.Errorf("selection handler unavailable")
	}

	opts := make([]SelectOption, len(req.Options))
	for i, opt := range req.Options {
		opts[i] = SelectOption{
			Label:       opt.Label,
			Description: opt.Description,
			HideOther:   false, // [Other] 입력을 기본적으로 허용합니다.
		}
	}

	res, err := m.selectHandler.RequestSelectCtx(ctx, req.Question, opts)
	if err != nil {
		return tools.QuestionResponse{SelectedIndex: -1}, err
	}

	return tools.QuestionResponse{
		SelectedIndex: res.Index,
		SelectedLabel: res.Label,
		IsOther:       res.IsOther,
	}, nil
}

func (m AppModel) resize() AppModel {
	m.input.setWidth(m.width)
	maxInputH := max(1, min(m.height-5, max(3, m.height/3)))
	m.input.setMaxHeight(maxInputH)
	m.statusBar.setWidth(m.width)
	m.shimmer.width = m.width
	return m
}
