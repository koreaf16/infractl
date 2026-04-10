// Package tui
// File: direct_renderer.go
// Description: BubbleTea 없이 직접 ANSI 출력하는 렌더러
// Responsibility: 에이전트 이벤트를 받아 stdout에 직접 리치 렌더링

package tui

import (
	"strings"
	"sync"
	"time"
)

// DirectRenderer는 BubbleTea 없이 직접 stdout에 출력하는 렌더러이다.
// Claude CLI의 inline 렌더링 방식을 따른다.
type DirectRenderer struct {
	state    *SessionState
	tw       *termWriter
	md       *mdRenderer
	progress *progressTree
	mu       sync.Mutex

	// 스트리밍 마크다운 상태
	tokens        string
	lastRender    time.Time
	streamWriter  *termWriter // 스트리밍 영역 전용
	streamCache   stableCache // 스트리밍 마크다운 캐시
	isStreaming   bool

	// shimmer 상태
	shimmer    shimmerState
	shimmerMu  sync.Mutex
	shimmerOn  bool
	shimmerDone chan struct{}

	width int
}

// NewDirectRenderer는 새 DirectRenderer를 생성한다.
func NewDirectRenderer(state *SessionState) *DirectRenderer {
	w := state.Width
	if w < 40 {
		w = 80
	}
	return &DirectRenderer{
		state:        state,
		tw:           newTermWriter(),
		md:           newMdRenderer(w - 6),
		progress:     newProgressTree(),
		streamWriter: newTermWriter(),
		width:        w,
	}
}

// StartShimmer는 shimmer 애니메이션을 시작한다.
func (r *DirectRenderer) StartShimmer(text string) {
	r.shimmerMu.Lock()
	defer r.shimmerMu.Unlock()
	if r.shimmerOn {
		return
	}
	r.shimmer = newShimmer()
	r.shimmer.width = r.width
	r.shimmer.Start(text)
	r.shimmerOn = true
	r.shimmerDone = make(chan struct{})

	go r.runShimmer()
}

// StopShimmer는 shimmer를 정지하고 해당 줄을 지운다.
func (r *DirectRenderer) StopShimmer() {
	r.shimmerMu.Lock()
	wasOn := r.shimmerOn
	r.shimmerOn = false
	r.shimmerMu.Unlock()

	if wasOn && r.shimmerDone != nil {
		<-r.shimmerDone
	}
	r.tw.OverwriteLine("")
}

func (r *DirectRenderer) runShimmer() {
	defer close(r.shimmerDone)
	for {
		r.shimmerMu.Lock()
		if !r.shimmerOn {
			r.shimmerMu.Unlock()
			return
		}
		r.shimmer.pos++
		view := r.shimmer.View()
		r.shimmerMu.Unlock()

		r.mu.Lock()
		r.tw.OverwriteLine(view)
		r.mu.Unlock()

		time.Sleep(100 * time.Millisecond)
	}
}

// RecordActivity는 마지막 활동 시각을 갱신한다.
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

// OnThinking은 thinking 상태를 시작한다.
func (r *DirectRenderer) OnThinking() {
	r.StartShimmer("thinking...")
}

// OnToken은 LLM 스트리밍 토큰을 처리한다.
func (r *DirectRenderer) OnToken(token string) {
	r.RecordActivity()
	r.mu.Lock()
	defer r.mu.Unlock()

	// 첫 토큰 시 shimmer 정리 후 스트리밍 시작
	if !r.isStreaming {
		r.stopShimmerInternal()
		r.tw.OverwriteLine("") // shimmer 줄 지우기
		r.isStreaming = true
		r.streamWriter.ResetLines()
	}

	r.tokens += token

	// 150ms 스로틀링
	if time.Since(r.lastRender) > 150*time.Millisecond {
		r.renderStream()
	}
}

