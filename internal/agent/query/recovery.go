// Package query
// File: recovery.go
// Description: LLM 에러 분류 스텁 — PTL / maxOutputTokens / media / fallback
// Responsibility: callLLM 에러를 Terminal 사유로 매핑하는 분류기 (Phase C 에서 실제 복구 로직 삽입)
//
// Ported from: claude_cli/src/query.ts:864-918 (에러 복구 분기)
// 핵심 패턴: isPTL → TerminalPromptTooLong, isMaxOut → escalate, isMedia → TerminalImageError,
//           isFallback → TerminalModelError

package query

import "strings"

// ErrorClass 는 LLM 호출 에러의 분류 결과다.
type ErrorClass int

const (
	// ErrorClassUnknown 은 분류되지 않은 에러다.
	ErrorClassUnknown ErrorClass = iota
	// ErrorClassPromptTooLong 은 컨텍스트 한도 초과 (HTTP 413 / "too long") 에러다.
	ErrorClassPromptTooLong
	// ErrorClassMaxOutputTokens 는 응답 토큰 한도 도달 에러다.
	ErrorClassMaxOutputTokens
	// ErrorClassMedia 는 미디어(이미지) 처리 에러다.
	ErrorClassMedia
	// ErrorClassFallback 은 재시도 가능한 일반 에러다.
	ErrorClassFallback
)

// ClassifyLLMError 는 에러를 ErrorClass 로 분류한다.
// Phase C 에서 실제 복구 로직이 이 분류를 기반으로 동작한다.
func ClassifyLLMError(err error) ErrorClass {
	if err == nil {
		return ErrorClassUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case isPTL(msg):
		return ErrorClassPromptTooLong
	case isMaxOutputTokens(msg):
		return ErrorClassMaxOutputTokens
	case isMedia(msg):
		return ErrorClassMedia
	case isFallback(msg):
		return ErrorClassFallback
	default:
		return ErrorClassUnknown
	}
}

// isPTL 은 "prompt too long" 또는 HTTP 413 관련 에러인지 판단한다.
// Phase D: 실제 Anthropic API 에러 코드 체크로 교체.
func isPTL(msg string) bool {
	return strings.Contains(msg, "413") ||
		strings.Contains(msg, "prompt too long") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "context_length_exceeded")
}

// isMaxOutputTokens 는 출력 토큰 한도 도달 에러인지 판단한다.
func isMaxOutputTokens(msg string) bool {
	return strings.Contains(msg, "max_tokens") ||
		strings.Contains(msg, "max output tokens") ||
		strings.Contains(msg, "output token limit") ||
		strings.Contains(msg, "stop_reason: max_tokens")
}

// isMedia 는 이미지/미디어 처리 에러인지 판단한다.
func isMedia(msg string) bool {
	return strings.Contains(msg, "invalid image") ||
		strings.Contains(msg, "unsupported media") ||
		strings.Contains(msg, "image too large") ||
		strings.Contains(msg, "media_type")
}

// isFallback 은 일시적 서버 에러 등 재시도 가능한 에러인지 판단한다.
func isFallback(msg string) bool {
	return strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "529") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests")
}
