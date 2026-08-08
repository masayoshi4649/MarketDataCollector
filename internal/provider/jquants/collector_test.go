package jquants

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type fakeAPIClient struct {
	response APIResponse
	err      error
	requests []fakeAPIRequest
}

type fakeAPIRequest struct {
	dataset string
	query   map[string]string
}

type manualPacerWait struct {
	interval time.Duration
	release  chan struct{}
}

type manualPacerClock struct {
	mu      sync.Mutex
	current time.Time
	waits   chan manualPacerWait
}

type pacerResult struct {
	id  int
	err error
}

/*
Fetch は、collectorから受けた要求を記録して設定済み結果を返します。

機能:
  - datasetとqueryを複製してテスト検証用に保持する
  - HTTP通信を行わず設定済みのAPIResponseまたはerrorを返す

引数:
  - ctx context.Context: collectorから渡された要求context
  - dataset string: 固定許可リストのdataset識別子
  - query map[string]string: 上流名へ変換済みのquery

返り値:
  - APIResponse: テストで設定した応答
  - error: テストで設定したエラー
*/
func (f *fakeAPIClient) Fetch(
	ctx context.Context,
	dataset string,
	query map[string]string,
) (APIResponse, error) {
	_ = ctx
	cloned := make(map[string]string, len(query))
	for key, value := range query {
		cloned[key] = value
	}
	f.requests = append(f.requests, fakeAPIRequest{dataset: dataset, query: cloned})
	return f.response, f.err
}

// ----------------------------------------

/*
newManualPacerClock は、実時間を待たずにレートキューを検証する手動clockを生成します。

機能:
  - 固定初期時刻と待機通知channelを初期化する
  - テストが明示的に解放するまでclockを進めない

引数:
  - なし

返り値:
  - *manualPacerClock: pacingStateへ注入できる手動clock
*/
func newManualPacerClock() *manualPacerClock {
	return &manualPacerClock{
		current: time.Date(2026, time.August, 8, 0, 0, 0, 0, time.UTC),
		waits:   make(chan manualPacerWait, 16),
	}
}

// ----------------------------------------

