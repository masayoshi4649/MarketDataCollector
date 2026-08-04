package nikkei225jp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const validCurrentBody = "A[511]=\"157.619_-1.902_-1.19_05:59_0_160.877_157.297\";\n" +
	"A[111]=\"64362.02_+2494.59_+4.03_07/31_0_65364.73_61948.23\";\n" +
	"A[181]=\"3214__0.00_07/31_0__\";\n"

// TestFetchCurrent は、1回の取得でヘッダー設定、解析、並び順、空欄保持を検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchCurrent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "test-client/1.0" {
			t.Errorf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		if request.Header.Get("Referer") != DefaultReferer {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/javascript")
		writer.Header().Set("Cache-Control", "public,max-age=2")
		writer.Header().Set("Last-Modified", "Sun, 02 Aug 2026 05:00:00 GMT")
		_, _ = writer.Write([]byte(validCurrentBody))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client(), UserAgent: "test-client/1.0"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchCurrent(context.Background())
	if err != nil {
		t.Fatalf("FetchCurrent() error = %v", err)
	}
	if len(data.Quotes) != 3 {
		t.Fatalf("len(Quotes) = %d", len(data.Quotes))
	}
	if data.Quotes[0].Code != "111" || data.Quotes[0].Name != "日本225" {
		t.Errorf("Quotes[0] = %+v", data.Quotes[0])
	}
	if data.Quotes[1].Code != "181" || data.Quotes[1].Change != nil || data.Quotes[1].High != nil {
		t.Errorf("Quotes[1] = %+v", data.Quotes[1])
	}
	if data.Metadata.ResponseBytes != int64(len(validCurrentBody)) {
		t.Errorf("ResponseBytes = %d", data.Metadata.ResponseBytes)
	}
}

// TestFetchCurrentWaitRespectsContext は、先行取得の完了待ちをコンテキストで中止できることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchCurrentWaitRespectsContext(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		<-release
		writer.Header().Set("Content-Type", "application/javascript")
		_, _ = writer.Write([]byte(validCurrentBody))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, fetchErr := client.FetchCurrent(context.Background())
		firstDone <- fetchErr
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.FetchCurrent(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("待機中止エラー = %v", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Errorf("先行FetchCurrent() error = %v", err)
	}
}

// TestFetchCurrentIgnoresUpstreamCacheDirectives は、上流のキャッシュ指示にかかわらず毎回無条件GETすることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchCurrentIgnoresUpstreamCacheDirectives(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Header.Get("If-Modified-Since") != "" || request.Header.Get("If-None-Match") != "" {
			t.Errorf("条件付きヘッダーが送られました")
		}
		writer.Header().Set("Content-Type", "application/javascript")
		writer.Header().Set("Cache-Control", "public, no-store")
		writer.Header().Set("Last-Modified", "Sun, 02 Aug 2026 05:00:00 GMT")
		writer.Header().Set("ETag", "\"not-stored\"")
		_, _ = writer.Write([]byte(validCurrentBody))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := client.FetchCurrent(context.Background()); err != nil {
			t.Fatalf("FetchCurrent() error = %v", err)
		}
	}
	if requests.Load() != 2 {
		t.Errorf("要求回数 = %d", requests.Load())
	}
}

// TestFetchCurrentRejectsRedirect は、既定取得先から別URLへリダイレクトしないことを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchCurrentRejectsRedirect(t *testing.T) {
	t.Parallel()

	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetRequests.Add(1)
		writer.Header().Set("Content-Type", "application/javascript")
		_, _ = writer.Write([]byte(validCurrentBody))
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	client, err := NewClient(Config{BaseURL: redirect.URL, HTTPClient: redirect.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.FetchCurrent(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("FetchCurrent() error = %v", err)
	}
	if targetRequests.Load() != 0 {
		t.Errorf("リダイレクト先の要求数 = %d", targetRequests.Load())
	}
}

// TestFetchCurrentRejectsInvalidResponses は、HTTP状態、MIME、本文形式、本文上限の異常を検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchCurrentRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		limit       int64
		want        string
	}{
		{name: "HTTPエラー", status: http.StatusServiceUnavailable, contentType: "application/javascript", body: "x", want: "HTTP 503"},
		{name: "HTTP 304", status: 304, contentType: "application/javascript", want: "HTTP 304"},
		{name: "MIMEエラー", status: http.StatusOK, contentType: "text/html", body: validCurrentBody, want: "Content-Type"},
		{name: "未知の記述", status: http.StatusOK, contentType: "application/javascript", body: validCurrentBody + "alert(1);", want: "未対応"},
		{name: "列不足", status: http.StatusOK, contentType: "application/javascript", body: "A[111]=\"1_2\";", want: "列数"},
		{name: "非有限値", status: http.StatusOK, contentType: "application/javascript", body: "A[111]=\"NaN_2_3_04:00_0_4_1\";", want: "有限値"},
		{name: "本文上限", status: http.StatusOK, contentType: "application/javascript", body: validCurrentBody, limit: 8, want: "上限"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", test.contentType)
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()

			config := Config{BaseURL: server.URL, HTTPClient: server.Client(), MaxResponseBytes: test.limit}
			client, err := NewClient(config)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.FetchCurrent(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("FetchCurrent() error = %v, want %q", err, test.want)
			}
		})
	}
}

// TestNewClientRejectsInvalidConfig は、不正なURL、パス、本文上限を拒否することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestNewClientRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{BaseURL: "ftp://example.com"},
		{BaseURL: "https://example.com/base"},
		{BaseURL: "https://user:secret@example.com"},
		{BaseURL: "https://example.com?source=test"},
		{BaseURL: "https://example.com#fragment"},
		{BaseURL: "https://example.com", CurrentPath: "relative"},
		{BaseURL: "https://example.com", CurrentPath: "//other.example/path"},
		{BaseURL: "https://example.com", MaxResponseBytes: -1},
		{BaseURL: "https://example.com", MaxChartResponseBytes: -1},
	}
	for _, config := range tests {
		if _, err := NewClient(config); err == nil {
			t.Errorf("NewClient(%+v) error = nil", config)
		}
	}
}