// OnResponse는 최종 응답을 렌더링한다.
func (r *DirectRenderer) OnResponse(content string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()

	// 스트리밍 영역 지우고 최종 렌더
	if r.isStreaming {
		r.streamWriter.ClearLive()
	}

	lines := r.md.Render(content)
	bracket := StyleResponseBracket.Render("  ⎿  ")
	indent := "     "
	for i, line := range lines {
		if i == 0 {
			r.tw.Println(bracket + line)
		} else {
			r.tw.Println(indent + line)
		}
	}

	r.isStreaming = false
	r.tokens = ""
}

// OnToolStart는 도구 실행 시작을 표시한다.
func (r *DirectRenderer) OnToolStart(_ /*toolID*/, name, target string, args map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 스트리밍 중이면 마무리
	if r.isStreaming {
		r.flushStream()
	}

	r.stopShimmerInternal()

	r.progress.AddTool("" /*toolID unused in direct mode*/, name, target)

	// 도구 헤더 출력
	bullet := StyleClaude().Render("●")
	toolName := StyleCmdBoxToolName.Render(toolDisplayName(name))
	arg := toolDisplayArg(name, args)
	if arg != "" {
		r.tw.Println(bullet + " " + toolName + StyleCmdBoxDim.Render("(") + arg + StyleCmdBoxDim.Render(")"))
	} else {
		r.tw.Println(bullet + " " + toolName)
	}

	// shimmer를 도구명으로 재시작
	r.startShimmerInternal(name + "...")
}

// OnToolOutput은 도구 실행 중 출력 라인을 표시한다.
func (r *DirectRenderer) OnToolOutput(_ /*toolID*/, line string) {
	r.RecordActivity()
	r.progress.SetOutput("" /*toolID*/, line)
}

// OnToolEnd는 도구 실행 완료를 표시한다.
func (r *DirectRenderer) OnToolEnd(_ /*toolID*/, name, result string, duration time.Duration, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()
	r.progress.CompleteTool("" /*toolID unused in direct mode*/, duration, success)

	// 요약 출력
	summary := toolSummary(name, nil, result, duration, success)
	r.tw.Println(summary)

	// shimmer 재시작 (다음 도구 또는 응답 대기)
	r.startShimmerInternal("thinking...")
}

// OnError는 에러를 출력한다.
func (r *DirectRenderer) OnError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()

	errStyle := StyleError.Render("  Error: " + err.Error())
	r.tw.Println(errStyle)
}

// OnUsageUpdate는 토큰/비용 정보를 업데이트한다.
func (r *DirectRenderer) OnUsageUpdate(inputTokens, outputTokens int, costUSD float64) {
	r.state.AddUsage(inputTokens, outputTokens, costUSD)
}

// Finish는 턴 종료 시 정리한다. 상태바 출력.
func (r *DirectRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()

	if r.isStreaming {
		r.flushStream()
	}

	// 상태바
	r.tw.Println(StatusLine(r.state))
	r.progress.Reset()
}

// --- internal helpers ---

func (r *DirectRenderer) renderStream() {
	rendered := r.md.RenderStreaming(r.tokens, &r.streamCache)
	if len(rendered) == 0 {
		return
	}

	bracket := StyleResponseBracket.Render("  ⎿  ")
	indent := "     "
	var sb strings.Builder
	for i, line := range rendered {
		if i == 0 {
			sb.WriteString(bracket + line)
		} else {
			sb.WriteString(indent + line)
		}
		if i < len(rendered)-1 {
			sb.WriteString("\n")
		}
	}
	r.streamWriter.PrintReplace(sb.String())
	r.lastRender = time.Now()
}

func (r *DirectRenderer) flushStream() {
	r.renderStream()
	r.streamWriter.ResetLines()
	r.streamCache.Reset()
	r.isStreaming = false
	r.tokens = ""
}

func (r *DirectRenderer) stopShimmerInternal() {
	r.shimmerMu.Lock()
	r.shimmerOn = false
	r.shimmerMu.Unlock()
	// done 채널 대기 없이 플래그만 끔 (lock 안에서 호출되므로)
}

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
	r.shimmerDone = make(chan struct{})
	r.shimmerMu.Unlock()

	go r.runShimmer()
}
