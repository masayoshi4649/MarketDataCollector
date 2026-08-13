package kabuscontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type fakeKabusControllerAPIClient struct {
	responses map[string][]APIResponse
	errors    map[string][]error
	requests  []fakeKabusControllerAPIRequest
}

type fakeKabusControllerAPIRequest struct {
	dataset    string
	parameters map[string]string
}

// Fetch は、Collectorから渡された要求を記録してdataset別のキューから結果を返します。
//
// 主な特徴:
//   - parametersを複製して後続変更からテスト記録を保護する
//   - errorキューをresponseキューより優先して返す
//   - 未設定datasetには空の正常応答を返す
//
// 引数:
//   - ctx context.Context: Collectorから伝播された要求context
//   - dataset string: 収集対象dataset
//   - parameters map[string]string: 検証・正規化済み入力
//
// 返り値:
//   - APIResponse: dataset別キューの先頭または空の正常応答
//   - error: dataset別errorキューの先頭
func (f *fakeKabusControllerAPIClient) Fetch(
	ctx context.Context,
	dataset string,
	parameters map[string]string,
) (APIResponse, error) {
	_ = ctx
	f.requests = append(f.requests, fakeKabusControllerAPIRequest{
		dataset: dataset, parameters: cloneStringMap(parameters),
	})
	if queue := f.errors[dataset]; len(queue) > 0 {
		err := queue[0]
		f.errors[dataset] = queue[1:]
		return APIResponse{}, err
	}
	if queue := f.responses[dataset]; len(queue) > 0 {
		response := queue[0]
		f.responses[dataset] = queue[1:]
		return response, nil
	}
	return APIResponse{Body: map[string]any{}, StatusCode: http.StatusOK}, nil
}

// ----------------------------------------

// TestCollectorDescriptorPublishesEighteenDatasets は、KabusControllerの公開仕様を検証します。
//
// 主な特徴:
//   - provider識別子、18 datasetの固定順、副作用説明を確認する
//   - Descriptor取得時に上流要求を発生させないことを確認する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorDescriptorPublishesEighteenDatasets(t *testing.T) {
	client := &fakeKabusControllerAPIClient{}
	collector, err := NewCollector(client)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	descriptor := collector.Descriptor()
	wantNames := []string{
		"future_registrations", "option_registrations", "market_data",
		"future_market_data", "option_market_data", "symbol_market_data",
		"kabus_ranking", "kabus_regulations", "derivative_symbol_resolver",
		"nt_pair_symbol_resolver", "arbitrary_board_snapshot", "option_chain_snapshot",
		"kabus_symbol_info", "kabus_primary_exchange", "kabus_fx_snapshot",
		"kabus_margin_premium", "kabus_api_soft_limits", "kabus_api_capacity",
	}
	if descriptor.Name != "kabus-controller" || descriptor.DisplayName != "KabusController" ||
		!strings.Contains(descriptor.Description, "自動登録") {
		t.Errorf("Descriptor() = %+v, provider識別子と副作用説明を期待", descriptor)
	}
	if len(descriptor.Datasets) != len(wantNames) {
		t.Fatalf("dataset件数 = %d, %dを期待", len(descriptor.Datasets), len(wantNames))
	}
	for index, dataset := range descriptor.Datasets {
		if dataset.Name != wantNames[index] || dataset.Description == "" || dataset.Parameters == nil {
			t.Errorf("Datasets[%d] = %+v, dataset %qと公開説明を期待", index, dataset, wantNames[index])
		}
	}
	if len(client.requests) != 0 {
		t.Errorf("Descriptor()が%d件のAPI要求を発生させました", len(client.requests))
	}
}

// ----------------------------------------

