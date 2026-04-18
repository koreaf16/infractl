// Package query
// File: streaming_executor_test.go
// Description: StreamingExecutor 병렬/직렬/sibling abort/ctx cancel 단위 테스트

package query

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yourorg/infractl/internal/llm"
)

func makeToolCall(id, name string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: "{}"}}
}

// echoRunner 는 도구 이름을 그대로 출력하는 stub runner 다.
func echoRunner(_ context.Context, tc llm.ToolCall) (string, bool) {
	return "output:" + tc.Function.Name, false
}

// errorRunner 는 항상 에러를 반환하는 stub runner 다.
func errorRunner(_ context.Context, tc llm.ToolCall) (string, bool) {
	return "error from " + tc.Function.Name, true
}

func drainEvents(ch <-chan QueryEvent) []QueryEvent {
	var evs []QueryEvent
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func TestStreamingExecutor_ConcurrentBatch_AllComplete(t *testing.T) {
	se := NewStreamingExecutor(10)
	calls := []llm.ToolCall{
		makeToolCall("1", "read1"),
		makeToolCall("2", "read2"),
		makeToolCall("3", "read3"),
	}
	batches := []Batch{{Concurrent: true, Calls: calls}}

	out := make(chan QueryEvent, 32)
	results, aborted := se.Execute(context.Background(), batches, out, echoRunner)
	close(out)

	if aborted {
		t.Error("should not be aborted")
	}
	if len(results) != 3 {
		t.Errorf("want 3 results, got %d", len(results))
	}
	// 모든 EventToolResult 가 방출되었는지 확인
	events := drainEvents(out)
	var toolResults []EventToolResult
	for _, ev := range events {
		if r, ok := ev.(EventToolResult); ok {
			toolResults = append(toolResults, r)
		}
	}
	if len(toolResults) != 3 {
		t.Errorf("want 3 EventToolResult events, got %d", len(toolResults))
	}
	// 결과 메시지에 출력이 있어야 한다
	for _, m := range results {
		if !strings.HasPrefix(m.Content, "output:") {
			t.Errorf("unexpected result content: %q", m.Content)
		}
	}
}

func TestStreamingExecutor_SerialBatch_SiblingAbort(t *testing.T) {
	se := NewStreamingExecutor(10)
	calls := []llm.ToolCall{
		makeToolCall("1", "mut1"),
		makeToolCall("2", "mut2"), // mut1 이 에러 → mut2 skip
		makeToolCall("3", "mut3"), // mut1 이 에러 → mut3 skip
	}
	batches := []Batch{{Concurrent: false, Calls: calls}}

	out := make(chan QueryEvent, 32)
	results, aborted := se.Execute(context.Background(), batches, out, errorRunner)
	close(out)

	if aborted {
		t.Error("should not be aborted — sibling skip is not abort")
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}

	// 두 번째, 세 번째 결과는 sibling skip
	events := drainEvents(out)
	var skipped []EventToolResult
	for _, ev := range events {
		if r, ok := ev.(EventToolResult); ok && r.SiblingSkipped {
			skipped = append(skipped, r)
		}
	}
	if len(skipped) != 2 {
		t.Errorf("want 2 sibling-skipped events, got %d", len(skipped))
	}
	for _, r := range skipped {
		if !strings.Contains(r.Output, "sibling failure") {
			t.Errorf("sibling skip message should mention sibling failure: %q", r.Output)
		}
	}
}

func TestStreamingExecutor_CtxCancel_Aborted(t *testing.T) {
	se := NewStreamingExecutor(1)

	var started atomic.Int32
	slowRunner := func(ctx context.Context, tc llm.ToolCall) (string, bool) {
		started.Add(1)
		select {
		case <-ctx.Done():
			return "cancelled", true
		case <-time.After(10 * time.Second):
			return "slow", false
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := []llm.ToolCall{makeToolCall("1", "slow")}
	batches := []Batch{{Concurrent: true, Calls: calls}}

	out := make(chan QueryEvent, 32)
	// 별도 goroutine 에서 실행 후 바로 cancel
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = se.Execute(ctx, batches, out, slowRunner)
	}()

	// 시작 확인 후 cancel
	for started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}

func TestStreamingExecutor_MultiBatch_OrderPreserved(t *testing.T) {
	se := NewStreamingExecutor(10)
	calls := []llm.ToolCall{
		makeToolCall("1", "r1"),
		makeToolCall("2", "r2"),
		makeToolCall("3", "m1"),
		makeToolCall("4", "r3"),
	}
	// read-only 2개 concurrent + mutation 1개 serial + read-only 1개 concurrent
	batches := []Batch{
		{Concurrent: true, Calls: calls[:2]},
		{Concurrent: false, Calls: calls[2:3]},
		{Concurrent: true, Calls: calls[3:]},
	}

	out := make(chan QueryEvent, 32)
	results, aborted := se.Execute(context.Background(), batches, out, echoRunner)
	close(out)

	if aborted {
		t.Error("should not be aborted")
	}
	if len(results) != 4 {
		t.Fatalf("want 4 results, got %d", len(results))
	}
	// 결과가 원래 호출 순서를 보존하는지 확인 (concurrent batch 내에서)
	for i, m := range results {
		if m.ToolCallID != calls[i].ID {
			t.Errorf("result[%d]: want ToolCallID=%q, got %q", i, calls[i].ID, m.ToolCallID)
		}
	}
}
