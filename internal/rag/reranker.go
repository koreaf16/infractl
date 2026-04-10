// Package rag
// File: reranker.go
// Description: 리랭커 인터페이스 및 Cohere 호환 HTTP 클라이언트 구현
// Responsibility: 검색 결과를 교차 인코더 모델로 재순위화하여 최종 top-K 반환

package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/yourorg/infractl/internal/config"
)

// Reranker는 검색 결과를 관련도 순으로 재정렬하는 인터페이스이다.
// nil이면 리랭킹 단계를 건너뛴다.
type Reranker interface {
	// Rerank는 query에 대한 documents의 관련도를 점수화하여 상위 topK 결과를 반환한다.
	Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error)
}

// RerankResult는 단일 리랭킹 결과이다.
type RerankResult struct {
	Index int     // 원본 documents 슬라이스에서의 인덱스
	Score float64 // 리랭커 모델의 관련도 점수 (높을수록 관련성 높음)
}

// NewReranker는 설정에 따라 Reranker를 생성한다.
// Endpoint 또는 Model이 비어 있으면 nil을 반환한다 (리랭킹 비활성화).
func NewReranker(cfg config.RerankerConfig) Reranker {
	if cfg.Endpoint == "" || cfg.Model == "" {
		return nil
	}
	topK := cfg.TopK
	if topK == 0 {
		topK = 5
	}
	return &httpReranker{
		endpoint: cfg.Endpoint,
		model:    cfg.Model,
		apiKey:   cfg.APIKey,
		topK:     topK,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// httpReranker는 Cohere 호환 /rerank API를 사용하는 리랭커 구현체이다.
type httpReranker struct {
	endpoint string
	model    string
	apiKey   string
	topK     int
	client   *http.Client
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

// Rerank는 Cohere 호환 /rerank 엔드포인트를 호출하여 결과를 재순위화한다.
func (r *httpReranker) Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
	if topK <= 0 {
		topK = r.topK
	}

	reqBody := rerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      topK,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rawBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rerank status %d: %s", resp.StatusCode, rawBody)
	}

	var result rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	slog.Debug("reranker response", "query_len", len(query), "docs", len(documents), "results", len(result.Results))

	out := make([]RerankResult, 0, len(result.Results))
	for _, r := range result.Results {
		out = append(out, RerankResult{
			Index: r.Index,
			Score: r.RelevanceScore,
		})
	}
	return out, nil
}
