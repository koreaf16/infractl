// Package infrainit
// File: wizard.go
// Description: infractl init 대화형 LLM 등록 위저드 전체 흐름 조율
// Responsibility: 단일/멀티 LLM, 임베딩, 리랭커 설정을 순차적으로 수집하고 config.Save() 호출

package infrainit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/tui"
)

// Run은 위저드의 전체 흐름을 실행한다.
// ctx는 /v1/models HTTP 호출에 전파된다.
func Run(ctx context.Context) error {
	dir, err := config.DefaultConfigDir()
	if err != nil {
		return fmt.Errorf("get config dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	fmt.Println()
	borderColor := lipgloss.NewStyle().Foreground(tui.ColorGeminiBox)
	width := 50
	sep := strings.Repeat("─", width)
	fmt.Println(borderColor.Render("╭"+sep+"╮"))
	fmt.Println(borderColor.Render("│") + " " +
		tui.StyleGeminiHeader.Render("infractl") +
		tui.StyleGeminiSubDesc.Render(" — 초기 설정 위저드"))
	fmt.Println(borderColor.Render("╰"+sep+"╯"))
	fmt.Println()

	// 1단계: LLM 구성 방식 선택
	multiLLM := selectLLMMode()

	cfg := &config.Config{}

	if multiLLM {
		// 멀티 LLM: general(필수) → reasoning(선택) → fast(선택)
		printSectionHeader("2단계", "General LLM 설정 (필수 — 에이전트의 기본 실행 모델)")
		cfg.Models.General = promptLLMConfig(ctx, "http://localhost:11434/v1", "qwen3:7b")

		printSectionHeader("3단계", "Reasoning LLM 설정 (선택 — 복잡한 분석·추론 작업용)")
		if promptYN("별도 설정하시겠습니까?", false) {
			cfg.Models.Reasoning = promptLLMConfig(ctx, cfg.Models.General.Endpoint, "qwen3:27b")
		}

		printSectionHeader("4단계", "Fast LLM 설정 (선택 — 단순 조회·포맷팅용 경량 모델)")
		if promptYN("별도 설정하시겠습니까?", false) {
			cfg.Models.Fast = promptLLMConfig(ctx, cfg.Models.General.Endpoint, "qwen3:1.7b")
		}
	} else {
		// 단일 LLM: general 하나만
		printSectionHeader("2단계", "LLM 설정 (모든 작업에 사용)")
		cfg.Models.General = promptLLMConfig(ctx, "http://localhost:11434/v1", "qwen3:7b")
	}

	// 임베딩 단계
	embeddingStep := "5단계"
	if !multiLLM {
		embeddingStep = "3단계"
	}
	printSectionHeader(embeddingStep, "임베딩 설정 (선택 — RAG 지식 베이스 검색용)")
	if promptYN("사용하시겠습니까?", false) {
		cfg.Embedding = promptEmbeddingConfig(ctx)
	}

	// 리랭커 단계
	rerankerStep := "6단계"
	if !multiLLM {
		rerankerStep = "4단계"
	}
	printSectionHeader(rerankerStep, "리랭커 설정 (선택 — 검색 결과 품질 향상용)")
	if promptYN("사용하시겠습니까?", false) {
		cfg.Reranker = promptRerankerConfig(ctx)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	path := filepath.Join(dir, "config.yaml")
	printSuccess("설정 저장: " + path)
	return nil
}

// selectLLMMode는 단일/멀티 LLM 구성 방식을 선택받는다.
// true=멀티, false=단일.
func selectLLMMode() bool {
	opts := []tui.SelectOption{
		{
			Label:       "단일 LLM",
			Description: "모든 작업에 하나의 모델을 사용합니다. 일반적인 용도에 충분합니다.",
			HideOther:   true,
		},
		{
			Label:       "멀티 LLM (티어 분리)",
			Description: "reasoning / general / fast 세 역할에 각각 다른 모델을 설정합니다.",
			HideOther:   true,
		},
	}

	result := tui.RunSelect("[1단계] LLM 구성 방식을 선택하세요", opts, 80)
	return result.Index == 1
}

// promptLLMConfig는 endpoint→API key→서버 탐색→모델 선택 순으로 LLM 설정을 수집한다.
// 사용자는 host:port 형태로 입력하며, 서버 유형은 자동으로 감지된다.
func promptLLMConfig(ctx context.Context, defaultEndpoint, defaultModel string) *config.LLMConfig {
	rawURL := promptText("Endpoint (예: http://192.168.1.10:11434)", defaultEndpoint)
	apiKey := promptSecret("API Key (없으면 엔터)")

	printProgress("서버 탐색 중...")
	result, err := DiscoverServer(ctx, rawURL, apiKey)
	if err != nil {
		slog.Warn("서버 탐색 실패, 직접 입력으로 전환합니다", "url", rawURL, "err", err)
		printProgressResult("", false)
	} else {
		printProgressResult(string(result.Type), true)
	}

	model := SelectModel("모델", result.Models, defaultModel)

	return &config.LLMConfig{
		Endpoint: result.Endpoint,
		Model:    model,
		APIKey:   apiKey,
		Mode:     "full",
		Timeout:  60,
	}
}

// printProgress는 탐색/연결 진행 중 메시지를 Gemini 스타일로 출력한다.
func printProgress(msg string) {
	fmt.Print(tui.StyleGeminiSubDesc.Render("  ⟳ "+msg))
}

// printProgressResult는 탐색 결과(성공/실패)를 한 줄로 출력한다.
func printProgressResult(serverType string, ok bool) {
	if ok {
		fmt.Println(" " + tui.StyleSuccess.Render("✓") + " " + tui.StyleGeminiOption.Render(serverType))
	} else {
		fmt.Println(" " + tui.StyleError.Render("✗ 실패"))
	}
}

// promptEmbeddingConfig는 임베딩 설정을 수집한다.
// 임베딩 서버는 LLM과 별도 포트/서버를 쓰는 경우가 많다 (TEI, infinity-emb 등).
// host:port 입력 후 서버 유형을 자동 탐색한다.
func promptEmbeddingConfig(ctx context.Context) config.EmbeddingConfig {
	rawURL := promptText("Endpoint (예: http://192.168.1.10:8080)", "http://localhost:11434")
	apiKey := promptSecret("API Key (없으면 엔터)")

	printProgress("임베딩 서버 탐색 중...")
	result, err := DiscoverServer(ctx, rawURL, apiKey)
	if err != nil {
		slog.Warn("임베딩 서버 탐색 실패, 직접 입력으로 전환합니다", "url", rawURL, "err", err)
		printProgressResult("", false)
	} else {
		printProgressResult(string(result.Type), true)
	}

	model := SelectModel("임베딩 모델", result.Models, "all-minilm")

	provider := inferProvider(result.Type)
	fmt.Println(tui.StyleGeminiSubDesc.Render("  Provider: ") + tui.StyleGeminiOption.Render(provider) + tui.StyleGeminiSubDesc.Render(" (자동 감지)"))

	return config.EmbeddingConfig{
		Provider: provider,
		Endpoint: result.Endpoint,
		Model:    model,
		APIKey:   apiKey,
	}
}

// promptRerankerConfig는 리랭커 설정을 수집한다.
// 리랭커 서버도 LLM/임베딩과 다른 엔드포인트를 쓰는 경우가 많다.
// host:port 입력 후 서버 유형을 자동 탐색한다.
func promptRerankerConfig(ctx context.Context) config.RerankerConfig {
	rawURL := promptText("Endpoint (예: http://192.168.1.10:8787)", "http://localhost:8787")
	apiKey := promptSecret("API Key (없으면 엔터)")

	printProgress("리랭커 서버 탐색 중...")
	result, err := DiscoverServer(ctx, rawURL, apiKey)
	if err != nil {
		slog.Warn("리랭커 서버 탐색 실패, 직접 입력으로 전환합니다", "url", rawURL, "err", err)
		printProgressResult("", false)
	} else {
		printProgressResult(string(result.Type), true)
	}

	model := SelectModel("리랭커 모델", result.Models, "bge-reranker-v2-m3")

	topKStr := promptText("상위 결과 수 (top_k)", "5")
	topK, err := strconv.Atoi(topKStr)
	if err != nil || topK < 1 {
		topK = 5
	}

	return config.RerankerConfig{
		Endpoint: result.Endpoint,
		Model:    model,
		APIKey:   apiKey,
		TopK:     topK,
	}
}

// inferProvider는 감지된 서버 유형으로부터 EmbeddingConfig.Provider 값을 결정한다.
func inferProvider(t ServerType) string {
	switch t {
	case ServerOllama:
		return "ollama"
	case ServerOpenAI:
		return "openai"
	default:
		return "custom"
	}
}
