// Package tui
// File: tool_summary.go
// Description: 도구 실행 결과 요약 생성 및 본문 미리보기 제한
// Responsibility: Claude CLI 스타일 결과 요약 ("⎿ Wrote N lines") 및 줄 수 제한

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/diff"
)

const maxPreviewLines = 10 // 결과 본문 기본 미리보기 줄 수

// toolSummary는 도구별 결과 요약 한 줄을 생성한다.
// 형식: "  ⎿  Done (1.2s)" 또는 "  ⎿  Wrote 84 lines to statusbar.go (0.3s)"
func toolSummary(toolName string, args map[string]any, result string, duration time.Duration, success bool) string {
	prefix := StyleResponseBracket.Render("  ⎿  ")
	dur := StyleCmdBoxDim.Render(" (" + formatElapsedShort(duration) + ")")

	switch toolName {
	case "file_write":
		return prefix + summaryFileWrite(args, result) + dur
	case "file_read":
		return prefix + summaryFileRead(args, result) + dur
	case "shell_exec":
		return prefix + summaryShellExec(result, success) + dur
	case "web_fetch":
		return prefix + summaryWebFetch(result) + dur
	case "web_search":
		return prefix + summaryWebSearch(args, result) + dur
	case "knowledge_search":
		return prefix + summarySearch(result) + dur
	default:
		// 일반 도구: 결과 첫 줄 요약
		first := firstLine(result)
		if len(first) > 80 {
			first = first[:77] + "..."
		}
		if first != "" {
			return prefix + StyleCmdBoxDim.Render(first) + dur
		}
		doneLabel := StyleSuccess.Render("Done")
		if !success {
			doneLabel = StyleError.Render("Failed")
		}
		return prefix + doneLabel + dur
	}
}

func summaryFileWrite(args map[string]any, result string) string {
	path := argString(args, "path")
	added, removed := countDiffLines(result)
	if added > 0 || removed > 0 {
		parts := []string{}
		if added > 0 {
			parts = append(parts, StyleSuccess.Render(fmt.Sprintf("+%d lines", added)))
		}
		if removed > 0 {
			parts = append(parts, StyleError.Render(fmt.Sprintf("-%d lines", removed)))
		}
		return fmt.Sprintf("Wrote %s to %s", strings.Join(parts, ", "), shortPath(path))
	}
	lines := countLines(result)
	return fmt.Sprintf("Wrote %d lines to %s", lines, shortPath(path))
}

func summaryFileRead(args map[string]any, result string) string {
	path := argString(args, "path")
	lines := countLines(result)
	return fmt.Sprintf("Read %d lines from %s", lines, shortPath(path))
}

func summaryShellExec(result string, success bool) string {
	lines := countLines(result)
	if success {
		return StyleSuccess.Render("✓") + fmt.Sprintf(" %d lines output", lines)
	}
	return StyleError.Render("✗") + fmt.Sprintf(" %d lines output (failed)", lines)
}

func summaryWebFetch(result string) string {
	size := len(result)
	if size > 1024*1024 {
		return fmt.Sprintf("Received %.1fMB", float64(size)/(1024*1024))
	}
	if size > 1024 {
		return fmt.Sprintf("Received %.1fKB", float64(size)/1024)
	}
	return fmt.Sprintf("Received %d bytes", size)
}

func summaryWebSearch(args map[string]any, result string) string {
	query := argString(args, "query")
	lines := countLines(result)
	if query != "" {
		return fmt.Sprintf("Found %d results for \"%s\"", lines, truncateStr(query, 40))
	}
	return fmt.Sprintf("Found %d results", lines)
}

func summarySearch(result string) string {
	lines := countLines(result)
	return fmt.Sprintf("Found %d matches", lines)
}

// truncateResult는 결과를 maxPreviewLines로 제한하고 초과분을 힌트로 표시한다.
func truncateResult(rendered string) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) <= maxPreviewLines {
		return rendered
	}
	preview := strings.Join(lines[:maxPreviewLines], "\n")
	remaining := len(lines) - maxPreviewLines
	hint := StyleCmdBoxDim.Render(fmt.Sprintf("  … +%d lines", remaining))
	return preview + "\n" + hint
}

// countDiffLines는 diff에서 추가/삭제 줄 수를 센다.
func countDiffLines(result string) (added, removed int) {
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
		}
	}
	// diff가 아닌 경우 0, 0 반환
	if !diff.IsDiff(result) {
		return 0, 0
	}
	return added, removed
}

// countLines는 텍스트의 줄 수를 반환한다.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// argString은 args 맵에서 문자열 값을 추출한다.
func argString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// shortPath는 긴 경로를 파일명 또는 마지막 2세그먼트로 축약한다.
func shortPath(path string) string {
	if len(path) <= 40 {
		return path
	}
	// 마지막 / 또는 \ 이후의 파일명
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return "..." + path[i:]
		}
	}
	return path
}

// firstLine은 텍스트의 첫 번째 줄을 반환한다.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