// TestCollectorSingleDatasetsNormalizeInputsAndMetadata は、単一GETの入力とmetadataを検証します。
//
// 主な特徴:
//   - 既定値、整数値float、booleanを安定文字列へ正規化する
//   - 銘柄自動登録、副作用なし、Bid・Ask注意、価格鮮度をdataset別に表す
//   - 上流JSONを変更せず返す
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorSingleDatasetsNormalizeInputsAndMetadata(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 13, 14, 43, 11, 0, time.UTC)
	priceTime := "2026-08-13T23:43:06+09:00"
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
		want       map[string]string
		body       any
		readOnly   bool
		register   bool
		bidAsk     bool
		fresh      bool
	}{
		{
			name: "登録一覧", dataset: "future_registrations", parameters: map[string]any{},
			want: map[string]string{}, body: map[string]any{"data": map[string]any{"futures": []any{}}}, readOnly: true,
		},
		{
			name: "ランキング既定市場", dataset: "kabus_ranking",
			parameters: map[string]any{"ranking_type": "2"},
			want:       map[string]string{"ranking_type": "2", "exchange_division": "ALL"},
			body:       map[string]any{"Ranking": []any{}}, readOnly: true,
		},
		{
			name: "規制の東証既定", dataset: "kabus_regulations",
			parameters: map[string]any{"symbol": "4419"},
			want:       map[string]string{"symbol": "4419", "exchange": "1"},
			body:       map[string]any{"Symbol": "4419@1"}, register: true,
		},
		{
			name: "先物resolverの整数値float", dataset: "derivative_symbol_resolver",
			parameters: map[string]any{"kind": "future", "product_code": "NK225mini", "deriv_month": float64(202609)},
			want:       map[string]string{"kind": "future", "product_code": "NK225mini", "deriv_month": "202609"},
			body:       map[string]any{"Symbol": "169090019"}, register: true,
		},
		{
			name: "任意板の鮮度", dataset: "arbitrary_board_snapshot",
			parameters: map[string]any{"symbol": "4419", "exchange": "1"},
			want:       map[string]string{"symbol": "4419", "exchange": "1"},
			body:       map[string]any{"CurrentPrice": json.Number("1269"), "CurrentPriceTime": priceTime},
			register:   true, bidAsk: true, fresh: true,
		},
		{
			name: "銘柄追加情報既定", dataset: "kabus_symbol_info",
			parameters: map[string]any{"symbol": "4419", "exchange": "1"},
			want:       map[string]string{"symbol": "4419", "exchange": "1", "add_info": "true"},
			body:       map[string]any{"Symbol": "4419@1"}, register: true,
		},
		{
			name: "為替既定通貨", dataset: "kabus_fx_snapshot", parameters: map[string]any{},
			want: map[string]string{"pair": "usdjpy"}, body: map[string]any{"Time": "23:43:06"}, readOnly: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := APIResponse{
				Body: testCase.body, SourceURL: "http://10.10.100.1:8080/source",
				StatusCode: http.StatusOK, FetchedAt: fetchedAt, ResponseBytes: 123,
			}
			client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{testCase.dataset: {response}}}
			collector, _ := NewCollector(client)
			result, err := collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			wantRequests := []fakeKabusControllerAPIRequest{{dataset: testCase.dataset, parameters: testCase.want}}
			if !reflect.DeepEqual(client.requests, wantRequests) {
				t.Errorf("APIClient要求 = %#v, %#vを期待", client.requests, wantRequests)
			}
			if !reflect.DeepEqual(result.Data, testCase.body) {
				t.Errorf("Data = %#v, 上流JSON全体を期待", result.Data)
			}
			if result.Metadata["read_only"] != testCase.readOnly ||
				result.Metadata["may_register_symbol"] == true != testCase.register ||
				result.Metadata["source_url"] != response.SourceURL ||
				result.Metadata["request_parameters"] == nil {
				t.Errorf("Metadata = %+v, dataset別付帯情報を期待", result.Metadata)
			}
			if (result.Metadata["bid_ask_warning"] != nil) != testCase.bidAsk {
				t.Errorf("bid_ask_warning = %#v, 有無%vを期待", result.Metadata["bid_ask_warning"], testCase.bidAsk)
			}
			if testCase.dataset == "kabus_ranking" {
				if result.Metadata["ranking_target_date_available"] != false ||
					result.Metadata["price_and_industry_clear_window_jst"] == nil ||
					result.Metadata["margin_ranking_update_schedule_jst"] == nil ||
					result.Metadata["empty_response_may_be_clear_window"] != true {
					t.Errorf("ranking schedule metadata = %+v", result.Metadata)
				}
			}
			if testCase.fresh {
				if result.Metadata["source_at"] == nil || result.Metadata["age_seconds"] != float64(5) || result.Metadata["is_stale"] != nil {
					t.Errorf("鮮度metadata = %+v", result.Metadata)
				}
			} else if result.Metadata["source_at"] != nil || result.Metadata["age_seconds"] != nil {
				t.Errorf("日付なし値から鮮度を推測しました: %+v", result.Metadata)
			}
		})
	}
}

// ----------------------------------------

