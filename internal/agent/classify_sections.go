package agent

import "log/slog"

var sectionNameMap = map[string][]PromptSection{
	"safety":          {SectionSafety},
	"error_recovery":  {SectionErrorRecovery},
	"grounding":       {SectionGrounding},
	"tool_priority":   {SectionToolPriority},
	"tool_selection":  {SectionToolSelection},
	"task_completion": {SectionTaskCompletion},
	"phase_planning":  {SectionPhasePlanning},
	"discovery":       {SectionDiscovery},
	"rag":             {SectionRAG},
	"behavior":        {SectionBehavior},
	"servers":         {SectionServers},
	"connectors":      {SectionConnectors},
	"learned_systems": {SectionLearnedSystems},
}

func ResolveAllSections(
	hasActiveServer bool,
	hasServers bool,
	hasConnectors bool,
	hasLearnedSystems bool,
	hasInfractlMD bool,
) SectionSet {
	allSections := []string{
		"safety", "error_recovery", "grounding", "tool_priority", "tool_selection",
		"task_completion", "phase_planning", "discovery", "rag", "behavior",
		"servers", "connectors", "learned_systems",
	}
	return ResolveSectionsFromList(
		allSections,
		true,
		hasActiveServer,
		hasServers,
		hasConnectors,
		hasLearnedSystems,
		hasInfractlMD,
	)
}

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
	}

	if needsTools {
		s[SectionTools] = true
		s[SectionServerFocus] = true
		if !hasActiveServer {
			s[SectionGuardrails] = true
		}
	}

	if hasInfractlMD {
		s[SectionInfractlMD] = true
	}

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

	// State-gated sections only when explicitly requested.
	if !hasServers {
		delete(s, SectionServers)
	}
	if !hasConnectors {
		delete(s, SectionConnectors)
	}
	if !hasLearnedSystems {
		delete(s, SectionLearnedSystems)
	}

	return s
}
