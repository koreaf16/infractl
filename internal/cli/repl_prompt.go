// Package cli
// File: repl_prompt.go
// Description: c-bata/go-prompt 기반 대화형 REPL 구현
// Responsibility: 고성능 자동완성 및 하단 고정 프롬프트 인터페이스 제공

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c-bata/go-prompt"
	"github.com/yourorg/infractl/internal/agent"
	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tui"
	"golang.org/x/term"
)

// PromptREPL은 go-prompt를 사용한 고기능 REPL이다.
type PromptREPL struct {
	*REPL
	ctx context.Context
}

// NewPromptREPL은 PromptREPL 인스턴스를 생성한다.
func NewPromptREPL(ag *agent.Agent, handler *REPLHandler, cfg *config.Config, st store.ServerStore, mgr *executor.Manager) *PromptREPL {
	repl, _ := NewREPL(ag, handler, cfg, st, mgr)
	return &PromptREPL{
		REPL: repl,
	}
}

// Run은 go-prompt 루프를 시작하며, 하단에 프롬프트를 고정한다.
func (r *PromptREPL) Run(ctx context.Context) error {
	r.ctx = ctx

	// 터미널 크기 감지
	width, height, err := term.GetSize(0)
	if err != nil || width <= 0 || height <= 0 {
		width, height = 80, 24
	}

	// 1. 하단 프롬프트를 위한 스크롤 영역 설정 (상단 1 ~ Height-3 라인만 스크롤)
	// ANSI: \033[top;bottomr
	fmt.Printf("\033[1;%dr", height-3)
	// 커서를 스크롤 영역 바깥인 하단으로 이동
	fmt.Printf("\033[%d;1H", height-2)

	// 배너 출력 (스크롤 영역 내부인 왼쪽 상단으로 이동하여 출력)
	fmt.Print("\033[H")
	if r.state != nil {
		tui.PrintBanner(r.state)
	} else {
		printBanner(r.cfg)
	}

	// 기존 히스토리 로드
	var history []string
	if r.historyStore != nil {
		if entries, err := r.historyStore.ListPromptHistory(ctx, 1000); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				history = append(history, entries[i].Input)
			}
		}
	}

	// go-prompt 옵션 설정
	options := []prompt.Option{
		prompt.OptionPrefix("> "),
		prompt.OptionTitle("infractl-repl"),
		prompt.OptionSuggestionBGColor(prompt.DarkGray),
		prompt.OptionSelectedSuggestionBGColor(prompt.LightGray),
		prompt.OptionSelectedSuggestionTextColor(prompt.Black),
		prompt.OptionMaxSuggestion(10),
		prompt.OptionHistory(history),
		prompt.OptionPrefixTextColor(prompt.Cyan),
		prompt.OptionAddKeyBind(prompt.KeyBind{
			Key: prompt.ControlC,
			Fn: func(b *prompt.Buffer) {
				// 종료 시 스크롤 영역 초기화
				fmt.Print("\033[r\033[m")
				fmt.Println("\n종료합니다.")
				return
			},
		}),
		prompt.OptionAddKeyBind(prompt.KeyBind{
			Key: prompt.ControlY,
			Fn: func(b *prompt.Buffer) {
				active := r.agent.ToggleYOROMode()
				msg := "✓ YORO Mode 비활성화"
				if active {
					msg = "⚡ YORO Mode 활성화 (백업/체크포인트 유지)"
				}
				fmt.Printf("\n%s\n", msg)
				w, h, _ := term.GetSize(0)
				if w <= 0 {
					w = 80
				}
				if h <= 0 {
					h = 24
				}
				r.renderBottomUI(w, h)
			},
		}),
	}

	p := prompt.New(
		r.executor,
		r.completer,
		options...,
	)

	// 렌더링 전 하단 UI 가이드라인 출력
	r.renderBottomUI(width, height)

	p.Run()

	// 종료 후 스크롤 영역 초기화
	fmt.Print("\033[r")
	return nil
}