// TestCollectorFreshnessUsesOldestPriceTimeAndCountsMissing は、一覧鮮度の保守的集計を検証します。
//
// 主な特徴:
//   - 複数板では最古時刻をsource_at、最新時刻をsource_at_latestへ分離する
//   - nullと解析不能文字列を欠損件数へ含める
//   - age_secondsを最新ではなく最古時刻から計算する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorFreshnessUsesOldestPriceTimeAndCountsMissing(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 13, 15, 0, 10, 0, time.UTC)
	oldest := "2026-08-13T23:59:50+09:00"
	latest := "2026-08-13T23:59:59+09:00"
	body := map[string]any{"data": []any{
		map[string]any{"CurrentPriceTime": latest},
		map[string]any{"CurrentPriceTime": oldest},
		map[string]any{"CurrentPriceTime": nil},
		map[string]any{"CurrentPriceTime": "23:59:59"},
		map[string]any{"Symbol": "時刻キー自体なし"},
	}}
	client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
		"future_market_data": {{Body: body, SourceURL: "http://x/futures", StatusCode: 200, FetchedAt: fetchedAt}},
	}}
	collector, _ := NewCollector(client)
	result, err := collector.Collect(context.Background(), "future_market_data", map[string]any{})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	parsedOldest, _ := time.Parse(time.RFC3339, oldest)
	parsedLatest, _ := time.Parse(time.RFC3339, latest)
	if result.Metadata["source_at"] != parsedOldest || result.Metadata["source_at_latest"] != parsedLatest ||
		result.Metadata["age_seconds"] != float64(20) || result.Metadata["source_time_parsed_count"] != 2 ||
		result.Metadata["source_time_missing_or_unparseable_count"] != 2 {
		t.Errorf("鮮度metadata = %+v, 最古・最新・解析2・欠損2を期待", result.Metadata)
	}
}

// ----------------------------------------

// TestCollectorRejectsInvalidParametersBeforeFetch は、公開入力の境界検証を確認します。
//
// 主な特徴:
//   - 未知dataset、未知項目、型、列挙、path形式、限月、resolver条件を拒否する
//   - 不正入力でAPIClientを呼び出さない
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorRejectsInvalidParametersBeforeFetch(t *testing.T) {
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
		kind       domain.ErrorKind
	}{
		{name: "未知dataset", dataset: "unknown", parameters: map[string]any{}, kind: domain.ErrorNotFound},
		{name: "未知項目", dataset: "market_data", parameters: map[string]any{"x": 1}, kind: domain.ErrorInvalidArgument},
		{name: "symbol不足", dataset: "symbol_market_data", parameters: map[string]any{}, kind: domain.ErrorInvalidArgument},
		{name: "symbol非string", dataset: "symbol_market_data", parameters: map[string]any{"symbol": 225}, kind: domain.ErrorInvalidArgument},
		{name: "symbol前後空白", dataset: "kabus_regulations", parameters: map[string]any{"symbol": " 4419"}, kind: domain.ErrorInvalidArgument},
		{name: "path注入", dataset: "arbitrary_board_snapshot", parameters: map[string]any{"symbol": "../4419", "exchange": "1"}, kind: domain.ErrorInvalidArgument},
		{name: "ranking型", dataset: "kabus_ranking", parameters: map[string]any{"ranking_type": 2}, kind: domain.ErrorInvalidArgument},
		{name: "ranking列挙", dataset: "kabus_ranking", parameters: map[string]any{"ranking_type": "16"}, kind: domain.ErrorInvalidArgument},
		{name: "市場列挙", dataset: "kabus_regulations", parameters: map[string]any{"symbol": "4419", "exchange": "2"}, kind: domain.ErrorInvalidArgument},
		{name: "限月の小数", dataset: "derivative_symbol_resolver", parameters: map[string]any{"kind": "future", "product_code": "NK225mini", "deriv_month": 202609.5}, kind: domain.ErrorInvalidArgument},
		{name: "存在しない月", dataset: "derivative_symbol_resolver", parameters: map[string]any{"kind": "future", "product_code": "NK225mini", "deriv_month": 202613}, kind: domain.ErrorInvalidArgument},
		{name: "先物へOP商品", dataset: "derivative_symbol_resolver", parameters: map[string]any{"kind": "future", "product_code": "NK225op", "deriv_month": 202609}, kind: domain.ErrorInvalidArgument},
		{name: "optionのstrike不足", dataset: "derivative_symbol_resolver", parameters: map[string]any{"kind": "option", "product_code": "NK225op", "deriv_month": 202609, "put_or_call": "C"}, kind: domain.ErrorInvalidArgument},
		{name: "限週へproduct混入", dataset: "derivative_symbol_resolver", parameters: map[string]any{"kind": "mini_option_weekly", "product_code": "NK225miniop", "deriv_month": 202609, "put_or_call": "C", "strike_price": 69000, "deriv_weekly": 1}, kind: domain.ErrorInvalidArgument},
		{name: "NT直近限月", dataset: "nt_pair_symbol_resolver", parameters: map[string]any{"deriv_month": 0}, kind: domain.ErrorInvalidArgument},
		{name: "chain中心0", dataset: "option_chain_snapshot", parameters: map[string]any{"option_code": "NK225op", "deriv_month": 202609, "center_strike": 0}, kind: domain.ErrorInvalidArgument},
		{name: "chain中心負数", dataset: "option_chain_snapshot", parameters: map[string]any{"option_code": "NK225op", "deriv_month": 202609, "center_strike": -1}, kind: domain.ErrorInvalidArgument},
		{name: "chain本数負数", dataset: "option_chain_snapshot", parameters: map[string]any{"option_code": "NK225op", "deriv_month": 202609, "center_strike": 69000, "strikes_each_side": -1}, kind: domain.ErrorInvalidArgument},
		{name: "chain本数超過", dataset: "option_chain_snapshot", parameters: map[string]any{"option_code": "NK225op", "deriv_month": 202609, "center_strike": 69000, "strikes_each_side": 21}, kind: domain.ErrorInvalidArgument},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{}
			collector, _ := NewCollector(client)
			_, err := collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			assertKabusControllerServiceErrorKind(t, err, testCase.kind)
			if len(client.requests) != 0 {
				t.Errorf("不正入力でAPIClientが%d回呼ばれました", len(client.requests))
			}
		})
	}
}

