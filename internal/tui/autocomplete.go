// Package tui
// File: autocomplete.go
// Description: readline 자동완성 제공자 (슬래시 명령, 서버명)
// Responsibility: Tab 키 입력 시 명령/서버/파일 자동완성 후보를 제공

package tui

import (
	"strings"

	"github.com/chzyer/readline"
)

// completionCommands는 기본 슬래시 명령 목록이다.
var completionCommands = []string{
	"/help", "/quit", "/exit", "/clear", "/model",
	"/tools", "/servers", "/connectors", "/mcp",
	"/sessions", "/history", "/knowledge", "/rag",
	"/cost",
}

// NewCompleter는 readline용 자동완성기를 생성한다.
// serverNames: 현재 등록된 서버 이름 목록.
func NewCompleter(serverNames []string) *readline.PrefixCompleter {
	// 슬래시 명령 아이템
	items := make([]readline.PrefixCompleterInterface, 0, len(completionCommands)+4)

	for _, cmd := range completionCommands {
		switch cmd {
		case "/sessions":
			items = append(items, readline.PcItem(cmd,
				readline.PcItem("restore"),
			))
		case "/mcp":
			items = append(items, readline.PcItem(cmd,
				readline.PcItem("reconnect"),
			))
		case "/knowledge":
			items = append(items, readline.PcItem(cmd,
				readline.PcItem("list"),
				readline.PcItem("search"),
			))
		case "/rag":
			items = append(items, readline.PcItem(cmd,
				readline.PcItem("sources"),
				readline.PcItem("search"),
			))
		case "/cost":
			items = append(items, readline.PcItem(cmd,
				readline.PcItem("week"),
				readline.PcItem("detail"),
			))
		default:
			items = append(items, readline.PcItem(cmd))
		}
	}

	return readline.NewPrefixCompleter(items...)
}

// DynamicCompleter는 런타임에 서버명 등을 반영하는 동적 자동완성기이다.
type DynamicCompleter struct {
	inner       *readline.PrefixCompleter
	serverNames []string
}

// NewDynamicCompleter는 동적 자동완성기를 생성한다.
func NewDynamicCompleter(serverNames []string) *DynamicCompleter {
	return &DynamicCompleter{
		inner:       NewCompleter(serverNames),
		serverNames: serverNames,
	}
}

// Do는 readline.AutoCompleter 인터페이스를 구현한다.
func (dc *DynamicCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])

	// 슬래시 명령이면 PrefixCompleter에 위임
	if strings.HasPrefix(lineStr, "/") {
		return dc.inner.Do(line, pos)
	}

	// "ssh " 다음에 서버명 자동완성
	if strings.HasPrefix(lineStr, "ssh ") || strings.Contains(lineStr, "서버") {
		prefix := extractLastWord(lineStr)
		return completeFromList(dc.serverNames, prefix, pos)
	}

	return nil, 0
}

// extractLastWord는 입력의 마지막 단어를 추출한다.
func extractLastWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// completeFromList는 목록에서 prefix에 매칭되는 완성 후보를 반환한다.
func completeFromList(candidates []string, prefix string, pos int) ([][]rune, int) {
	var matches [][]rune
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			suffix := c[len(prefix):]
			matches = append(matches, []rune(suffix+" "))
		}
	}
	return matches, len(prefix)
}
