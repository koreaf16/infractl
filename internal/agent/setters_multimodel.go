// Package agent
// File: setters_multimodel.go
// Description: 멀티 LLM 레지스트리 및 라우터 주입 세터
// Responsibility: Agent에 llmRegistry와 llmRouter를 주입하는 인터페이스 제공

package agent

import "github.com/yourorg/infractl/internal/llm"

// SetLLMRegistry는 멀티 LLM 티어 레지스트리를 주입한다.
// 주입하면 서브에이전트 위임 시 티어별 클라이언트를 선택할 수 있다.
func (a *Agent) SetLLMRegistry(reg *llm.Registry) {
	a.llmRegistry = reg
}

// SetLLMRouter는 LLM 티어 라우터를 주입한다.
// 주입하면 작업 복잡도에 따라 general LLM이 티어를 선택하여 위임한다.
func (a *Agent) SetLLMRouter(r *llm.Router) {
	a.llmRouter = r
}
