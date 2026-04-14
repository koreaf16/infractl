// Package agent
// File: classify.go
// Description: General LLM의 자기 분류로 필요한 도구·섹션·실행 모델을 결정하는 로직
// Responsibility: 사용자 요청을 분석하여 ToolGroups와 PromptSections를 결정

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/yourorg/infractl/internal/cost"
	"github.com/yourorg/infractl/internal/llm"
)

// ClassifyResult는 LLM 분류 단계에서 결정된 도구·섹션·모델 선택 결과이다.
type ClassifyResult struct {
	NeedsTools     bool     `json:"needs_tools"`
	ToolGroups     []string `json:"tool_groups"`
	PromptSections []string `json:"prompt_sections"`
	Tier           string   `json:"tier"` // "fast", "general", "reasoning"
}

// classifySystemPrompt는 분류 단계의 시스템 프롬프트이다.
const classifySystemPrompt = `You are infractl, an AI infrastructure management agent.
Analyze the user's request and determine:
1. TOOLS: Which tool groups and instruction sections are needed?
2. MODEL: Which LLM tier is most suitable for execution?
   - "fast": For simple queries, formatting, summarizing search results, merging information, or trivial data processing.
   - "general": For standard infrastructure operations and shell tasks.
   - "reasoning": 복잡한 계획 수립(Plan Mode), 대규모 문서/소스코드 분석, 고난도 기술 자료 탐색/분석, 아키텍처 설계, 또는 고난도 트러블슈팅

## needs_tools 판단 기준 (CRITICAL)
Set needs_tools=FALSE for:
- Greetings, farewells: "하이", "안녕", "hi", "hello", "bye", "잘가"
- Thanks/acknowledgement: "감사", "고마워", "thanks", "ㄱㅅ", "넵", "ㅎㅎ"
- Casual chitchat with no infrastructure intent
- Simple yes/no affirmations: "응", "네", "ok", "okay"

Set needs_tools=TRUE only when the request requires:
- Reading server/system state (CPU, memory, disk, processes, logs)
- Executing commands or managing files on a server
- Querying or modifying databases, services, or containers
- Searching/registering knowledge, managing SSH servers

Examples:
- "하이?" → needs_tools: false, tier: fast
- "안녕하세요" → needs_tools: false, tier: fast
- "고마워" → needs_tools: false, tier: fast
- "서버 목록 보여줘" → needs_tools: true, tool_groups: ["server_mgmt"]
- "CPU 사용량 확인해줘" → needs_tools: true, tool_groups: ["system_info"]
- "로그 확인해줘" → needs_tools: true, tool_groups: ["file_ops", "shell"]

## Tool Groups
- system_info: Read OS info, CPU/memory/disk usage, running processes
- file_ops: Read/write files, transfer files, tail logs
- shell: Execute shell commands on the currently focused & connected server
- network: Network interface info, Kubernetes cluster queries
- service_mgmt: Check/start/stop system services (systemd, etc.)
- server_mgmt: Register, remove, or focus which server to operate on
- connector: SSH/DB remote connections; activate connectors to the focused server
- discovery: Auto-detect services on a server, probe connector types
- knowledge: Search/store knowledge base entries and RAG documents
- web: Web search and URL fetch
- orchestration: Background jobs, subagents, checkpoints, scheduling

Use the 'classify_request' tool to specify your requirements.`

// shortChatPattern은 LLM 호출 없이 즉시 needs_tools=false로 판단할 수 있는 짧은 인사/잡담 패턴이다.
var shortChatPattern = regexp.MustCompile(`(?i)^(하이|안녕|hi|hello|hey|ㅎㅇ|ㅎㅎ|감사|고마워|thank(s| you)?|ㄱㅅ|넵|네|응|ㅇㅇ|ok|okay|bye|ㅂㅂ|잘자|잘가|수고|좋아|알겠어|알겠습니다|오케이|굿)[?!.\s]*$`)

