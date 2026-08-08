package polymarket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

type liveReadOnlyTransport struct {
	base     http.RoundTripper
	mu       sync.Mutex
	requests int
}

// RoundTrip は、live smoke testの外部要求が公開読取専用境界内にあることを確認します。
//
// 機能:
//   - GET以外のHTTP methodを通信前に拒否する
//   - Gamma、Data、CLOBの公式origin以外を拒否する
//   - Authorization、API key、Cookieが送信されないことを確認する
//   - 安全性確認後の要求だけを標準transportへ委譲する
//
// 引数:
//   - request *http.Request: Clientが生成した外部HTTP要求
//
// 返り値:
//   - *http.Response: 公式APIの応答
//   - error: 読取専用境界違反または通信の失敗
func (t *liveReadOnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet {
		return nil, fmt.Errorf("live Polymarket testが禁止method %qを要求しました", request.Method)
	}
	allowedHosts := map[string]struct{}{
		"gamma-api.polymarket.com": {},
		"data-api.polymarket.com":  {},
		"clob.polymarket.com":      {},
	}
	if _, allowed := allowedHosts[request.URL.Hostname()]; !allowed || request.URL.Scheme != "https" {
		return nil, fmt.Errorf("live Polymarket testが許可外originを要求しました: %s", request.URL.Redacted())
	}
	for _, name := range []string{"Authorization", "X-API-Key", "POLY-API-KEY", "POLY-SIGNATURE", "POLY-PASSPHRASE", "Cookie"} {
		if request.Header.Get(name) != "" {
			return nil, fmt.Errorf("live Polymarket testの公開API要求に禁止header %qが含まれています", name)
		}
	}
	t.mu.Lock()
	t.requests++
	t.mu.Unlock()
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(request)
}

// Count は、live transportが公式APIへ送った要求数を返します。
//
// 機能:
//   - mutex下で並行安全に要求数を読み取る
//
// 引数:
//   - なし
//
// 返り値:
//   - int: 読取専用検査を通過したHTTP要求数
func (t *liveReadOnlyTransport) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.requests
}

// TestLivePublicReadOnlyEndpoints は、3種類のPolymarket公式APIへのopt-in smoke testを行います。
//
// 機能:
//   - LIVE_POLYMARKET=1の場合だけ外部通信する
//   - Gamma search、Data leaderboard、CLOB server timeを認証なしGETで取得する
//   - 各1回のFetchが1回のみのHTTP要求と非nil応答を生むことを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし。LIVE_POLYMARKETが1以外の通常テストではスキップする
func TestLivePublicReadOnlyEndpoints(t *testing.T) {
	if os.Getenv("LIVE_POLYMARKET") != "1" {
		t.Skip("LIVE_POLYMARKET=1の場合だけPolymarket公式APIへ接続します")
	}
	transport := &liveReadOnlyTransport{base: http.DefaultTransport}
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	client, err := NewClient(ClientConfig{HTTPClient: httpClient, MaxResponseBytes: DefaultMaxResponseBytes})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	tests := []struct {
		name    string
		dataset string
		query   url.Values
	}{
		{name: "Gamma search", dataset: "search", query: url.Values{"q": {"bitcoin"}, "limit_per_type": {"1"}, "page": {"1"}, "events_status": {"active"}, "keep_closed_markets": {"0"}, "search_profiles": {"false"}}},
		{name: "Data leaderboard", dataset: "leaderboard"},
		{name: "CLOB server time", dataset: "server_time"},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			response, fetchErr := client.Fetch(ctx, testCase.dataset, testCase.query)
			if fetchErr != nil {
				var apiError *APIError
				if errors.As(fetchErr, &apiError) {
					t.Fatalf("Fetch() HTTP %d endpoint=%s retry-after=%q: %v", apiError.StatusCode, apiError.Endpoint, apiError.RetryAfter, fetchErr)
				}
				t.Fatalf("Fetch() error = %v", fetchErr)
			}
			if response.StatusCode != http.StatusOK || response.Body == nil || response.SourceURL == "" || response.Endpoint == "" {
				t.Errorf("Fetch() response = %+v", response)
			}
			if actual := transport.Count(); actual != index+1 {
				t.Errorf("%sまでのHTTP要求数 = %d, %dを期待", testCase.name, actual, index+1)
			}
		})
	}
}