// ----------------------------------------

// TestCollectorNTPairResolvesAndVerifiesExplicitMonth は、NT同限月コード解決を検証します。
//
// 主な特徴:
//   - TOPIXminiと日経microへ同じ明示限月を送る
//   - SymbolNameのyy/MMをYYYYMMへ展開して両脚を検証する
//   - 2要求と銘柄登録副作用をmetadataへ記録する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorNTPairResolvesAndVerifiesExplicitMonth(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 13, 14, 0, 0, 0, time.UTC)
	client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
		"derivative_symbol_resolver": {
			{Body: map[string]any{"Symbol": "161090006", "SymbolName": "ﾐﾆTOPIX先物 26/09"}, SourceURL: "http://x/topix", StatusCode: 200, FetchedAt: fetchedAt, ResponseBytes: 10},
			{Body: map[string]any{"Symbol": "169090019", "SymbolName": "日経225micro 26/09"}, SourceURL: "http://x/nikkei", StatusCode: 200, FetchedAt: fetchedAt.Add(time.Second), ResponseBytes: 11},
		},
	}}
	collector, _ := NewCollector(client)
	result, err := collector.Collect(context.Background(), "nt_pair_symbol_resolver", map[string]any{
		"deriv_month": 202609, "nikkei_product_code": "NK225micro",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantRequests := []fakeKabusControllerAPIRequest{
		{dataset: "derivative_symbol_resolver", parameters: map[string]string{"kind": "future", "product_code": "TOPIXmini", "deriv_month": "202609"}},
		{dataset: "derivative_symbol_resolver", parameters: map[string]string{"kind": "future", "product_code": "NK225micro", "deriv_month": "202609"}},
	}
	if !reflect.DeepEqual(client.requests, wantRequests) {
		t.Errorf("APIClient要求 = %#v, %#vを期待", client.requests, wantRequests)
	}
	data := result.Data.(map[string]any)
	if data["kind"] != "nt_pair" || data["same_contract_month"] != true || data["all_contracts_verified"] != true ||
		data["usable_for_nt"] != true || data["execution_blocked"] != false {
		t.Errorf("NT結果 = %+v, 同限月の検証成功を期待", data)
	}
	contracts := data["contracts"].([]any)
	for index, contract := range contracts {
		leg := contract.(map[string]any)
		if leg["contract_month"] != 202609 || leg["contract_month_verified"] != true || leg["exchange"] != 2 {
			t.Errorf("contracts[%d] = %+v, 検証済み202609・exchange 2を期待", index, leg)
		}
	}
	if result.Metadata["upstream_requests"] != 2 || result.Metadata["read_only"] != false ||
		result.Metadata["may_register_symbol"] != true || result.Metadata["contract_month"] != 202609 {
		t.Errorf("NT metadata = %+v", result.Metadata)
	}
}

// ----------------------------------------

// TestCollectorNTPairBlocksUnverifiedContracts は、限月を確認できないNTペアを利用不可にします。
//
// 主な特徴:
//   - 片脚が別限月の場合とSymbolNameを解析できない場合を検証する
//   - 銘柄コード自体は返してもNT案への利用を明示的にブロックする
//   - 確認できなかった脚についてwarningを返す
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorNTPairBlocksUnverifiedContracts(t *testing.T) {
	testCases := []struct {
		name             string
		nikkeiSymbolName string
	}{
		{name: "片脚が別限月", nikkeiSymbolName: "日経225mini 26/12"},
		{name: "片脚の限月を解析不能", nikkeiSymbolName: "日経225mini 限月不明"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
				"derivative_symbol_resolver": {
					{Body: map[string]any{"Symbol": "161090006", "SymbolName": "ﾐﾆTOPIX先物 26/09"}, StatusCode: http.StatusOK},
					{Body: map[string]any{"Symbol": "169090018", "SymbolName": testCase.nikkeiSymbolName}, StatusCode: http.StatusOK},
				},
			}}
			collector, _ := NewCollector(client)
			result, err := collector.Collect(context.Background(), "nt_pair_symbol_resolver", map[string]any{
				"deriv_month": 202609,
			})
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			data := result.Data.(map[string]any)
			if data["same_contract_month"] != false || data["all_contracts_verified"] != false ||
				data["usable_for_nt"] != false || data["execution_blocked"] != true {
				t.Errorf("NT検証結果 = %+v, 利用不可・実行ブロックを期待", data)
			}
			warnings := data["warnings"].([]string)
			if len(warnings) != 1 || !strings.Contains(warnings[0], "日経225") {
				t.Errorf("warnings = %#v, 日経225側の限月警告1件を期待", warnings)
			}
			contracts := data["contracts"].([]any)
			if contracts[1].(map[string]any)["contract_month_verified"] != false {
				t.Errorf("日経225脚 = %+v, 限月未確認を期待", contracts[1])
			}
		})
	}
}

