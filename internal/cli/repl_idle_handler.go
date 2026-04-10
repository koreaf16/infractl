// Package cli
// File: repl_idle_handler.go
// Description: readline 기반 인터랙티브 프롬프트 유휴 입력 핸들러
// Responsibility: 명령 실행 중 유휴 상태 감지 시 readline으로 사용자 입력 요청

package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yourorg/infractl/internal/agent"
	"github.com/yourorg/infractl/internal/tui"
)

// REPLIdleInputHandler는 readline REPL 환경에서 유휴 프롬프트를 처리한다.
// TUI 모드 없이 순수 텍스트 입력으로 사용자에게 응답을 요청한다.
type REPLIdleInputHandler struct{}

// RequestIdleInput은 agent.IdleInputHandler를 구현한다.
// 터미널에 컨텍스트를 출력하고 stdin에서 응답을 읽는다.
func (h *REPLIdleInputHandler) RequestIdleInput(ctx context.Context, req agent.IdleInputRequest) (agent.IdleInputResponse, error) {
	fmt.Println()
	fmt.Println(tui.StyleWarning.Render(" ⏸ 명령이 입력을 기다리고 있습니다"))
	fmt.Printf(" 명령: %s", req.Command)
	if req.Target != "" && req.Target != "localhost" {
		fmt.Printf(" (대상: %s)", req.Target)
	}
	fmt.Println()

	if len(req.LastLines) > 0 {
		fmt.Println(" 마지막 출력:")
		for _, line := range req.LastLines {
			fmt.Println("   │ " + line)
		}
	}

	doneCh := make(chan struct {
		input string
		abort bool
	}, 1)

	go func() {
		fmt.Print(" 전송할 내용 입력 (Enter=빈 줄 전송, q=중단): ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		text := strings.TrimRight(line, "\r\n")
		if strings.ToLower(text) == "q" {
			doneCh <- struct {
				input string
				abort bool
			}{"", true}
		} else {
			doneCh <- struct {
				input string
				abort bool
			}{text, false}
		}
	}()

	select {
	case <-ctx.Done():
		return agent.IdleInputResponse{Abort: true}, nil
	case result := <-doneCh:
		return agent.IdleInputResponse{Input: result.input, Abort: result.abort}, nil
	}
}
