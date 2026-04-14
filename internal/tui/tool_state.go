// Package tui
// File: tool_state.go
// Description: 병렬 실행 중인 도구들의 활성 상태를 관리하는 맵 자료구조
// Responsibility: toolID 기반 도구별 상태(셸 출력, 시작시각) 추적 및 조회

package tui

import "time"

// activeToolState는 실행 중인 단일 도구의 상태를 보관한다.
type activeToolState struct {
	toolID       string
	toolName     string
	target       string
	args         map[string]any
	startTime    time.Time
	shellLines   []string // 도구별 슬라이딩 stdout 버퍼 (최대 shellBoxMaxLines)
	backgrounded bool     // Ctrl+B로 백그라운드로 이동된 경우 true
}

// activeToolMap은 병렬 실행 중인 도구들을 삽입 순서대로 추적한다.
// map + 순서 슬라이스 쌍으로 O(1) 조회와 삽입 순서 유지를 모두 제공한다.
type activeToolMap struct {
	ids    []string
	states map[string]*activeToolState
}

// Add는 새 도구 실행을 맵에 등록한다.
func (m *activeToolMap) Add(toolID, toolName, target string, args map[string]any) {
	if m.states == nil {
		m.states = make(map[string]*activeToolState)
	}
	m.ids = append(m.ids, toolID)
	m.states[toolID] = &activeToolState{
		toolID:    toolID,
		toolName:  toolName,
		target:    target,
		args:      args,
		startTime: time.Now(),
	}
}

// AppendOutput은 toolID 도구의 셸 출력 버퍼에 줄을 추가한다.
func (m *activeToolMap) AppendOutput(toolID, line string) {
	if s, ok := m.states[toolID]; ok {
		s.shellLines = appendShellLine(s.shellLines, line, shellBoxMaxLines)
	}
}

// Remove는 toolID 도구를 맵에서 제거한다.
func (m *activeToolMap) Remove(toolID string) {
	delete(m.states, toolID)
	for i, id := range m.ids {
		if id == toolID {
			m.ids = append(m.ids[:i], m.ids[i+1:]...)
			return
		}
	}
}

// RunningCount는 현재 실행 중인 도구 수를 반환한다.
func (m *activeToolMap) RunningCount() int {
	return len(m.ids)
}

// MostRecent는 가장 최근에 시작된 도구 상태를 반환한다. 없으면 nil.
func (m *activeToolMap) MostRecent() *activeToolState {
	if len(m.ids) == 0 {
		return nil
	}
	return m.states[m.ids[len(m.ids)-1]]
}

// MostRecentForeground는 백그라운드가 아닌 가장 최근 도구 상태를 반환한다. 없으면 nil.
func (m *activeToolMap) MostRecentForeground() *activeToolState {
	for i := len(m.ids) - 1; i >= 0; i-- {
		s := m.states[m.ids[i]]
		if !s.backgrounded {
			return s
		}
	}
	return nil
}

// IsBackgrounded는 toolID 도구가 백그라운드 상태인지 반환한다.
func (m *activeToolMap) IsBackgrounded(toolID string) bool {
	if s, ok := m.states[toolID]; ok {
		return s.backgrounded
	}
	return false
}

// BackgroundAll은 실행 중인 모든 도구를 백그라운드로 전환하고 개수를 반환한다.
func (m *activeToolMap) BackgroundAll() int {
	count := 0
	for _, s := range m.states {
		if !s.backgrounded {
			s.backgrounded = true
			count++
		}
	}
	return count
}

// BackgroundCount는 현재 백그라운드 중인 도구 수를 반환한다.
func (m *activeToolMap) BackgroundCount() int {
	count := 0
	for _, s := range m.states {
		if s.backgrounded {
			count++
		}
	}
	return count
}

// Clear는 맵을 초기화한다.
func (m *activeToolMap) Clear() {
	m.ids = nil
	m.states = nil
}