// ----------------------------------------

// TestCollectorNTPairRejectsMissingResolverFields は、利用不能なコード解決応答を拒否します。
//
// 主な特徴:
//   - Symbol欠落とSymbolName欠落を上流形式異常として扱う
//   - 空のNT銘柄コードを正常なペアとして公開しない
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorNTPairRejectsMissingResolverFields(t *testing.T) {
	testCases := []struct {
		name string
		body map[string]any
	}{
		{name: "Symbol欠落", body: map[string]any{"SymbolName": "日経225mini 26/09"}},
		{name: "SymbolName欠落", body: map[string]any{"Symbol": "169090018"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
				"derivative_symbol_resolver": {
					{Body: map[string]any{"Symbol": "161090006", "SymbolName": "ﾐﾆTOPIX先物 26/09"}, StatusCode: http.StatusOK},
					{Body: testCase.body, StatusCode: http.StatusOK},
				},
			}}
			collector, _ := NewCollector(client)
			_, err := collector.Collect(context.Background(), "nt_pair_symbol_resolver", map[string]any{
				"deriv_month": 202609,
			})
			assertKabusControllerServiceErrorKind(t, err, domain.ErrorUpstream)
		})
	}
}

// ----------------------------------------

// TestCollectorOptionChainJoinsRegisteredBoards は、登録済みOPチェーン合成を検証します。
//
// 主な特徴:
//   - 登録一覧と板をsymbolで結合し、中心前後のstrikeを返す
//   - CurrentPriceとBuy1・Sell1から品質フラグを生成する
//   - 未登録自動生成と建玉推測を行わないことを確認する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorOptionChainJoinsRegisteredBoards(t *testing.T) {
	const (
		selectedOldestTime = "2026-08-13T23:43:00+09:00"
		selectedLatestTime = "2026-08-13T23:43:06+09:00"
		registrationTime   = "2026-08-13T07:45:00.6275788Z"
	)
	fetchedAt := time.Date(2026, time.August, 13, 14, 43, 10, 0, time.UTC)
	registrations := make([]any, 0, 6)
	boards := make([]any, 0, 7)
	for _, strike := range []int64{68750, 69000, 69250} {
		for _, side := range []string{"C", "P"} {
			symbol := fmt.Sprintf("%d%s", strike, side)
			registrations = append(registrations, map[string]any{
				"optionCode": "NK225op", "derivMonth": json.Number("202609"),
				"strikePrice": json.Number(strconvInt(strike)), "putOrCall": side, "symbol": symbol,
			})
			if symbol == "69250P" {
				continue
			}
			board := map[string]any{
				"Symbol": symbol, "CurrentPrice": json.Number("100"),
				"Buy1":     map[string]any{"Price": json.Number("99")},
				"Sell1":    map[string]any{"Price": json.Number("101")},
				"BidPrice": json.Number("101"), "AskPrice": json.Number("99"),
				"TradingVolume": nil, "CurrentPriceTime": nil,
			}
			if symbol == "68750C" {
				board["CurrentPriceTime"] = selectedOldestTime
			}
			if symbol == "69000C" {
				board["TradingVolume"] = json.Number("100")
				board["CurrentPriceTime"] = selectedLatestTime
			}
			if symbol == "69000P" {
				board["CurrentPrice"] = nil
				board["Buy1"] = map[string]any{"Price": json.Number("0")}
				board["TradingVolume"] = json.Number("0")
				board["CurrentPriceTime"] = "時刻形式不正"
			}
			boards = append(boards, board)
		}
	}
	boards = append(boards,
		map[string]any{"Symbol": "OUTSIDE-OLD", "CurrentPriceTime": "2020-01-01T00:00:00Z"},
		map[string]any{"Symbol": "OUTSIDE-NEW", "CurrentPriceTime": "2030-01-01T00:00:00Z"},
	)
	client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
		"option_registrations": {{Body: map[string]any{
			"status": "ok",
			"data": map[string]any{
				"state": "succeeded", "updatedAt": registrationTime, "options": registrations,
			},
		}, SourceURL: "http://x/registrations", StatusCode: 200}},
		"option_market_data": {{
			Body: map[string]any{"data": boards}, SourceURL: "http://x/boards", StatusCode: 200, FetchedAt: fetchedAt,
		}},
	}}
	collector, _ := NewCollector(client)
	result, err := collector.Collect(context.Background(), "option_chain_snapshot", map[string]any{
		"option_code": "NK225op", "deriv_month": 202609, "center_strike": 69000, "strikes_each_side": 1,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantRequests := []fakeKabusControllerAPIRequest{
		{dataset: "option_registrations", parameters: map[string]string{}},
		{dataset: "option_market_data", parameters: map[string]string{}},
	}
	if !reflect.DeepEqual(client.requests, wantRequests) {
		t.Errorf("APIClient要求 = %#v, %#vを期待", client.requests, wantRequests)
	}
	data := result.Data.(map[string]any)
	rows := data["strikes"].([]any)
	if data["center_strike"] != int64(69000) || len(rows) != 3 || data["open_interest_available"] != false ||
		data["automatic_registration"] != false || data["automatic_unregistration"] != false {
		t.Errorf("option chain = %+v", data)
	}
	volumeAvailability := data["volume_availability"].(map[string]any)
	quoteTimeAvailability := data["quote_time_availability"].(map[string]any)
	if volumeAvailability["registered_contract_count"] != 6 || volumeAvailability["board_contract_count"] != 5 ||
		volumeAvailability["available_contract_count"] != 1 || volumeAvailability["complete"] != false ||
		quoteTimeAvailability["registered_contract_count"] != 6 || quoteTimeAvailability["board_contract_count"] != 5 ||
		quoteTimeAvailability["available_contract_count"] != 2 || quoteTimeAvailability["complete"] != false {
		t.Errorf("OP時刻・出来高availability = volume:%+v time:%+v", volumeAvailability, quoteTimeAvailability)
	}
	registrationSnapshot := data["registration_snapshot"].(map[string]any)
	if registrationSnapshot["status"] != "ok" || registrationSnapshot["state"] != "succeeded" ||
		registrationSnapshot["updated_at"] != registrationTime {
		t.Errorf("registration_snapshot = %+v, 登録成功状態と更新時刻を期待", registrationSnapshot)
	}
	call := rows[1].(map[string]any)["call"].(map[string]any)
	if call["has_board"] != true || call["has_current_price"] != true ||
		call["has_buy_quote"] != true || call["has_sell_quote"] != true || call["has_two_sided_quote"] != true {
		t.Errorf("Call品質 = %+v, 現在値・両側気配ありを期待", call)
	}
	put := rows[1].(map[string]any)["put"].(map[string]any)
	if put["has_board"] != true || put["has_current_price"] != false ||
		put["has_buy_quote"] != false || put["has_sell_quote"] != true || put["has_two_sided_quote"] != false {
		t.Errorf("Put品質 = %+v, 現在値・買気配なし、売気配ありを期待", put)
	}
	missing := data["missing"].([]any)
	if len(missing) != 1 || missing[0].(map[string]any)["reason"] != "board_missing" {
		t.Errorf("missing = %+v, 69250Pの板欠損1件を期待", missing)
	}
	if result.Metadata["upstream_requests"] != 2 || result.Metadata["read_only"] != true || result.Metadata["bid_ask_warning"] == nil {
		t.Errorf("option chain metadata = %+v", result.Metadata)
	}
	parsedSelectedOldest, _ := time.Parse(time.RFC3339, selectedOldestTime)
	parsedSelectedLatest, _ := time.Parse(time.RFC3339, selectedLatestTime)
	if result.Metadata["source_at"] != parsedSelectedOldest || result.Metadata["source_at_latest"] != parsedSelectedLatest ||
		result.Metadata["age_seconds"] != float64(10) || result.Metadata["source_time_parsed_count"] != 2 ||
		result.Metadata["source_time_missing_or_unparseable_count"] != 3 {
		t.Errorf("option chain鮮度metadata = %+v, 選択チェーン内の時刻だけによる集計を期待", result.Metadata)
	}
}

// ----------------------------------------

// TestCollectorAPICapacityReturnsOnlyAnUpperBound は、登録容量合成の不確実性を検証します。
//
// 主な特徴:
//   - ソフトリミット、先物登録、OP登録を3要求で取得する
//   - controller既知件数から残枠上限を計算し、確定値ではないと明示する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorAPICapacityReturnsOnlyAnUpperBound(t *testing.T) {
	client := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
		"kabus_api_soft_limits": {{Body: map[string]any{"FutureMini": json.Number("200")}, SourceURL: "http://x/limits", StatusCode: 200}},
		"future_registrations": {{Body: map[string]any{"status": "ok", "data": map[string]any{
			"state": "succeeded", "futures": []any{
				map[string]any{"symbol": "F1"}, map[string]any{"symbol": "F2"}, map[string]any{"symbol": "F2"},
			},
		}}, SourceURL: "http://x/futures", StatusCode: 200}},
		"option_registrations": {{Body: map[string]any{"status": "ok", "data": map[string]any{
			"state": "succeeded", "options": []any{
				map[string]any{"symbol": "O1"}, map[string]any{"symbol": "O2"}, map[string]any{"symbol": "F1"}, map[string]any{},
			},
		}}, SourceURL: "http://x/options", StatusCode: 200}},
	}}
	collector, _ := NewCollector(client)
	result, err := collector.Collect(context.Background(), "kabus_api_capacity", map[string]any{})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	data := result.Data.(map[string]any)
	if data["registration_limit"] != 50 || data["controller_known_future_count"] != 3 ||
		data["controller_known_option_count"] != 4 || data["controller_known_count"] != 7 ||
		data["controller_known_unique_symbol_count"] != 4 ||
		data["controller_registration_missing_symbol_count"] != 1 ||
		data["controller_registration_duplicate_count"] != 2 ||
		data["remaining_upper_bound"] != 46 || data["remaining_is_exact"] != false ||
		data["other_clients_or_stock_registrations_unknown"] != true ||
		data["shared_limit_membership_verified"] != false || data["calculation_assumption"] == "" {
		t.Errorf("capacity = %+v, raw既知7件・一意4件・残枠上限46を期待", data)
	}
	if result.Metadata["upstream_requests"] != 3 || len(result.Metadata["source_urls"].([]string)) != 3 {
		t.Errorf("capacity metadata = %+v", result.Metadata)
	}
}

