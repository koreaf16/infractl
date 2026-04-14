// Package tui
// File: direct_renderer_events.go
// Description: DirectRenderer의 에이전트 이벤트 핸들러 구현
// Responsibility: OnToken/OnToolStart/OnToolEnd/OnResponse 등 이벤트를 liveArea에 반영

package tui

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// OnThinking은 thinking 상태를 시작한다.
func (r *DirectRenderer) OnThinking(tier string, model string) {
	r.thinkingLabel = ThinkingLabel(tier, model)
	r.StartShimmer(r.thinkingLabel)
}

// OnToken은 LLM 스트리밍 토큰을 처리한다.
func (r *DirectRenderer) OnToken(token string) {
	r.RecordActivity()
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isStreaming {
		r.stopShimmerInternal()
		r.isStreaming = true
		r.box.SetTitle("response")
		r.box.Reset()
	}

	r.tokens += token

	if time.Since(r.lastRender) > 150*time.Millisecond {
		r.renderStreamToBox()
		r.lastRender = time.Now()
	}
}

// OnResponse는 최종 응답을 영구 출력한다.
func (r *DirectRenderer) OnResponse(content string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()

	// 스트리밍 중 최종 콘텐츠가 없으면 누적 tokens 사용
	finalContent := content
	if r.isStreaming && content == "" && r.tokens != "" {
		finalContent = r.tokens
	}

	if finalContent != "" {
		lines := r.md.Render(finalContent)
		for i, l := range lines {
			lines[i] = "   " + l
		}
		text := strings.Join(lines, "\n")
		r.box.Reset()
		r.box.PrintPermanent(text)
	}

	r.isStreaming = false
	r.tokens = ""
	r.streamCache.Reset()
}

// OnToolStart는 도구 실행 시작을 처리한다.
func (r *DirectRenderer) OnToolStart(toolID, name, target string, args map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()
	r.shellLines = nil
	r.shellToolName = name

	desc, _ := args["description"].(string)
	if phaseID, phaseName, ok := parsePhaseFromDescription(desc); ok {
		r.progress.AddToolWithPhase(toolID, name, target, phaseID, phaseName, args)
	} else {
		r.progress.AddTool(toolID, name, target, args)
	}

	// 스트리밍 텍스트가 있다면 도구 실행 전에 일반 텍스트로 영구 출력한다.
	if r.isStreaming {
		lines := r.md.Render(r.tokens)
		for i, l := range lines {
			lines[i] = "   " + l
		}
		text := strings.Join(lines, "\n")
		r.box.PrintPermanent(text)
		r.streamCache.Reset()
		r.isStreaming = false
		r.tokens = ""
	}

	// ● 도구 헤더를 출력한다. 툴 사용 메시지이므로 ⎿ 브래킷을 붙인다.
	headerLine := buildToolHeaderLine(name, target, args)
	headerLine = StyleResponseBracket.Render("  ⎿  ") + headerLine
	r.box.Reset()
	r.box.SetTitle(name)
	r.box.PrintPermanent(headerLine)
	label := r.thinkingLabel
	if label == "" {
		label = "thinking..."
	}
	r.startShimmerInternal(label)
}

// OnToolOutput은 도구 실행 중 출력 라인을 기록하고 라이브 박스에 즉시 표시한다.
// 패스워드 프롬프트 같은 입력 대기 메시지가 실행 중에 보이도록 즉시 렌더링한다.
func (r *DirectRenderer) OnToolOutput(toolID, line string) {
	r.RecordActivity()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.progress.SetOutput(toolID, line)
	trimmed := strings.TrimRight(line, "\r\n")
	if trimmed != "" {
		r.shellLines = appendShellLine(r.shellLines, trimmed, shellBoxMaxLines)
		r.box.SetContent(r.shellLines)
		r.box.Redraw()
	}
}

// OnToolEnd는 도구 실행 완료를 처리한다.
func (r *DirectRenderer) OnToolEnd(toolID, name, result string, duration time.Duration, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()
	r.progress.CompleteTool(toolID, duration, success)

	// 모든 도구를 박스로 렌더링한다.
	var args map[string]any
	for _, item := range r.progress.items {
		if item.toolID == toolID {
			args = item.args
			break
		}
	}
	contentLines := r.shellLines
	if !isShellBoxTool(name) {
		contentLines = toolBoxContent(name, args, result, success)
	}
	r.box.Reset()
	r.box.PrintPermanent(renderShellBoxCompleted(name, args, contentLines, duration, success, r.width))

	r.shellLines = nil
	r.shellToolName = ""

	label := r.thinkingLabel
	if label == "" {
		label = "thinking..."
	}
	r.startShimmerInternal(label)
}

// OnError는 에러를 영구 출력한다.
func (r *DirectRenderer) OnError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()
	slog.Error("agent error in DirectRenderer", "err", err)
	errText := StyleError.Render("  Error: " + err.Error())
	r.box.PrintPermanent(errText)
}

// OnUsageUpdate는 토큰/비용 정보를 업데이트한다.
func (r *DirectRenderer) OnUsageUpdate(inputTokens, outputTokens int, costUSD float64) {
	r.state.AddUsage(inputTokens, outputTokens, costUSD)
}

// Finish는 턴 종료 시 박스를 삭제하고 상태바를 출력한다.
func (r *DirectRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopShimmerInternal()

	if r.isStreaming {
		lines := r.md.Render(r.tokens)
		for i, l := range lines {
			lines[i] = "   " + l
		}
		text := strings.Join(lines, "\n")
		r.box.PrintPermanent(text)
		r.isStreaming = false
		r.tokens = ""
		r.streamCache.Reset()
	}

	r.box.Clear()
	fmt.Fprintln(os.Stdout, StatusLine(r.state))
	r.progress.Reset()
}

// PrintNotification은 실행 중 비동기 알림을 안전하게 출력한다.
// shimmer를 잠시 멈추고 알림을 영구 출력한 뒤 shimmer를 재시작한다.
func (r *DirectRenderer) PrintNotification(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.shimmerMu.Lock()
	wasShimming := r.shimmerOn
	r.shimmerMu.Unlock()

	r.stopShimmerInternal()
	r.box.PrintPermanent(text)

	if wasShimming {
		label := r.thinkingLabel
		if label == "" {
			label = "thinking..."
		}
		r.startShimmerInternal(label)
	}
}

// --- internal helpers ---

// renderStreamToBox는 누적 tokens를 박스 콘텐츠로 렌더링한다.
func (r *DirectRenderer) renderStreamToBox() {
	rendered := r.md.RenderStreaming(r.tokens, &r.streamCache)
	if len(rendered) == 0 {
		return
	}
	r.box.SetContent(rendered)
	r.box.Redraw()
}

// buildToolHeaderLine은 도구 시작 헤더 한 줄을 생성한다.
func buildToolHeaderLine(name, target string, args map[string]any) string {
	bullet := StyleClaude().Render("●")
	toolName := StyleCmdBoxToolName.Render(toolDisplayName(name))
	arg := toolDisplayArg(name, args)
	if arg != "" {
		return bullet + " " + toolName + StyleCmdBoxDim.Render("(") + arg + StyleCmdBoxDim.Render(")")
	}
	if target != "" && target != "localhost" {
		return bullet + " " + toolName + StyleCmdBoxDim.Render(" → ") + StyleCmdBoxTarget.Render(target)
	}
	return bullet + " " + toolName
}
