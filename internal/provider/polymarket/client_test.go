package polymarket

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestClientFetchResolvesEveryDynamicPublicRoute は、動的selectorを正しい公開GETへ変換することを検証します。
//
// 機能:
//   - GammaのID・slug・関連タグ・コメント分岐を固定する
//   - CLOBの価格種別、市場一覧種別、condition・token path分岐を固定する
//   - selectorを上流queryへ残さず、1回のFetchにつき1回だけ通信することを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchResolvesEveryDynamicPublicRoute(t *testing.T) {
	tests := []struct {
		name        string
		dataset     string
		query       url.Values
		wantPath    string
		wantQuery   url.Values
		contentType string
		body        string
	}{
		{name: "event ID", dataset: "event", query: url.Values{"id": {"123"}}, wantPath: "/events/123", body: `{}`},
		{name: "event slug", dataset: "event", query: url.Values{"slug": {"event-slug"}}, wantPath: "/events/slug/event-slug", body: `{}`},
		{name: "market ID", dataset: "market", query: url.Values{"id": {"456"}}, wantPath: "/markets/456", body: `{}`},
		{name: "market slug", dataset: "market", query: url.Values{"slug": {"market-slug"}}, wantPath: "/markets/slug/market-slug", body: `{}`},
		{name: "tag ID", dataset: "tag", query: url.Values{"id": {"12"}, "include_template": {"true"}}, wantPath: "/tags/12", wantQuery: url.Values{"include_template": {"true"}}, body: `{}`},
		{name: "tag slug", dataset: "tag", query: url.Values{"slug": {"politics"}}, wantPath: "/tags/slug/politics", body: `{}`},
		{name: "related relationship", dataset: "related_tags", query: url.Values{"id": {"12"}, "resolved_tags": {"false"}, "status": {"active"}}, wantPath: "/tags/12/related-tags", wantQuery: url.Values{"status": {"active"}}, body: `[]`},
		{name: "related tag objects", dataset: "related_tags", query: url.Values{"slug": {"sports"}, "resolved_tags": {"true"}, "omit_empty": {"true"}}, wantPath: "/tags/slug/sports/related-tags/tags", wantQuery: url.Values{"omit_empty": {"true"}}, body: `[]`},
		{name: "series item", dataset: "series_item", query: url.Values{"id": {"99"}, "include_chat": {"false"}}, wantPath: "/series/99", wantQuery: url.Values{"include_chat": {"false"}}, body: `{}`},
		{name: "comments list", dataset: "comments", query: url.Values{"parent_entity_type": {"Event"}, "parent_entity_id": {"7"}}, wantPath: "/comments", wantQuery: url.Values{"parent_entity_type": {"Event"}, "parent_entity_id": {"7"}}, body: `[]`},
		{name: "comment ID", dataset: "comments", query: url.Values{"comment_id": {"7"}, "get_positions": {"true"}}, wantPath: "/comments/7", wantQuery: url.Values{"get_positions": {"true"}}, body: `[]`},
		{name: "comments address", dataset: "comments", query: url.Values{"user_address": {testWalletAddress}, "limit": {"10"}}, wantPath: "/comments/user_address/" + testWalletAddress, wantQuery: url.Values{"limit": {"10"}}, body: `[]`},
		{name: "best bid", dataset: "token_price", query: url.Values{"token_id": {"123"}, "price_type": {"best_bid"}}, wantPath: "/price", wantQuery: url.Values{"token_id": {"123"}, "side": {"BUY"}}, body: `{"price":0.45}`},
		{name: "best ask", dataset: "token_price", query: url.Values{"token_id": {"123"}, "price_type": {"best_ask"}}, wantPath: "/price", wantQuery: url.Values{"token_id": {"123"}, "side": {"SELL"}}, body: `{"price":0.46}`},
		{name: "midpoint", dataset: "token_price", query: url.Values{"token_id": {"123"}, "price_type": {"midpoint"}}, wantPath: "/midpoint", wantQuery: url.Values{"token_id": {"123"}}, body: `{"mid":"0.455"}`},
		{name: "last trade", dataset: "token_price", query: url.Values{"token_id": {"123"}, "price_type": {"last_trade"}}, wantPath: "/last-trade-price", wantQuery: url.Values{"token_id": {"123"}}, body: `{"price":"0.45","side":"BUY"}`},
		{name: "simplified markets", dataset: "clob_markets", query: url.Values{"kind": {"simplified"}, "next_cursor": {"MA=="}}, wantPath: "/simplified-markets", wantQuery: url.Values{"next_cursor": {"MA=="}}, body: `{}`},
		{name: "sampling markets", dataset: "clob_markets", query: url.Values{"kind": {"sampling"}}, wantPath: "/sampling-markets", body: `{}`},
		{name: "sampling simplified markets", dataset: "clob_markets", query: url.Values{"kind": {"sampling_simplified"}}, wantPath: "/sampling-simplified-markets", body: `{}`},
		{name: "CLOB condition", dataset: "clob_market", query: url.Values{"condition_id": {testConditionID}}, wantPath: "/clob-markets/" + testConditionID, body: `{}`},
		{name: "CLOB token", dataset: "market_by_token", query: url.Values{"token_id": {testTokenID}}, wantPath: "/markets-by-token/" + testTokenID, body: `{}`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodGet {
					t.Errorf("method = %s, GETを期待", request.Method)
				}
				if request.URL.Path != testCase.wantPath {
					t.Errorf("path = %q, %qを期待", request.URL.Path, testCase.wantPath)
				}
				if request.URL.Query().Encode() != testCase.wantQuery.Encode() {
					t.Errorf("query = %v, %vを期待", request.URL.Query(), testCase.wantQuery)
				}
				contentType := testCase.contentType
				if contentType == "" {
					contentType = "application/json"
				}
				writer.Header().Set("Content-Type", contentType)
				_, _ = io.WriteString(writer, testCase.body)
			}))
			defer server.Close()

			client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)
			originalQuery := cloneValues(testCase.query)
			if _, err := client.Fetch(context.Background(), testCase.dataset, testCase.query); err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if requests.Load() != 1 {
				t.Errorf("request数 = %d, 1を期待", requests.Load())
			}
			if !reflect.DeepEqual(testCase.query, originalQuery) {
				t.Errorf("呼出元queryが変更されました: actual=%v original=%v", testCase.query, originalQuery)
			}
		})
	}
}