// runSelfClassification은 General LLM을 통해 사용자 요청에 필요한 도구·섹션·모델을 결정한다.
// 짧은 인사/잡담은 LLM 호출 없이 즉시 needs_tools=false로 반환한다.
func (a *Agent) runSelfClassification(ctx context.Context, userInput string) (ClassifyResult, error) {
	trimmed := strings.TrimSpace(userInput)

	// 빠른 경로: 명백한 인사/잡담은 LLM 호출 없이 즉시 반환
	if len([]rune(trimmed)) <= 20 && shortChatPattern.MatchString(trimmed) {
		slog.Debug("fast-path classification: greeting/chat detected", "input", trimmed)
		llm.LogSystemEvent("Classification", "Fast-path triggered: greeting/chat detected.\nSelected Tier: fast\nNeedsTools: false")
		return ClassifyResult{NeedsTools: false, Tier: "fast"}, nil
	}

	client, _, modelName := a.resolveClientForTier("general")

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: classifySystemPrompt},
		{Role: llm.RoleUser, Content: userInput},
	}

	toolDefs := []llm.ToolDef{
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        "classify_request",
				Description: "Load specific tools, context sections, and select execution model for the current request.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"needs_tools": map[string]interface{}{
							"type":        "boolean",
							"description": "True if tools are needed to fulfill the request. False for greetings, chitchat, thanks, or simple conversation.",
						},
						"tool_groups": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
								"enum": []string{
									"system_info", "file_ops", "shell", "network",
									"service_mgmt", "discovery", "knowledge", "web",
									"server_mgmt", "connector", "orchestration",
								},
							},
						},
						"prompt_sections": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
								"enum": []string{
									"safety", "install", "error_recovery", "grounding",
									"tool_priority", "tool_selection", "task_completion",
									"phase_planning", "discovery",
								},
							},
						},
						"tier": map[string]interface{}{
							"type":        "string",
							"description": "Select the execution LLM tier.",
							"enum":        []string{"fast", "general", "reasoning"},
						},
						"reasoning": map[string]interface{}{
							"type":        "string",
							"description": "Why these resources and this model were selected.",
						},
					},
					"required": []string{"needs_tools", "tier", "reasoning"},
				},
			},
		},
	}

	toolChoice := map[string]interface{}{
		"type":     "function",
		"function": map[string]interface{}{"name": "classify_request"},
	}

	resp, err := client.Chat(ctx, messages, toolDefs, toolChoice)
	if err != nil {
		return ClassifyResult{}, err
	}

	// 비용 기록 (분류 단계)
	if a.costTracker != nil {
		a.costTracker.Record(ctx, modelName, resp.InputTokens, resp.OutputTokens, cost.SourceClassify, a.currentSessionID)
	}

	if len(resp.ToolCalls) == 0 {
		// 분류 도구를 호출하지 않은 경우: 단순 대화로 간주하여 안전하게 needs_tools=false로 폴백
		slog.Debug("classify_request tool not called, falling back to needs_tools=false")
		llm.LogSystemEvent("Classification", "No classify_request tool call. Falling back to NeedsTools=false, Tier=fast.")
		return ClassifyResult{NeedsTools: false, Tier: "fast"}, nil
	}

	var result ClassifyResult
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &result); err != nil {
		return ClassifyResult{}, fmt.Errorf("parse judgment: %w", err)
	}

	// 분류 결과(도구 응답)를 명시적 시스템 이벤트로 로그에 기록
	llm.LogSystemEvent("Classification", fmt.Sprintf("Selected Tier: %s\nNeedsTools: %v\nGroups: %v\nSections: %v\nReasoning: %s", 
		result.Tier, result.NeedsTools, result.ToolGroups, result.PromptSections, getReasoningFromRaw(resp.ToolCalls[0].Function.Arguments)))

	slog.Info("LLM self-judgment", "needs_tools", result.NeedsTools, "groups", result.ToolGroups, "sections", result.PromptSections)
	return result, nil
}

func getReasoningFromRaw(raw string) string {
	var parsed struct {
		Reasoning string `json:"reasoning"`
	}
	_ = json.Unmarshal([]byte(raw), &parsed)
	return parsed.Reasoning
}
