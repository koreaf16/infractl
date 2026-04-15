package tools

import (
	"context"
	"strings"
	"testing"
)

func TestAskUserQuestionToolExecuteFreeTextQuestion(t *testing.T) {
	var captured QuestionRequest
	tool := &AskUserQuestionTool{
		QuestionCb: func(ctx context.Context, req QuestionRequest) (QuestionResponse, error) {
			captured = req
			return QuestionResponse{IsOther: true, UserInput: "19.20 테스트용"}, nil
		},
	}

	got, err := tool.Execute(context.Background(), map[string]interface{}{
		"question": "Oracle 19c에서 어떤 버전으로 설치할까요?",
		"header":   "Install Clarification",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.Question != "Oracle 19c에서 어떤 버전으로 설치할까요?" {
		t.Fatalf("captured question = %q", captured.Question)
	}
	if len(captured.Options) != 0 {
		t.Fatalf("captured options = %d, want 0", len(captured.Options))
	}
	if !captured.AllowCustomInput {
		t.Fatal("AllowCustomInput = false, want true")
	}
	if captured.Header != "Install Clarification" {
		t.Fatalf("Header = %q", captured.Header)
	}
	if !strings.Contains(got, "User provided input: 19.20 테스트용") {
		t.Fatalf("Execute() = %q", got)
	}
}

func TestAskUserQuestionToolExecuteSelectQuestion(t *testing.T) {
	var captured QuestionRequest
	tool := &AskUserQuestionTool{
		QuestionCb: func(ctx context.Context, req QuestionRequest) (QuestionResponse, error) {
			captured = req
			return QuestionResponse{SelectedIndex: 1, SelectedLabel: "프로덕션용"}, nil
		},
	}

	got, err := tool.Execute(context.Background(), map[string]interface{}{
		"question": "설치 목적이 무엇인가요?",
		"options": []interface{}{
			map[string]interface{}{"label": "테스트/개발용", "description": "개발 또는 검증 환경에 설치"},
			map[string]interface{}{"label": "프로덕션용", "description": "실서비스 환경에 설치"},
		},
		"allow_custom_input": false,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.AllowCustomInput {
		t.Fatal("AllowCustomInput = true, want false")
	}
	if len(captured.Options) != 2 {
		t.Fatalf("captured options = %d, want 2", len(captured.Options))
	}
	if captured.Header != "Clarification Needed" {
		t.Fatalf("Header = %q", captured.Header)
	}
	if !strings.Contains(got, "User selected option 2: 프로덕션용") {
		t.Fatalf("Execute() = %q", got)
	}
}

func TestAskUserQuestionToolExecuteInvalidOptionCount(t *testing.T) {
	tool := &AskUserQuestionTool{
		QuestionCb: func(ctx context.Context, req QuestionRequest) (QuestionResponse, error) {
			t.Fatal("QuestionCb should not be called")
			return QuestionResponse{}, nil
		},
	}

	got, err := tool.Execute(context.Background(), map[string]interface{}{
		"question": "하나만 있는 선택지",
		"options": []interface{}{
			map[string]interface{}{"label": "하나", "description": "설명"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "options must contain 2-5 items") {
		t.Fatalf("Execute() = %q", got)
	}
}