// TestClientFetchPreservesRepeatedAndCSVQueryEncoding は、APIごとの配列query表現を保持することを検証します。
//
// 機能:
//   - Gammaのform explode=trueを同名queryの反復として送る
//   - Dataのform explode=falseを単一CSV値として送る
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchPreservesRepeatedAndCSVQueryEncoding(t *testing.T) {
	conditionA := "0x" + strings.Repeat("a", 64)
	conditionB := "0x" + strings.Repeat("b", 64)
	tests := []struct {
		name      string
		dataset   string
		query     url.Values
		queryName string
		want      []string
	}{
		{name: "Gamma repeat", dataset: "series", query: url.Values{"slug": {"alpha", "beta"}}, queryName: "slug", want: []string{"alpha", "beta"}},
		{name: "Data CSV", dataset: "holders", query: url.Values{"market": {conditionA + "," + conditionB}}, queryName: "market", want: []string{conditionA + "," + conditionB}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if actual := request.URL.Query()[testCase.queryName]; !slicesEqual(actual, testCase.want) {
					t.Errorf("query %q = %v, %vを期待", testCase.queryName, actual, testCase.want)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `[]`)
			}))
			defer server.Close()

			client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)
			if _, err := client.Fetch(context.Background(), testCase.dataset, testCase.query); err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
		})
	}
}

// TestClientFetchRejectsCollectorOnlyAndUnknownQueries は、Client直呼びのquery境界を検証します。
//
// 機能:
//   - collectorだけが解釈する公開parameter名を上流queryとして拒否する
//   - 他datasetの上流query名と完全な未知名を通信前に拒否する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchRejectsCollectorOnlyAndUnknownQueries(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)

	tests := []struct {
		dataset string
		query   url.Values
	}{
		{dataset: "search", query: url.Values{"q": {"test"}, "include_closed_markets": {"true"}}},
		{dataset: "user_activity", query: url.Values{"user": {testWalletAddress}, "include_deposits_and_withdrawals": {"true"}}},
		{dataset: "search", query: url.Values{"q": {"test"}, "after_cursor": {"cursor"}}},
		{dataset: "server_time", query: url.Values{"unknown": {"value"}}},
	}
	for _, testCase := range tests {
		if _, err := client.Fetch(context.Background(), testCase.dataset, testCase.query); err == nil {
			t.Errorf("dataset %q query=%v のFetch() error = nil", testCase.dataset, testCase.query)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("拒否queryでrequest数 = %d, 0を期待", requests.Load())
	}
}

