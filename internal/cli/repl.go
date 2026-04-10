// Package cli
// File: repl.go
// Description: readline 기반 대화형 REPL 및 CLI 이벤트 핸들러 구현
// Responsibility: 사용자 입력 처리, 슬래시 명령 라우팅, 에이전트 이벤트의 터미널 출력

package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/chzyer/readline"
	"github.com/yourorg/infractl/internal/agent"
	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/mcp"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tui"
	"golang.org/x/term"
)

const version = "0.1.0"

// REPL은 표준 입출력 기반의 순수 CLI 대화형 인터페이스이다.
type REPL struct {
	agent        *agent.Agent
	handler      *REPLHandler
	richHandler  *tui.RichHandler // 리치 렌더링용 (nil이면 기본 StreamModel)
	state        *tui.SessionState
	cfg          *config.Config
	store        store.ServerStore
	manager      *executor.Manager
	connectorMgr *connector.Manager  // Phase 4: /connectors 명령용
	mcpClients   []*mcp.Client       // Phase 5: /mcp 런타임 상태 표시용
	sessionStore   store.SessionStore   // Phase 5: /sessions 명령용
	execLogStore   store.ExecLogStore   // Phase 5: /history 명령용
	historyStore   store.HistoryStore   // Phase 5: 프롬프트 히스토리
	knowledgeStore store.KnowledgeStore // Phase 6: /knowledge 명령용
	ragSourceStore store.RAGSourceStore // Phase 7: /rag 명령용
	costTracker    CostTracker          // Phase 8: /cost 명령용
	checkpointMgr  CheckpointManager   // Phase 8: /checkpoints 명령용
	hooksManager   HooksManager        // Phase 8: /hooks 명령용
	scheduleMgr    ScheduleManager     // Phase 8: /schedules 명령용
}

// NewREPL은 REPL 인스턴스를 생성한다.
func NewREPL(ag *agent.Agent, handler *REPLHandler, cfg *config.Config, st store.ServerStore, mgr *executor.Manager) (*REPL, error) {
	return &REPL{agent: ag, handler: handler, cfg: cfg, store: st, manager: mgr}, nil
}

// SetConnectorManager는 커넥터 매니저를 주입한다.
func (r *REPL) SetConnectorManager(mgr *connector.Manager) {
	r.connectorMgr = mgr
}

// SetMCPClients는 MCP 클라이언트 목록을 주입한다 (런타임 상태 표시용).
func (r *REPL) SetMCPClients(clients []*mcp.Client) {
	r.mcpClients = clients
}

// SetSessionStore는 세션 저장소를 주입한다.
func (r *REPL) SetSessionStore(s store.SessionStore) {
	r.sessionStore = s
}

// SetExecLogStore는 실행 이력 저장소를 주입한다.
func (r *REPL) SetExecLogStore(s store.ExecLogStore) {
	r.execLogStore = s
}

// SetHistoryStore는 프롬프트 히스토리 저장소를 주입한다.
func (r *REPL) SetHistoryStore(s store.HistoryStore) {
	r.historyStore = s
}

// SetKnowledgeStore는 지식 저장소를 주입한다.
func (r *REPL) SetKnowledgeStore(s store.KnowledgeStore) {
	r.knowledgeStore = s
}

// SetRichHandler는 리치 렌더링 핸들러를 주입한다.
func (r *REPL) SetRichHandler(h *tui.RichHandler, state *tui.SessionState) {
	r.richHandler = h
	r.state = state
}

// Run은 REPL 루프를 실행한다.
func (r *REPL) Run(ctx context.Context) error {
	// 리치 모드면 tui 스타일 배너, 아니면 기존 배너
	if r.state != nil {
		tui.PrintBanner(r.state)
	} else {
		printBanner(r.cfg)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// readline 프롬프트: 순수 텍스트만 (ANSI 없음 — Windows readline ANSI panic 방지)
	prompt := "> "
	// 자동완성: 슬래시 명령 + 서버명
	var serverNames []string
	if servers, err := r.store.List(ctx); err == nil {
		for _, s := range servers {
			serverNames = append(serverNames, s.Name)
		}
	}
	completer := tui.NewDynamicCompleter(serverNames)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:            prompt,
		HistoryFile:       "",
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
		AutoComplete:      completer,
	})
	if err != nil {
		return fmt.Errorf("init readline: %w", err)
	}
	defer rl.Close()

	// 기존 히스토리 로드
	if r.historyStore != nil {
		if entries, err := r.historyStore.ListPromptHistory(ctx, 1000); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				rl.SaveHistory(entries[i].Input)
			}
		}
	}

	// 첫 실행 시 새 세션 생성
	if r.sessionStore != nil {
		if id, err := r.agent.NewSession(ctx); err == nil && id > 0 {
			slog.Debug("repl session created", "id", id)
		}
	}

	for {
		// 리치 모드: 입력 전 상단 border 출력
		if r.state != nil {
			tui.PrintInputBorderTop(r.state.Width)
		}
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				fmt.Println("\n종료합니다.")
				return nil
			}
			continue
		} else if err == io.EOF {
			fmt.Println("\n종료합니다.")
			return nil
		} else if err != nil {
			return fmt.Errorf("입력 오류: %w", err)
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if r.historyStore != nil {
			r.historyStore.SavePromptHistory(ctx, input, r.agent.CurrentSessionID())
		}

		if strings.HasPrefix(input, "/") {
			if r.handleSlashCommand(ctx, input) {
				return nil
			}
			continue
		}

		// Phase 5: 첫 입력으로 세션 제목 업데이트
		r.agent.UpdateSessionTitle(ctx, input)

		// 유저 입력 헤더 출력
		if r.state != nil {
			tui.PrintUserHeader(input)
		}

		runCtx, runCancel := context.WithCancel(ctx)

		// 리치 모드면 RichModel, 아니면 기존 StreamModel
		var done chan struct{}
		if r.richHandler != nil {
			done = r.richHandler.StartTurn(runCtx)
		} else {
			done = r.handler.PrepareStream()
		}

		err = r.agent.Run(runCtx, input)
		runCancel()

		// 리치 모드: FinishTurn이 상태바 출력 + done 채널 닫기
		if r.richHandler != nil {
			r.richHandler.FinishTurn()
		} else {
			<-done
		}

		if err != nil {
			slog.Error("agent run error", "err", err)
		}
		fmt.Println()
	}
}

