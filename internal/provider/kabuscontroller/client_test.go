package kabuscontroller

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

/*
TestClientFetchUsesExactGETPathsAndHeaders は、6 datasetの固定HTTP要求を検証します。

機能:
  - 各datasetを仕様どおりのGET pathへ1回だけ送信する
  - 個別銘柄のsymbolを末尾の1 path segmentへ変換する
  - User-Agent、Accept、空queryと正常応答の付帯情報を確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchUsesExactGETPathsAndHeaders(t *testing.T) {
	testCases := []struct {
		name     string
		dataset  string
		symbol   string
		wantPath string
	}{
		{
			name:     "先物登録一覧",
			dataset:  "future_registrations",
			wantPath: "/api/trade/registrations/future",
		},
		{
			name:     "オプション登録一覧",
			dataset:  "option_registrations",
			wantPath: "/api/trade/registrations/option",
		},
		{
			name:     "全板情報",
			dataset:  "market_data",
			wantPath: "/api/trade/market-data",
		},
		{
			name:     "先物板情報",
			dataset:  "future_market_data",
			wantPath: "/api/trade/market-data/future",
		},
		{
			name:     "オプション板情報",
			dataset:  "option_market_data",
			wantPath: "/api/trade/market-data/option",
		},
		{
			name:     "個別銘柄板情報",
			dataset:  "symbol_market_data",
			symbol:   "NK225M-2026.09",
			wantPath: "/api/trade/market-data/NK225M-2026.09",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			payload := `{"data":[{"symbol":"NK225"}]}`
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodGet || request.URL.Path != testCase.wantPath {
					t.Errorf("HTTP要求 = %s %s, GET %sを期待", request.Method, request.URL.Path, testCase.wantPath)
				}
				if request.URL.RawQuery != "" {
					t.Errorf("query = %q, 空を期待", request.URL.RawQuery)
				}
				if request.Header.Get("User-Agent") != "kabus-controller-test/1.0" ||
					request.Header.Get("Accept") != "application/json" ||
					request.Header.Get("Accept-Encoding") != "gzip" {
					t.Errorf("HTTP header = %v, User-Agent、Accept、Accept-Encodingの固定値を期待", request.Header)
				}
				contentType := "application/json; charset=utf-8"
				if testCase.dataset == "symbol_market_data" {
					contentType = "application/vnd.kabus+json"
				}
				writer.Header().Set("Content-Type", contentType)
				_, _ = io.WriteString(writer, payload)
			}))
			defer server.Close()

			client, err := NewClient(ClientConfig{
				BaseURL: server.URL, HTTPClient: server.Client(),
				UserAgent: "kabus-controller-test/1.0", MaxResponseBytes: 4096,
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			response, err := client.Fetch(context.Background(), testCase.dataset, testCase.symbol)
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if requests.Load() != 1 {
				t.Errorf("HTTP要求回数 = %d, 1を期待", requests.Load())
			}
			if response.SourceURL != server.URL+testCase.wantPath ||
				response.StatusCode != http.StatusOK || response.FetchedAt.IsZero() ||
				response.ResponseBytes != int64(len(payload)) {
				t.Errorf("APIResponse付帯情報 = %+v", response)
			}
			body, ok := response.Body.(map[string]any)
			if !ok || len(body["data"].([]any)) != 1 {
				t.Errorf("APIResponse.Body = %#v, JSON objectを期待", response.Body)
			}
		})
	}
}

// ----------------------------------------

/*
TestClientFetchDoesNotFollowRedirects は、固定GETのリダイレクト境界を検証します。

機能:
  - 3xx応答を追跡せずAPIErrorとして返す
  - リダイレクト先へ2件目のHTTP要求を送らない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/redirected" {
			t.Error("固定endpoint外のリダイレクト先へ到達しました")
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{}`)
			return
		}
		http.Redirect(writer, request, "/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), DefaultMaxResponseBytes)
	_, err := client.Fetch(context.Background(), "market_data", "")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("Fetch() error = %v, HTTP 307のAPIErrorを期待", err)
	}
	if requests.Load() != 1 {
		t.Errorf("HTTP要求回数 = %d, 固定endpointへの1件だけを期待", requests.Load())
	}
}

// ----------------------------------------

/*
TestNewClientAppliesDefaults は、KabusController API clientの既定接続設定を確認します。

機能:
  - 空のClientConfigへ現在の既定オリジン、User-Agent、本文上限を適用する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewClientAppliesDefaults(t *testing.T) {
	client, err := NewClient(ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.baseURL.String() != DefaultBaseURL ||
		client.userAgent != DefaultUserAgent ||
		client.maxResponseBytes != DefaultMaxResponseBytes {
		t.Errorf("Client既定値 = base:%q User-Agent:%q max:%d", client.baseURL.String(), client.userAgent, client.maxResponseBytes)
	}
}

// ----------------------------------------

/*
TestNewClientRejectsInvalidConfiguration は、KabusController API clientの接続設定検証を確認します。

機能:
  - 非HTTP・path・userinfo・query・fragment付きbase URLを拒否する
  - 空白または制御文字を含むUser-Agentと負の本文上限を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewClientRejectsInvalidConfiguration(t *testing.T) {
	testCases := []struct {
		name   string
		config ClientConfig
	}{
		{name: "HTTP以外", config: ClientConfig{BaseURL: "file:///tmp/kabus-controller"}},
		{name: "path付き", config: ClientConfig{BaseURL: "http://127.0.0.1:8080/api"}},
		{name: "userinfo付き", config: ClientConfig{BaseURL: "http://user:pass@127.0.0.1:8080"}},
		{name: "query付き", config: ClientConfig{BaseURL: "http://127.0.0.1:8080?x=1"}},
		{name: "fragment付き", config: ClientConfig{BaseURL: "http://127.0.0.1:8080#fragment"}},
		{name: "空白User-Agent", config: ClientConfig{BaseURL: DefaultBaseURL, UserAgent: " "}},
		{name: "制御文字User-Agent", config: ClientConfig{BaseURL: DefaultBaseURL, UserAgent: "client\x7f"}},
		{name: "負の本文上限", config: ClientConfig{BaseURL: DefaultBaseURL, MaxResponseBytes: -1}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewClient(testCase.config); err == nil {
				t.Errorf("NewClient(%+v) error = nil, 設定検証エラーを期待", testCase.config)
			}
		})
	}
}

// ----------------------------------------

/*
TestClientFetchPreservesJSONNumbers は、上流JSONの数値精度を検証します。

機能:
  - 2の53乗を超える整数と小数をjson.Numberとして保持する
  - float64への暗黙変換による桁落ちを防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchPreservesJSONNumbers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"sequence":9007199254740993,"price":12345.67890123456789}`)
	}))
	defer server.Close()

	client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), DefaultMaxResponseBytes)
	response, err := client.Fetch(context.Background(), "market_data", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	body := response.Body.(map[string]any)
	sequence, sequenceOK := body["sequence"].(json.Number)
	price, priceOK := body["price"].(json.Number)
	if !sequenceOK || sequence.String() != "9007199254740993" ||
		!priceOK || price.String() != "12345.67890123456789" {
		t.Errorf("JSON数値 = sequence:%#v price:%#v, json.Numberによる精度保持を期待", body["sequence"], body["price"])
	}
}

// ----------------------------------------

/*
TestClientFetchRejectsInvalidContentAndJSON は、成功応答の形式検証を確認します。

機能:
  - JSON以外のContent-Type、不正JSON、余分なJSON値、不正UTF-8を拒否する
  - 形式エラーを正常なAPIResponseとして公開しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRejectsInvalidContentAndJSON(t *testing.T) {
	testCases := []struct {
		name        string
		contentType string
		body        []byte
		wantError   string
	}{
		{name: "JSON以外のMIME", contentType: "text/plain", body: []byte(`{}`), wantError: "Content-Type"},
		{name: "不正JSON", contentType: "application/json", body: []byte(`{"data":`), wantError: "復号"},
		{name: "余分なJSON値", contentType: "application/json", body: []byte(`{} {}`), wantError: "余分な値"},
		{name: "不正UTF-8", contentType: "application/json", body: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}, wantError: "UTF-8"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", testCase.contentType)
				_, _ = writer.Write(testCase.body)
			}))
			defer server.Close()

			client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), DefaultMaxResponseBytes)
			_, err := client.Fetch(context.Background(), "market_data", "")
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Errorf("Fetch() error = %v, %qを含むエラーを期待", err, testCase.wantError)
			}
		})
	}
}

// ----------------------------------------

/*
TestClientFetchAppliesBodyLimitBeforeAndAfterGzip は、応答本文上限を検証します。

機能:
  - 非圧縮JSONが上限を超えた場合に拒否する
  - 圧縮本文が上限内でもgzip展開後に上限を超えた場合に拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchAppliesBodyLimitBeforeAndAfterGzip(t *testing.T) {
	payload := []byte(`{"data":"` + strings.Repeat("x", 256) + `"}`)
	testCases := []struct {
		name       string
		compressed bool
	}{
		{name: "非圧縮本文", compressed: false},
		{name: "gzip展開後本文", compressed: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			responseBody := payload
			if testCase.compressed {
				responseBody = gzipKabusControllerTestBytes(t, payload)
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if testCase.compressed {
					writer.Header().Set("Content-Encoding", "gzip")
				}
				_, _ = writer.Write(responseBody)
			}))
			defer server.Close()

			client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), 128)
			_, err := client.Fetch(context.Background(), "market_data", "")
			if err == nil || !strings.Contains(err.Error(), "上限") {
				t.Errorf("Fetch() error = %v, 本文上限エラーを期待", err)
			}
		})
	}
}

// ----------------------------------------

/*
TestClientFetchRejectsCompressedBodyOverLimit は、gzip圧縮本文自体の応答上限を検証します。

機能:
  - 展開後JSONが小さくてもgzip形式の転送本文が上限を超えた場合に拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRejectsCompressedBodyOverLimit(t *testing.T) {
	compressed := gzipKabusControllerTestBytes(t, []byte(`{}`))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Encoding", "gzip")
		writer.Header().Set("Content-Length", fmt.Sprint(len(compressed)))
		_, _ = writer.Write(compressed)
	}))
	defer server.Close()

	client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), int64(len(compressed)-1))
	_, err := client.Fetch(context.Background(), "market_data", "")
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Errorf("Fetch() error = %v, gzip圧縮本文の上限エラーを期待", err)
	}
}

// ----------------------------------------

/*
TestClientFetchReturnsSafeAPIError は、非2xx応答の安全な表現を検証します。

機能:
  - HTTP状態、Retry-After、固定endpointをerrors.As可能なAPIErrorへ保持する
  - 上流応答本文を公開エラー文字列へ含めない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchReturnsSafeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Retry-After", "11")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"secret":"upstream-private-body"}`)
	}))
	defer server.Close()

	client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), DefaultMaxResponseBytes)
	_, err := client.Fetch(context.Background(), "symbol_market_data", "NK225M")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusTooManyRequests ||
		apiError.RetryAfter != "11" || apiError.Endpoint != "/api/trade/market-data/:symbol" {
		t.Fatalf("Fetch() error = %#v, 状態を保持したAPIErrorを期待", err)
	}
	if strings.Contains(err.Error(), "upstream-private-body") {
		t.Errorf("APIErrorへ上流本文が漏えいしています: %v", err)
	}
}

// ----------------------------------------

/*
TestClientFetchRejectsInvalidDatasetAndSymbolBeforeHTTP は、通信前の固定入力検証を確認します。

機能:
  - 未知dataset、固定datasetへのsymbol、個別datasetの空・不正・衝突symbolを拒否する
  - 不正入力ではHTTP要求を1件も発生させない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRejectsInvalidDatasetAndSymbolBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), DefaultMaxResponseBytes)

	testCases := []struct {
		name    string
		dataset string
		symbol  string
	}{
		{name: "未知dataset", dataset: "unknown"},
		{name: "固定datasetへのsymbol", dataset: "market_data", symbol: "NK225M"},
		{name: "空symbol", dataset: "symbol_market_data"},
		{name: "前後空白", dataset: "symbol_market_data", symbol: " NK225M "},
		{name: "path区切り", dataset: "symbol_market_data", symbol: "../NK225M"},
		{name: "固定future経路", dataset: "symbol_market_data", symbol: "future"},
		{name: "固定option経路", dataset: "symbol_market_data", symbol: "option"},
		{name: "100文字超", dataset: "symbol_market_data", symbol: strings.Repeat("A", 101)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := client.Fetch(context.Background(), testCase.dataset, testCase.symbol); err == nil {
				t.Error("Fetch() error = nil, 入力エラーを期待")
			}
		})
	}
	if requests.Load() != 0 {
		t.Errorf("不正入力によるHTTP要求回数 = %d, 0を期待", requests.Load())
	}
}

// ----------------------------------------

/*
TestClientFetchHonorsCanceledContext は、HTTP要求へのcontext伝播を検証します。

機能:
  - キャンセル済みcontextで通信を開始しない
  - ラップされた通信エラーからcontext.Canceledをerrors.Isで判定可能にする

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchHonorsCanceledContext(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client := newKabusControllerHTTPTestClient(t, server.URL, server.Client(), DefaultMaxResponseBytes)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Fetch(ctx, "market_data", "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch() error = %v, context.Canceledを期待", err)
	}
	if requests.Load() != 0 {
		t.Errorf("キャンセル済み要求のHTTP到達回数 = %d, 0を期待", requests.Load())
	}
}

// ----------------------------------------

/*
newKabusControllerHTTPTestClient は、HTTP単体テスト用clientを生成します。

機能:
  - テストサーバーのオリジンとHTTP clientをClientConfigへ設定する
  - client生成失敗を呼び出し元テストの致命的失敗として報告する

引数:
  - t *testing.T: テスト状態を管理する値
  - baseURL string: httptest serverのオリジン
  - httpClient *http.Client: テストサーバーへ接続するHTTP client
  - maximum int64: 展開後JSON本文の最大バイト数

返り値:
  - *Client: 検証済みのKabusController API client
*/
func newKabusControllerHTTPTestClient(
	t *testing.T,
	baseURL string,
	httpClient *http.Client,
	maximum int64,
) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		BaseURL: baseURL, HTTPClient: httpClient,
		UserAgent: "kabus-controller-test/1.0", MaxResponseBytes: maximum,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

// ----------------------------------------

/*
gzipKabusControllerTestBytes は、テスト用JSONをgzip形式へ圧縮します。

機能:
  - gzip展開後の本文上限を検証するためのHTTP応答本文を生成する

引数:
  - t *testing.T: テスト状態を管理する値
  - value []byte: 圧縮するJSON本文

返り値:
  - []byte: gzip形式の本文
*/
func gzipKabusControllerTestBytes(t *testing.T, value []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(value); err != nil {
		t.Fatalf("gzip.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return buffer.Bytes()
}
