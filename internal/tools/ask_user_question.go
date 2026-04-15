// Package tools
// File: ask_user_question.go
// Description: Ask a question to the user and receive structured feedback.
// Responsibility: Allow the LLM to clarify ambiguous targets or requests via the TUI.

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
)

type AskUserQuestionTool struct {
	QuestionCb func(ctx context.Context, req QuestionRequest) (QuestionResponse, error)
	FormCb     func(ctx context.Context, req FormRequest) (FormResponse, error)
}

func (t *AskUserQuestionTool) Name() string {
	return "ask_user_question"
}

func (t *AskUserQuestionTool) Description() string {
	return "사용자에게 지금 당장 필요한 단일 질문 하나를 던집니다. 선택형이면 서로 배타적인 실제 답변 후보만 options에 넣고, 자유응답이면 options를 생략하세요. 확인 항목 목록이나 후속 작업 목록을 선택지처럼 나열하지 마세요."
}

func (t *AskUserQuestionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "사용자에게 보여줄 단일 질문 내용. 여러 확인 항목을 나열하지 말고 지금 필요한 1개의 질문만 작성하세요.",
			},
			"options": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"label":       map[string]interface{}{"type": "string", "description": "선택지 라벨 (1-5단어)"},
						"description": map[string]interface{}{"type": "string", "description": "선택지에 대한 구체적인 설명"},
					},
					"required": []string{"label", "description"},
				},
				"minItems":    2,
				"maxItems":    5,
				"description": "서로 배타적인 실제 답변 후보 리스트 (2-5개). 체크리스트나 확인 필요 항목 목록을 넣지 마세요. 자유응답 질문이면 생략하세요.",
			},
			"allow_custom_input": map[string]interface{}{
				"type":        "boolean",
				"description": "선택지 외에 자유 입력도 허용할지 여부. options를 생략하면 자동으로 자유입력 모드가 됩니다.",
			},
			"header": map[string]interface{}{
				"type":        "string",
				"description": "질문 UI 상단 헤더. 비워두면 기본 헤더를 사용합니다.",
			},
			"fields": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":          map[string]interface{}{"type": "string", "description": "필드 식별자 (영문, 언더스코어)"},
						"label":         map[string]interface{}{"type": "string", "description": "사용자에게 보여줄 필드 이름"},
						"placeholder":   map[string]interface{}{"type": "string", "description": "입력란 힌트 텍스트"},
						"required":      map[string]interface{}{"type": "boolean", "description": "필수 입력 여부"},
						"default_value": map[string]interface{}{"type": "string", "description": "기본값"},
					},
					"required": []string{"name", "label"},
				},
				"minItems":    1,
				"description": "다중 필드 입력이 필요한 경우 사용. options와 함께 사용할 수 없습니다.",
			},
		},
		"required": []string{"question"},
	}
}

func (t *AskUserQuestionTool) IsReadOnly() bool {
	return true
}

func (t *AskUserQuestionTool) IsEnabled() bool {
	return true
}

func (t *AskUserQuestionTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	// fields 파라미터가 있으면 폼 입력 모드로 분기
	if rawFields, exists := args["fields"]; exists && rawFields != nil {
		return t.executeForm(ctx, args, rawFields)
	}

	if t.QuestionCb == nil {
		return "Error: question handler not initialized", nil
	}

	question, ok := args["question"].(string)
	if !ok || strings.TrimSpace(question) == "" {
		return "Error: missing or invalid question", nil
	}

	var optsRaw []interface{}
	if raw, exists := args["options"]; exists && raw != nil {
		var ok bool
		optsRaw, ok = raw.([]interface{})
		if !ok {
			return "Error: missing or invalid options", nil
		}
		if len(optsRaw) < 2 || len(optsRaw) > 5 {
			return "Error: options must contain 2-5 items when provided", nil
		}
	}

	options := make([]QuestionOption, len(optsRaw))
	for i, o := range optsRaw {
		m, ok := o.(map[string]interface{})
		if !ok {
			return fmt.Sprintf("Error: invalid option at index %d", i), nil
		}
		label, _ := m["label"].(string)
		desc, _ := m["description"].(string)
		label = strings.TrimSpace(label)
		desc = strings.TrimSpace(desc)
		if label == "" {
			return fmt.Sprintf("Error: missing label at index %d", i), nil
		}
		options[i] = QuestionOption{Label: label, Description: desc}
	}

	allowCustomInput, _ := args["allow_custom_input"].(bool)
	if len(options) == 0 {
		allowCustomInput = true
	}

	header, _ := args["header"].(string)
	header = strings.TrimSpace(header)
	if header == "" {
		header = "Clarification Needed"
	}

	resp, err := t.QuestionCb(ctx, QuestionRequest{
		Question:         strings.TrimSpace(question),
		Options:          options,
		AllowCustomInput: allowCustomInput,
		Header:           header,
	})
	if err != nil {
		return fmt.Sprintf("Error: user interaction failed: %s", err), nil
	}

	if resp.IsOther {
		if strings.TrimSpace(resp.UserInput) == "" {
			return "User chose custom input but did not provide a value.", nil
		}
		return fmt.Sprintf("User provided input: %s", resp.UserInput), nil
	}

	if resp.SelectedIndex < 0 {
		return "User canceled the question without choice.", nil
	}

	return fmt.Sprintf("User selected option %d: %s (%s)", resp.SelectedIndex+1, resp.SelectedLabel, resp.UserInput), nil
}

// executeForm은 fields 파라미터를 사용한 다중 필드 폼 입력을 처리한다.
func (t *AskUserQuestionTool) executeForm(ctx context.Context, args map[string]interface{}, rawFields interface{}) (string, error) {
	if t.FormCb == nil {
		return "Error: form handler not initialized", nil
	}

	fieldsArr, ok := rawFields.([]interface{})
	if !ok || len(fieldsArr) == 0 {
		return "Error: fields must be a non-empty array", nil
	}

	fields := make([]FormFieldDef, 0, len(fieldsArr))
	for i, rawField := range fieldsArr {
		m, ok := rawField.(map[string]interface{})
		if !ok {
			return fmt.Sprintf("Error: invalid field at index %d", i), nil
		}
		name, _ := m["name"].(string)
		label, _ := m["label"].(string)
		name = strings.TrimSpace(name)
		label = strings.TrimSpace(label)
		if name == "" || label == "" {
			return fmt.Sprintf("Error: field at index %d missing name or label", i), nil
		}
		placeholder, _ := m["placeholder"].(string)
		required, _ := m["required"].(bool)
		defaultValue, _ := m["default_value"].(string)
		fields = append(fields, FormFieldDef{
			Name:         name,
			Label:        label,
			Placeholder:  placeholder,
			Required:     required,
			DefaultValue: defaultValue,
		})
	}

	title, _ := args["question"].(string)
	title = strings.TrimSpace(title)
	header, _ := args["header"].(string)
	header = strings.TrimSpace(header)

	resp, err := t.FormCb(ctx, FormRequest{
		Title:  title,
		Fields: fields,
		Header: header,
	})
	if err != nil {
		return fmt.Sprintf("Error: form interaction failed: %s", err), nil
	}

	if resp.Cancelled {
		return "User cancelled the form without submitting.", nil
	}

	var parts []string
	for _, field := range fields {
		val := resp.Values[field.Name]
		parts = append(parts, fmt.Sprintf("%s=%s", field.Name, val))
	}
	return fmt.Sprintf("Form submitted: %s", strings.Join(parts, ", ")), nil
}