// TestClientFetchRejectsUnsafeRouteSelectorsWithoutRequest は、path selector注入を通信前に拒否します。
//
// 機能:
//   - id、slug、token、condition、addressへslash、escape、dot segmentを入れた要求を拒否する
//   - 不正selectorで上流へ一度も接続しないことを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchRejectsUnsafeRouteSelectorsWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)

	selectors := []struct {
		name    string
		dataset string
		key     string
	}{
		{name: "ID", dataset: "event", key: "id"},
		{name: "slug", dataset: "event", key: "slug"},
		{name: "token", dataset: "market_by_token", key: "token_id"},
		{name: "condition", dataset: "clob_market", key: "condition_id"},
		{name: "address", dataset: "comments", key: "user_address"},
	}
	unsafeValues := []string{"/", "%2f", "..", "valid/segment"}
	for _, selector := range selectors {
		for _, unsafeValue := range unsafeValues {
			name := selector.name + "_" + strings.ReplaceAll(unsafeValue, "/", "slash")
			t.Run(name, func(t *testing.T) {
				query := url.Values{selector.key: {unsafeValue}}
				if _, err := client.Fetch(context.Background(), selector.dataset, query); err == nil {
					t.Errorf("dataset %q selector %q=%q のFetch() error = nil", selector.dataset, selector.key, unsafeValue)
				}
			})
		}
	}
	if _, err := client.Fetch(context.Background(), "market_by_token", url.Values{"token_id": {strings.Repeat("1", 101)}}); err == nil {
		t.Error("最大長を超えるtoken_idのFetch() error = nil")
	}
	if requests.Load() != 0 {
		t.Errorf("不正selectorでrequest数 = %d, 0を期待", requests.Load())
	}
}

// TestClientFetchRejectsAmbiguousOrEmptySelectorsWithoutFallback は、不正selectorの別endpoint化を防ぎます。
//
// 機能:
//   - 単一値selectorの空値と多重値を拒否する
//   - commentsのcomment ID不正時に一覧endpointへフォールバックしない
//   - related_tagsのresolved_tags不正時にrelationship endpointへフォールバックしない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchRejectsAmbiguousOrEmptySelectorsWithoutFallback(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[]`)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)

	tests := []struct {
		name    string
		dataset string
		query   url.Values
	}{
		{name: "event empty ID", dataset: "event", query: url.Values{"id": {""}}},
		{name: "event repeated ID", dataset: "event", query: url.Values{"id": {"1", "2"}}},
		{name: "comment empty ID", dataset: "comments", query: url.Values{"comment_id": {""}}},
		{name: "comment repeated ID", dataset: "comments", query: url.Values{"comment_id": {"1", "2"}}},
		{name: "comment empty address", dataset: "comments", query: url.Values{"user_address": {""}}},
		{name: "comment repeated address", dataset: "comments", query: url.Values{"user_address": {testWalletAddress, testWalletAddress}}},
		{name: "related empty resolved selector", dataset: "related_tags", query: url.Values{"id": {"1"}, "resolved_tags": {""}}},
		{name: "related repeated resolved selector", dataset: "related_tags", query: url.Values{"id": {"1"}, "resolved_tags": {"true", "false"}}},
		{name: "related invalid resolved selector", dataset: "related_tags", query: url.Values{"id": {"1"}, "resolved_tags": {"invalid"}}},
		{name: "token price repeated selector", dataset: "token_price", query: url.Values{"token_id": {"123"}, "price_type": {"best_bid", "best_ask"}}},
		{name: "CLOB markets empty selector", dataset: "clob_markets", query: url.Values{"kind": {""}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := client.Fetch(context.Background(), testCase.dataset, testCase.query); err == nil {
				t.Errorf("Fetch() error = nil: query=%v", testCase.query)
			}
		})
	}
	if requests.Load() != 0 {
		t.Errorf("不正selectorでrequest数 = %d, 0を期待", requests.Load())
	}
}

