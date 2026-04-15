// Package agent
// File: loop_ambiguity.go
// Description: 서버 접속 vs DB 접속 모호성 해결 — 사용자에게 질문하여 의도를 명확화
// Responsibility: 모호한 접속 요청 감지, 사용자 질문 발송, 서버 전용 접속 처리

package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

var (
	serverConnectPattern = regexp.MustCompile(`(?i)\b(ssh|server|host)\b|서버|접속|로그인`)
	dbConnectPattern     = regexp.MustCompile(`(?i)\b(db|database|oracle|mysql|pdb|instance|sid|sqlplus)\b|로그인|접속`)

	// dbInstanceConnectPattern은 분류기와 무관하게 DB 인스턴스/서비스 직접 접속을 나타내는 패턴이다.
	// "인스턴스 접속", "PDB 접속", "sqlplus", "DB 로그인" 등 connector 툴이 반드시 필요한 요청을 감지한다.
	dbInstanceConnectPattern = regexp.MustCompile(`(?i)(인스턴스|pdb|sqlplus|instance|sid)\s*(접속|에\s*접속|로\s*접속|login|connect)|접속.*인스턴스|인스턴스.*접속`)
)

// resolveAmbiguity는 사용자 입력이 서버 접속과 DB 접속 중 어느 쪽인지 모호할 때
// questionHandler를 통해 사용자에게 질문하여 의도를 명확화한다.
func (a *Agent) resolveAmbiguity(ctx context.Context, userInput string, classification ClassifyResult, servers []store.Server) (bool, string) {
	if a.questionHandler == nil {
		return false, ""
	}

	mentionsCount := 0
	for _, srv := range servers {
		if mentionsAlias(userInput, srv.Name) {
			mentionsCount++
		}
	}

	isAmbiguous := false
	if mentionsCount > 1 {
		isAmbiguous = true
	} else if mentionsCount == 1 {
		hasServerSignal := serverConnectPattern.MatchString(userInput)
		hasDBSignal := dbConnectPattern.MatchString(userInput)
		if hasServerSignal && hasDBSignal {
			isAmbiguous = true
		}
	}

	isConnectorSelected := false
	for _, g := range classification.ToolGroups {
		if g == "connector" || g == "discovery" {
			isConnectorSelected = true
			break
		}
	}
	if isConnectorSelected && strings.Contains(userInput, "서버") && !strings.Contains(userInput, "PDB") && !strings.Contains(userInput, "인스턴스") {
		isAmbiguous = true
	}

	if !isAmbiguous {
		return false, ""
	}

	resp, err := a.questionHandler.RequestQuestion(ctx, tools.QuestionRequest{
		Header:   "Connect",
		Question: "서버에만 접속할까요, 아니면 DB/서비스까지 활성화할까요?",
		Options: []tools.QuestionOption{
			{Label: "서버만 접속", Description: "SSH 서버 연결 및 기본 환경 확인 (hostname, whoami 등)"},
			{Label: "DB/서비스까지 접속", Description: "서버 접속 후 DB 인스턴스/서비스 탐색 및 활성화"},
		},
	})
	if err != nil {
		return false, ""
	}

	if resp.SelectedLabel == "서버만 접속" {
		return true, "server_only"
	}
	return true, "full_connect"
}

// handleServerOnlyConnect는 서버 전용 접속을 처리한다.
// SSH 서버에만 연결하고 기본 검증 명령(hostname, whoami, pwd)을 실행한다.
func (a *Agent) handleServerOnlyConnect(ctx context.Context, userInput string, servers []store.Server) error {
	var targetSrv *store.Server
	for _, srv := range servers {
		if mentionsAlias(userInput, srv.Name) {
			targetSrv = &srv
			break
		}
	}

	if targetSrv != nil {
		a.applyActiveServer(targetSrv)
	}

	target := "localhost"
	if a.activeServer != nil {
		target = a.activeServer.Name
	}

	exec, err := a.manager.Get(target)
	if err != nil {
		return fmt.Errorf("executor not found: %w", err)
	}

	verificationCmd := "hostname && whoami && pwd"
	a.handler.OnThinking("fast", a.modelName)
	res, err := exec.Execute(ctx, verificationCmd)
	if err != nil {
		a.handler.OnError(fmt.Errorf("verification failed: %w", err))
	}

	combined := res.Stdout
	if res.Stderr != "" {
		combined += "\n" + res.Stderr
	}

	finalResp := fmt.Sprintf("서버 접속이 완료되었습니다.\n\n**실행 결과 (%s):**\n```\n%s\n```", target, strings.TrimSpace(combined))
	a.handler.OnResponse(finalResp)

	assistantMsg := llm.Message{
		Role:    llm.RoleAssistant,
		Content: finalResp,
	}
	a.history = append(a.history, assistantMsg)
	a.saveSessionMessage(ctx, assistantMsg)

	return nil
}
