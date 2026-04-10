package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourorg/infractl/internal/agent"
	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/mcp"
	"github.com/yourorg/infractl/internal/store"
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
	confirmHandler   *TUIConfirmHandler
	yoloMode         bool
	idle             idleState
	privilege        privilegePromptState

	thinkingLabel string
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
	ConfirmHandler   *TUIConfirmHandler
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
	m.confirmHandler = opts.ConfirmHandler
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
		if strings.HasPrefix(displayInput, "/") {
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

	case ThinkingStartMsg:
		m.thinkingLabel = ThinkingLabel(msg.Tier, msg.Model)
		m.shimmer.SetText(m.thinkingLabel)
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

func (m AppModel) resize() AppModel {
	m.input.setWidth(m.width)
	maxInputH := max(1, min(m.height-5, max(3, m.height/3)))
	m.input.setMaxHeight(maxInputH)
	m.statusBar.setWidth(m.width)
	m.shimmer.width = m.width
	return m
}
