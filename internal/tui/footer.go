// Package tui
// File: footer.go
// Description: 입력창 아래 키보드 힌트 footer 렌더링
// Responsibility: 현재 상태에 맞는 키보드 단축키 힌트를 한 줄로 표시

package tui

import (
	"strings"

	"github.com/muesli/reflow/ansi"
)

// footerState는 footer 렌더링에 필요한 상태이다.
type footerState int

const (
	footerIdle        footerState = iota
	footerBusy                    // 에이전트 실행 중
	footerConfirm                 // 확인 오버레이 활성
	footerSelection               // 인터랙티브 셀렉션 활성
	footerIdlePrompt              // 쉘 명령 유휴 입력 오버레이 활성
)

// renderFooter는 현재 상태에 맞는 키보드 힌트를 렌더링한다.
// Claude CLI의 PromptInputFooterLeftSide.tsx 패턴 — 1줄, · 구분.
func renderFooter(state footerState, width int) string {
	hints := footerHints(state)
	return buildFooterLine(hints, width)
}

// footerHints는 상태별 힌트 목록을 반환한다.
func footerHints(state footerState) []string {
	switch state {
	case footerBusy:
		return []string{
			hintKey("Esc") + " 전체 중단",
			hintKey("Enter") + " 대기열 추가",
		}
	case footerConfirm:
		return []string{
			hintKey("y") + " 확인",
			hintKey("n") + " 취소",
			hintKey("Esc") + " 취소",
		}
	case footerSelection:
		return []string{
			hintKey("↑↓") + " 이동",
			hintKey("Enter") + " 선택",
			hintKey("Esc") + " 취소",
		}
	case footerIdlePrompt:
		return []string{
			hintKey("Enter") + " 전송 (빈 입력=기본값)",
			hintKey("Esc") + " 중단",
		}
	default: // footerIdle
		return []string{
			hintKey("Esc") + " 중단",
			hintKey("↑↓") + " 히스토리",
			hintKey("Alt+Enter") + " 줄바꿈",
			hintKey("Space") + " 붙여넣기 접기/펼치기",
			hintKey("/help") + " 명령",
		}
	}
}

// hintKey는 키 이름을 bold로 렌더링한다.
func hintKey(key string) string {
	return StyleFooterHint.Bold(true).Render(key)
}

// buildFooterLine은 힌트들을 · 구분자로 연결하고 폭 제한에 맞춘다.
func buildFooterLine(hints []string, width int) string {
	sep := StyleFooterDim.Render(" · ")
	sepWidth := ansi.PrintableRuneWidth(sep)

	var parts []string
	totalWidth := 2 // 좌측 패딩 "  "
	for _, h := range hints {
		hWidth := ansi.PrintableRuneWidth(h)
		needed := hWidth
		if len(parts) > 0 {
			needed += sepWidth
		}
		if totalWidth+needed > width {
			break // 폭 초과 시 나머지 생략
		}
		parts = append(parts, h)
		totalWidth += needed
	}

	return "  " + strings.Join(parts, sep)
}
