// Package tools
// File: verify_complete.go
// Description: 작업 완료 검증 및 종료 선언 도구
// Responsibility: 에이전트가 작업을 마무리하기 전 결과를 검증했음을 시스템에 알림

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
)

// VerifyCompleteTool은 에이전트가 작업을 완전히 마쳤음을 명시적으로 선언하도록 강제하는 도구이다.
type VerifyCompleteTool struct{}

func (t *VerifyCompleteTool) Name() string {
	return "verify_complete"
}

func (t *VerifyCompleteTool) Description() string {
	return "작업이 완전히 완료되었음을 선언하고 종료합니다. 최종 결과를 보고하기 직전에 사용하며, 반드시 그 전에 결과를 검증하는 명령(ls, status 등)을 수행했어야 합니다."
}

func (t *VerifyCompleteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "수행한 작업의 최종 요약 (한국어)",
			},
			"verification_evidence": map[string]interface{}{
				"type":        "string",
				"description": "작업이 성공했음을 증명하는 증거 (예: 성공한 명령 출력 결과, 파일 생성 확인 등)",
			},
		},
		"required": []string{"summary", "verification_evidence"},
	}
}

func (t *VerifyCompleteTool) IsReadOnly() bool { return true }

func (t *VerifyCompleteTool) IsEnabled() bool {
	return true
}

func (t *VerifyCompleteTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	summary, _ := args["summary"].(string)
	evidence, _ := args["verification_evidence"].(string)

	if strings.TrimSpace(summary) == "" || strings.TrimSpace(evidence) == "" {
		return "Error: 'summary' 및 'verification_evidence' 인자가 비어 있습니다. " +
			"수행한 작업 내용과 그 결과(명령어 출력 등)를 상세히 포함하여 다시 호출해 주십시오. " +
			"이 정보 없이는 작업을 마무리할 수 없습니다.", nil
	}

	return fmt.Sprintf("작업 완료가 시스템에 공식 접수되었습니다.\n요약: %s\n증거: %s\n\n[SYSTEM] 검증이 완료되었습니다. 이제 사용자에게 최종 응답을 제공하고 대화를 마무리하십시오.", summary, evidence), nil
}
