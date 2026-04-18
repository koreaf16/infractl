package agent

import (
	"regexp"
)

var webIntentPattern = regexp.MustCompile(`(?i)(search the web|search the internet|browse|look up|official docs?|documentation|docs?\b|release notes?|changelog|latest|current|recent|newest|up-to-date|up to date|today|this year|verify|fact check|\bweb\b|인터넷|웹|공식\s*문서|최신|최근|검증|확인|찾아봐|검색해봐|크롤링|온라인)`)

func applyWebSearchHints(result *ClassifyResult, userInput string) bool {
	if result == nil || !webIntentPattern.MatchString(userInput) {
		return false
	}

	changed := false
	if !result.NeedsTools {
		result.NeedsTools = true
		changed = true
	}
	if result.Tier == "fast" {
		result.Tier = "general"
		changed = true
	}
	if appendUniqueStrings(&result.ToolGroups, "web") {
		changed = true
	}
	if appendUniqueStrings(&result.PromptSections, "grounding", "tool_selection") {
		changed = true
	}
	return changed
}

func appendUniqueStrings(dst *[]string, values ...string) bool {
	if dst == nil {
		return false
	}
	existing := make(map[string]bool, len(*dst))
	for _, item := range *dst {
		existing[item] = true
	}

	changed := false
	for _, value := range values {
		if existing[value] {
			continue
		}
		*dst = append(*dst, value)
		existing[value] = true
		changed = true
	}
	return changed
}
