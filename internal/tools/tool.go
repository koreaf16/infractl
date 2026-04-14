// Package tools
// File: tool.go
// Description: 에이전트 도구 인터페이스 및 위험도 타입 정의
// Responsibility: 도구 추상화 인터페이스와 위험도 수준 타입 제공

package tools

import (
	"context"

	"github.com/yourorg/infractl/internal/executor"
)

// RiskLevel은 도구 실행의 위험도 수준을 나타낸다.
type RiskLevel string

const (
	// RiskNone은 읽기 전용 작업으로 바로 실행한다.
	RiskNone RiskLevel = "none"
	// RiskLow는 1회 확인(y/n) 후 실행한다.
	RiskLow RiskLevel = "low"
	// RiskMedium은 2회 확인 + 대상 명시 후 실행한다.
	RiskMedium RiskLevel = "medium"
	// RiskHigh는 3회 확인 + 대상 이름 직접 입력 후 실행한다.
	RiskHigh RiskLevel = "high"
)

// RiskOrd는 RiskLevel을 정수로 변환하여 비교를 가능하게 한다.
// RiskLevel이 string 타입이라 >= 비교가 불가능하므로 이 헬퍼를 사용한다.
func RiskOrd(r RiskLevel) int {
	switch r {
	case RiskNone:
		return 0
	case RiskLow:
		return 1
	case RiskMedium:
		return 2
	case RiskHigh:
		return 3
	default:
		return 0
	}
}

// Tool은 에이전트가 LLM을 통해 호출할 수 있는 도구 인터페이스이다.
// 구현체: ShellExecTool, FileReadTool, FileWriteTool, ProcessListTool, NetworkInfoTool 등
type Tool interface {
	// Name은 도구의 고유 이름을 반환한다. (예: "shell_exec")
	Name() string

	// Description은 LLM이 도구 선택에 사용할 설명을 반환한다.
	// 언제 사용해야 하는지를 명확히 기술해야 한다.
	Description() string

	// Parameters는 OpenAI function calling 형식의 JSON Schema를 반환한다.
	Parameters() map[string]interface{}

	// RiskLevel은 이 도구 실행의 위험도 수준을 반환한다.
	RiskLevel() RiskLevel

	// IsReadOnly는 이 도구가 시스템 상태를 변경하지 않는지를 반환한다.
	// true이면 여러 도구를 병렬로 실행할 수 있다.
	// false이면 다른 도구와 직렬로 순차 실행해야 한다.
	IsReadOnly() bool

	// IsEnabled는 현재 컨텍스트에서 이 도구를 LLM에 제공할지 반환한다.
	// false이면 도구가 레지스트리에 등록되어 있어도 LLM에 전달되지 않는다.
	IsEnabled() bool

	// Execute는 도구를 실행하고 결과를 문자열로 반환한다.
	// args는 LLM이 생성한 JSON 인자를 파싱한 map이다.
	// 실행 실패도 Go error가 아닌 결과 문자열로 반환하여 LLM이 분석할 수 있게 한다.
	Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error)
}

// QuestionOption은 질문의 선택지 정보이다.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionRequest는 사용자에게 질문을 던질 때의 요청 정보이다.
type QuestionRequest struct {
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
	// Header는 선택 박스 상단에 표시되는 카테고리 라벨이다.
	// 빈 문자열이면 "Answer Questions" 가 기본으로 사용된다.
	// 보안 확인: "⚠  Security Confirm"
	// AI 질문:   "Answer Questions"
	Header string `json:"header,omitempty"`
}

// QuestionResponse는 사용자의 질문 답변 응답이다.
type QuestionResponse struct {
	SelectedIndex int    `json:"selected_index"` // 선택한 인덱스 (0~), -1이면 선택 안 함
	SelectedLabel string `json:"selected_label"` // 선택한 라벨
	UserInput     string `json:"user_input"`     // 직접 입력한 경우의 텍스트
	IsOther       bool   `json:"is_other"`       // 직접 입력 여부
}