// ----------------------------------------

// TestCollectorRejectsUnsuccessfulRegistrationStates は、未完了登録一覧を合成に使用しません。
//
// 主な特徴:
//   - root statusがok以外の場合を上流形式異常として扱う
//   - data.stateがsucceeded以外の場合を上流形式異常として扱う
//   - OPチェーンと登録容量の双方で不完全な一覧を正常値にしない
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorRejectsUnsuccessfulRegistrationStates(t *testing.T) {
	invalidStatus := map[string]any{
		"status": "error", "data": map[string]any{"state": "succeeded", "options": []any{}},
	}
	invalidState := map[string]any{
		"status": "ok", "data": map[string]any{"state": "pending", "options": []any{}},
	}
	validFutures := map[string]any{
		"status": "ok", "data": map[string]any{"state": "succeeded", "futures": []any{}},
	}
	validOptions := map[string]any{
		"status": "ok", "data": map[string]any{"state": "succeeded", "options": []any{}},
	}
	testCases := []struct {
		name      string
		dataset   string
		responses map[string][]APIResponse
	}{
		{
			name: "OPチェーンのstatus異常", dataset: "option_chain_snapshot",
			responses: map[string][]APIResponse{
				"option_registrations": {{Body: invalidStatus, StatusCode: http.StatusOK}},
				"option_market_data":   {{Body: map[string]any{"data": []any{}}, StatusCode: http.StatusOK}},
			},
		},
		{
			name: "OPチェーンのstate未完了", dataset: "option_chain_snapshot",
			responses: map[string][]APIResponse{
				"option_registrations": {{Body: invalidState, StatusCode: http.StatusOK}},
				"option_market_data":   {{Body: map[string]any{"data": []any{}}, StatusCode: http.StatusOK}},
			},
		},
		{
			name: "登録容量のstatus異常", dataset: "kabus_api_capacity",
			responses: map[string][]APIResponse{
				"kabus_api_soft_limits": {{Body: map[string]any{}, StatusCode: http.StatusOK}},
				"future_registrations":  {{Body: map[string]any{"status": "error", "data": map[string]any{"state": "succeeded", "futures": []any{}}}, StatusCode: http.StatusOK}},
				"option_registrations":  {{Body: validOptions, StatusCode: http.StatusOK}},
			},
		},
		{
			name: "登録容量のstate未完了", dataset: "kabus_api_capacity",
			responses: map[string][]APIResponse{
				"kabus_api_soft_limits": {{Body: map[string]any{}, StatusCode: http.StatusOK}},
				"future_registrations":  {{Body: validFutures, StatusCode: http.StatusOK}},
				"option_registrations":  {{Body: map[string]any{"status": "ok", "data": map[string]any{"state": "pending", "options": []any{}}}, StatusCode: http.StatusOK}},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{responses: testCase.responses}
			collector, _ := NewCollector(client)
			parameters := map[string]any{}
			if testCase.dataset == "option_chain_snapshot" {
				parameters = map[string]any{
					"option_code": "NK225op", "deriv_month": 202609, "center_strike": 69000,
				}
			}
			_, err := collector.Collect(context.Background(), testCase.dataset, parameters)
			assertKabusControllerServiceErrorKind(t, err, domain.ErrorUpstream)
		})
	}
}

