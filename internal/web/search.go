// Package web
// File: search.go
// Description: SearXNG JSON API 웹 검색 클라이언트
// Responsibility: SearXNG 인스턴스에 질의하여 검색 결과 반환

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

const (
	SearXNGBaseURL = "http://192.168.0.3:30080"
	searchTimeout  = 10 * time.Second
	maxResults     = 10
)

// SearchResult는 웹 검색 결과 단일 항목이다.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searxngResponse는 SearXNG JSON 응답 구조이다.
type searxngResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

// Search는 SearXNG에서 query를 검색하여 최대 limit개의 결과를 반환한다.
func Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > maxResults {
		limit = 5
	}

	reqCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	params := url.Values{
		"q":      {query},
		"format": {"json"},
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, SearXNGBaseURL+"/search", strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("searxng search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: searchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("searxng search failed", "query", query, "err", err)
		return nil, fmt.Errorf("searxng unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("searxng search error status", "query", query, "status", resp.Status)
		return nil, fmt.Errorf("searxng returned status %s (check if JSON format is enabled)", resp.Status)
	}

	// 인코딩 자동 감지 및 변환 리더 생성 (UTF-8로 변환)
	utf8Reader, err := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("create charset reader: %w", err)
	}

	body, err := io.ReadAll(io.LimitReader(utf8Reader, 512*1024))
	if err != nil {
		slog.Warn("searxng read body failed", "err", err)
		return nil, nil
	}

	var parsed searxngResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		slog.Warn("searxng parse failed", "err", err)
		return nil, nil
	}

	results := make([]SearchResult, 0, limit)
	for _, r := range parsed.Results {
		if len(results) >= limit {
			break
		}
		title := strings.TrimSpace(r.Title)
		u := strings.TrimSpace(r.URL)
		if title == "" || u == "" {
			continue
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     u,
			Snippet: strings.TrimSpace(r.Content),
		})
	}

	slog.Debug("searxng search done", "query", query, "results", len(results))
	return results, nil
}