// handleSlashCommand는 슬래시 명령을 처리한다. true를 반환하면 종료를 의미한다.
func (r *REPL) handleSlashCommand(ctx context.Context, input string) bool {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/quit", "/exit", "/q":
		fmt.Println("종료합니다.")
		return true
	case "/help", "/h":
		printHelp()
	case "/tools":
		rawTools := r.agent.Tools()
		infos := make([]toolInfo, len(rawTools))
		for i, t := range rawTools {
			infos[i] = toolInfo{name: t.Name(), description: t.Description()}
		}
		printTools(infos)
	case "/clear":
		r.agent.ClearHistory()
		fmt.Println("✓ 대화 히스토리를 초기화했습니다.")
	case "/model":
		printModelInfo(r.cfg)
	case "/servers":
		r.handleServersCommand(ctx, parts)
	case "/connectors":
		r.printConnectors()
	case "/mcp":
		if len(parts) >= 3 && parts[1] == "reconnect" {
			r.reconnectMCP(ctx, parts[2])
		} else {
			r.printMCPStatus()
		}
	case "/sessions":
		if len(parts) >= 3 && parts[1] == "restore" {
			n, err := strconv.Atoi(parts[2])
			if err != nil {
				fmt.Println("사용법: /sessions restore <번호>")
			} else {
				r.restoreSession(ctx, n)
			}
		} else {
			r.printSessions(ctx)
		}
	case "/yoro":
		active := r.agent.ToggleYOROMode()
		if active {
			fmt.Println("⚡ YORO Mode 활성화 — 확인 없이 바로 실행합니다. 백업/체크포인트는 유지됩니다.")
			fmt.Println("  다시 /yoro 입력 시 비활성화됩니다.")
		} else {
			fmt.Println("✓ YORO Mode 비활성화 — 정상 확인 흐름으로 복귀합니다.")
		}
	case "/auto-confirm":
		r.handleAutoConfirm()
	case "/history":
		r.printHistory(ctx)
	case "/knowledge":
		r.handleKnowledgeCommand(ctx, parts)
	case "/rag":
		r.handleRAGCommand(ctx, parts)
	case "/cost":
		r.handleCostCommand(ctx, parts)
	case "/checkpoints":
		r.handleCheckpointsCommand(ctx, parts)
	case "/hooks":
		r.handleHooksCommand(ctx, parts)
	case "/schedules":
		r.handleSchedulesCommand(ctx, parts)
	default:
		fmt.Printf("알 수 없는 명령: %s\n/help를 입력하면 사용 가능한 명령을 확인할 수 있습니다.\n", cmd)
	}
	return false
}

// REPLHandler는 agent.EventHandler를 구현하여 터미널에 에이전트 이벤트를 스트리밍한다.
type REPLHandler struct {
	prog *tea.Program
}