// TestClientFetchRejectsQueriesIncompatibleWithCommentRoute は、コメント分岐ごとのquery境界を検証します。
//
// 機能:
//   - comment ID endpointにはget_positions以外を送らない
//   - user address endpointには一覧ページングquery以外を送らない
//   - 単一dataset内のpath分岐で別endpoint用queryを静かに転送しない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchRejectsQueriesIncompatibleWithCommentRoute(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[]`)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)

	tests := []url.Values{
		{"comment_id": {"1"}, "limit": {"10"}},
		{"comment_id": {"1"}, "parent_entity_type": {"Event"}, "parent_entity_id": {"2"}},
		{"user_address": {testWalletAddress}, "get_positions": {"true"}},
		{"user_address": {testWalletAddress}, "holders_only": {"true"}},
	}
	for _, query := range tests {
		if _, err := client.Fetch(context.Background(), "comments", query); err == nil {
			t.Errorf("comments query=%v のFetch() error = nil", query)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("route不整合queryでrequest数 = %d, 0を期待", requests.Load())
	}
}

// TestClientFetchAcceptsServerTimeTextPlainAndPreservesNumbers は、CLOB時刻とJSON精度を検証します。
//
// 機能:
//   - `/time`に限り公式実応答のtext/plainを許可する
//   - 整数と小数をfloat64へ丸めずjson.Numberとして保持する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchAcceptsServerTimeTextPlainAndPreservesNumbers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/time" {
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(writer, `1786172400`)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"large":123456789012345678901234567890,"decimal":0.0450000000000000001}`)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)

	timeResponse, err := client.Fetch(context.Background(), "server_time", nil)
	if err != nil {
		t.Fatalf("server_time Fetch() error = %v", err)
	}
	if actual, ok := timeResponse.Body.(json.Number); !ok || actual.String() != "1786172400" {
		t.Errorf("server_time body = %#v, json.Numberを期待", timeResponse.Body)
	}

	response, err := client.Fetch(context.Background(), "sports", nil)
	if err != nil {
		t.Fatalf("sports Fetch() error = %v", err)
	}
	body, ok := response.Body.(map[string]any)
	if !ok {
		t.Fatalf("sports body型 = %T, map[string]anyを期待", response.Body)
	}
	if body["large"].(json.Number).String() != "123456789012345678901234567890" || body["decimal"].(json.Number).String() != "0.0450000000000000001" {
		t.Errorf("JSON数値が保持されていません: %#v", body)
	}
}

// TestClientFetchHandlesGzipAndResponseLimits は、圧縮前後の本文上限を検証します。
//
// 機能:
//   - gzip JSONを展開してdecodeする
//   - 圧縮前、展開後、identity本文の上限超過をそれぞれ拒否する
//   - 未知Content-Encodingを拒否する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchHandlesGzipAndResponseLimits(t *testing.T) {
	validCompressed := gzipBytes(t, []byte(`{"value":12345678901234567890}`))
	largeExpanded := gzipBytes(t, []byte(`{"value":"`+strings.Repeat("a", 256)+`"}`))
	tests := []struct {
		name        string
		body        []byte
		encoding    string
		maximum     int64
		wantError   string
		wantDecoded bool
	}{
		{name: "gzip success", body: validCompressed, encoding: "gzip", maximum: 128, wantDecoded: true},
		{name: "compressed limit", body: append([]byte(nil), largeExpanded...), encoding: "gzip", maximum: int64(len(largeExpanded) - 1), wantError: "圧縮前"},
		{name: "expanded limit", body: largeExpanded, encoding: "gzip", maximum: 128, wantError: "gzip展開後"},
		{name: "identity limit", body: []byte(`{"value":"` + strings.Repeat("b", 64) + `"}`), maximum: 32, wantError: "圧縮前"},
		{name: "unknown encoding", body: []byte(`{}`), encoding: "br", maximum: 128, wantError: "Content-Encoding"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if testCase.encoding != "" {
					writer.Header().Set("Content-Encoding", testCase.encoding)
				}
				_, _ = writer.Write(testCase.body)
			}))
			defer server.Close()
			client := newHTTPTestClient(t, server.URL, nil, testCase.maximum)

			response, err := client.Fetch(context.Background(), "sports", nil)
			if testCase.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
					t.Fatalf("Fetch() error = %v, %qを含むerrorを期待", err, testCase.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if testCase.wantDecoded {
				body, ok := response.Body.(map[string]any)
				if !ok || body["value"].(json.Number).String() != "12345678901234567890" {
					t.Errorf("gzip decode結果 = %#v", response.Body)
				}
			}
		})
	}
}

// TestClientFetchRejectsInvalidJSONAndNonJSONMIME は、成功応答の形式境界を検証します。
//
// 機能:
//   - 不正JSON、複数JSON値、通常endpointのtext/plainを拒否する
//   - API上の成功状態だけで不正本文を受理しないことを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchRejectsInvalidJSONAndNonJSONMIME(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "invalid JSON", contentType: "application/json", body: `{"x":`},
		{name: "extra JSON", contentType: "application/json", body: `{} []`},
		{name: "non JSON MIME", contentType: "text/plain", body: `{}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", testCase.contentType)
				_, _ = io.WriteString(writer, testCase.body)
			}))
			defer server.Close()
			client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)
			if _, err := client.Fetch(context.Background(), "sports", nil); err == nil {
				t.Error("Fetch() error = nil")
			}
		})
	}
}

