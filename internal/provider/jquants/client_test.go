package jquants

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
TestClientFetchSendsFixedRequestAndDecodesJSON は、固定endpointへのHTTP要求とJSON応答を検証します。

機能:
  - GET、固定path、url.Valuesで符号化したquery、認証・識別・受信ヘッダーを確認する
  - 正常応答の本文とqueryを含まない取得元URLを返すことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchSendsFixedRequestAndDecodesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v2/equities/bars/daily" {
			t.Errorf("HTTP要求 = %s %s, 固定GET pathを期待", request.Method, request.URL.Path)
		}
		if actual := request.URL.Query().Get("code"); actual != "A B&=日本" {
			t.Errorf("code query = %q, 安全に復号された値を期待", actual)
		}
		if request.Header.Get("x-api-key") != "test-api-key" ||
			request.Header.Get("User-Agent") != "client-test/1.0" ||
			request.Header.Get("Accept") != "application/json" ||
			request.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("HTTPヘッダーが期待値と一致しません")
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(writer, `{"data":[{"code":"86970"}],"pagination_key":"next"}`)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, APIKey: "test-api-key", HTTPClient: server.Client(),
		UserAgent: "client-test/1.0", MaxResponseBytes: 4096,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	response, err := client.Fetch(
		context.Background(),
		"equities_bars_daily",
		map[string]string{"code": "A B&=日本"},
	)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if response.StatusCode != http.StatusOK || response.SourceURL != server.URL+"/v2/equities/bars/daily" ||
		response.FetchedAt.IsZero() || response.ResponseBytes == 0 {
		t.Errorf("APIResponse付帯情報 = %+v, 取得結果を期待", response)
	}
	body, ok := response.Body.(map[string]any)
	if !ok || body["pagination_key"] != "next" {
		t.Errorf("APIResponse.Body = %#v, 復号済みJSONを期待", response.Body)
	}
}

// ----------------------------------------