/*
Now は、手動clockの現在時刻を返します。

機能:
  - mutex下で現在時刻を読み取り、キューgoroutineとテストの競合を防ぐ

引数:
  - なし

返り値:
  - time.Time: 手動clockの現在時刻
*/
func (c *manualPacerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// ----------------------------------------

/*
Wait は、テストに待機間隔を通知し、明示解放またはキュー変更まで待ちます。

機能:
  - 解放時だけ指定間隔分の手動clockを進める
  - キャンセルによるキュー変更時はclockを進めずfalseを返す

引数:
  - interval time.Duration: キューが要求した待機間隔
  - queueChanged <-chan struct{}: キュー変更通知を受け取るchannel

返り値:
  - bool: テストが待機を解放した場合はtrue、キュー変更の場合はfalse
*/
func (c *manualPacerClock) Wait(
	interval time.Duration,
	queueChanged <-chan struct{},
) bool {
	release := make(chan struct{})
	c.waits <- manualPacerWait{interval: interval, release: release}
	select {
	case <-release:
		c.mu.Lock()
		c.current = c.current.Add(interval)
		c.mu.Unlock()
		return true
	case <-queueChanged:
		return false
	}
}

// ----------------------------------------

/*
TestDescriptorFiltersDatasetsByContract は、契約別の公開dataset数を検証します。

機能:
  - Free、Light、Standard、Premiumの段階的なAPI公開範囲を確認する
  - Standardが17 APIとBulk 2 APIの19件を公開することを確認する
  - Add-onが基本プランへ独立して追加されることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestDescriptorFiltersDatasetsByContract(t *testing.T) {
	testCases := []struct {
		name    string
		options Options
		count   int
	}{
		{name: "free", options: Options{Plan: "free"}, count: 6},
		{name: "light", options: Options{Plan: "light"}, count: 10},
		{name: "standard", options: Options{Plan: "standard"}, count: 19},
		{name: "premium", options: Options{Plan: "premium"}, count: 25},
		{name: "standard-minute", options: Options{Plan: "standard", Addons: []string{"minute"}}, count: 21},
		{name: "standard-tdnet", options: Options{Plan: "standard", Addons: []string{"tdnet"}}, count: 22},
		{name: "premium-all", options: Options{Plan: "premium", Addons: []string{"minute", "tdnet"}}, count: 30},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			collector, err := NewCollector(&fakeAPIClient{}, testCase.options)
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			descriptor := collector.Descriptor()
			if descriptor.Name != "jquants" || len(descriptor.Datasets) != testCase.count {
				t.Fatalf("Descriptor() = %+v, dataset %d件を期待", descriptor, testCase.count)
			}
		})
	}
}

// ----------------------------------------

/*
TestStandardDescriptorExcludesUnavailableFeatures は、Standardの非公開範囲を検証します。

機能:
  - Premium専用APIと未契約Add-onをDescriptorから除外する
  - fins_summaryのPremium限定cursorをquery一覧から除外する
  - Bulk endpointのAllowedからPremium専用pathを除外する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestStandardDescriptorExcludesUnavailableFeatures(t *testing.T) {
	client := &fakeAPIClient{}
	collector, err := NewCollector(client, Options{Plan: "standard"})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	descriptor := collector.Descriptor()
	if hasDatasetDescriptor(descriptor, "fins_details") ||
		hasDatasetDescriptor(descriptor, "equities_bars_minute") ||
		hasDatasetDescriptor(descriptor, "td_list") {
		t.Fatalf("Standard Descriptor() = %+v, Premium・Add-on datasetの除外を期待", descriptor)
	}
	if !hasDatasetDescriptor(descriptor, "edinet_major_shareholders") ||
		!hasDatasetDescriptor(descriptor, "bulk_get") {
		t.Fatalf("Standard Descriptor() = %+v, Standard APIとBulk APIを期待", descriptor)
	}

	fins := findDatasetDescriptor(t, descriptor, "fins_summary")
	if hasParameterDescriptor(fins, "cursor") {
		t.Errorf("fins_summary parameters = %+v, Standardでcursorの除外を期待", fins.Parameters)
	}
	bulk := findDatasetDescriptor(t, descriptor, "bulk_list")
	endpoint := findParameterDescriptor(t, bulk, "endpoint")
	if containsString(endpoint.Allowed, "/fins/details") || !containsString(endpoint.Allowed, "/equities/bars/daily") {
		t.Errorf("bulk endpoint Allowed = %+v, Standardの固定許可範囲と不一致", endpoint.Allowed)
	}
	_, err = collector.Collect(context.Background(), "fins_details", map[string]any{"code": "86970"})
	assertServiceErrorKind(t, err, domain.ErrorNotFound)
	if len(client.requests) != 0 {
		t.Errorf("非公開datasetでAPIClientが%d回呼ばれました", len(client.requests))
	}
}

// ----------------------------------------

/*
TestNewCollectorRejectsInvalidDependenciesAndOptions は、constructor入力を検証します。

機能:
  - nilと型付きnilのAPIClientを拒否する
  - 未対応plan、重複Add-on、FreeプランのAdd-onを拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewCollectorRejectsInvalidDependenciesAndOptions(t *testing.T) {
	var typedNil *fakeAPIClient
	testCases := []struct {
		name    string
		client  APIClient
		options Options
	}{
		{name: "nil-client", client: nil},
		{name: "typed-nil-client", client: typedNil},
		{name: "unknown-plan", client: &fakeAPIClient{}, options: Options{Plan: "enterprise"}},
		{name: "duplicate-addon", client: &fakeAPIClient{}, options: Options{Addons: []string{"minute", "minute"}}},
		{name: "free-addon", client: &fakeAPIClient{}, options: Options{Plan: "free", Addons: []string{"tdnet"}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewCollector(testCase.client, testCase.options); err == nil {
				t.Fatal("NewCollector() error = nil, 不正入力の拒否を期待")
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectReturnsOnePageAndSafeMetadata は、正常収集契約を検証します。

機能:
  - pagination_keyを含む応答でも自動後続取得を行わない
  - APIResponse全体をDataへ保持する
  - source、仕様日、plan、HTTP状態をmetadataへ付与しAPIキーを含めない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectReturnsOnePageAndSafeMetadata(t *testing.T) {
	body := map[string]any{
		"data":           []any{map[string]any{"Code": "86970"}},
		"pagination_key": "next-page",
	}
	client := &fakeAPIClient{response: APIResponse{
		Body: body, SourceURL: "https://api.jquants.com/v2/equities/bars/daily?code=86970", StatusCode: 200,
	}}
	collector, err := NewCollector(client, Options{Plan: "standard"})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	collector.pacing = nil
	result, err := collector.Collect(context.Background(), "equities_bars_daily", map[string]any{"code": "86970"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(client.requests) != 1 || client.requests[0].dataset != "equities_bars_daily" {
		t.Fatalf("APIClient要求 = %+v, 1ページの1要求を期待", client.requests)
	}
	if !reflect.DeepEqual(result.Data, body) {
		t.Errorf("Data = %+v, APIResponse全体を期待", result.Data)
	}
	if result.Metadata["source_name"] != "J-Quants API" ||
		result.Metadata["api_version"] != "v2" ||
		result.Metadata["plan"] != "standard" ||
		result.Metadata["upstream_status"] != 200 ||
		result.Metadata["read_only"] != true || result.Metadata["on_demand"] != true {
		t.Errorf("Metadata = %+v, J-Quants固定metadataを期待", result.Metadata)
	}
	if _, exists := result.Metadata["api_key"]; exists {
		t.Errorf("Metadata = %+v, APIキーを含めることはできません", result.Metadata)
	}
	if result.Metadata["specification_reviewed_date"] != SpecificationReviewedDate ||
		result.Metadata["specification_release_date"] != SpecificationReleaseDate {
		t.Errorf("Metadata = %+v, 仕様確認日を期待", result.Metadata)
	}
}

// ----------------------------------------

/*
TestCollectTranslatesTDNetAndTradeQueries は、特殊query変換を検証します。

機能:
  - disc_noをdiscNoへ変換する
  - disc_itemsをdiscItemsへ変換する
  - equities_tradesをBulk一覧へ変換しendpointを固定する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectTranslatesTDNetAndTradeQueries(t *testing.T) {
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
		want       map[string]string
	}{
		{
			name: "td-files", dataset: "td_files",
			parameters: map[string]any{"disc_no": "20260808001", "docs": "g,s"},
			want:       map[string]string{"discNo": "20260808001", "docs": "g,s"},
		},
		{
			name: "td-list", dataset: "td_list",
			parameters: map[string]any{"date": "2026-08-08", "disc_items": "111,222"},
			want:       map[string]string{"date": "2026-08-08", "discItems": "111,222"},
		},
		{
			name: "equities-trades", dataset: "equities_trades",
			parameters: map[string]any{"date": "2026-08-08"},
			want: map[string]string{
				"from": "2026-08-08", "to": "2026-08-08", "endpoint": "/equities/trades",
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeAPIClient{response: APIResponse{Body: map[string]any{}, StatusCode: 200}}
			collector, err := NewCollector(client, Options{
				Plan: "standard", Addons: []string{"minute", "tdnet"},
			})
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			collector.pacing = nil
			if _, err := collector.Collect(context.Background(), testCase.dataset, testCase.parameters); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(client.requests) != 1 || !reflect.DeepEqual(client.requests[0].query, testCase.want) {
				t.Errorf("APIClient query = %+v, 期待値は%+v", client.requests, testCase.want)
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectorRejectsInvalidQueriesBeforeFetch は、queryの厳密検証を確認します。

機能:
  - 未知項目、string以外、空文字、必須不足を拒否する
  - Bulk、cursor、TDnet、EDINETの日付・排他条件を拒否する
  - 不正時にAPIClientを呼び出さないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorRejectsInvalidQueriesBeforeFetch(t *testing.T) {
	testCases := []struct {
		name       string
		plan       string
		addons     []string
		dataset    string
		parameters map[string]any
	}{
		{name: "unknown", plan: "standard", dataset: "equities_master", parameters: map[string]any{"unknown": "x"}},
		{name: "non-string", plan: "standard", dataset: "equities_master", parameters: map[string]any{"code": 86970}},
		{name: "empty", plan: "standard", dataset: "equities_master", parameters: map[string]any{"code": "  "}},
		{name: "daily-required", plan: "standard", dataset: "equities_bars_daily", parameters: map[string]any{}},
		{name: "daily-range-needs-code", plan: "standard", dataset: "equities_bars_daily", parameters: map[string]any{"date": "2026-08-08", "from": "2026-08-01"}},
		{name: "earnings-exactly-one", plan: "standard", dataset: "fins_earnings_date", parameters: map[string]any{"code": "86970", "date": "2026-08-08"}},
		{name: "standard-cursor", plan: "standard", dataset: "fins_summary", parameters: map[string]any{"date": "2026-08-08", "cursor": "x"}},
		{name: "premium-cursor-needs-date", plan: "premium", dataset: "fins_summary", parameters: map[string]any{"code": "86970", "cursor": "x"}},
		{name: "premium-cursor-code-exclusive", plan: "premium", dataset: "fins_summary", parameters: map[string]any{"code": "86970", "date": "2026-08-08", "cursor": "x"}},
		{name: "premium-cursor-not-today", plan: "premium", dataset: "fins_summary", parameters: map[string]any{"date": "2026-08-07", "cursor": "x"}},
		{name: "premium-cursor-invalid-date", plan: "premium", dataset: "fins_details", parameters: map[string]any{"date": "2026/08/08", "cursor": "x"}},
		{name: "pagination-cursor", plan: "premium", dataset: "fins_details", parameters: map[string]any{"date": "2026-08-08", "cursor": "x", "pagination_key": "y"}},
		{name: "edinet-exclusive", plan: "standard", dataset: "edinet_major_shareholders", parameters: map[string]any{"edinet_code": "E00001", "code": "86970"}},
		{name: "bulk-list-exclusive", plan: "standard", dataset: "bulk_list", parameters: map[string]any{"endpoint": "/equities/bars/daily", "date": "2026-08-08"}},
		{name: "bulk-list-range-needs-endpoint", plan: "standard", dataset: "bulk_list", parameters: map[string]any{"date": "2026-08-08", "from": "2026-08-01"}},
		{name: "bulk-get-partial", plan: "standard", dataset: "bulk_get", parameters: map[string]any{"endpoint": "/equities/bars/daily"}},
		{name: "bulk-get-mixed", plan: "standard", dataset: "bulk_get", parameters: map[string]any{"key": "file", "date": "2026-08-08"}},
		{name: "bulk-premium-endpoint", plan: "standard", dataset: "bulk_list", parameters: map[string]any{"endpoint": "/fins/details"}},
		{name: "td-date-code", plan: "standard", addons: []string{"tdnet"}, dataset: "td_list", parameters: map[string]any{"date": "2026-08-08", "code": "86970"}},
		{name: "td-range-pair", plan: "standard", addons: []string{"tdnet"}, dataset: "td_list", parameters: map[string]any{"code": "86970", "from": "2026-08-01"}},
		{name: "td-cursor-date", plan: "standard", addons: []string{"tdnet"}, dataset: "td_list", parameters: map[string]any{"code": "86970", "cursor": "x"}},
		{name: "td-cursor-not-today", plan: "standard", addons: []string{"tdnet"}, dataset: "td_list", parameters: map[string]any{"date": "2026-08-07", "cursor": "x"}},
		{name: "derivative-date", plan: "standard", dataset: "derivatives_bars_daily_options_225", parameters: map[string]any{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeAPIClient{}
			collector, err := NewCollector(client, Options{Plan: testCase.plan, Addons: testCase.addons})
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			collector.now = func() time.Time {
				return time.Date(2026, time.August, 7, 15, 30, 0, 0, time.UTC)
			}
			collector.pacing = nil
			_, err = collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			assertServiceErrorKind(t, err, domain.ErrorInvalidArgument)
			if len(client.requests) != 0 {
				t.Errorf("不正queryでAPIClientが%d回呼ばれました", len(client.requests))
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectorAcceptsValidSpecialQueries は、特殊条件の正常系を検証します。

機能:
  - Bulk keyとendpoint/dateの両形式を受け付ける
  - Premium財務cursorとTDnet期間queryを受け付ける
  - EDINETコード単独と空売り公表日を受け付ける

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorAcceptsValidSpecialQueries(t *testing.T) {
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
	}{
		{name: "bulk-key", dataset: "bulk_get", parameters: map[string]any{"key": "file-key"}},
		{name: "bulk-pair", dataset: "bulk_get", parameters: map[string]any{"endpoint": "/equities/bars/daily", "date": "2026-08-08"}},
		{name: "fins-cursor-compact-date", dataset: "fins_summary", parameters: map[string]any{"date": "20260808", "cursor": "next"}},
		{name: "td-range", dataset: "td_list", parameters: map[string]any{"code": "86970", "from": "2026-08-01", "to": "2026-08-08"}},
		{name: "td-cursor", dataset: "td_list", parameters: map[string]any{"date": "2026-08-08", "cursor": "next"}},
		{name: "edinet-code", dataset: "edinet_major_shareholders", parameters: map[string]any{"edinet_code": "E00001"}},
		{name: "short-sale-date", dataset: "markets_short_sale_report", parameters: map[string]any{"disc_date": "2026-08-08"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeAPIClient{response: APIResponse{Body: map[string]any{}, StatusCode: 200}}
			collector, err := NewCollector(client, Options{
				Plan: "premium", Addons: []string{"minute", "tdnet"},
			})
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			collector.now = func() time.Time {
				return time.Date(2026, time.August, 7, 15, 30, 0, 0, time.UTC)
			}
			collector.pacing = nil
			if _, err := collector.Collect(context.Background(), testCase.dataset, testCase.parameters); err != nil {
				t.Errorf("Collect() error = %v, 正常queryを期待", err)
			}
			if len(client.requests) != 1 {
				t.Errorf("APIClient呼び出し回数 = %d, 1を期待", len(client.requests))
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectorClassifiesAPIStatus は、J-Quants HTTP状態の共通分類を検証します。

機能:
  - 400、403、429、5xxを指定されたServiceErrorへ変換する
  - context deadlineを再分類せずserviceへ渡す

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorClassifiesAPIStatus(t *testing.T) {
	testCases := []struct {
		status int
		kind   domain.ErrorKind
	}{
		{status: 400, kind: domain.ErrorInvalidArgument},
		{status: 403, kind: domain.ErrorProviderUnavailable},
		{status: 429, kind: domain.ErrorProviderUnavailable},
		{status: 500, kind: domain.ErrorUpstream},
		{status: 302, kind: domain.ErrorUpstream},
	}
	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("status-%d", testCase.status), func(t *testing.T) {
			client := &fakeAPIClient{err: &APIError{StatusCode: testCase.status, Endpoint: "/v2/equities/master"}}
			collector, err := NewCollector(client, Options{Plan: "standard"})
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			collector.pacing = nil
			_, err = collector.Collect(context.Background(), "equities_master", nil)
			assertServiceErrorKind(t, err, testCase.kind)
		})
	}

	deadlineErr := classifyAPIError(endpointSpecs[0], context.DeadlineExceeded)
	if !errors.Is(deadlineErr, context.DeadlineExceeded) {
		t.Errorf("deadline error = %v, context.DeadlineExceededを期待", deadlineErr)
	}
}

// ----------------------------------------

/*
TestCollectorUsesMinuteQuotaForBulkAddonData は、Bulk経由の株価Add-on取得に専用枠を適用することを検証します。

機能:
  - endpoint条件とkey条件の分足・ティック要求をminute枠へ分類する
  - 通常のBulk要求は基本プラン枠へ分類する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorUsesMinuteQuotaForBulkAddonData(t *testing.T) {
	collector, err := NewCollector(
		&fakeAPIClient{},
		Options{Plan: "premium", Addons: []string{"minute"}},
	)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	testCases := []struct {
		name           string
		dataset        string
		query          map[string]string
		minuteExpected bool
	}{
		{
			name: "分足endpoint", dataset: "bulk_list",
			query: map[string]string{"endpoint": "/equities/bars/minute"}, minuteExpected: true,
		},
		{
			name: "ティックendpoint", dataset: "bulk_list",
			query: map[string]string{"endpoint": "/equities/trades"}, minuteExpected: true,
		},
		{
			name: "ティックkey", dataset: "bulk_get",
			query: map[string]string{"key": "equities/trades/historical/2026/file.csv.gz"}, minuteExpected: true,
		},
		{
			name: "通常endpoint", dataset: "bulk_list",
			query: map[string]string{"endpoint": "/equities/bars/daily"}, minuteExpected: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := collector.endpoints[testCase.dataset]
			quotas := collector.requestQuotas(spec, testCase.query)
			minuteUsed := len(quotas) == 1 && quotas[0] == collector.pacing.minute
			baseUsed := len(quotas) == 1 && quotas[0] == collector.pacing.base
			if minuteUsed != testCase.minuteExpected || baseUsed == testCase.minuteExpected {
				t.Errorf(
					"quota選択 = (minute=%t, base=%t), minuteExpected=%t",
					minuteUsed,
					baseUsed,
					testCase.minuteExpected,
				)
			}
		})
	}
}

// ----------------------------------------

/*
TestCollectorSelectsRequiredRateQuotas は、endpoint種別ごとに必要な独立quotaが選択されることを検証します。

機能:
  - 通常APIにbase quotaだけが適用されることを確認する
  - 財務APIにbaseとfinsの二重quotaが適用されることを確認する
  - 分足とTDnetに独立Add-on quotaだけが適用されることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorSelectsRequiredRateQuotas(t *testing.T) {
	collector, err := NewCollector(
		&fakeAPIClient{},
		Options{Plan: "premium", Addons: []string{"minute", "tdnet"}},
	)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	testCases := []struct {
		name    string
		dataset string
		query   map[string]string
		want    []*rateQuota
	}{
		{name: "通常", dataset: "equities_master", want: []*rateQuota{collector.pacing.base}},
		{
			name: "財務", dataset: "fins_summary",
			want: []*rateQuota{collector.pacing.base, collector.pacing.fins},
		},
		{name: "分足", dataset: "equities_bars_minute", want: []*rateQuota{collector.pacing.minute}},
		{name: "TDnet", dataset: "td_list", want: []*rateQuota{collector.pacing.tdnet}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual := collector.requestQuotas(collector.endpoints[testCase.dataset], testCase.query)
			if len(actual) != len(testCase.want) {
				t.Fatalf("quota数 = %d, %dを期待", len(actual), len(testCase.want))
			}
			for index := range actual {
				if actual[index] != testCase.want[index] {
					t.Fatalf("quota[%d] = %p, %pを期待", index, actual[index], testCase.want[index])
				}
			}
		})
	}
}

// ----------------------------------------

/*
TestPacingStateUsesHalfOfOfficialRate は、全レート枠が公式上限の50%で設定されることを検証します。

機能:
  - Freeの2.5要求/分を24秒間隔で正確に表現する
  - Light、Standard、Premiumの基本間隔を検証する
  - 財務、分足・ティック、TDnetの独立間隔を検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestPacingStateUsesHalfOfOfficialRate(t *testing.T) {
	testCases := []struct {
		plan         Plan
		baseInterval time.Duration
	}{
		{plan: PlanFree, baseInterval: 24 * time.Second},
		{plan: PlanLight, baseInterval: 2 * time.Second},
		{plan: PlanStandard, baseInterval: time.Second},
		{plan: PlanPremium, baseInterval: 240 * time.Millisecond},
	}
	for _, testCase := range testCases {
		t.Run(string(testCase.plan), func(t *testing.T) {
			state := newPacingState(testCase.plan)
			if state.base.interval != testCase.baseInterval {
				t.Errorf("base.interval = %s, %sを期待", state.base.interval, testCase.baseInterval)
			}
			if state.fins.interval != 2*time.Second {
				t.Errorf("fins.interval = %s, 2sを期待", state.fins.interval)
			}
			if state.minute.interval != 2*time.Second {
				t.Errorf("minute.interval = %s, 2sを期待", state.minute.interval)
			}
			if state.tdnet.interval != 1200*time.Millisecond {
				t.Errorf("tdnet.interval = %s, 1.2sを期待", state.tdnet.interval)
			}
		})
	}
}

// ----------------------------------------

/*
TestPacingQueuePreservesFIFOOrderAndInterval は、並行要求の到着順と均等間隔を検証します。

機能:
  - 待機者を1件ずつ確実にキューへ登録して到着順を固定する
  - 手動clockを1スロットずつ進めてFIFO順の解放を確認する
  - キューが要求する全待機間隔が設定値と一致することを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestPacingQueuePreservesFIFOOrderAndInterval(t *testing.T) {
	const interval = time.Minute
	clock := newManualPacerClock()
	pacing := newPacingState(PlanStandard)
	pacing.base = newRateQuota(1, interval)
	pacing.tdnet = newRateQuota(1, interval)
	pacing.now = clock.Now
	pacing.waitInterval = clock.Wait

	_, err := pacing.Execute(
		context.Background(),
		[]*rateQuota{pacing.base},
		func(context.Context) (APIResponse, error) { return APIResponse{}, nil },
	)
	if err != nil {
		t.Fatalf("初回Execute() error = %v", err)
	}

	results := make(chan pacerResult, 3)
	executionOrder := make(chan int, 3)
	for id := 1; id <= 3; id++ {
		quota := pacing.base
		if id == 2 {
			quota = pacing.tdnet
		}
		go func(requestID int, requestQuota *rateQuota) {
			_, executeErr := pacing.Execute(
				context.Background(),
				[]*rateQuota{requestQuota},
				func(context.Context) (APIResponse, error) {
					executionOrder <- requestID
					return APIResponse{}, nil
				},
			)
			results <- pacerResult{id: requestID, err: executeErr}
		}(id, quota)
		waitForPacerQueueLength(t, pacing, id)
	}

	pacing.mu.Lock()
	sequences := []uint64{
		pacing.queue[0].sequence,
		pacing.queue[1].sequence,
		pacing.queue[2].sequence,
	}
	pacing.mu.Unlock()
	if !reflect.DeepEqual(sequences, []uint64{2, 3, 4}) {
		t.Fatalf("受付連番 = %v, [2 3 4]を期待", sequences)
	}

	for waitIndex := 1; waitIndex <= 2; waitIndex++ {
		var wait manualPacerWait
		select {
		case wait = <-clock.waits:
		case <-time.After(time.Second):
			t.Fatalf("%d番目の待機通知がありません", waitIndex)
		}
		if wait.interval != interval {
			t.Errorf("%d番目の待機間隔 = %s, %sを期待", waitIndex, wait.interval, interval)
		}
		close(wait.release)
	}

	for expectedID := 1; expectedID <= 3; expectedID++ {
		select {
		case actualID := <-executionOrder:
			if actualID != expectedID {
				t.Fatalf("API実行順 = %d, %dを期待", actualID, expectedID)
			}
		case <-time.After(time.Second):
			t.Fatalf("%d番目のAPI要求が実行されません", expectedID)
		}
	}
	for completed := 0; completed < 3; completed++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("Execute(id=%d) error = %v", result.id, result.err)
			}
		case <-time.After(time.Second):
			t.Fatal("Executeの完了結果がありません")
		}
	}
	waitForPacerIdle(t, pacing)
}

// ----------------------------------------

/*
TestPacingQueueCancellationRemovesWaiterAndAdvancesQueue は、キャンセル後に後続要求が進むことを検証します。

機能:
  - 先頭の待機者をcontextキャンセルでキューから除く
  - キャンセル要求が将来枠を消費せず、後続が同じ残り間隔で進むことを確認する
  - キュー空後にランナーgoroutineが終了することを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestPacingQueueCancellationRemovesWaiterAndAdvancesQueue(t *testing.T) {
	const interval = time.Minute
	clock := newManualPacerClock()
	pacing := newPacingState(PlanStandard)
	pacing.base = newRateQuota(1, interval)
	pacing.now = clock.Now
	pacing.waitInterval = clock.Wait
	_, err := pacing.Execute(
		context.Background(),
		[]*rateQuota{pacing.base},
		func(context.Context) (APIResponse, error) { return APIResponse{}, nil },
	)
	if err != nil {
		t.Fatalf("初回Execute() error = %v", err)
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	cancelResult := make(chan error, 1)
	canceledExecuted := make(chan struct{}, 1)
	go func() {
		_, executeErr := pacing.Execute(
			cancelContext,
			[]*rateQuota{pacing.base},
			func(context.Context) (APIResponse, error) {
				canceledExecuted <- struct{}{}
				return APIResponse{}, nil
			},
		)
		cancelResult <- executeErr
	}()
	waitForPacerQueueLength(t, pacing, 1)
	select {
	case <-clock.waits:
	case <-time.After(time.Second):
		t.Fatal("キャンセル対象の待機通知がありません")
	}

	nextResult := make(chan error, 1)
	go func() {
		_, executeErr := pacing.Execute(
			context.Background(),
			[]*rateQuota{pacing.base},
			func(context.Context) (APIResponse, error) { return APIResponse{}, nil },
		)
		nextResult <- executeErr
	}()
	waitForPacerQueueLength(t, pacing, 2)
	cancel()
	select {
	case err := <-cancelResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("キャンセル待機のerror = %v, context.Canceledを期待", err)
		}
	case <-time.After(time.Second):
		t.Fatal("キャンセル待機者が終了しません")
	}

	var nextWait manualPacerWait
	select {
	case nextWait = <-clock.waits:
	case <-time.After(time.Second):
		t.Fatal("後続要求の待機通知がありません")
	}
	if nextWait.interval != interval {
		t.Errorf("キャンセル後の待機間隔 = %s, %sを期待", nextWait.interval, interval)
	}
	close(nextWait.release)
	select {
	case err := <-nextResult:
		if err != nil {
			t.Fatalf("後続Execute() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("キャンセル後の後続要求が進みません")
	}
	select {
	case <-canceledExecuted:
		t.Fatal("キャンセル済み要求のexecuteが呼ばれました")
	default:
	}
	waitForPacerIdle(t, pacing)
}

// ----------------------------------------

/*
waitForPacerQueueLength は、レートキューが指定待機者数になるまで短時間待機します。

機能:
  - mutex下でqueue長を読み取る
  - 1秒以内に期待数にならなければテストを失敗させる

引数:
  - t *testing.T: テスト状態を管理する値
  - pacing *pacingState: 確認する全class共通FIFOレートキュー
  - expected int: 期待する待機者数

返り値:
  - なし
*/
func waitForPacerQueueLength(t *testing.T, pacing *pacingState, expected int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pacing.mu.Lock()
		length := len(pacing.queue)
		pacing.mu.Unlock()
		if length == expected {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("キュー長が%dになりません", expected)
}

// ----------------------------------------

/*
waitForPacerIdle は、レートキューのランナーが終了するまで短時間待機します。

機能:
  - queueが空かつrunningがfalseになったことをmutex下で確認する
  - 1秒以内に終了しなければgoroutine残留としてテストを失敗させる

引数:
  - t *testing.T: テスト状態を管理する値
  - pacing *pacingState: 確認する全class共通FIFOレートキュー

返り値:
  - なし
*/
func waitForPacerIdle(t *testing.T, pacing *pacingState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pacing.mu.Lock()
		idle := !pacing.running && len(pacing.queue) == 0
		pacing.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("レートキューのランナーが終了しません")
}

// ----------------------------------------

/*
assertServiceErrorKind は、共通ServiceErrorの分類を確認します。

機能:
  - errors.AsでServiceErrorを抽出してKindを期待値と比較する

引数:
  - t *testing.T: テスト状態を管理する値
  - err error: 確認するエラー
  - expected domain.ErrorKind: 期待する共通エラー分類

返り値:
  - なし
*/
func assertServiceErrorKind(t *testing.T, err error, expected domain.ErrorKind) {
	t.Helper()
	var serviceErr *domain.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Kind != expected {
		t.Fatalf("error = %v, %sを期待", err, expected)
	}
}

// ----------------------------------------

/*
hasDatasetDescriptor は、provider仕様にdatasetが含まれるか確認します。

機能:
  - dataset名の完全一致で公開有無を判定する

引数:
  - descriptor domain.ProviderDescriptor: 確認するprovider仕様
  - name string: 検索するdataset名

返り値:
  - bool: datasetが含まれる場合はtrue
*/
func hasDatasetDescriptor(descriptor domain.ProviderDescriptor, name string) bool {
	for _, dataset := range descriptor.Datasets {
		if dataset.Name == name {
			return true
		}
	}
	return false
}

// ----------------------------------------

/*
findDatasetDescriptor は、provider仕様から指定datasetを取得します。

機能:
  - dataset名の完全一致で検索し、存在しない場合はテストを停止する

引数:
  - t *testing.T: テスト状態を管理する値
  - descriptor domain.ProviderDescriptor: 検索するprovider仕様
  - name string: 取得するdataset名

返り値:
  - domain.DatasetDescriptor: 一致したdataset仕様
*/
func findDatasetDescriptor(
	t *testing.T,
	descriptor domain.ProviderDescriptor,
	name string,
) domain.DatasetDescriptor {
	t.Helper()
	for _, dataset := range descriptor.Datasets {
		if dataset.Name == name {
			return dataset
		}
	}
	t.Fatalf("dataset %qがDescriptorにありません", name)
	return domain.DatasetDescriptor{}
}

// ----------------------------------------

/*
hasParameterDescriptor は、dataset仕様にquery項目が含まれるか確認します。

機能:
  - parameter名の完全一致で公開有無を判定する

引数:
  - descriptor domain.DatasetDescriptor: 確認するdataset仕様
  - name string: 検索するparameter名

返り値:
  - bool: parameterが含まれる場合はtrue
*/
func hasParameterDescriptor(descriptor domain.DatasetDescriptor, name string) bool {
	for _, parameter := range descriptor.Parameters {
		if parameter.Name == name {
			return true
		}
	}
	return false
}

// ----------------------------------------

/*
findParameterDescriptor は、dataset仕様から指定query項目を取得します。

機能:
  - parameter名の完全一致で検索し、存在しない場合はテストを停止する

引数:
  - t *testing.T: テスト状態を管理する値
  - descriptor domain.DatasetDescriptor: 検索するdataset仕様
  - name string: 取得するparameter名

返り値:
  - domain.ParameterDescriptor: 一致したparameter仕様
*/
func findParameterDescriptor(
	t *testing.T,
	descriptor domain.DatasetDescriptor,
	name string,
) domain.ParameterDescriptor {
	t.Helper()
	for _, parameter := range descriptor.Parameters {
		if parameter.Name == name {
			return parameter
		}
	}
	t.Fatalf("parameter %qがdataset %qにありません", name, descriptor.Name)
	return domain.ParameterDescriptor{}
}

// ----------------------------------------

/*
containsString は、文字列一覧に指定値が含まれるか確認します。

機能:
  - Bulk endpointのAllowedを完全一致で検証する

引数:
  - values []string: 検索対象一覧
  - expected string: 検索する文字列

返り値:
  - bool: 一覧に文字列が含まれる場合はtrue
*/
func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