// TestClientFetchClassifiesHTTPErrorWithoutLeakingBody は、非2xx応答の安全な分類を検証します。
//
// 機能:
//   - HTTP状態、Retry-After、固定endpointをAPIErrorへ保持する
//   - 上流本文を公開error文字列へ含めない
//   - 非2xxを自動再試行しない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchClassifiesHTTPErrorWithoutLeakingBody(t *testing.T) {
	const secretBody = `upstream-diagnostic-secret`
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Retry-After", "7")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, secretBody)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, DefaultMaxResponseBytes)

	_, err := client.Fetch(context.Background(), "sports", nil)
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("Fetch() error = %T %v, *APIErrorを期待", err, err)
	}
	if apiError.StatusCode != http.StatusTooManyRequests || apiError.RetryAfter != "7" || apiError.Endpoint != "/sports" {
		t.Errorf("APIError = %+v", apiError)
	}
	if strings.Contains(err.Error(), secretBody) {
		t.Errorf("公開errorが上流本文を含みます: %v", err)
	}
	if requests.Load() != 1 {
		t.Errorf("request数 = %d, 自動再試行しない1回を期待", requests.Load())
	}
}

// TestNewClientClonesCallerHTTPClientAndFollowsNormalRedirect は、HTTP client所有権とredirectを検証します。
//
// 機能:
//   - 呼出元http.Clientを複製し設定を破壊しない
//   - 標準redirectを追跡し最終URLをmetadataへ保持する
//   - 呼出元が指定したredirect方針は複製先で維持する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNewClientClonesCallerHTTPClientAndFollowsNormalRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/sports":
			http.Redirect(writer, request, "/redirected", http.StatusFound)
		case "/redirected":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `[]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	original := &http.Client{Timeout: 3 * time.Second}
	client := newHTTPTestClient(t, server.URL, original, DefaultMaxResponseBytes)
	if client.httpClient == original {
		t.Fatal("NewClientが呼出元http.Clientのポインターを保持しました")
	}
	client.httpClient.Timeout = time.Second
	if original.Timeout != 3*time.Second || original.CheckRedirect != nil {
		t.Errorf("呼出元http.Clientが変更されました: %+v", original)
	}

	response, err := client.Fetch(context.Background(), "sports", nil)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !strings.HasSuffix(response.SourceURL, "/redirected") {
		t.Errorf("SourceURL = %q, redirect後URLを期待", response.SourceURL)
	}

	redirectError := errors.New("redirect policy")
	custom := &http.Client{CheckRedirect: func(request *http.Request, via []*http.Request) error {
		return redirectError
	}}
	customClient := newHTTPTestClient(t, server.URL, custom, DefaultMaxResponseBytes)
	if _, err := customClient.Fetch(context.Background(), "sports", nil); !errors.Is(err, redirectError) {
		t.Errorf("custom redirect Fetch() error = %v, 指定方針のerrorを期待", err)
	}
}

// TestNewClientDropsCallerCookieJar は、公開APIへcallerのCookieを持ち込まないことを検証します。
//
// 機能:
//   - 呼出元Jarに保存済みのCookieをPolymarket要求へ送らない
//   - 上流Set-Cookieで呼出元Jarを更新しない
//   - caller client自体とそのJarを変更せず、内部cloneだけJarなしにする
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNewClientDropsCallerCookieJar(t *testing.T) {
	var receivedCookie atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedCookie.Store(request.Header.Get("Cookie"))
		http.SetCookie(writer, &http.Cookie{Name: "upstream", Value: "new-secret", Path: "/"})
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[]`)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("テストserver URLを解析できません: %v", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "session", Value: "caller-secret", Path: "/"}})
	original := &http.Client{Jar: jar}
	client := newHTTPTestClient(t, server.URL, original, DefaultMaxResponseBytes)
	if client.httpClient.Jar != nil {
		t.Fatal("内部HTTP clientにcaller CookieJarが残っています")
	}
	if original.Jar != jar {
		t.Fatal("呼出元HTTP clientのCookieJarが変更されました")
	}

	if _, err := client.Fetch(context.Background(), "sports", nil); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if actual, _ := receivedCookie.Load().(string); actual != "" {
		t.Errorf("公開API要求へCookieが送信されました: %q", actual)
	}
	cookies := jar.Cookies(serverURL)
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "caller-secret" {
		t.Errorf("呼出元CookieJarが上流応答で変更されました: %+v", cookies)
	}
}

