package restapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type fakeService struct {
	dataList domain.DataList
	result   domain.CollectResponse
	err      error
	requests []domain.CollectRequest
}

/*
DataList は、テストで設定した固定一覧を返します。

機能:
  - RESTのdatalist応答へ設定済み値を提供する

引数:
  - なし

返り値:
  - domain.DataList: テスト用の固定一覧
*/
func (f *fakeService) DataList() domain.DataList {
	return f.dataList
}

/*
Collect は、RESTから渡された要求を記録して固定結果を返します。

機能:
  - 共通DTOへの復号結果をテスト検証用に保存する

引数:
  - ctx context.Context: REST要求のコンテキスト
  - request domain.CollectRequest: 復号済みの収集要求

返り値:
  - domain.CollectResponse: 設定済み結果
  - error: 設定済みエラー
*/
func (f *fakeService) Collect(
	ctx context.Context,
	request domain.CollectRequest,
) (domain.CollectResponse, error) {
	_ = ctx
	f.requests = append(f.requests, request)
	return f.result, f.err
}

// ----------------------------------------

/*
TestServerReturnsSharedDataListAndCollection は、RESTの共通入出力を検証します。

機能:
  - GET /api/datalistが共通一覧を返すことを確認する
  - POST /api/collectがJSON入力を共通サービスへ渡して結果を返すことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServerReturnsSharedDataListAndCollection(t *testing.T) {
	service := &fakeService{
		dataList: domain.DataList{Version: domain.APIVersion, Providers: []domain.ProviderDescriptor{{Name: "test"}}},
		result: domain.CollectResponse{
			Version: domain.APIVersion, Provider: "test", Dataset: "prices",
			Data: map[string]any{"price": 123, "large_integer": json.Number("9007199254740993")},
		},
	}
	server := newTestServer(t, service)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/datalist", nil)
	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), `"providers"`) {
		t.Fatalf("datalist応答 = %d %s", listRecorder.Code, listRecorder.Body.String())
	}
	if strings.Contains(listRecorder.Body.String(), `"enabled"`) {
		t.Errorf("datalist応答に登録済みproviderの不要なenabled項目があります: %s", listRecorder.Body.String())
	}

	body := `{"provider":"test","dataset":"prices","parameters":{"symbol":"A"}}`
	collectRequest := httptest.NewRequest(http.MethodPost, "/api/collect", strings.NewReader(body))
	collectRequest.Header.Set("Content-Type", "application/json")
	collectRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(collectRecorder, collectRequest)
	if collectRecorder.Code != http.StatusOK {
		t.Fatalf("collect応答 = %d %s", collectRecorder.Code, collectRecorder.Body.String())
	}
	if !strings.Contains(collectRecorder.Body.String(), `"large_integer":9007199254740993`) {
		t.Errorf("REST JSON = %s, 2^53超整数の精度保持を期待", collectRecorder.Body.String())
	}
	if len(service.requests) != 1 || service.requests[0].Parameters["symbol"] != "A" {
		t.Errorf("共通サービス要求 = %+v, symbol=Aを期待", service.requests)
	}
	var response domain.CollectResponse
	if err := json.Unmarshal(collectRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("collect応答のJSON復号 error = %v", err)
	}
	if response.Provider != "test" || response.Dataset != "prices" {
		t.Errorf("collect応答 = %+v, 共通識別子を期待", response)
	}
	if collectRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Error("Cache-Control=no-storeがありません")
	}
}

// ----------------------------------------

/*
TestServerRejectsInvalidRequestsAndMapsErrors は、REST境界と失敗分類を検証します。

機能:
  - JSON以外、未知項目、過大本文を早期拒否する
  - 共通の未知providerエラーをHTTP 404へ変換する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServerRejectsInvalidRequestsAndMapsErrors(t *testing.T) {
	service := &fakeService{err: domain.NewServiceError(
		domain.ErrorNotFound, "providerが見つかりません", nil,
	)}
	server := newTestServer(t, service)

	testCases := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{name: "JSON以外", body: `{}`, contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "未知項目", body: `{"provider":"x","dataset":"y","unknown":true}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "大文字違い", body: `{"Provider":"x","dataset":"y"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "重複項目", body: `{"provider":"x","provider":"y","dataset":"z"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "parametersのnull", body: `{"provider":"x","dataset":"y","parameters":null}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "過大本文", body: strings.Repeat("x", 1025), contentType: "application/json", wantStatus: http.StatusRequestEntityTooLarge},
		{name: "未知provider", body: `{"provider":"x","dataset":"y"}`, contentType: "application/json", wantStatus: http.StatusNotFound},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/collect", strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", testCase.contentType)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != testCase.wantStatus {
				t.Errorf("状態コード = %d, 期待値は%d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
		})
	}
	if retryAfter := httptestCollect(t, server, `{"provider":"x","dataset":"y"}`).Header().Get("Retry-After"); retryAfter != "" {
		t.Errorf("未知providerのRetry-After = %q, 空を期待", retryAfter)
	}
}

// ----------------------------------------

/*
TestServerHandlesMethodsAndJSONEncodingFailure は、HTTP境界の例外応答を検証します。

機能:
  - 既知パスへの非対応メソッドを405とAllowヘッダーで拒否する
  - JSONへ変換できない値を部分的な成功応答にせず500へ変換する
  - 一時的なprovider利用不能にだけRetry-Afterを付ける

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServerHandlesMethodsAndJSONEncodingFailure(t *testing.T) {
	service := &fakeService{
		dataList: domain.DataList{
			Version: domain.APIVersion,
			Providers: []domain.ProviderDescriptor{{
				Name: "test",
				Datasets: []domain.DatasetDescriptor{{
					Name: "prices",
					Parameters: []domain.ParameterDescriptor{{
						Name:    "invalid",
						Type:    "number",
						Default: math.NaN(),
					}},
				}},
			}},
		},
	}
	server := newTestServer(t, service)

	methodRequest := httptest.NewRequest(http.MethodPost, "/api/datalist", nil)
	methodRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(methodRecorder, methodRequest)
	if methodRecorder.Code != http.StatusMethodNotAllowed || methodRecorder.Header().Get("Allow") != http.MethodGet {
		t.Errorf("非対応メソッド応答 = %d Allow=%q", methodRecorder.Code, methodRecorder.Header().Get("Allow"))
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/datalist", nil)
	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusInternalServerError || !strings.Contains(listRecorder.Body.String(), `"code":"INTERNAL"`) {
		t.Errorf("JSON生成失敗応答 = %d %s", listRecorder.Code, listRecorder.Body.String())
	}

	service.err = domain.NewServiceError(domain.ErrorProviderUnavailable, "一時的に利用できません", nil)
	recorder := httptestCollect(t, server, `{"provider":"x","dataset":"y"}`)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" {
		t.Errorf("一時利用不能応答 = %d Retry-After=%q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
}

/*
newTestServer は、ログを破棄するRESTテストサーバーを生成します。

機能:
  - 1 KiBの本文上限と偽サービスでServerを初期化する

引数:
  - t *testing.T: テスト状態を管理する値
  - service Service: テストで利用する共通サービス

返り値:
  - *Server: 初期化済みRESTサーバー
*/
func newTestServer(t *testing.T, service Service) *Server {
	t.Helper()
	server, err := New(service, 1024, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server
}

// ----------------------------------------

/*
httptestCollect は、JSONの収集要求をテストサーバーへ送信します。

機能:
  - application/jsonのPOST /api/collect要求を組み立てて実行する

引数:
  - t *testing.T: テスト状態を管理する値
  - server *Server: 要求先のRESTテストサーバー
  - body string: JSON要求本文

返り値:
  - *httptest.ResponseRecorder: 記録済みHTTP応答
*/
func httptestCollect(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/collect", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}
