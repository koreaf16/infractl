// Package tui
// File: background_render.go
// Description: 백그라운드 완료된 툴의 간결한 알림 메시지를 렌더링한다.
// Responsibility: Ctrl+B로 백그라운드화된 툴이 완료되었을 때 스크롤백용 한 줄 요약 생성

package tui

import (
	"fmt"
	"time"
)

// renderBackgroundDone은 백그라운드 완료 알림을 한 줄로 렌더링한다.
// 형식: "  ⎿  ✓ Shell completed in background (1.2s)"
func renderBackgroundDone(name string, duration time.Duration, success bool) string {
	bracket := StyleResponseBracket.Render("  ⎿  ")
	dur := StyleCmdBoxDim.Render(fmt.Sprintf(" (%s)", formatElapsedShort(duration)))
	displayName := StyleCmdBoxToolName.Render(toolDisplayName(name))
	suffix := StyleCmdBoxDim.Render(" completed in background")
	if success {
		return bracket + displayName + suffix + dur
	}
	return bracket + StyleError.Render("✗") + " " + displayName + suffix + dur
}