// TestClientFetchHandlesMaxInt64BodyLimitWithoutOverflow は、本文上限加算のoverflowを検証します。
//
// 機能:
//   - MaxInt64上限でもmaximum+1を負数へoverflowさせない
//   - 小容量JSONを欠落なく読み取る
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClientFetchHandlesMaxInt64BodyLimitWithoutOverflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"ok":true}`)
	}))
	defer server.Close()
	client := newHTTPTestClient(t, server.URL, nil, math.MaxInt64)

	response, err := client.Fetch(context.Background(), "sports", nil)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	body, ok := response.Body.(map[string]any)
	if !ok || body["ok"] != true {
		t.Errorf("Body = %#v, 完全なJSON objectを期待", response.Body)
	}
}

// TestNewClientRejectsUnsafeConfiguration は、client生成時の接続設定検証を確認します。
//
// 機能:
//   - path、userinfo、query、非HTTP schemeを含むbase URLを拒否する
//   - 制御文字を含むUser-Agentと非正数本文上限を拒否する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNewClientRejectsUnsafeConfiguration(t *testing.T) {
	tests := []ClientConfig{
		{GammaBaseURL: "https://example.com/path"},
		{CLOBBaseURL: "https://user@example.com"},
		{DataBaseURL: "https://example.com?x=1"},
		{GammaBaseURL: "file:///tmp/data"},
		{UserAgent: "bad\r\nvalue"},
		{MaxResponseBytes: -1},
	}
	for _, config := range tests {
		if _, err := NewClient(config); err == nil {
			t.Errorf("NewClient(%+v) error = nil", config)
		}
	}
}

const (
	testWalletAddress = "0x1111111111111111111111111111111111111111"
	testConditionID   = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testTokenID       = "123456789012345678901234567890123456789012345678901234567890123456789"
)

// newHTTPTestClient は、3 APIを同じテストserverへ向けたClientを生成します。
//
// 機能:
//   - テストごとのHTTP clientと本文上限をClientConfigへ設定する
//   - 生成失敗をテスト失敗として即時終了する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - baseURL string: 3 API共通のテストorigin
//   - httpClient *http.Client: 任意の呼出元client。nilで標準設定
//   - maximum int64: 応答本文上限
//
// 返り値:
//   - *Client: テスト用Polymarket client
func newHTTPTestClient(t *testing.T, baseURL string, httpClient *http.Client, maximum int64) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		GammaBaseURL:     baseURL,
		CLOBBaseURL:      baseURL,
		DataBaseURL:      baseURL,
		HTTPClient:       httpClient,
		UserAgent:        "MarketDataCollector-test",
		MaxResponseBytes: maximum,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

// gzipBytes は、テスト本文をgzip圧縮します。
//
// 機能:
//   - gzip writerへ本文を書き、closeまで成功したbyte列を返す
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - body []byte: 圧縮する本文
//
// 返り値:
//   - []byte: gzip圧縮済み本文
func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("gzip.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return buffer.Bytes()
}

// slicesEqual は、文字列sliceの順序と内容が一致するか返します。
//
// 機能:
//   - nilと空sliceを同じ長さ0として扱い、各要素を順番に比較する
//
// 引数:
//   - left []string: 左辺slice
//   - right []string: 右辺slice
//
// 返り値:
//   - bool: 全要素が一致する場合はtrue
func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
