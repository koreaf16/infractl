// Package tools
// File: ask_user_question.go
// Description: 사용자에게 다중 선택형 질문을 던지는 도구
// Responsibility: LLM이 사용자에게 의도를 묻거나 선택을 요청할 때 사용되는 도구 정의

package tools

import (
	"context"
	"fmt"

	"github.com/yourorg/infractl/internal/executor"
)

// AskUserQuestionTool은 사용자에게 질문을 던지고 답변을 받는 도구이다.
type AskUserQuestionTool struct {
	// QuestionCb는 UI와 통신하여 질문을 던지고 답변을 받는 콜백 함수이다.
	QuestionCb func(ctx context.Context, req QuestionRequest) (QuestionResponse, error)
}

func (t *AskUserQuestionTool) Name() string {
	return "ask_user_question"
}

func (t *AskUserQuestionTool) Description() string {
	return "사용자에게 질문을 던지고 여러 선택지 중 하나를 고르도록 요청합니다. 의도가 불분명하거나 여러 해결책 중 하나를 선택해야 할 때 사용하십시오."
}

func (t *AskUserQuestionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "사용자에게 물어볼 명확하고 구체적인 질문 내용 (예: '어떤 데이터베이스 버전을 설치할까요?')",
			},
			"options": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"label":       map[string]interface{}{"type": "string", "description": "선택지 라벨 (1-5단어)"},
						"description": map[string]interface{}{"type": "string", "description": "선택지에 대한 추가 정보나 영향"},
					},
					"required": []string{"label", "description"},
				},
				"minItems": 2,
				"maxItems": 5,
				"description": "사용자에게 제공할 선택지 목록 (2-5개)",
			},
		},
		"required": []string{"question", "options"},
	}
}

func (t *AskUserQuestionTool) RiskLevel() RiskLevel {
	return RiskNone // 질문 자체는 위험하지 않음
}

func (t *AskUserQuestionTool) IsReadOnly() bool {
	return true
}

func (t *AskUserQuestionTool) IsEnabled() bool {
	return true
}

func (t *AskUserQuestionTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	if t.QuestionCb == nil {
		return "Error: question handler not initialized", nil
	}

	question, ok := args["question"].(string)
	if !ok {
		return "Error: missing or invalid question", nil
	}

	optsRaw, ok := args["options"].([]interface{})
	if !ok {
		return "Error: missing or invalid options", nil
	}

	options := make([]QuestionOption, len(optsRaw))
	for i, o := range optsRaw {
		m, ok := o.(map[string]interface{})
		if !ok {
			return fmt.Sprintf("Error: invalid option at index %d", i), nil
		}
		label, _ := m["label"].(string)
		desc, _ := m["description"].(string)
		options[i] = QuestionOption{Label: label, Description: desc}
	}

	resp, err := t.QuestionCb(ctx, QuestionRequest{
		Question: question,
		Options:  options,
	})
	if err != nil {
		return fmt.Sprintf("Error: user interaction failed: %s", err), nil
	}

	if resp.IsOther {
		return fmt.Sprintf("User chose 'Other' with input: %s", resp.UserInput), nil
	}

	if resp.SelectedIndex < 0 {
		return "User canceled the question without choice.", nil
	}

	return fmt.Sprintf("User selected option %d: %s (%s)", resp.SelectedIndex+1, resp.SelectedLabel, resp.UserInput), nil
}
