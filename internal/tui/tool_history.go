// Package tui
// File: tool_history.go
// Description: 현재 세션에서 완료된 도구 실행 이력 보관
// Responsibility: ToolEndMsg 수신 시 이력 항목 추가 및 Ctrl+O 오버레이에 데이터 제공

package tui

import "time"

const maxToolHistory = 200 // 세션당 최대 보관 이력 수

// toolHistoryEntry는 단일 도구 실행 완료 기록이다.
type toolHistoryEntry struct {
	toolID     string
	toolName   string
	target     string
	result     string
	duration   time.Duration
	success    bool
	finishedAt time.Time
	shellLines []string // 도구 실행 중 수집된 stdout 줄 (shell_exec용)
}

// toolHistory는 현재 세션의 도구 실행 이력을 관리한다.
type toolHistory struct {
	entries []toolHistoryEntry
}

// Add는 완료된 도구 실행을 이력에 추가한다.
// maxToolHistory를 초과하면 가장 오래된 항목을 제거한다.
func (h *toolHistory) Add(e toolHistoryEntry) {
	e.finishedAt = time.Now()
	h.entries = append(h.entries, e)
	if len(h.entries) > maxToolHistory {
		h.entries = h.entries[len(h.entries)-maxToolHistory:]
	}
}

// Len은 이력 항목 수를 반환한다.
func (h *toolHistory) Len() int {
	return len(h.entries)
}

// Get은 인덱스로 항목을 반환한다. 범위를 벗어나면 zero value를 반환한다.
func (h *toolHistory) Get(i int) (toolHistoryEntry, bool) {
	if i < 0 || i >= len(h.entries) {
		return toolHistoryEntry{}, false
	}
	return h.entries[i], true
}
