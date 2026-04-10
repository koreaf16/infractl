// Package tui
// File: app.go
// Description: bubbletea 메인 앱 모델 — TUI의 루트 컴포넌트
// Responsibility: 전체 레이아웃 관리, 이벤트 라우팅, 에이전트 goroutine 실행

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

// AppModel은 bubbletea 메인 앱 모델이다.
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

	// Claude CLI 스타일 shimmer 애니메이션
	shimmer  shimmerState
	progress *progressTree // 도구 실행 진행 트리

	// context line (입력바 위 상태 표시)
	sp          spinner.Model
	activeTools activeToolMap // 병렬 실행 중인 도구 상태 맵 (toolID 기반)

	// 메시지 큐 (busy 중 추가 입력을 순서대로 보관)
	queue inputQueue
	// 인터랙티브 셀렉션 컴포넌트
	selection selectionState

	selectHandler    *TUISelectHandler  // /server 슬래시 명령 선택 UI용
	activeServer     *store.Server      // 현재 접속된 타겟 서버
	connectorMgr     *connector.Manager // Phase 5: /connectors 표시용
	mcpClients       []*mcp.Client      // Phase 5: /mcp 런타임 상태용
	sessionStore     store.SessionStore // Phase 5: 세션 영속화
	execLogStore     store.ExecLogStore // Phase 5: 실행 이력 조회
	currentSessionID int64              // Phase 5: 현재 세션 ID
	confirm          confirmState        // Phase 5: 확인 오버레이 상태
	confirmHandler   *TUIConfirmHandler  // YOLO 모드 활성화를 위한 핸들러 참조
	yoloMode         bool                // true이면 확인 요청을 자동 승인
	idle             idleState           // 쉘 명령 유휴 입력 오버레이 상태

	// 턴 통계
	stats turnStats

	// 도구 이력 (Ctrl+O 오버레이)
	history    toolHistory
	histOverlay toolOverlayState

	// 인라인 모드 출력
	box          *ProgramBox       // Program.Println()으로 채팅 스크롤백 출력
	parker       *CursorParkWriter // 커서 파킹 (nil이면 비활성)
	mdRend       *mdRenderer       // 스크롤백/스트리밍 마크다운 렌더링
	streamTokens string      // 스트리밍 중 누적 토큰
	streamLines  []string   // View()에 표시할 스트리밍 미리보기 줄
	streamCache  stableCache // 스트리밍 마크다운 안정적 캐시
	lastStreamAt time.Time   // 스트리밍 미리보기 마지막 렌더 시각
}

// AppOptions는 NewApp에 전달되는 선택적 의존성이다.
type AppOptions struct {
	InitialSessionID int64
	HistoryStore     store.HistoryStore
	ConnectorMgr     *connector.Manager
	MCPClients       []*mcp.Client
	SessionStore     store.SessionStore
	ExecLogStore     store.ExecLogStore
	CursorParker     *CursorParkWriter  // 인라인 모드 커서 파킹 (nil이면 비활성)
	ProgramBox       *ProgramBox        // 인라인 모드 Println 출력 (nil이면 비활성)
	SelectHandler    *TUISelectHandler  // /server 슬래시 명령 선택 UI용 (nil이면 비활성)
	ConfirmHandler   *TUIConfirmHandler // YOLO 모드 공유를 위한 확인 핸들러 (nil이면 비활성)
}

// NewApp은 새 AppModel을 생성한다.
func NewApp(
	ag *agent.Agent,
	cfg *config.Config,
	st store.ServerStore,
	mgr *executor.Manager,
) AppModel {
	servers, _ := st.List(context.Background())
	ctx, cancel := context.WithCancel(context.Background())

	chat := newChatView(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = StyleSpinner

	return AppModel{
		chat:      chat,
		input:     newInputBar(80),
		statusBar: newStatusBar(cfg.LLM.Model, len(servers)),
		shimmer:   newShimmer(),
		progress:  newProgressTree(),
		sp:        sp,
		ag:        ag,
		store:     st,
		manager:   mgr,
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		mdRend:    newMdRenderer(74), // 초기 폭 (WindowSizeMsg 수신 시 갱신)
	}
}

// NewAppWithOptions는 Phase 5 의존성을 포함하여 AppModel을 생성한다.
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
		tea.ShowCursor, // 인라인 모드: BubbleTea가 기본으로 숨기는 커서를 다시 표시
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
			cmd := m.shimmer.Tick()
			if cmd != nil {
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
		// 첫 resize 시 배너를 스크롤백에 출력
		if firstResize {
			servers, _ := m.store.List(context.Background())
			m.box.Println(welcomeBanner(m.cfg.LLM.Model, len(servers)))
		}

	case tea.MouseMsg:
		// 인라인 모드에서는 터미널 네이티브 스크롤 사용
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
			// 실행 중: 큐에 적재
			m.queue.Enqueue(displayInput, expandedInput)
			if m.box != nil {
				m.box.Println(renderSystemLine(
					fmt.Sprintf("⏳ 대기열 추가 [%d]: %s", m.queue.Len(), truncateForQueue(displayInput, 60))))
			}
			return m, nil
		}
		// idle: 즉시 실행
		if m.sessionStore != nil && m.currentSessionID == 0 {
			cmds = append(cmds, m.createSessionCmd(displayInput))
		} else {
			m.ag.UpdateSessionTitle(m.ctx, displayInput)
		}
		m.busy = true
		m.activeTools.Clear()
		m.progress.Reset()
		m.stats.Start()
		m.box.Println(renderUserInputLine(displayInput))
		shimmerCmd := m.shimmer.Start("thinking...")
		cmds = append(cmds, m.runAgent(expandedInput), m.sp.Tick, shimmerCmd)
		return m, tea.Batch(cmds...)

	case SystemMsg:
		if m.box != nil {
			m.box.Println(renderSystemLine(string(msg)))
		}

	case TokenMsg:
		m.shimmer.RecordActivity()
		m.streamTokens += string(msg)
		if time.Since(m.lastStreamAt) > 150*time.Millisecond {
			m.streamLines = renderStreamingPreview(m.streamTokens, m.mdRend, &m.streamCache, 0)
			m.lastStreamAt = time.Now()
		}

	case ThinkingTokenMsg:
		m.shimmer.RecordActivity()
		// AppModel(인라인 모드)은 추론 토큰을 직접 출력하지 않고
		// shimmer 활동만 기록하여 지연 감지 방지

	}

	// 도구 이벤트 위임 (ToolStart/ShellOutput/ToolEnd/ResponseDone/Error/AgentDone/SubagentEvent)
	if m2, cmd, handled := m.handleToolMsg(msg); handled {
		return m2, cmd
	}

	// 시스템 이벤트 위임 (Confirm/IdleInput/Select/ActiveServer)
	if m2, cmd, handled := m.handleSystemMsg(msg); handled {
		return m2, cmd
	}

	// 채팅창 업데이트 (스피너 애니메이션 등)
	chatCmd := m.chat.Update(msg)
	if chatCmd != nil {
		cmds = append(cmds, chatCmd)
	}

	return m, tea.Batch(cmds...)
}
