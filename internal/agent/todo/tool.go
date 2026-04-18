// Package todo defines TodoWrite and TodoRead tools for the agent.
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/tools"
)

// WriteToolName is the LLM-facing TodoWrite tool name.
const WriteToolName = "TodoWrite"

// ReadToolName is the LLM-facing TodoRead tool name.
const ReadToolName = "TodoRead"

// WriteTool writes or updates the session todo list.
type WriteTool struct {
	Tracker *Tracker
}

func (t *WriteTool) Name() string { return WriteToolName }
func (t *WriteTool) Description() string {
	return "Write or update the todo list for a multi-step task. Use this before mutation tools when the task has several steps."
}
func (t *WriteTool) IsReadOnly() bool { return false }
func (t *WriteTool) IsEnabled() bool  { return true }

func (t *WriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"todos": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":      map[string]interface{}{"type": "string", "description": "unique todo id"},
						"content": map[string]interface{}{"type": "string", "description": "todo text"},
						"title":   map[string]interface{}{"type": "string", "description": "legacy alias for content"},
						"status":  map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "completed"}, "description": "todo status"},
						"deps":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "dependency todo ids"},
					},
					"required": []string{"id", "status"},
				},
				"description": "todo items",
			},
		},
		"required": []string{"todos"},
	}
}

func (t *WriteTool) Execute(_ context.Context, args map[string]interface{}, _ executor.Executor) (tools.ToolOutcome, error) {
	raw, err := json.Marshal(args["todos"])
	if err != nil {
		return tools.ToolOutcome{}, fmt.Errorf("todo write: marshal todos: %w", err)
	}
	var items []struct {
		ID      string   `json:"id"`
		Content string   `json:"content"`
		Title   string   `json:"title"`
		Status  string   `json:"status"`
		Deps    []string `json:"deps"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return tools.ToolOutcome{}, fmt.Errorf("todo write: parse todos: %w", err)
	}

	for _, it := range items {
		content := strings.TrimSpace(it.Content)
		if content == "" {
			content = strings.TrimSpace(it.Title)
		}
		if content == "" {
			err := fmt.Errorf("todo write: item %q requires content", it.ID)
			return tools.ToolOutcome{ErrorMessage: err.Error()}, err
		}

		item := TodoItem{
			ID:      it.ID,
			Content: content,
			Status:  Status(it.Status),
			Deps:    it.Deps,
		}
		existing, exists := t.Tracker.Store().Get(it.ID)
		if !exists {
			if err := t.Tracker.Add(item); err != nil {
				return tools.ToolOutcome{ErrorMessage: err.Error()}, fmt.Errorf("todo write: add: %w", err)
			}
			continue
		}
		if Status(it.Status) == existing.Status {
			continue
		}
		switch Status(it.Status) {
		case StatusInProgress:
			if err := t.Tracker.SetInProgress(it.ID); err != nil {
				return tools.ToolOutcome{ErrorMessage: err.Error()}, fmt.Errorf("todo write: set in_progress: %w", err)
			}
		case StatusCompleted:
			if err := t.Tracker.SetCompleted(it.ID); err != nil {
				return tools.ToolOutcome{ErrorMessage: err.Error()}, fmt.Errorf("todo write: set completed: %w", err)
			}
		default:
			if err := t.Tracker.Store().Update(it.ID, Status(it.Status)); err != nil {
				return tools.ToolOutcome{ErrorMessage: err.Error()}, fmt.Errorf("todo write: update: %w", err)
			}
		}
	}

	return tools.ToolOutcome{
		Content: Render(t.Tracker.Store().List()),
		Success: true,
	}, nil
}

// ReadTool returns the current todo list.
type ReadTool struct {
	Store *Store
}

func (t *ReadTool) Name() string        { return ReadToolName }
func (t *ReadTool) Description() string { return "Read the current todo list." }
func (t *ReadTool) IsReadOnly() bool    { return true }
func (t *ReadTool) IsEnabled() bool     { return true }

func (t *ReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ReadTool) Execute(_ context.Context, _ map[string]interface{}, _ executor.Executor) (tools.ToolOutcome, error) {
	return tools.ToolOutcome{
		Content: Render(t.Store.List()),
		Success: true,
	}, nil
}