// RequestConfirm은 위험 작업 실행 전 터미널에서 사용자 확인을 받는다.
// agent.ConfirmationHandler 인터페이스를 구현한다.
func (h *REPLHandler) RequestConfirm(ctx context.Context, req agent.ConfirmRequest) (agent.ConfirmResponse, error) {
	// 현재 streamer가 동작 중이면 일시 중단
	if h.prog != nil {
		h.prog.Send(DoneMsg{})
	}

	icon := "⚠️"
	if req.RiskLevel == "high" {
		icon = "⚠️⚠️⚠️ 고위험"
	}
	fmt.Printf("\n%s [%d/%d] %s\n", icon, req.Step, req.TotalSteps, req.Description)

	if req.NeedInput {
		fmt.Printf("대상 이름을 직접 입력하세요 (취소: Enter): ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		input := strings.TrimSpace(line)
		if input == "" {
			return agent.ConfirmResponse{Confirmed: false}, nil
		}
		return agent.ConfirmResponse{Confirmed: true, UserInput: input}, nil
	}

	fmt.Print("계속하시겠습니까? (y/n): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return agent.ConfirmResponse{Confirmed: answer == "y" || answer == "yes"}, nil
}

// PrepareStream은 에이전트 실행 시 터미널을 스트리밍 렌더러로 전환한다.
func (h *REPLHandler) PrepareStream() chan struct{} {
	m := NewStreamModel()
	h.prog = tea.NewProgram(m)
	done := make(chan struct{})
	go func() {
		if _, err := h.prog.Run(); err != nil {
			slog.Error("streamer error", "err", err)
		}
		close(done)
	}()
	return done
}

func (h *REPLHandler) OnThinking() {
	if h.prog != nil {
		h.prog.Send(ThinkingMsg{})
	}
}

func (h *REPLHandler) OnThinkingToken(token string) {
	if h.prog != nil {
		h.prog.Send(ThinkingTokenMsg(token))
	}
}

func (h *REPLHandler) OnToken(token string) {
	if h.prog != nil {
		h.prog.Send(TokenMsg(token))
	}
}

func (h *REPLHandler) OnToolStart(_ /*toolID*/, toolName string, target string, _ map[string]interface{}) {
	if h.prog != nil {
		msg := toolName
		if target != "" && target != "localhost" {
			msg = toolName + " → " + target
		}
		h.prog.Send(ToolMsg(msg))
	}
}

func (h *REPLHandler) OnToolOutput(_ /*toolID*/, _ string) {}

func (h *REPLHandler) OnToolEnd(_ /*toolID*/, _ string, _ string, _ time.Duration, _ bool) {}

func (h *REPLHandler) OnResponse(_ string) {
	if h.prog != nil {
		h.prog.Send(DoneMsg{})
	}
}

func (h *REPLHandler) OnError(err error) {
	if h.prog != nil {
		h.prog.Send(ErrorMsg{Err: err})
	}
}

func (h *REPLHandler) OnUsageUpdate(_, _ int, _ float64, _ int64) {
	// REPL 모드에서는 사용하지 않음
}

func (h *REPLHandler) OnJobComplete(jobID int, description string, success bool) {
	icon := "✓"
	if !success {
		icon = "✗"
	}
	fmt.Printf("\n%s 백그라운드 작업 #%d 완료: %s\n", icon, jobID, description)
}

// handleAutoConfirm은 /auto-confirm 슬래시 명령을 처리한다.
// 리치 선택 UI로 인터랙티브 프롬프트 자동 처리 방식을 선택하고 에이전트에 적용한다.
func (r *REPL) handleAutoConfirm() {
	width := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}

	// 패턴 핸들러에서 감지 패턴 예시 수집
	patternHandler := agent.NewPatternIdleInputHandler(nil)
	examples := patternHandler.PatternExamples()
	patternPreview := strings.Join(examples, "\n")

	options := []tui.SelectOption{
		{
			Label:       "패턴 매칭 자동응답",
			Description: "정규식으로 공통 프롬프트 감지 후 즉시 응답",
			Tag:         "Recommended",
			Preview:     "감지 패턴 목록:\n\n" + patternPreview + "\n\n패턴 미일치 시 → 중단(abort)",
			HideOther:   true,
		},
		{
			Label:       "LLM 판단 위임",
			Description: "LLM이 프롬프트 내용을 분석해 y/n/enter 결정",
			Preview:     "LLM이 마지막 출력 라인을 읽고\n자동으로 응답을 결정합니다.\n\n응답 불가 시 → 터미널 입력 요청\n(password, 이름 입력 등)",
			HideOther:   true,
		},
		{
			Label:       "항상 터미널 입력",
			Description: "자동 응답 없이 항상 사용자에게 직접 입력 요청",
			Preview:     "인터랙티브 프롬프트 감지 시마다\n터미널에 컨텍스트를 출력하고\n사용자 입력을 기다립니다.\n\nEnter = 빈 줄 전송\nq     = 명령 중단",
			HideOther:   true,
		},
	}

	result := tui.RunSelect("자동 처리 방식 선택", options, width)

	var chosen agent.IdleInputHandler
	switch result.Index {
	case 0:
		chosen = agent.NewPatternIdleInputHandler(nil)
		fmt.Println("✓ 패턴 매칭 자동응답으로 설정했습니다.")
	case 1:
		replFallback := &REPLIdleInputHandler{}
		chosen = r.agent.NewSmartIdleInputHandler(replFallback)
		fmt.Println("✓ LLM 판단 위임으로 설정했습니다.")
	case 2:
		chosen = &REPLIdleInputHandler{}
		fmt.Println("✓ 항상 터미널 입력으로 설정했습니다.")
	default:
		fmt.Println("취소되었습니다.")
		return
	}

	r.agent.SetIdleInputHandler(chosen)
}
