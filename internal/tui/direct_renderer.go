// Package tui
// File: direct_renderer.go
// Description: BubbleTea 없이 직접 ANSI 출력하는 렌더러 — 구조체 및 shimmer 관리
// Responsibility: DirectRenderer 구조체 정의, 초기화, shimmer goroutine 생명주기 제어

package tui

import (
	"sync"
	"time"
)

// DirectRenderer는 BubbleTea 없이 직접 stdout에 출력하는 렌더러이다.
// Claude CLI의 inline 렌더링 방식을 따른다.
// 이벤트 핸들러는 direct_renderer_events.go에 정의된다.
type DirectRenderer struct {
	state    *SessionState
	box      *liveArea     // 고정 높이 라이브 박스 (항상 화면 하단에 유지)
	md       *mdRenderer
	progress *progressTree
	mu       sync.Mutex

	// 스트리밍 마크다운 상태
	tokens      string
	lastRender  time.Time
	streamCache stableCache
	isStreaming bool

	// 셸 출력 추적 (도구 완료 시 영구 박스로 출력)
	shellLines    []string // 현재 도구의 셸 출력 슬라이딩 버퍼
	shellToolName string   // 현재 도구 이름 (셸 박스 제목용)

	// shimmer 상태
	shimmer       shimmerState
	shimmerMu     sync.Mutex
	shimmerOn     bool
	shimmerGen    int        // shimmer 세대 카운터 — stop→start 사이 이전 goroutine 종료 보장
	shimmerDone   chan struct{}
	thinkingLabel string // 도구 완료 후 shimmer 재시작 시 재사용

	width int
	}

// NewDirectRenderer는 새 DirectRenderer를 생성한다.
func NewDirectRenderer(state *SessionState) *DirectRenderer {
	w := state.Width
	if w < 40 {
		w = 80
	}
	return &DirectRenderer{
		state:    state,
		box:      newLiveArea(w),
		md:       newMdRenderer(w - 6),
		progress: newProgressTree(),
		width:    w,
	}
}

// StartShimmer는 shimmer 애니메이션을 시작한다 (공개 API).
func (r *DirectRenderer) StartShimmer(text string) {
	r.shimmerMu.Lock()
	if r.shimmerOn {
		r.shimmerMu.Unlock()
		return
	}
	r.shimmer = newShimmer()
	r.shimmer.width = r.width
	r.shimmer.Start(text)
	r.shimmerOn = true
	r.shimmerGen++
	gen := r.shimmerGen
	done := make(chan struct{})
	r.shimmerDone = done
	r.shimmerMu.Unlock()
	go r.runShimmerWithGen(gen, done)
}

// StopShimmer는 shimmer를 정지하고 goroutine 종료를 기다린다 (공개 API).
func (r *DirectRenderer) StopShimmer() {
	r.shimmerMu.Lock()
	wasOn := r.shimmerOn
	r.shimmerOn = false
	r.shimmerMu.Unlock()
	if wasOn && r.shimmerDone != nil {
		<-r.shimmerDone
	}
}

func (r *DirectRenderer) runShimmerWithGen(gen int, done chan struct{}) {
	defer close(done)
	for {
		r.shimmerMu.Lock()
		if !r.shimmerOn || r.shimmerGen != gen {
			r.shimmerMu.Unlock()
			return
		}
		r.shimmer.pos++
		view := r.shimmer.View()
		r.shimmerMu.Unlock()

		r.mu.Lock()
		// r.mu 획득 후 shimmerOn/gen을 재확인해 stale draw를 방지한다.
		r.shimmerMu.Lock()
		shouldDraw := r.shimmerOn && r.shimmerGen == gen
		r.shimmerMu.Unlock()
		if shouldDraw {
			r.box.SetHeader(view)
			r.box.Redraw()
		}
		r.mu.Unlock()

		time.Sleep(100 * time.Millisecond)
	}
}

// PauseForPrompt는 shimmer를 정지하고 라이브 박스를 비워 터미널 프롬프트가 보이도록 한다.
// 비밀번호 입력 등 직접 stdin/stdout 조작이 필요한 경우 호출한다.
func (r *DirectRenderer) PauseForPrompt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopShimmerInternal()
	r.box.Reset()
}

// ResumeAfterPrompt는 프롬프트 완료 후 shimmer를 재시작한다.
func (r *DirectRenderer) ResumeAfterPrompt(label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startShimmerInternal(label)
}

// RecordActivity는 shimmer 마지막 활동 시각을 갱신한다.
func (r *DirectRenderer) RecordActivity() {
	r.shimmerMu.Lock()
	r.shimmer.RecordActivity()
	r.shimmerMu.Unlock()
}

// SetShimmerText는 shimmer 텍스트를 변경한다.
func (r *DirectRenderer) SetShimmerText(text string) {
	r.shimmerMu.Lock()
	r.shimmer.SetText(text)
	r.shimmerMu.Unlock()
}

// SetShimmerHint는 shimmer 우측에 표시할 보조 텍스트를 설정한다.
func (r *DirectRenderer) SetShimmerHint(hint string) {
	r.shimmerMu.Lock()
	r.shimmer.hint = hint
	r.shimmerMu.Unlock()
}

// stopShimmerInternal은 shimmer를 정지한다.
// 호출자는 r.mu를 보유해야 한다.
// shimmer goroutine이 r.mu를 획득할 때 shimmerOn=false를 확인하고 draw를 생략한다.
func (r *DirectRenderer) stopShimmerInternal() {
	r.shimmerMu.Lock()
	r.shimmerOn = false
	r.shimmerMu.Unlock()
}

// startShimmerInternal은 shimmer goroutine을 시작한다.
// 호출자는 r.mu를 보유해야 한다.
func (r *DirectRenderer) startShimmerInternal(text string) {
	r.shimmerMu.Lock()
	if r.shimmerOn {
		r.shimmerMu.Unlock()
		return
	}
	r.shimmer = newShimmer()
	r.shimmer.width = r.width
	r.shimmer.Start(text)
	r.shimmerOn = true
	r.shimmerGen++
	gen := r.shimmerGen
	done := make(chan struct{})
	r.shimmerDone = done
	r.shimmerMu.Unlock()
	go r.runShimmerWithGen(gen, done)
}
