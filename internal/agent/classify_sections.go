// Package agent
// File: classify_sections.go
// Description: 분류 결과와 에이전트 상태를 조합하여 SectionSet을 생성하는 로직
// Responsibility: 명시된 섹션 목록과 상태 게이팅을 SectionSet으로 변환

package agent

import "log/slog"

// sectionNameMap은 classify_request 도구의 section 이름을 PromptSection 상수로 매핑한다.
// 하나의 이름이 여러 섹션을 활성화할 수 있다 (예: grounding → grounding + ports).
var sectionNameMap = map[string][]PromptSection{
	"safety":          {SectionSafety},
	"error_recovery":  {SectionErrorRecovery},
	"grounding":       {SectionGrounding},
	"tool_priority":   {SectionToolPriority},
	"tool_selection":  {SectionToolSelection},
	"task_completion": {SectionTaskCompletion},
	"phase_planning":  {SectionPhasePlanning},
	"discovery":       {SectionDiscovery},
}

func ResolveAllSections(
	hasActiveServer bool,
	hasServers bool,
	hasConnectors bool,
	hasLearnedSystems bool,
	hasInfractlMD bool,
) SectionSet {
	allSections := []string{
		"safety", "install", "error_recovery", "grounding",
		"tool_priority", "tool_selection", "task_completion",
		"phase_planning", "discovery",
	}
	return ResolveSectionsFromList(
		allSections,
		true, // needsTools: ResolveAllSections는 항상 도구 포함
		hasActiveServer,
		hasServers,
		hasConnectors,
		hasLearnedSystems,
		hasInfractlMD,
	)
}

// ResolveSectionsFromList는 명시된 섹션 이름 목록과 에이전트 상태를 기반으로 SectionSet을 조립한다.
//
// 항상 포함 섹션: SectionCore, SectionEnvironment
// needsTools 게이팅: SectionTools, SectionRAG, SectionBehavior, SectionServerFocus 등 무거운 섹션
// 상태 게이팅: hasServers, hasActiveServer, hasConnectors, hasLearnedSystems, hasInfractlMD
func ResolveSectionsFromList(
	promptSections []string,
	needsTools bool,
	hasActiveServer bool,
	hasServers bool,
	hasConnectors bool,
	hasLearnedSystems bool,
	hasInfractlMD bool,
) SectionSet {
	s := SectionSet{
		SectionCore:        true,
		SectionEnvironment: true,
		SectionBehavior:    needsTools,
		SectionServerFocus: needsTools,
	}

	// 도구 사용 시에만 포함되는 무거운 섹션들
	if needsTools {
		s[SectionTools] = true
		s[SectionRAG] = true
		if hasServers {
			s[SectionServers] = true
		}
		if !hasActiveServer {
			s[SectionGuardrails] = true
		}
		if hasConnectors {
			s[SectionConnectors] = true
		}
		if hasLearnedSystems {
			s[SectionLearnedSystems] = true
		}
	}

	if hasInfractlMD {
		s[SectionInfractlMD] = true
	}

	// 요청된 섹션 추가
	for _, name := range promptSections {
		sections, ok := sectionNameMap[name]
		if !ok {
			slog.Warn("unknown prompt section", "section", name)
			continue
		}
		for _, sec := range sections {
			s[sec] = true
		}
	}

	return s
}