// ----------------------------------------

// TestCollectorClassifiesClientAndCompositeErrors は、上流失敗の共通分類を検証します。
//
// 主な特徴:
//   - 銘柄404、一覧404、429、期限、複合形式異常を安定分類へ変換する
//   - 元エラーをerror chainへ保持する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
func TestCollectorClassifiesClientAndCompositeErrors(t *testing.T) {
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
		err        error
		kind       domain.ErrorKind
	}{
		{name: "銘柄404", dataset: "kabus_primary_exchange", parameters: map[string]any{"symbol": "4419"}, err: &APIError{StatusCode: 404}, kind: domain.ErrorNotFound},
		{name: "一覧404", dataset: "future_registrations", parameters: map[string]any{}, err: &APIError{StatusCode: 404}, kind: domain.ErrorUpstream},
		{name: "混雑", dataset: "kabus_ranking", parameters: map[string]any{"ranking_type": "2"}, err: &APIError{StatusCode: 429}, kind: domain.ErrorProviderUnavailable},
		{name: "期限", dataset: "market_data", parameters: map[string]any{}, err: context.DeadlineExceeded, kind: domain.ErrorTimeout},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeKabusControllerAPIClient{errors: map[string][]error{testCase.dataset: {testCase.err}}}
			collector, _ := NewCollector(client)
			_, err := collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			assertKabusControllerServiceErrorKind(t, err, testCase.kind)
			if !errors.Is(err, testCase.err) {
				t.Errorf("Collect() error = %v, 元エラー %vを保持することを期待", err, testCase.err)
			}
		})
	}

	formatClient := &fakeKabusControllerAPIClient{responses: map[string][]APIResponse{
		"option_registrations": {{Body: map[string]any{"data": "invalid"}, StatusCode: 200}},
		"option_market_data":   {{Body: map[string]any{"data": []any{}}, StatusCode: 200}},
	}}
	collector, _ := NewCollector(formatClient)
	_, err := collector.Collect(context.Background(), "option_chain_snapshot", map[string]any{
		"option_code": "NK225op", "deriv_month": 202609, "center_strike": 69000,
	})
	assertKabusControllerServiceErrorKind(t, err, domain.ErrorUpstream)

	childError := &APIError{StatusCode: http.StatusNotFound}
	childClient := &fakeKabusControllerAPIClient{errors: map[string][]error{
		"option_registrations": {childError},
	}}
	collector, _ = NewCollector(childClient)
	_, err = collector.Collect(context.Background(), "option_chain_snapshot", map[string]any{
		"option_code": "NK225op", "deriv_month": 202609, "center_strike": 69000,
	})
	assertKabusControllerServiceErrorKind(t, err, domain.ErrorUpstream)
}

