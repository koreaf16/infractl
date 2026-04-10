// Package web
// File: search.go
// Description: DuckDuckGo Lite 웹 검색 스크래퍼
// Responsibility: API 키 없이 DuckDuckGo Lite HTML을 파싱하여 검색 결과 반환

package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	ddgLiteURL    = "https://lite.duckduckgo.com/lite"
	searchTimeout = 10 * time.Second
	maxResults    = 10
)

// SearchResult는 웹 검색 결과 단일 항목이다.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// SearchDDG는 DuckDuckGo Lite에서 query를 검색하여 최대 limit개의 결과를 반환한다.
// 스크래핑 실패 시 빈 결과를 반환한다 (에러 X, slog.Warn).
func SearchDDG(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > maxResults {
		limit = 5
	}

	reqCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	formData := url.Values{"q": {query}, "kl": {"wt-wt"}}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ddgLiteURL,
		strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ddg search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: searchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("ddg search failed", "query", query, "err", err)
		return nil, nil // 폐쇄망 등 실패: 빈 결과 반환
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		slog.Warn("ddg read body failed", "err", err)
		return nil, nil
	}

	results := parseDDGResults(body, limit)
	slog.Debug("ddg search done", "query", query, "results", len(results))
	return results, nil
}

// parseDDGResults는 DDG Lite HTML에서 검색 결과를 추출한다.
// DDG Lite 구조: <a class="result-link"> 링크, <td class="result-snippet"> 설명
func parseDDGResults(body []byte, limit int) []SearchResult {
	var results []SearchResult
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))

	var current SearchResult
	inLink := false
	inSnippet := false

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		token := tokenizer.Token()

		switch tt {
		case html.StartTagToken:
			switch token.Data {
			case "a":
				cls := attrVal(token.Attr, "class")
				if strings.Contains(cls, "result-link") {
					inLink = true
					current = SearchResult{URL: attrVal(token.Attr, "href")}
				}
			case "td":
				cls := attrVal(token.Attr, "class")
				if strings.Contains(cls, "result-snippet") {
					inSnippet = true
				}
			}

		case html.EndTagToken:
			if token.Data == "a" && inLink {
				inLink = false
			}
			if token.Data == "td" && inSnippet {
				inSnippet = false
				// snippet이 끝나면 결과 완성
				if current.URL != "" && current.Title != "" {
					results = append(results, current)
					current = SearchResult{}
					if len(results) >= limit {
						return results
					}
				}
			}

		case html.TextToken:
			text := strings.TrimSpace(token.Data)
			if text == "" {
				continue
			}
			if inLink {
				current.Title += text
			}
			if inSnippet {
				current.Snippet += text + " "
			}
		}
	}

	return results
}
