package kabuscontroller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type fakeKabusControllerAPIClient struct {
	response APIResponse
	err      error
	requests []fakeKabusControllerAPIRequest
}

type fakeKabusControllerAPIRequest struct {
	dataset string
	symbol  string
}

/*
Fetch は、Collectorから渡された要求を記録して設定済み結果を返します。

機能:
  - datasetとsymbolをテスト検証用に保持する
  - HTTP通信を行わず設定済みAPIResponseまたはerrorを返す

引数:
  - ctx context.Context: Collectorから伝播された要求context
  - dataset string: 収集対象の固定dataset識別子
  - symbol string: 個別銘柄datasetの銘柄コード

返り値:
  - APIResponse: テストで設定した応答
  - error: テストで設定したエラー
*/
func (f *fakeKabusControllerAPIClient) Fetch(
	ctx context.Context,
	dataset string,
	symbol string,
) (APIResponse, error) {
	_ = ctx
	f.requests = append(f.requests, fakeKabusControllerAPIRequest{dataset: dataset, symbol: symbol})
	return f.response, f.err
}

// ----------------------------------------

/*
TestCollectorDescriptorPublishesSixReadOnlyDatasets は、KabusControllerの公開仕様を検証します。

機能:
  - provider識別子、表示名、6 datasetの固定順を確認する
  - symbol_market_dataだけが必須stringのsymbol入力を公開することを確認する
  - Descriptor取得時にAPIClientへ要求しないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorDescriptorPublishesSixReadOnlyDatasets(t *testing.T) {
	client := &fakeKabusControllerAPIClient{}
	collector, err := NewCollector(client)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	descriptor := collector.Descriptor()
	if descriptor.Name != "kabus-controller" || descriptor.DisplayName != "KabusController" {
		t.Errorf("Descriptor() = %+v, provider識別子と表示名を期待", descriptor)
	}
	wantNames := []string{
		"future_registrations",
		"option_registrations",
		"market_data",
		"future_market_data",
		"option_market_data",
		"symbol_market_data",
	}
	if len(descriptor.Datasets) != len(wantNames) {
		t.Fatalf("dataset件数 = %d, %dを期待", len(descriptor.Datasets), len(wantNames))
	}
	for index, dataset := range descriptor.Datasets {
		if dataset.Name != wantNames[index] || dataset.Description == "" {
			t.Errorf("Datasets[%d] = %+v, dataset %qと日本語説明を期待", index, dataset, wantNames[index])
		}
		if dataset.Name != "symbol_market_data" {
			if len(dataset.Parameters) != 0 {
				t.Errorf("dataset %qのParameters = %+v, 空を期待", dataset.Name, dataset.Parameters)
			}
			continue
		}
		if len(dataset.Parameters) != 1 {
			t.Fatalf("symbol_market_dataのParameters = %+v, 1件を期待", dataset.Parameters)
		}
		parameter := dataset.Parameters[0]
		if parameter.Name != "symbol" || parameter.Type != "string" || !parameter.Required ||
			parameter.Description == "" {
			t.Errorf("symbol parameter = %+v, 必須string仕様を期待", parameter)
		}
	}
	if len(client.requests) != 0 {
		t.Errorf("Descriptor()が%d件のAPI要求を発生させました", len(client.requests))
	}
}

// ----------------------------------------

/*
TestCollectorEveryDatasetFetchesOnceAndReturnsMetadata は、正常収集と付帯情報を検証します。

機能:
  - 6 datasetがそれぞれAPIClientを1回だけ呼び出す
  - 個別銘柄だけが検証済みsymbolをAPIClientへ渡す
  - 上流JSONを変更せず、安全な固定metadataを付けて返す

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorEveryDatasetFetchesOnceAndReturnsMetadata(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 12, 1, 2, 3, 0, time.UTC)
	for _, spec := range endpointSpecs {
		t.Run(spec.Dataset, func(t *testing.T) {
			body := map[string]any{"dataset": spec.Dataset}
			client := &fakeKabusControllerAPIClient{response: APIResponse{
				Body: body, SourceURL: "http://10.10.100.1:8080" + spec.Path,
				StatusCode: http.StatusOK, FetchedAt: fetchedAt, ResponseBytes: 123,
			}}
			collector, err := NewCollector(client)
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			parameters := map[string]any{}
			wantSymbol := ""
			if spec.RequiresSymbol {
				wantSymbol = "NK225M-2026.09"
				parameters["symbol"] = wantSymbol
			}
			result, err := collector.Collect(context.Background(), spec.Dataset, parameters)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			wantRequest := []fakeKabusControllerAPIRequest{{dataset: spec.Dataset, symbol: wantSymbol}}
			if !reflect.DeepEqual(client.requests, wantRequest) {
				t.Errorf("APIClient要求 = %+v, %+vを期待", client.requests, wantRequest)
			}
			if !reflect.DeepEqual(result.Data, body) {
				t.Errorf("Data = %#v, 上流JSON全体を期待", result.Data)
			}
			if result.Metadata["source_name"] != "KabusController" ||
				result.Metadata["source_url"] != client.response.SourceURL ||
				result.Metadata["endpoint"] != spec.Path ||
				result.Metadata["upstream_status"] != http.StatusOK ||
				result.Metadata["upstream_fetched"] != fetchedAt ||
				result.Metadata["response_bytes"] != int64(123) ||
				result.Metadata["read_only"] != true || result.Metadata["on_demand"] != true {
				t.Errorf("Metadata = %+v, 固定取得情報を期待", result.Metadata)
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectorRejectsUnknownMissingAndInvalidParametersBeforeFetch は、Collectorの入力検証を確認します。

機能:
  - 未知datasetをNOT_FOUNDへ分類する
  - 未知項目、symbol不足、非string、空白、不正文字、固定path衝突をINVALID_ARGUMENTへ分類する
  - 不正入力ではAPIClientを呼び出さない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorRejectsUnknownMissingAndInvalidParametersBeforeFetch(t *testing.T) {
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
		kind       domain.ErrorKind
	}{
		{name: "未知dataset", dataset: "unknown", parameters: map[string]any{}, kind: domain.ErrorNotFound},
		{name: "固定datasetの未知項目", dataset: "market_data", parameters: map[string]any{"symbol": "NK225M"}, kind: domain.ErrorInvalidArgument},
		{name: "個別datasetの未知項目", dataset: "symbol_market_data", parameters: map[string]any{"symbol": "NK225M", "extra": true}, kind: domain.ErrorInvalidArgument},
		{name: "symbol不足", dataset: "symbol_market_data", parameters: map[string]any{}, kind: domain.ErrorInvalidArgument},
		{name: "symbol非string", dataset: "symbol_market_data", parameters: map[string]any{"symbol": 225}, kind: domain.ErrorInvalidArgument},
		{name: "symbol空文字", dataset: "symbol_market_data", parameters: map[string]any{"symbol": ""}, kind: domain.ErrorInvalidArgument},
		{name: "symbol前後空白", dataset: "symbol_market_data", parameters: map[string]any{"symbol": " NK225M "}, kind: domain.ErrorInvalidArgument},
		{name: "symbol不正文字", dataset: "symbol_market_data", parameters: map[string]any{"symbol": "NK225/M"}, kind: domain.ErrorInvalidArgument},
		{name: "symbol固定path衝突", dataset: "symbol_market_data", parameters: map[string]any{"symbol": "future"}, kind: domain.ErrorInvalidArgument},
		{name: "symbol長さ超過", dataset: "symbol_market_data", parameters: map[string]any{"symbol": string(make([]byte, 101))}, kind: domain.ErrorInvalidArgument},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{}
			collector, err := NewCollector(client)
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			_, err = collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			assertKabusControllerServiceErrorKind(t, err, testCase.kind)
			if len(client.requests) != 0 {
				t.Errorf("不正入力でAPIClientが%d回呼ばれました", len(client.requests))
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectorClassifiesClientErrors は、上流失敗の共通エラー分類を検証します。

機能:
  - 400・422、個別404、408・504、認証・混雑・利用不能状態を対応するErrorKindへ変換する
  - 一覧404、5xx、JSON・通信失敗をUPSTREAM_ERRORへ変換する
  - context deadlineとcancelを原因判定可能な状態で分類する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorClassifiesClientErrors(t *testing.T) {
	testCases := []struct {
		name    string
		dataset string
		err     error
		kind    domain.ErrorKind
	}{
		{name: "HTTP400", dataset: "market_data", err: &APIError{StatusCode: http.StatusBadRequest}, kind: domain.ErrorInvalidArgument},
		{name: "HTTP422", dataset: "market_data", err: &APIError{StatusCode: http.StatusUnprocessableEntity}, kind: domain.ErrorInvalidArgument},
		{name: "個別HTTP404", dataset: "symbol_market_data", err: &APIError{StatusCode: http.StatusNotFound}, kind: domain.ErrorNotFound},
		{name: "一覧HTTP404", dataset: "market_data", err: &APIError{StatusCode: http.StatusNotFound}, kind: domain.ErrorUpstream},
		{name: "HTTP408", dataset: "market_data", err: &APIError{StatusCode: http.StatusRequestTimeout}, kind: domain.ErrorTimeout},
		{name: "HTTP504", dataset: "market_data", err: &APIError{StatusCode: http.StatusGatewayTimeout}, kind: domain.ErrorTimeout},
		{name: "HTTP401", dataset: "market_data", err: &APIError{StatusCode: http.StatusUnauthorized}, kind: domain.ErrorProviderUnavailable},
		{name: "HTTP403", dataset: "market_data", err: &APIError{StatusCode: http.StatusForbidden}, kind: domain.ErrorProviderUnavailable},
		{name: "HTTP425", dataset: "market_data", err: &APIError{StatusCode: http.StatusTooEarly}, kind: domain.ErrorProviderUnavailable},
		{name: "HTTP429", dataset: "market_data", err: &APIError{StatusCode: http.StatusTooManyRequests}, kind: domain.ErrorProviderUnavailable},
		{name: "HTTP503", dataset: "market_data", err: &APIError{StatusCode: http.StatusServiceUnavailable}, kind: domain.ErrorProviderUnavailable},
		{name: "HTTP500", dataset: "market_data", err: &APIError{StatusCode: http.StatusInternalServerError}, kind: domain.ErrorUpstream},
		{name: "deadline", dataset: "market_data", err: context.DeadlineExceeded, kind: domain.ErrorTimeout},
		{name: "cancel", dataset: "market_data", err: context.Canceled, kind: domain.ErrorProviderUnavailable},
		{name: "JSON・通信", dataset: "market_data", err: errors.New("decode failed"), kind: domain.ErrorUpstream},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{err: testCase.err}
			collector, err := NewCollector(client)
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			parameters := map[string]any{}
			if testCase.dataset == "symbol_market_data" {
				parameters["symbol"] = "NK225M"
			}
			_, err = collector.Collect(context.Background(), testCase.dataset, parameters)
			assertKabusControllerServiceErrorKind(t, err, testCase.kind)
			if !errors.Is(err, testCase.err) {
				t.Errorf("Collect() error = %v, 元エラー %vを保持することを期待", err, testCase.err)
			}
		})
	}
}

// ----------------------------------------

/*
TestNewCollectorRejectsNilAndTypedNilClient は、Collector依存関係のnil検証を確認します。

機能:
  - nil interfaceと型付きnilポインターを起動時に拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewCollectorRejectsNilAndTypedNilClient(t *testing.T) {
	if _, err := NewCollector(nil); err == nil {
		t.Error("NewCollector(nil) error = nil, 拒否を期待")
	}
	var typedNil *fakeKabusControllerAPIClient
	if _, err := NewCollector(typedNil); err == nil {
		t.Error("NewCollector(typed nil) error = nil, 拒否を期待")
	}
}

// ----------------------------------------

/*
assertKabusControllerServiceErrorKind は、共通エラー分類を検証します。

機能:
  - error chainから*domain.ServiceErrorを取り出して期待するErrorKindと比較する

引数:
  - t *testing.T: テスト状態を管理する値
  - err error: Collectorが返したエラー
  - want domain.ErrorKind: 期待する共通エラー分類

返り値:
  - なし
*/
func assertKabusControllerServiceErrorKind(t *testing.T, err error, want domain.ErrorKind) {
	t.Helper()
	var serviceError *domain.ServiceError
	if !errors.As(err, &serviceError) || serviceError.Kind != want {
		t.Fatalf("error = %v, %sのServiceErrorを期待", err, want)
	}
}

// ----------------------------------------

/*
ExampleCollector_Collect は、個別銘柄板情報を収集する公開入力形式を示します。

機能:
  - symbol_market_dataへsymbolだけを指定する最小要求例を実行する

引数:
  - なし

返り値:
  - なし
*/
func ExampleCollector_Collect() {
	client := &fakeKabusControllerAPIClient{response: APIResponse{
		Body: map[string]any{"symbol": "NK225M"}, StatusCode: http.StatusOK,
	}}
	collector, _ := NewCollector(client)
	result, _ := collector.Collect(
		context.Background(),
		"symbol_market_data",
		map[string]any{"symbol": "NK225M"},
	)
	fmt.Println(result.Data)
	// Output: map[symbol:NK225M]
}
