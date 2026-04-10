// Package agent
// File: compaction_prompt.go
// Description: context compaction용 LLM 프롬프트 상수 및 응답 포매팅
// Responsibility: 인프라 도메인 특화 9섹션 요약 프롬프트 정의와 결과 포매팅

package agent

import (
	"regexp"
	"strings"
)

// compactionSystemPrompt는 compaction LLM 호출의 시스템 역할 메시지이다.
const compactionSystemPrompt = `You are summarizing a technical infrastructure management conversation for context compaction.
Be precise and factual. Respond in the same language the user was using (Korean or English).`

// infraCompactPrompt는 인프라 관리 도메인에 최적화된 9섹션 요약 지침이다.
// <analysis> 스크래치패드로 LLM이 누락 항목을 자기 검토한 후
// <summary> 블록에 최종 결과를 작성한다.
const infraCompactPrompt = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.
Your entire response must be an <analysis> block followed by a <summary> block.

Your task is to create a detailed summary of this infrastructure management conversation.
The summary will replace the conversation history — preserve all critical operational context.

Before your final summary, wrap your thinking in <analysis> tags:
1. Chronologically review each exchange
2. Identify all servers touched, commands executed, and findings discovered
3. Note any errors encountered and how they were resolved
4. Flag any pending or in-progress operations

Your <summary> MUST include these 9 sections:

1. Primary Request and Intent
   — What the operator asked for and the overall goal

2. Infrastructure Context
   — Active server: name, OS, IP/port, active session status
   — Active connectors: Oracle/MySQL/Tomcat sessions and their states
   — Servers referenced: any servers mentioned during the session

3. Operations Performed
   — Commands executed (with key output snippets)
   — Connectors activated or deactivated
   — Configuration changes made

4. Findings and Metrics
   — Performance data: CPU, memory, disk, network
   — Service states: running/stopped/degraded
   — DB metrics: tablespace usage, active sessions, locks, top SQL

5. Errors and Resolutions
   — Each error encountered and how it was resolved
   — Any workarounds or temporary fixes applied

6. All User Messages
   — List every user message verbatim (critical for intent tracking)

7. Pending Tasks
   — Tasks requested but not yet completed

8. Current Work
   — Precisely what was being done when compaction was triggered
   — Include exact command/query in progress if any

9. Next Step
   — The immediate next action if work was interrupted
   — Omit if no clear next step

Format your response exactly as:
<analysis>
[your completeness check reasoning]
</analysis>

<summary>
1. Primary Request and Intent:
[detail]

2. Infrastructure Context:
- Active server: [name] ([OS]) — [active/none]
- Active connectors: [list or "none"]
- Servers referenced: [list]

3. Operations Performed:
- [action]: [brief result]

4. Findings and Metrics:
- [metric]: [value]

5. Errors and Resolutions:
- [error]: [resolution or "unresolved"]

6. All User Messages:
- "[verbatim message]"

7. Pending Tasks:
- [task or "none"]

8. Current Work:
[precise description]

9. Next Step:
[next action or "none"]
</summary>`

// analysisTagRe는 <analysis>...</analysis> 블록을 제거하기 위한 정규식이다.
var analysisTagRe = regexp.MustCompile(`(?s)<analysis>.*?</analysis>`)

// summaryTagRe는 <summary>...</summary> 블록에서 내용을 추출하기 위한 정규식이다.
var summaryTagRe = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)

// multiNewlineRe는 3개 이상 연속 빈 줄을 정리하기 위한 정규식이다.
var multiNewlineRe = regexp.MustCompile(`\n{3,}`)

// formatCompactSummary는 LLM 응답에서 <analysis> 스크래치패드를 제거하고
// <summary> 태그 내용만 추출하여 반환한다.
// 태그가 없으면 원본 응답을 그대로 반환한다.
func formatCompactSummary(raw string) string {
	result := analysisTagRe.ReplaceAllString(raw, "")

	if m := summaryTagRe.FindStringSubmatch(result); len(m) > 1 {
		result = m[1]
	}

	result = multiNewlineRe.ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}
