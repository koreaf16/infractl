// Package tui
// File: slash_detect.go
// Description: 슬래시 명령 vs 경로(/) 구분 함수
// Responsibility: 입력이 슬래시 명령인지 아닌지 판단 (경로와 혼동 방지)

package tui

import "strings"

// knownTUICommands는 TUI 모드에서 처리되는 슬래시 명령 집합이다.
var knownTUICommands = map[string]bool{
	"/quit": true, "/exit": true, "/q": true,
	"/help": true,
	"/tools": true, "/clear": true, "/model": true,
	"/servers": true, "/server": true,
	"/connectors": true, "/mcp": true,
	"/sessions": true, "/history": true,
	"/yoro": true, "/knowledge": true, "/rag": true,
	"/cost": true, "/checkpoints": true, "/hooks": true, "/schedules": true,
	"/osessions": true,
}

// IsSlashCommand는 입력이 슬래시 명령인지 판단한다.
// /home/sandbox 처럼 첫 토큰에 '/'가 2개 이상이면 경로로 간주하여 false를 반환한다.
// /help, /servers 처럼 알려진 명령인 경우에만 true를 반환한다.
func IsSlashCommand(input string) bool {
	if !strings.HasPrefix(input, "/") {
		return false
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false
	}
	word := fields[0]
	// /home/sandbox/download 처럼 '/'가 여러 개면 경로
	if strings.Count(word, "/") != 1 {
		return false
	}
	return knownTUICommands[word]
}

// IsSlashCommandPrefix는 자동완성 트리거 여부를 판단한다.
// 현재까지 입력된 텍스트가 슬래시 명령을 타이핑 중인 경우 true를 반환한다.
// /home/ 같은 경로 입력 중에는 false를 반환하여 자동완성을 억제한다.
func IsSlashCommandPrefix(lineStr string) bool {
	if !strings.HasPrefix(lineStr, "/") {
		return false
	}
	// 공백이 포함되면 첫 단어만 검사
	word := lineStr
	if idx := strings.IndexByte(lineStr, ' '); idx >= 0 {
		word = lineStr[:idx]
	}
	// 첫 토큰에 '/'가 2개 이상이면 경로
	return strings.Count(word, "/") == 1
}