// ----------------------------------------

// TestNewCollectorRejectsNilAndTypedNilClient は、Collector依存関係のnil検証を確認します。
//
// 主な特徴:
//   - nil interfaceと型付きnilポインターを起動時に拒否する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//
// 返り値:
//   - なし
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

// assertKabusControllerServiceErrorKind は、共通エラー分類を検証します。
//
// 主な特徴:
//   - error chainからServiceErrorを取り出して期待分類と比較する
//
// 引数:
//   - t *testing.T: テスト状態を管理する値
//   - err error: Collectorが返したエラー
//   - want domain.ErrorKind: 期待する共通分類
//
// 返り値:
//   - なし
func assertKabusControllerServiceErrorKind(t *testing.T, err error, want domain.ErrorKind) {
	t.Helper()
	var serviceError *domain.ServiceError
	if !errors.As(err, &serviceError) || serviceError.Kind != want {
		t.Fatalf("error = %v, %sのServiceErrorを期待", err, want)
	}
}

// ----------------------------------------

// strconvInt は、テスト用int64を10進文字列へ変換します。
//
// 主な特徴:
//   - fmtの既定10進表現をjson.Number生成へ利用する
//
// 引数:
//   - value int64: 変換する整数
//
// 返り値:
//   - string: 10進整数文字列
func strconvInt(value int64) string {
	return fmt.Sprintf("%d", value)
}