/*
TestClientFetchRejectsUnknownQueryBeforeHTTP は、dataset固有のquery許可リストを検証します。

機能:
  - endpointSpecsにない上流query名をHTTP通信前に拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRejectsUnknownQueryBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, APIKey: "test-api-key", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Fetch(
		context.Background(),
		"equities_master",
		map[string]string{"unknown": "value"},
	)
	if err == nil || !strings.Contains(err.Error(), "未知のquery項目") {
		t.Fatalf("Fetch() error = %v, 未知queryのエラーを期待", err)
	}
	if requests.Load() != 0 {
		t.Errorf("HTTP要求回数 = %d, 0を期待", requests.Load())
	}
}

// ----------------------------------------

/*
TestClientFetchRequiresFixedQuery は、ForcedQueryの固定値を検証します。

機能:
  - 株価ティックdatasetで固定されたBulk endpoint値の欠落と改変を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRequiresFixedQuery(t *testing.T) {
	client, err := NewClient(ClientConfig{APIKey: "test-api-key"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	for _, query := range []map[string]string{
		{},
		{"endpoint": "/equities/master"},
	} {
		_, err := client.Fetch(context.Background(), "equities_trades", query)
		if err == nil || !strings.Contains(err.Error(), "固定query項目") {
			t.Errorf("Fetch(query=%v) error = %v, 固定queryのエラーを期待", query, err)
		}
	}
}

// ----------------------------------------

/*
TestClientClonesHTTPClientAndFollowsRedirect は、redirect追跡とAPIキー転送範囲を検証します。

機能:
  - 同一originのredirectを追跡してx-api-keyを維持する
  - 異なるoriginのredirectも追跡するがx-api-keyは転送しない
  - 既存CheckRedirectがキーを再設定しても異なるoriginでは最終的に除去する
  - metadataの取得元URLへredirect後の実URLを反映する
  - 呼び出し元HTTP clientのCheckRedirectを変更しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientClonesHTTPClientAndFollowsRedirect(t *testing.T) {
	t.Run("同一origin", func(t *testing.T) {
		var redirectedAPIKey string
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/v2/equities/master" {
				http.Redirect(writer, request, server.URL+"/redirected", http.StatusFound)
				return
			}
			redirectedAPIKey = request.Header.Get("x-api-key")
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{}`)
		}))
		defer server.Close()

		originalHTTPClient := server.Client()
		client, err := NewClient(ClientConfig{
			BaseURL: server.URL, APIKey: "test-api-key", HTTPClient: originalHTTPClient,
		})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		response, err := client.Fetch(context.Background(), "equities_master", map[string]string{})
		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}
		if redirectedAPIKey != "test-api-key" {
			t.Errorf("同一origin redirectのx-api-key = %q, 維持を期待", redirectedAPIKey)
		}
		if response.SourceURL != server.URL+"/redirected" {
			t.Errorf("同一origin redirectのsource URL = %q", response.SourceURL)
		}
		if originalHTTPClient.CheckRedirect != nil {
			t.Error("呼び出し元HTTP clientのCheckRedirectが変更されました")
		}
	})

	t.Run("異なるorigin", func(t *testing.T) {
		var redirected atomic.Bool
		var previousPolicyCalled atomic.Bool
		var redirectedAPIKey string
		target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			redirected.Store(true)
			redirectedAPIKey = request.Header.Get("x-api-key")
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{}`)
		}))
		defer target.Close()
		origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, target.URL, http.StatusFound)
		}))
		defer origin.Close()

		originalHTTPClient := origin.Client()
		originalHTTPClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			previousPolicyCalled.Store(true)
			request.Header.Set("x-api-key", "test-api-key")
			return nil
		}
		client, err := NewClient(ClientConfig{
			BaseURL: origin.URL, APIKey: "test-api-key", HTTPClient: originalHTTPClient,
		})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		response, err := client.Fetch(context.Background(), "equities_master", map[string]string{})
		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}
		if !redirected.Load() {
			t.Fatal("異なるoriginのredirect先へ要求されていません")
		}
		if !previousPolicyCalled.Load() {
			t.Fatal("呼び出し元HTTP clientのCheckRedirectが呼ばれていません")
		}
		if redirectedAPIKey != "" {
			t.Errorf("異なるorigin redirectのx-api-key = %q, 未送信を期待", redirectedAPIKey)
		}
		if response.SourceURL != target.URL {
			t.Errorf("異なるorigin redirectのsource URL = %q", response.SourceURL)
		}
		if originalHTTPClient.CheckRedirect == nil {
			t.Error("呼び出し元HTTP clientのCheckRedirectが変更されました")
		}
	})
}

// ----------------------------------------

/*
TestClientFetchExpandsGzip は、gzip応答の手動展開と二重展開防止を検証します。

機能:
  - Content-Encodingが残るgzip本文を展開する
  - Transportが展開済みと示した本文をContent-Encodingが残っていても再展開しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchExpandsGzip(t *testing.T) {
	payload := []byte(`{"data":[{"code":"86970"}]}`)
	for _, testCase := range []struct {
		name         string
		uncompressed bool
	}{
		{name: "手動展開", uncompressed: false},
		{name: "Transport展開済み", uncompressed: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var body []byte
			if testCase.uncompressed {
				body = payload
			} else {
				body = gzipBytes(t, payload)
			}
			httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type":     []string{"application/json"},
						"Content-Encoding": []string{"gzip"},
					},
					Body:         io.NopCloser(strings.NewReader(string(body))),
					Request:      request,
					Uncompressed: testCase.uncompressed,
				}, nil
			})}
			client, err := NewClient(ClientConfig{APIKey: "test-api-key", HTTPClient: httpClient})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			response, err := client.Fetch(context.Background(), "equities_master", map[string]string{})
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if _, ok := response.Body.(map[string]any); !ok {
				t.Errorf("APIResponse.Body = %#v, JSON objectを期待", response.Body)
			}
		})
	}
}

// ----------------------------------------

/*
TestClientFetchPreservesJSONNumberAndRejectsExtraValue は、厳密JSON復号を検証します。

機能:
  - 2の53乗を超える整数をjson.Numberとして保持する
  - 先頭JSON値の後ろにある余分なJSON値を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchPreservesJSONNumberAndRejectsExtraValue(t *testing.T) {
	responses := make(chan string, 2)
	responses <- `{"large":9007199254740993}`
	responses <- `{} {}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, <-responses)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, APIKey: "test-api-key", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.Fetch(context.Background(), "equities_master", map[string]string{})
	if err != nil {
		t.Fatalf("1回目のFetch() error = %v", err)
	}
	value, ok := response.Body.(map[string]any)["large"].(json.Number)
	if !ok || value.String() != "9007199254740993" {
		t.Errorf("large = %#v, 精度を保ったjson.Numberを期待", response.Body)
	}
	_, err = client.Fetch(context.Background(), "equities_master", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "余分な値") {
		t.Errorf("2回目のFetch() error = %v, 余分なJSON値のエラーを期待", err)
	}
}

// ----------------------------------------

/*
TestClientFetchAppliesExpandedBodyLimit は、gzip展開後の本文上限を検証します。

機能:
  - 圧縮状態では小さくても展開後に上限を超えるJSON本文を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchAppliesExpandedBodyLimit(t *testing.T) {
	payload := []byte(`{"data":"` + strings.Repeat("x", 256) + `"}`)
	compressed := gzipBytes(t, payload)
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":     []string{"application/json"},
				"Content-Encoding": []string{"gzip"},
			},
			Body: io.NopCloser(strings.NewReader(string(compressed))), Request: request,
		}, nil
	})}
	client, err := NewClient(ClientConfig{
		APIKey: "test-api-key", HTTPClient: httpClient, MaxResponseBytes: 64,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Fetch(context.Background(), "equities_master", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "上限64バイト") {
		t.Errorf("Fetch() error = %v, 展開後本文上限のエラーを期待", err)
	}
}

// ----------------------------------------

/*
TestClientFetchLimitsGzipHeader は、gzipヘッダーも本文上限より多く読み込まないことを検証します。

機能:
  - 終端されない長いgzipファイル名ヘッダーを展開前の段階で制限する
  - gzipヘッダーを使った本文上限の迂回を防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchLimitsGzipHeader(t *testing.T) {
	const maxResponseBytes = 64
	gzipHeader := []byte{0x1f, 0x8b, 0x08, 0x08, 0, 0, 0, 0, 0, 0xff}
	malformed := append(gzipHeader, bytes.Repeat([]byte{'a'}, 1024)...)
	var observed bytes.Buffer
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":     []string{"application/json"},
				"Content-Encoding": []string{"gzip"},
			},
			Body: io.NopCloser(io.TeeReader(bytes.NewReader(malformed), &observed)), Request: request,
		}, nil
	})}
	client, err := NewClient(ClientConfig{
		APIKey: "test-api-key", HTTPClient: httpClient, MaxResponseBytes: maxResponseBytes,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Fetch(context.Background(), "equities_master", nil); err == nil {
		t.Fatal("Fetch() error = nil, 不正gzipヘッダーの拒否を期待")
	}
	if observed.Len() > maxResponseBytes {
		t.Errorf("gzip圧縮本文の読み取り量 = %d, 上限%d以下を期待", observed.Len(), maxResponseBytes)
	}
}

// ----------------------------------------

/*
TestClientFetchHandlesMaximumInt64BodyLimit は、本文上限加算の整数オーバーフローを防ぐことを検証します。

機能:
  - int64最大値を本文上限に指定しても小さなJSON応答を正常に読み取る

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchHandlesMaximumInt64BodyLimit(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
			Request:    request,
		}, nil
	})}
	client, err := NewClient(ClientConfig{
		APIKey: "test-api-key", HTTPClient: httpClient, MaxResponseBytes: (1 << 63) - 1,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Fetch(context.Background(), "equities_master", nil); err != nil {
		t.Fatalf("Fetch() error = %v, int64最大値の本文上限で正常応答を期待", err)
	}
}

// ----------------------------------------

/*
TestClientFetchRejectsInvalidMIME は、JSON以外のContent-Typeを拒否することを検証します。

機能:
  - JSON形式の本文でもtext/plain応答を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRejectsInvalidMIME(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, APIKey: "test-api-key", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Fetch(context.Background(), "equities_master", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "Content-Type") {
		t.Errorf("Fetch() error = %v, MIME検証エラーを期待", err)
	}
}

// ----------------------------------------

/*
TestClientFetchReturnsSafeAPIError は、非2xx応答の安全な分類を検証します。

機能:
  - 400、403、429、5xxをerrors.As可能なAPIErrorにする
  - 429のRetry-Afterを保持する
  - APIキーと上流応答本文をエラー文字列へ含めない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchReturnsSafeAPIError(t *testing.T) {
	for _, testCase := range []struct {
		status     int
		retryAfter string
	}{
		{status: http.StatusBadRequest},
		{status: http.StatusForbidden},
		{status: http.StatusTooManyRequests, retryAfter: "17"},
		{status: http.StatusInternalServerError},
	} {
		t.Run(fmt.Sprintf("HTTP%d", testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Retry-After", testCase.retryAfter)
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.status)
				_, _ = io.WriteString(writer, `{"message":"upstream-secret-body"}`)
			}))
			defer server.Close()
			client, err := NewClient(ClientConfig{
				BaseURL: server.URL, APIKey: "disclosure-test-api-key", HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.Fetch(context.Background(), "equities_master", map[string]string{})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != testCase.status ||
				apiErr.RetryAfter != testCase.retryAfter || apiErr.Endpoint != "/v2/equities/master" {
				t.Fatalf("Fetch() error = %#v, 状態を保持したAPIErrorを期待", err)
			}
			if strings.Contains(err.Error(), "disclosure-test-api-key") ||
				strings.Contains(err.Error(), "upstream-secret-body") {
				t.Errorf("APIErrorへ秘密値または上流本文が漏えいしています: %v", err)
			}
		})
	}
}

// ----------------------------------------

/*
TestClientFetchAcceptsEmptyStatus210 は、データなしの空HTTP 210応答を検証します。

機能:
  - Content-Typeと本文がないHTTP 210を正常応答として返す

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchAcceptsEmptyStatus210(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(statusNoData)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, APIKey: "test-api-key", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	response, err := client.Fetch(context.Background(), "equities_master", map[string]string{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if response.StatusCode != statusNoData || response.Body != nil || response.ResponseBytes != 0 {
		t.Errorf("APIResponse = %+v, 空のHTTP 210応答を期待", response)
	}
}

// ----------------------------------------

/*
TestClientFetchRejectsUnknownDatasetAndHonorsContext は、通信前検証とcontext伝播を検証します。

機能:
  - 未知datasetをHTTP通信前に拒否する
  - キャンセル済みcontextによる通信失敗をerrors.Isで判定可能にする

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRejectsUnknownDatasetAndHonorsContext(t *testing.T) {
	client, err := NewClient(ClientConfig{APIKey: "test-api-key"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Fetch(context.Background(), "unknown", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "未対応") {
		t.Errorf("未知datasetのFetch() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Fetch(ctx, "equities_master", map[string]string{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("キャンセル済みFetch() error = %v, context.Canceledを期待", err)
	}
}

// ----------------------------------------

/*
TestClientFetchRedactsOnlyAPIKeyFromTransportError は、通信エラーの最小限の伏字処理を検証します。

機能:
  - RoundTripperの診断情報とquery値を保持する
  - 診断情報にAPIキーが含まれた場合だけ値を伏せる
  - errors.Isで元の通信エラーを判定できる状態を保持する
  - error chainからAPIキーを含む元エラーへ到達できないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestClientFetchRedactsOnlyAPIKeyFromTransportError(t *testing.T) {
	const apiKey = "transport-disclosure-api-key"
	const queryValue = "transport-disclosure-query"
	originalError := fmt.Errorf("query=%s api-key=%s", queryValue, apiKey)
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return nil, originalError
	})}
	client, err := NewClient(ClientConfig{APIKey: apiKey, HTTPClient: httpClient})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Fetch(
		context.Background(),
		"equities_master",
		map[string]string{"code": queryValue},
	)
	if err == nil {
		t.Fatal("Fetch() error = nil, 通信エラーを期待")
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Errorf("通信エラーにAPIキーが含まれています: %v", err)
	}
	if !strings.Contains(err.Error(), queryValue) {
		t.Errorf("通信エラーにquery診断情報がありません: %v", err)
	}
	if !errors.Is(err, originalError) {
		t.Errorf("通信エラーから元原因を判定できません: %v", err)
	}
	for chained := err; chained != nil; chained = errors.Unwrap(chained) {
		if strings.Contains(chained.Error(), apiKey) {
			t.Errorf("通信エラーchainにAPIキーが含まれています: %v", chained)
		}
	}
}

// ----------------------------------------

/*
TestNewClientRejectsInvalidConfigWithoutSecretDisclosure は、接続設定の安全な検証を確認します。

機能:
  - path、userinfo、query付きbase URL、APIキー不備、User-Agent不備、本文上限不備を拒否する
  - 検証エラーへAPIキーの実値を含めない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewClientRejectsInvalidConfigWithoutSecretDisclosure(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		config ClientConfig
	}{
		{name: "base URLのpath", config: ClientConfig{BaseURL: "https://example.test/v2", APIKey: "test-api-key"}},
		{name: "base URLのuserinfo", config: ClientConfig{BaseURL: "https://user:pass@example.test", APIKey: "test-api-key"}},
		{name: "base URLのquery", config: ClientConfig{BaseURL: "https://example.test?x=1", APIKey: "test-api-key"}},
		{name: "空APIキー", config: ClientConfig{}},
		{name: "APIキーの前後空白", config: ClientConfig{APIKey: " disclosure-test-key "}},
		{name: "APIキーの改行", config: ClientConfig{APIKey: "disclosure-test-key\nvalue"}},
		{name: "User-Agent", config: ClientConfig{APIKey: "test-api-key", UserAgent: "client\x7f"}},
		{name: "本文上限", config: ClientConfig{APIKey: "test-api-key", MaxResponseBytes: -1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewClient(testCase.config)
			if err == nil {
				t.Fatal("NewClient() error = nil, 設定検証エラーを期待")
			}
			if strings.Contains(err.Error(), "disclosure-test-key") {
				t.Errorf("NewClient() errorへAPIキーの実値が含まれています: %v", err)
			}
		})
	}
}

// ----------------------------------------

// roundTripperFunc は、関数をhttp.RoundTripperとして利用するテスト用adapterです。
type roundTripperFunc func(*http.Request) (*http.Response, error)

/*
RoundTrip は、設定されたテスト用関数へHTTP要求を委譲します。

機能:
  - 実ネットワークを使わず任意のHTTP応答を返す

引数:
  - request *http.Request: J-Quants clientが生成したHTTP要求

返り値:
  - *http.Response: テスト用関数が生成したHTTP応答
  - error: テスト用関数が返した通信エラー
*/
func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// ----------------------------------------

/*
gzipBytes は、テスト用JSON本文をgzip形式へ圧縮します。

機能:
  - HTTP gzip応答として利用できるbyte列を生成する

引数:
  - t *testing.T: テスト状態を管理する値
  - value []byte: 圧縮するJSON本文

返り値:
  - []byte: gzip圧縮済み本文
*/
func gzipBytes(t *testing.T, value []byte) []byte {
	t.Helper()
	var builder strings.Builder
	writer := gzip.NewWriter(&builder)
	if _, err := writer.Write(value); err != nil {
		t.Fatalf("gzip.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return []byte(builder.String())
}
