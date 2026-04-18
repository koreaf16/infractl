// Package query
// File: recovery_test.go
// Description: LLM 에러 분류 단위 테스트

package query

import (
	"errors"
	"testing"
)

func TestClassifyLLMError_Nil(t *testing.T) {
	if got := ClassifyLLMError(nil); got != ErrorClassUnknown {
		t.Errorf("nil error: want Unknown, got %v", got)
	}
}

func TestClassifyLLMError_PTL(t *testing.T) {
	cases := []string{
		"http 413: request entity too large",
		"prompt too long for model",
		"too many tokens in context",
		"context_length_exceeded error",
	}
	for _, msg := range cases {
		got := ClassifyLLMError(errors.New(msg))
		if got != ErrorClassPromptTooLong {
			t.Errorf("msg=%q: want PromptTooLong, got %v", msg, got)
		}
	}
}

func TestClassifyLLMError_MaxOutputTokens(t *testing.T) {
	cases := []string{
		"stop_reason: max_tokens reached",
		"max output tokens exceeded",
		"hit output token limit",
	}
	for _, msg := range cases {
		got := ClassifyLLMError(errors.New(msg))
		if got != ErrorClassMaxOutputTokens {
			t.Errorf("msg=%q: want MaxOutputTokens, got %v", msg, got)
		}
	}
}

func TestClassifyLLMError_Media(t *testing.T) {
	cases := []string{
		"invalid image format",
		"unsupported media type provided",
		"image too large to process",
	}
	for _, msg := range cases {
		got := ClassifyLLMError(errors.New(msg))
		if got != ErrorClassMedia {
			t.Errorf("msg=%q: want Media, got %v", msg, got)
		}
	}
}

func TestClassifyLLMError_Fallback(t *testing.T) {
	cases := []string{
		"model is overloaded",
		"http 529: service unavailable",
		"rate limit exceeded",
		"too many requests",
	}
	for _, msg := range cases {
		got := ClassifyLLMError(errors.New(msg))
		if got != ErrorClassFallback {
			t.Errorf("msg=%q: want Fallback, got %v", msg, got)
		}
	}
}

func TestClassifyLLMError_Unknown(t *testing.T) {
	got := ClassifyLLMError(errors.New("some other error"))
	if got != ErrorClassUnknown {
		t.Errorf("want Unknown, got %v", got)
	}
}
