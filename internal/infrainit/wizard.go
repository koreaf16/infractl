// Package infrainit
// File: wizard.go
// Description: infractl init 대화형 LLM 등록 위저드 전체 흐름 조율
// Responsibility: 단일/멀티 LLM, 임베딩, 리랭커 설정을 순차적으로 수집하고 config.Save() 호출

package infrainit

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yourorg/infractl/internal/config"
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

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("infractl 초기 설정")
	fmt.Println("══════════════════════════════════")

	// 1단계: LLM 구성 방식 선택
	multiLLM := selectLLMMode(reader)

	cfg := &config.Config{}

	if multiLLM {
		// 멀티 LLM: general(필수) → reasoning(선택) → fast(선택)
		printSectionHeader("2단계", "General LLM 설정 (필수 — 에이전트의 기본 실행 모델)")
		cfg.Models.General = promptLLMConfig(ctx, reader, "http://localhost:11434/v1", "qwen3:7b")

		printSectionHeader("3단계", "Reasoning LLM 설정 (선택 — 복잡한 분석·추론 작업용)")
		if promptYN(reader, "별도 설정하시겠습니까?", false) {
			cfg.Models.Reasoning = promptLLMConfig(ctx, reader, cfg.Models.General.Endpoint, "qwen3:27b")
		}

		printSectionHeader("4단계", "Fast LLM 설정 (선택 — 단순 조회·포맷팅용 경량 모델)")
		if promptYN(reader, "별도 설정하시겠습니까?", false) {
			cfg.Models.Fast = promptLLMConfig(ctx, reader, cfg.Models.General.Endpoint, "qwen3:1.7b")
		}
	} else {
		// 단일 LLM: general 하나만
		printSectionHeader("2단계", "LLM 설정 (모든 작업에 사용)")
		cfg.Models.General = promptLLMConfig(ctx, reader, "http://localhost:11434/v1", "qwen3:7b")
	}

	// 임베딩 단계
	embeddingStep := "5단계"
	if !multiLLM {
		embeddingStep = "3단계"
	}
	printSectionHeader(embeddingStep, "임베딩 설정 (선택 — RAG 지식 베이스 검색용)")
	if promptYN(reader, "사용하시겠습니까?", false) {
		cfg.Embedding = promptEmbeddingConfig(ctx, reader)
	}

	// 리랭커 단계
	rerankerStep := "6단계"
	if !multiLLM {
		rerankerStep = "4단계"
	}
	printSectionHeader(rerankerStep, "리랭커 설정 (선택 — 검색 결과 품질 향상용)")
	if promptYN(reader, "사용하시겠습니까?", false) {
		cfg.Reranker = promptRerankerConfig(ctx, reader)
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
func selectLLMMode(reader *bufio.Reader) bool {
	fmt.Println()
	fmt.Println("[1단계] LLM 구성 방식을 선택하세요:")
	fmt.Println()
	fmt.Println("  1) 단일 LLM")
	fmt.Println("     모든 작업에 하나의 모델을 사용합니다.")
	fmt.Println("     설정이 간단하고, 일반적인 용도에 충분합니다.")
	fmt.Println()
	fmt.Println("  2) 멀티 LLM (티어 분리)")
	fmt.Println("     reasoning / general / fast 세 역할에 각각 다른 모델을 설정합니다.")
	fmt.Println("     - general  : 필수. 에이전트의 기본 실행 모델")
	fmt.Println("     - reasoning: 선택. 복잡한 분석·추론 작업에 대형 모델 사용")
	fmt.Println("     - fast     : 선택. 단순 조회·포맷팅에 경량 모델 사용")
	fmt.Println("     general만 설정하고 나머지는 건너뛸 수 있습니다.")
	fmt.Println()

	for {
		fmt.Print("  선택 [1]: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" || input == "1" {
			return false
		}
		if input == "2" {
			return true
		}
		fmt.Println("  1 또는 2를 입력하세요.")
	}
}

// promptLLMConfig는 endpoint→API key→서버 탐색→모델 선택 순으로 LLM 설정을 수집한다.
// 사용자는 host:port 형태로 입력하며, 서버 유형은 자동으로 감지된다.
func promptLLMConfig(ctx context.Context, reader *bufio.Reader, defaultEndpoint, defaultModel string) *config.LLMConfig {
	rawURL := promptText(reader, "Endpoint (예: http://192.168.1.10:11434)", defaultEndpoint)
	apiKey := promptSecret("API Key (없으면 엔터)")

	fmt.Print("  ※ 서버 탐색 중...")
	result, err := DiscoverServer(ctx, rawURL, apiKey)
	if err != nil {
		slog.Warn("서버 탐색 실패, 직접 입력으로 전환합니다", "url", rawURL, "err", err)
		fmt.Println(" 실패")
	} else {
		fmt.Printf(" %s\n", result.Type)
	}

	model := SelectModel(reader, "모델", result.Models, defaultModel)

	return &config.LLMConfig{
		Endpoint: result.Endpoint,
		Model:    model,
		APIKey:   apiKey,
		Mode:     "full",
		Timeout:  60,
	}
}

// promptEmbeddingConfig는 임베딩 설정을 수집한다.
// 임베딩 서버는 LLM과 별도 포트/서버를 쓰는 경우가 많다 (TEI, infinity-emb 등).
// host:port 입력 후 서버 유형을 자동 탐색한다.
func promptEmbeddingConfig(ctx context.Context, reader *bufio.Reader) config.EmbeddingConfig {
	rawURL := promptText(reader, "Endpoint (예: http://192.168.1.10:8080)", "http://localhost:11434")
	apiKey := promptSecret("API Key (없으면 엔터)")

	fmt.Print("  ※ 서버 탐색 중...")
	result, err := DiscoverServer(ctx, rawURL, apiKey)
	if err != nil {
		slog.Warn("임베딩 서버 탐색 실패, 직접 입력으로 전환합니다", "url", rawURL, "err", err)
		fmt.Println(" 실패")
	} else {
		fmt.Printf(" %s\n", result.Type)
	}

	model := SelectModel(reader, "임베딩 모델", result.Models, "all-minilm")

	provider := inferProvider(result.Type)
	fmt.Printf("  프로바이더: %s (자동 감지)\n", provider)

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
func promptRerankerConfig(ctx context.Context, reader *bufio.Reader) config.RerankerConfig {
	rawURL := promptText(reader, "Endpoint (예: http://192.168.1.10:8787)", "http://localhost:8787")
	apiKey := promptSecret("API Key (없으면 엔터)")

	fmt.Print("  ※ 서버 탐색 중...")
	result, err := DiscoverServer(ctx, rawURL, apiKey)
	if err != nil {
		slog.Warn("리랭커 서버 탐색 실패, 직접 입력으로 전환합니다", "url", rawURL, "err", err)
		fmt.Println(" 실패")
	} else {
		fmt.Printf(" %s\n", result.Type)
	}

	model := SelectModel(reader, "리랭커 모델", result.Models, "bge-reranker-v2-m3")

	topKStr := promptText(reader, "상위 결과 수 (top_k)", "5")
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
