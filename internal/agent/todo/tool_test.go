package todo

import (
	"context"
	"strings"
	"testing"
)

func TestWriteToolAcceptsTitleFallback(t *testing.T) {
	store := NewStore()
	tool := &WriteTool{Tracker: NewTracker(store)}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"todos": []map[string]interface{}{
			{"id": "1", "title": "Upload Oracle installer", "status": "pending"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(out.Content, "Upload Oracle installer") {
		t.Fatalf("rendered todo missing title fallback: %q", out.Content)
	}

	item, ok := store.Get("1")
	if !ok {
		t.Fatal("todo item was not stored")
	}
	if item.Content != "Upload Oracle installer" {
		t.Fatalf("Content = %q, want title fallback", item.Content)
	}
}

func TestWriteToolRejectsEmptyContent(t *testing.T) {
	tool := &WriteTool{Tracker: NewTracker(NewStore())}

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"todos": []map[string]interface{}{
			{"id": "1", "status": "pending"},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected missing content error")
	}
}
