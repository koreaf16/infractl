// Package tui
// File: permission_ui.go
// Description: 도구별 맞춤 권한 승인 UI (Claude CLI PermissionDialog 스타일)
// Responsibility: 위험 작업 실행 전 명령 미리보기 + 선택지 제시

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/agent"
)

// PermissionChoice는 권한 승인 결과이다.
type PermissionChoice int

const (
	PermissionAllow      PermissionChoice = iota
	PermissionAllowOnce
	PermissionDeny
)

// RunPermissionUI는 도구 실행 전 권한 승인 UI를 표시한다.
// Claude CLI의 PermissionDialog 스타일.
func RunPermissionUI(req agent.ConfirmRequest, width int) agent.ConfirmResponse {
	tw := newTermWriter()

	// 상단 border
	borderColor := ColorWarning
	riskLabel := "MEDIUM"
	if req.RiskLevel == "high" {
		borderColor = ColorError
		riskLabel = "HIGH"
	} else if req.RiskLevel == "low" {
		borderColor = ColorSuccess
		riskLabel = "LOW"
	}

	borderStyle := StylePromptBorder.Foreground(borderColor)
	topLine := borderStyle.Render("──") + " " +
		StyleCmdBoxToolName.Render(req.ToolName) + " " +
		borderStyle.Render(strings.Repeat("─", max(width-len(req.ToolName)-5, 10)))
	tw.Println(topLine)

	// 작업 설명
	tw.Println(borderStyle.Render("│") + "  " + req.Description)
	tw.Println(borderStyle.Render("│"))

	// 대상 + 위험도
	if req.Target != "" && req.Target != "localhost" {
		tw.Println(borderStyle.Render("│") + "  " +
			StyleCmdBoxDim.Render("Target: ") + StyleCmdBoxTarget.Render(req.Target))
	}
	tw.Println(borderStyle.Render("│") + "  " +
		StyleCmdBoxDim.Render("Risk: ") + riskColorStyle(req.RiskLevel).Render("⚠ "+riskLabel))

	tw.Println(borderStyle.Render("│"))

	// 하단 border
	bottomLine := borderStyle.Render("─" + strings.Repeat("─", max(width-1, 10)))
	tw.Println(bottomLine)

	// 대상 이름 입력이 필요한 경우
	if req.NeedInput {
		return runInputConfirm(req)
	}

	// 선택지
	options := []SelectOption{
		{Label: "Allow", Description: "이 작업을 실행합니다"},
		{Label: "Deny", Description: "이 작업을 거부합니다"},
	}

	result := RunSelect("계속하시겠습니까?", options, width)
	switch result.Index {
	case 0:
		return agent.ConfirmResponse{Confirmed: true}
	default:
		return agent.ConfirmResponse{Confirmed: false}
	}
}

// runInputConfirm은 대상 이름을 직접 입력받아 확인한다.
func runInputConfirm(req agent.ConfirmRequest) agent.ConfirmResponse {
	fmt.Printf("  대상 이름을 입력하세요 (%s): ", StyleCmdBoxTarget.Render(req.TargetName))
	result := readFreeText("")
	if strings.TrimSpace(result.Label) == req.TargetName {
		return agent.ConfirmResponse{Confirmed: true, UserInput: result.Label}
	}
	fmt.Println(StyleError.Render("  이름이 일치하지 않습니다. 작업을 거부합니다."))
	return agent.ConfirmResponse{Confirmed: false}
}

// riskColorStyle은 위험도에 따른 스타일을 반환한다.
func riskColorStyle(level interface{}) StyleFunc {
	switch fmt.Sprintf("%v", level) {
	case "high":
		return StyleError
	case "low":
		return StyleSuccess
	default:
		return StyleWarning
	}
}

// StyleFunc는 lipgloss.Style의 타입 별칭이다.
type StyleFunc = lipgloss.Style