func (r *PromptREPL) renderBottomUI(width, height int) {
	// 하단 고정 UI (구분선 및 상태바)
	fmt.Printf("\033[%d;1H", height-2)
	fmt.Print(strings.Repeat("─", width))
	fmt.Printf("\033[%d;1H", height-1)
	// YORO 모드 상태 표시
	if r.agent != nil && r.agent.IsYOROMode() {
		fmt.Print("⚡ YORO MODE ")
	}
}

// executor는 사용자가 Enter를 눌렀을 때 실행되는 메인 로직이다.
func (r *PromptREPL) executor(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	// 입력 후 go-prompt가 새 줄을 만들기 전에 한 줄 위로 올려서 출력 시작 위치 조정
	fmt.Print("\033[A")

	if r.historyStore != nil {
		r.historyStore.SavePromptHistory(r.ctx, input, r.agent.CurrentSessionID())
	}

	if strings.HasPrefix(input, "/") {
		if r.handleSlashCommand(r.ctx, input) {
			return
		}
		return
	}

	r.agent.UpdateSessionTitle(r.ctx, input)

	runCtx, runCancel := context.WithCancel(r.ctx)
	defer runCancel()

	var done chan struct{}
	if r.richHandler != nil {
		done = r.richHandler.StartTurn(runCtx)
	} else {
		done = r.handler.PrepareStream()
	}

	err := r.agent.Run(runCtx, input)

	if r.richHandler != nil {
		r.richHandler.FinishTurn()
	} else {
		<-done
	}

	if err != nil {
		slog.Error("agent run error", "err", err)
		fmt.Printf("\nError: %v\n", err)
	}

	// 출력이 끝난 후 다시 하단 UI를 그려서 위치 고정 유지
	width, height, _ := term.GetSize(0)
	if width <= 0 || height <= 0 {
		width, height = 80, 24
	}
	r.renderBottomUI(width, height)
	// 다시 프롬프트 위치로 복귀
	fmt.Printf("\033[%d;3H", height-1)
}

// completer는 실시간 자동완성 제안을 생성한다.
func (r *PromptREPL) completer(d prompt.Document) []prompt.Suggest {
	line := d.TextBeforeCursor()
	if !strings.HasPrefix(line, "/") {
		return []prompt.Suggest{}
	}

	suggestions := []prompt.Suggest{
		{Text: "/help", Description: "도움말 보기"},
		{Text: "/tools", Description: "사용 가능한 도구 목록"},
		{Text: "/model", Description: "현재 사용 중인 모델 정보"},
		{Text: "/clear", Description: "대화 히스토리 초기화"},
		{Text: "/servers", Description: "등록된 서버 목록 및 관리"},
		{Text: "/connectors", Description: "활성 커넥터 상태"},
		{Text: "/mcp", Description: "MCP 서버 상태 및 재연결"},
		{Text: "/sessions", Description: "세션 목록 및 복구"},
		{Text: "/history", Description: "실행 이력 확인"},
		{Text: "/knowledge", Description: "학습된 지식 관리"},
		{Text: "/rag", Description: "RAG 데이터 소스 관리"},
		{Text: "/cost", Description: "토큰 사용량 및 비용 확인"},
		{Text: "/checkpoints", Description: "체크포인트 관리"},
		{Text: "/hooks", Description: "자동화 훅 관리"},
		{Text: "/schedules", Description: "스케줄된 작업 관리"},
		{Text: "/exit", Description: "REPL 종료"},
		{Text: "/quit", Description: "REPL 종료"},
	}

	if strings.HasPrefix(line, "/servers ") {
		servers, _ := r.store.List(r.ctx)
		for _, s := range servers {
			suggestions = append(suggestions, prompt.Suggest{
				Text:        s.Name,
				Description: fmt.Sprintf("%s (%s)", s.Host, s.User),
			})
		}
	}

	return prompt.FilterHasPrefix(suggestions, line, true)
}
