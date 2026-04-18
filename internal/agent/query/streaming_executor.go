// Package query
// File: streaming_executor.go
// Description: 병렬 + 즉시 yield + sibling abort 기반 도구 실행기
// Responsibility: Batch 단위 도구 실행 — concurrent batch 는 errgroup 병렬,
//                 serial batch 는 직렬 + mutation 실패 시 sibling skip
//
// Ported from: claude_cli/src/services/tools/StreamingToolExecutor.ts:40-519
// 핵심 패턴:
//   - isConcurrencySafe → concurrent batch (errgroup) / serial batch (직렬)
//   - 결과 도착 즉시 채널 push (immediate yield)
//   - sibling abort: mutation 실패 → 동일 serial batch 내 후속 도구 skip
//   - ctx cancel 시 즉시 중단 + 남은 도구 합성 에러 메시지

package query

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/yourorg/infractl/internal/llm"
)

// StreamingExecutor 는 도구 배치를 실행하고 결과를 채널에 즉시 방출한다.
// claude_cli: StreamingToolExecutor (StreamingToolExecutor.ts:40)
type StreamingExecutor struct {
	maxConcurrency int
}

// NewStreamingExecutor 는 StreamingExecutor 를 생성한다.
// maxConcurrency 는 동시 실행 허용 도구 수다 (0이면 MaxToolConcurrency() 기본값 사용).
func NewStreamingExecutor(maxConcurrency int) *StreamingExecutor {
	if maxConcurrency <= 0 {
		maxConcurrency = MaxToolConcurrency()
	}
	return &StreamingExecutor{maxConcurrency: maxConcurrency}
}

// Execute 는 batches 를 순서대로 실행하고 결과 메시지 목록을 반환한다.
// 각 도구가 완료되는 즉시 EventToolResult 를 out 채널에 방출한다.
// aborted=true 이면 ctx 취소 또는 치명 에러로 실행이 중단된 것이다.
func (se *StreamingExecutor) Execute(
	ctx context.Context,
	batches []Batch,
	out chan<- QueryEvent,
	runTool ToolRunner,
) (results []llm.Message, aborted bool) {
	for _, batch := range batches {
		var batchResults []llm.Message
		if batch.Concurrent {
			batchResults, aborted = se.executeConcurrent(ctx, batch.Calls, out, runTool)
		} else {
			batchResults, aborted = se.executeSerial(ctx, batch.Calls, out, runTool)
		}
		results = append(results, batchResults...)
		if aborted {
			return results, true
		}
	}
	return results, false
}

// executeConcurrent 는 하나의 concurrent batch 를 errgroup 으로 병렬 실행한다.
// 도구가 완료되는 즉시 EventToolResult 를 방출한다. 반환 메시지는 원래 순서를 보존한다.
// claude_cli: runToolsConcurrently (toolOrchestration.ts:152-177)
func (se *StreamingExecutor) executeConcurrent(
	ctx context.Context,
	calls []llm.ToolCall,
	out chan<- QueryEvent,
	runTool ToolRunner,
) ([]llm.Message, bool) {
	type immediateResult struct {
		idx     int
		tc      llm.ToolCall
		output  string
		isError bool
	}

	immediateOut := make(chan immediateResult, len(calls))
	msgs := make([]llm.Message, len(calls))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(se.maxConcurrency)

	for i, tc := range calls {
		i, tc := i, tc
		g.Go(func() error {
			send(gctx, out, EventToolUseStart{ID: tc.ID, Name: tc.Function.Name, Input: tc.Function.Arguments})

			var output string
			var isError bool
			if runTool != nil {
				output, isError = runTool(gctx, tc)
			} else {
				output = fmt.Sprintf("error: no tool runner for '%s'", tc.Function.Name)
				isError = true
			}
			immediateOut <- immediateResult{i, tc, output, isError}
			return nil
		})
	}

	// 모든 goroutine 완료 후 채널을 닫는다.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = g.Wait()
		close(immediateOut)
	}()

	// 결과 도착 즉시 EventToolResult 방출 (order 무관)
	for r := range immediateOut {
		msgs[r.idx] = llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: r.tc.ID,
			Content:    r.output,
		}
		send(ctx, out, EventToolResult{
			ID:      r.tc.ID,
			Name:    r.tc.Function.Name,
			Output:  r.output,
			IsError: r.isError,
		})
	}

	wg.Wait()
	return msgs, ctx.Err() != nil
}

// executeSerial 은 하나의 serial batch 를 순서대로 실행한다.
// mutation 도구 실패 시 동일 배치 내 이후 도구를 skip (sibling abort).
// claude_cli: runToolsSerially (toolOrchestration.ts:118-150) + siblingAbortController (BashTool.tsx)
func (se *StreamingExecutor) executeSerial(
	ctx context.Context,
	calls []llm.ToolCall,
	out chan<- QueryEvent,
	runTool ToolRunner,
) ([]llm.Message, bool) {
	results := make([]llm.Message, 0, len(calls))
	mutationFailed := false
	mutationFailReason := ""

	for _, tc := range calls {
		if ctx.Err() != nil {
			return results, true
		}

		send(ctx, out, EventToolUseStart{ID: tc.ID, Name: tc.Function.Name, Input: tc.Function.Arguments})

		// sibling skip: 이 배치 내 이전 도구가 실패한 경우
		if mutationFailed {
			skipMsg := fmt.Sprintf("skipped due to sibling failure: %s", mutationFailReason)
			send(ctx, out, EventToolResult{
				ID:             tc.ID,
				Name:           tc.Function.Name,
				Output:         skipMsg,
				IsError:        true,
				SiblingSkipped: true,
			})
			results = append(results, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				Content:    skipMsg,
			})
			continue
		}

		// 실제 실행
		var output string
		var isError bool
		if runTool != nil {
			output, isError = runTool(ctx, tc)
		} else {
			output = fmt.Sprintf("error: no tool runner for '%s'", tc.Function.Name)
			isError = true
		}

		send(ctx, out, EventToolResult{
			ID:      tc.ID,
			Name:    tc.Function.Name,
			Output:  output,
			IsError: isError,
		})
		results = append(results, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    output,
		})

		// mutation 실패 기록 → 다음 도구 skip
		if isError {
			mutationFailed = true
			mutationFailReason = fmt.Sprintf("'%s' returned error", tc.Function.Name)
			slog.Warn("serial tool failed, subsequent tools in batch will be skipped",
				"tool", tc.Function.Name,
			)
		}
	}

	return results, false
}
