package polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type polymarketFakeRequest struct {
	dataset string
	query   url.Values
}

type polymarketFakeClient struct {
	mu       sync.Mutex
	response APIResponse
	err      error
	requests []polymarketFakeRequest
}

// Fetch は、collectorから受けた1要求を記録して設定済み結果を返します。
//
// 機能:
//   - datasetと同名反復を含むqueryを複製する
//   - 並行Collectからの記録をmutexで保護する
//   - HTTP通信を行わずAPIResponseまたはerrorを返す
//
// 引数:
//   - ctx context.Context: collectorから渡される要求context
//   - dataset string: 固定許可リストのdataset識別子
//   - query url.Values: 上流名と配列形式へ変換済みのquery
//
// 返り値:
//   - APIResponse: テストで設定した成功応答
//   - error: テストで設定した失敗
func (f *polymarketFakeClient) Fetch(ctx context.Context, dataset string, query url.Values) (APIResponse, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, polymarketFakeRequest{dataset: dataset, query: cloneValues(query)})
	return f.response, f.err
}

// Requests は、記録済み要求のスナップショットを返します。
//
// 機能:
//   - 内部sliceとqueryを複製して呼出側からの変更を防ぐ
//
// 引数:
//   - なし
//
// 返り値:
//   - []polymarketFakeRequest: 要求順のdatasetとquery
func (f *polymarketFakeClient) Requests() []polymarketFakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]polymarketFakeRequest, len(f.requests))
	for index, request := range f.requests {
		result[index] = polymarketFakeRequest{dataset: request.dataset, query: cloneValues(request.query)}
	}
	return result
}

// TestCollectorDescriptorIsReadOnlyAndDoesNotFetch は、Descriptorの公開範囲と無通信性を検証します。
//
// 機能:
//   - polymarketの37 datasetが固定順で公開されることを確認する
//   - Descriptor取得でAPIClient.Fetchを呼ばないことを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestCollectorDescriptorIsReadOnlyAndDoesNotFetch(t *testing.T) {
	client := &polymarketFakeClient{}
	collector, err := NewCollector(client)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	descriptor := collector.Descriptor()
	if descriptor.Name != "polymarket" || descriptor.DisplayName != "Polymarket" || len(descriptor.Datasets) != 37 {
		t.Fatalf("Descriptor() = %+v, Polymarket 37 datasetを期待", descriptor)
	}
	for index, dataset := range descriptor.Datasets {
		if dataset.Name != endpointSpecs[index].Dataset {
			t.Errorf("Descriptor().Datasets[%d].Name = %q, %qを期待", index, dataset.Name, endpointSpecs[index].Dataset)
		}
	}
	if requests := client.Requests(); len(requests) != 0 {
		t.Errorf("Descriptor()が%d件の要求を発生させました", len(requests))
	}
}

// TestCollectorEveryDatasetPerformsExactlyOneFetch は、37 datasetすべての1 collect対1 GETを検証します。
//
// 機能:
//   - 各datasetの必須入力と動的route selectorに最小の有効値を与える
//   - 1回のCollect成功に対しAPIClient.Fetchが正確に1回だけ呼ばれることを確認する
//   - すべての結果に認証不要、読取専用、仕様確認日のmetadataが付くことを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestCollectorEveryDatasetPerformsExactlyOneFetch(t *testing.T) {
	client := &polymarketFakeClient{response: APIResponse{
		Body: map[string]any{}, SourceURL: "https://example.invalid/source", StatusCode: http.StatusOK,
		FetchedAt: time.Date(2026, time.August, 8, 0, 0, 0, 0, time.UTC), ResponseBytes: 2,
	}}
	collector, err := NewCollector(client)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	for class := range collector.pacing.intervals {
		collector.pacing.intervals[class] = 0
	}

	for index, spec := range endpointSpecs {
		parameters := minimalValidParameters(spec)
		result, collectErr := collector.Collect(context.Background(), spec.Dataset, parameters)
		if collectErr != nil {
			t.Errorf("dataset %q Collect() error = %v, parameters = %#v", spec.Dataset, collectErr, parameters)
			continue
		}
		requests := client.Requests()
		if len(requests) != index+1 {
			t.Fatalf("dataset %qまでのFetch数 = %d, %dを期待", spec.Dataset, len(requests), index+1)
		}
		if requests[index].dataset != spec.Dataset {
			t.Errorf("Fetch dataset = %q, %qを期待", requests[index].dataset, spec.Dataset)
		}
		if result.Metadata["api_service"] != string(spec.Service) || result.Metadata["public"] != true || result.Metadata["authentication_required"] != false || result.Metadata["read_only"] != true || result.Metadata["specification_reviewed_date"] != SpecificationReviewDate {
			t.Errorf("dataset %q metadata = %#v", spec.Dataset, result.Metadata)
		}
	}
}

// TestValidateAndBuildUpstreamQueryMappings は、公閏入力から公式queryへの変換を検証します。
//
// 機能:
//   - Gammaの反復配列とDataのCSV配列を区別する
//   - searchとactivityのローカルbooleanを公式queryへ変換する
//   - commentsのitem、user、listごとに公式query集合を切り替える
//   - 絶対時刻指定時はprice_historyの既定intervalを送らない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestValidateAndBuildUpstreamQueryMappings(t *testing.T) {
	wallet := "0x" + repeatText("1", 40)
	conditionA := "0x" + repeatText("a", 64)
	conditionB := "0x" + repeatText("b", 64)
	tests := []struct {
		name       string
		dataset    string
		parameters map[string]any
		want       url.Values
	}{
		{name: "markets scalar tag", dataset: "markets", parameters: map[string]any{"tag_id": 7}, want: url.Values{"limit": {"10"}, "closed": {"false"}, "tag_id": {"7"}, "order": {"volume24hr"}, "ascending": {"false"}}},
		{name: "markets repeated tags", dataset: "markets", parameters: map[string]any{"tag_ids": []int{7, 9}}, want: url.Values{"limit": {"10"}, "closed": {"false"}, "tag_id": {"7", "9"}, "order": {"volume24hr"}, "ascending": {"false"}}},
		{name: "data csv arrays", dataset: "trades", parameters: map[string]any{"markets": []string{conditionA, conditionB}}, want: url.Values{"market": {conditionA + "," + conditionB}, "limit": {"100"}, "offset": {"0"}, "takerOnly": {"true"}}},
		{name: "search active", dataset: "search", parameters: map[string]any{"query": "election"}, want: url.Values{"q": {"election"}, "limit_per_type": {"5"}, "page": {"1"}, "events_status": {"active"}, "keep_closed_markets": {"0"}, "search_profiles": {"false"}}},
		{name: "search closed", dataset: "search", parameters: map[string]any{"query": "election", "include_closed_markets": true}, want: url.Values{"q": {"election"}, "limit_per_type": {"5"}, "page": {"1"}, "keep_closed_markets": {"1"}, "search_profiles": {"false"}}},
		{name: "activity excludes deposits", dataset: "user_activity", parameters: map[string]any{"address": wallet}, want: url.Values{"user": {wallet}, "limit": {"100"}, "offset": {"0"}, "sortBy": {"TIMESTAMP"}, "sortDirection": {"DESC"}, "excludeDepositsWithdrawals": {"true"}}},
		{name: "activity includes deposits", dataset: "user_activity", parameters: map[string]any{"address": wallet, "include_deposits_and_withdrawals": true}, want: url.Values{"user": {wallet}, "limit": {"100"}, "offset": {"0"}, "sortBy": {"TIMESTAMP"}, "sortDirection": {"DESC"}, "excludeDepositsWithdrawals": {"false"}}},
		{name: "comment item", dataset: "comments", parameters: map[string]any{"comment_id": 42, "get_positions": true}, want: url.Values{"comment_id": {"42"}, "get_positions": {"true"}}},
		{name: "comment user", dataset: "comments", parameters: map[string]any{"user_address": wallet}, want: url.Values{"user_address": {wallet}, "limit": {"100"}, "offset": {"0"}, "ascending": {"false"}}},
		{name: "comment list", dataset: "comments", parameters: map[string]any{"parent_entity_type": "Event", "parent_entity_id": 5}, want: url.Values{"limit": {"100"}, "offset": {"0"}, "ascending": {"false"}, "parent_entity_type": {"Event"}, "parent_entity_id": {"5"}, "get_positions": {"false"}, "holders_only": {"false"}}},
		{name: "absolute price history", dataset: "price_history", parameters: map[string]any{"token_id": "123", "start_timestamp": 10, "end_timestamp": 20}, want: url.Values{"market": {"123"}, "fidelity": {"60"}, "startTs": {"10"}, "endTs": {"20"}}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			spec := mustEndpoint(t, testCase.dataset)
			values, err := validateParameters(spec, testCase.parameters)
			if err != nil {
				t.Fatalf("validateParameters() error = %v", err)
			}
			actual, err := buildUpstreamQuery(spec, values, testCase.parameters)
			if err != nil {
				t.Fatalf("buildUpstreamQuery() error = %v", err)
			}
			if !reflect.DeepEqual(actual, testCase.want) {
				t.Errorf("query = %#v, %#vを期待", actual, testCase.want)
			}
		})
	}
}

// TestCollectorRejectsInvalidParametersWithoutFetch は、dataset固有の入力違反を無通信で拒否することを検証します。
//
// 機能:
//   - 未知項目、selector欠落、配列排他、時刻範囲、filter pairを確認する
//   - scalar tag_idとarray tag_idsの排他を確認する
//   - 入力失敗をINVALID_ARGUMENTとし、Fetchを呼ばない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestCollectorRejectsInvalidParametersWithoutFetch(t *testing.T) {
	wallet := "0x" + repeatText("1", 40)
	condition := "0x" + repeatText("a", 64)
	tests := []struct {
		name       string
		dataset    string
		parameters map[string]any
	}{
		{name: "unknown parameter", dataset: "sports", parameters: map[string]any{"url": "https://example.com"}},
		{name: "missing selector", dataset: "event", parameters: nil},
		{name: "both selectors", dataset: "market", parameters: map[string]any{"id": 1, "slug": "one"}},
		{name: "tag aliases", dataset: "markets", parameters: map[string]any{"tag_id": 1, "tag_ids": []int{2}}},
		{name: "market and event", dataset: "trades", parameters: map[string]any{"markets": []string{condition}, "event_ids": []int{1}}},
		{name: "nonpositive event", dataset: "trades", parameters: map[string]any{"event_ids": []int{0}}},
		{name: "duplicate array", dataset: "trades", parameters: map[string]any{"event_ids": []int{1, 1}}},
		{name: "too many activity types", dataset: "user_activity", parameters: map[string]any{"address": wallet, "types": []string{"TRADE", "SPLIT", "MERGE", "REDEEM", "REWARD", "CONVERSION", "DEPOSIT", "WITHDRAWAL", "YIELD", "MAKER_REBATE", "TAKER_REBATE", "REFERRAL_REWARD", "TRADE"}}},
		{name: "one absolute timestamp", dataset: "price_history", parameters: map[string]any{"token_id": "1", "start_timestamp": 10}},
		{name: "interval with absolute", dataset: "price_history", parameters: map[string]any{"token_id": "1", "interval": "1d", "start_timestamp": 10, "end_timestamp": 20}},
		{name: "reversed timestamps", dataset: "trades", parameters: map[string]any{"start_timestamp": 20, "end_timestamp": 10}},
		{name: "half filter pair", dataset: "trades", parameters: map[string]any{"filter_type": "CASH"}},
		{name: "comments mixed routes", dataset: "comments", parameters: map[string]any{"comment_id": 1, "user_address": wallet}},
		{name: "comments half parent", dataset: "comments", parameters: map[string]any{"parent_entity_type": "Event"}},
		{name: "comments item list query", dataset: "comments", parameters: map[string]any{"comment_id": 1, "limit": 2}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &polymarketFakeClient{}
			collector, err := NewCollector(client)
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			_, collectErr := collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			assertServiceErrorKind(t, collectErr, domain.ErrorInvalidArgument)
			if requests := client.Requests(); len(requests) != 0 {
				t.Errorf("入力違反で%d件のFetchが発生しました", len(requests))
			}
		})
	}
}

// TestPaginationMetadataNeverInventsTotals は、ページング情報が上流応答以上を推測しないことを検証します。
//
// 機能:
//   - page応答の総ページ数は公式pagination objectにある場合だけ公開する
//   - offset応答は配列長からhas_more、next_offset、総件数を推測しない
//   - keysetは公式next cursorの有無と終端sentinelだけを使う
//   - comments詳細routeはpaginationなし、user routeはoffsetとして扱う
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestPaginationMetadataNeverInventsTotals(t *testing.T) {
	pageWithoutTotals := buildPaginationMetadata(paginationPage, "search", map[string]any{"page": json.Number("2")}, map[string]any{"events": []any{1, 2}})
	if pageWithoutTotals["total_pages_known"] != false || pageWithoutTotals["has_more_known"] != false {
		t.Errorf("page metadata = %#v, 未知flagを期待", pageWithoutTotals)
	}
	for _, forbidden := range []string{"total_pages", "total_results", "has_more", "next_page"} {
		if _, exists := pageWithoutTotals[forbidden]; exists {
			t.Errorf("page metadataが%qを推測しました: %#v", forbidden, pageWithoutTotals)
		}
	}

	pageWithTotals := buildPaginationMetadata(paginationPage, "search", map[string]any{"page": json.Number("3")}, map[string]any{"pagination": map[string]any{"totalPages": json.Number("8"), "totalResults": json.Number("77"), "hasMore": true, "nextPage": json.Number("4")}})
	if pageWithTotals["total_pages_known"] != true || pageWithTotals["total_pages"] != json.Number("8") || pageWithTotals["total_results"] != json.Number("77") || pageWithTotals["has_more"] != true || pageWithTotals["next_page"] != json.Number("4") {
		t.Errorf("page metadata = %#v, 公式pagination値の保持を期待", pageWithTotals)
	}

	offset := buildPaginationMetadata(paginationOffset, "trades", map[string]any{"limit": json.Number("2"), "offset": json.Number("4")}, []any{1, 2})
	if offset["request_limit"] != json.Number("2") || offset["request_offset"] != json.Number("4") || offset["has_more_known"] != false || offset["total_pages_known"] != false {
		t.Errorf("offset metadata = %#v", offset)
	}
	for _, forbidden := range []string{"total_pages", "total_results", "has_more", "next_offset"} {
		if _, exists := offset[forbidden]; exists {
			t.Errorf("offset metadataが%qを推測しました: %#v", forbidden, offset)
		}
	}

	keyset := buildPaginationMetadata(paginationKeyset, "events", map[string]any{"after_cursor": "abc"}, map[string]any{"nextCursor": "def"})
	if keyset["request_cursor"] != "abc" || keyset["next_cursor"] != "def" || keyset["has_more_known"] != true || keyset["has_more"] != true {
		t.Errorf("keyset metadata = %#v", keyset)
	}
	terminal := buildPaginationMetadata(paginationKeyset, "clob_markets", nil, map[string]any{"next_cursor": "LTE="})
	if terminal["request_cursor"] != "" || terminal["next_cursor"] != "LTE=" || terminal["has_more"] != false {
		t.Errorf("terminal keyset metadata = %#v", terminal)
	}

	response := APIResponse{Endpoint: "/comments/1", StatusCode: http.StatusOK}
	itemMetadata := buildMetadata(mustEndpoint(t, "comments"), response, map[string]any{"comment_id": json.Number("1")}, map[string]any{})
	if _, exists := itemMetadata["pagination"]; exists {
		t.Errorf("comment item metadata = %#v, paginationなしを期待", itemMetadata)
	}
	userMetadata := buildMetadata(mustEndpoint(t, "comments"), response, map[string]any{"user_address": "0x" + repeatText("1", 40), "limit": json.Number("10"), "offset": json.Number("20")}, []any{})
	userPaging, ok := userMetadata["pagination"].(map[string]any)
	if !ok || userPaging["mode"] != string(paginationOffset) || userPaging["request_limit"] != json.Number("10") || userPaging["request_offset"] != json.Number("20") {
		t.Errorf("comment user metadata = %#v", userMetadata)
	}
}

// TestClassifyCollectErrorIncludesHTTP425 は、上流HTTP状態の共通分類を検証します。
//
// 機能:
//   - CLOBがレート制御時に返す425をPROVIDER_UNAVAILABLEへ分類する
//   - 入力、未検出、時間切れ、その他上流失敗の境界を固定する
//   - APIErrorをServiceErrorのcauseチェーンに保持する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestClassifyCollectErrorIncludesHTTP425(t *testing.T) {
	tests := []struct {
		status int
		kind   domain.ErrorKind
	}{
		{status: http.StatusBadRequest, kind: domain.ErrorInvalidArgument},
		{status: http.StatusUnprocessableEntity, kind: domain.ErrorInvalidArgument},
		{status: http.StatusNotFound, kind: domain.ErrorNotFound},
		{status: http.StatusUnauthorized, kind: domain.ErrorProviderUnavailable},
		{status: http.StatusForbidden, kind: domain.ErrorProviderUnavailable},
		{status: http.StatusTooEarly, kind: domain.ErrorProviderUnavailable},
		{status: http.StatusTooManyRequests, kind: domain.ErrorProviderUnavailable},
		{status: http.StatusRequestTimeout, kind: domain.ErrorTimeout},
		{status: http.StatusGatewayTimeout, kind: domain.ErrorTimeout},
		{status: http.StatusInternalServerError, kind: domain.ErrorUpstream},
	}
	for _, testCase := range tests {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			cause := &APIError{StatusCode: testCase.status, Message: "上流エラー", Endpoint: "/test"}
			actual := classifyCollectError(cause)
			assertServiceErrorKind(t, actual, testCase.kind)
			if !errors.Is(actual, cause) {
				t.Errorf("classifyCollectError()のerror chainからAPIErrorが失われました: %v", actual)
			}
		})
	}
}

// TestRequestRateClassesApplyServiceAndEndpointLimits は、要求ごとのquota classを検証します。
//
// 機能:
//   - 個別quotaのあるendpointでservice generalと個別classの両方を適用する
//   - general自身のendpointでclassを重複させない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestRequestRateClassesApplyServiceAndEndpointLimits(t *testing.T) {
	tests := []struct {
		dataset string
		want    []rateClass
	}{
		{dataset: "search", want: []rateClass{rateGammaGeneral, rateGammaSearch}},
		{dataset: "trades", want: []rateClass{rateDataGeneral, rateDataTrades}},
		{dataset: "order_book", want: []rateClass{rateCLOBGeneral, rateCLOBQuote}},
		{dataset: "sports", want: []rateClass{rateGammaGeneral}},
		{dataset: "server_time", want: []rateClass{rateCLOBGeneral}},
	}
	for _, testCase := range tests {
		actual := requestRateClasses(mustEndpoint(t, testCase.dataset))
		if !reflect.DeepEqual(actual, testCase.want) {
			t.Errorf("dataset %q rate classes = %v, %vを期待", testCase.dataset, actual, testCase.want)
		}
	}
}

// TestPacingStateExecutesGlobalFIFO は、全dataset共通の単一FIFOを検証します。
//
// 機能:
//   - 先頭要求が実行中の間に後続2件を順番にenqueueする
//   - serviceやrate classが異なっても追い越さず実行順を維持する
//   - 通信実行を常に1件へ直列化する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestPacingStateExecutesGlobalFIFO(t *testing.T) {
	pacing := newPacingState()
	for class := range pacing.intervals {
		pacing.intervals[class] = 0
	}
	firstRelease := make(chan struct{})
	started := make(chan int, 3)
	results := make(chan error, 3)
	var active int32
	var maximum int32

	launch := func(id int, classes []rateClass, block bool) {
		go func() {
			_, err := pacing.Execute(context.Background(), classes, func(context.Context) (APIResponse, error) {
				current := atomic.AddInt32(&active, 1)
				for {
					old := atomic.LoadInt32(&maximum)
					if current <= old || atomic.CompareAndSwapInt32(&maximum, old, current) {
						break
					}
				}
				started <- id
				if block {
					<-firstRelease
				}
				atomic.AddInt32(&active, -1)
				return APIResponse{}, nil
			})
			results <- err
		}()
	}

	launch(1, []rateClass{rateGammaGeneral}, true)
	if actual := receiveInt(t, started); actual != 1 {
		t.Fatalf("最初の実行ID = %d, 1を期待", actual)
	}
	launch(2, []rateClass{rateDataGeneral}, false)
	waitForPacingQueueLength(t, pacing, 1)
	launch(3, []rateClass{rateCLOBGeneral}, false)
	waitForPacingQueueLength(t, pacing, 2)
	close(firstRelease)
	if actual := receiveInt(t, started); actual != 2 {
		t.Errorf("第2実行ID = %d, 2を期待", actual)
	}
	if actual := receiveInt(t, started); actual != 3 {
		t.Errorf("第3実行ID = %d, 3を期待", actual)
	}
	for index := 0; index < 3; index++ {
		if err := receiveError(t, results); err != nil {
			t.Errorf("Execute() error = %v", err)
		}
	}
	if actual := atomic.LoadInt32(&maximum); actual != 1 {
		t.Errorf("同時実行数 = %d, 1を期待", actual)
	}
}

// TestPacingStateWaitsForStrictestHalfRateClass は、同一endpointの開始間隔を検証します。
//
// 機能:
//   - 2回目の要求でservice generalとendpoint classのうち厳しい方を待つ
//   - 公式quotaの50パーセントで計算された間隔をそのまま待機関数へ渡す
//   - 手動clockを進め、実時間を待たずに検証する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestPacingStateWaitsForStrictestHalfRateClass(t *testing.T) {
	pacing := newPacingState()
	var clockMutex sync.Mutex
	current := time.Date(2026, time.August, 8, 0, 0, 0, 0, time.UTC)
	waits := make([]time.Duration, 0, 1)
	pacing.now = func() time.Time {
		clockMutex.Lock()
		defer clockMutex.Unlock()
		return current
	}
	pacing.waitInterval = func(interval time.Duration, _ <-chan struct{}) bool {
		clockMutex.Lock()
		waits = append(waits, interval)
		current = current.Add(interval)
		clockMutex.Unlock()
		return true
	}
	classes := requestRateClasses(mustEndpoint(t, "search"))
	execute := func(context.Context) (APIResponse, error) { return APIResponse{}, nil }
	if _, err := pacing.Execute(context.Background(), classes, execute); err != nil {
		t.Fatalf("初回Execute() error = %v", err)
	}
	if _, err := pacing.Execute(context.Background(), classes, execute); err != nil {
		t.Fatalf("2回目Execute() error = %v", err)
	}
	clockMutex.Lock()
	actualWaits := append([]time.Duration(nil), waits...)
	clockMutex.Unlock()
	want := pacing.intervals[rateGammaSearch]
	if !reflect.DeepEqual(actualWaits, []time.Duration{want}) {
		t.Errorf("待機間隔 = %v, [%s]を期待", actualWaits, want)
	}
}

// TestPacingStateRemovesCanceledQueuedRequest は、FIFO待機中のcontext取消しを検証します。
//
// 機能:
//   - 通信未開始の取消し要求をqueueから削除する
//   - 取消し要求のexecute関数を1回も呼ばない
//   - 取消し後の後続要求を順番どおり進める
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestPacingStateRemovesCanceledQueuedRequest(t *testing.T) {
	pacing := newPacingState()
	for class := range pacing.intervals {
		pacing.intervals[class] = 0
	}
	firstRelease := make(chan struct{})
	started := make(chan int, 2)
	firstResult := make(chan error, 1)
	thirdResult := make(chan error, 1)

	go func() {
		_, err := pacing.Execute(context.Background(), []rateClass{rateGammaGeneral}, func(context.Context) (APIResponse, error) {
			started <- 1
			<-firstRelease
			return APIResponse{}, nil
		})
		firstResult <- err
	}()
	if actual := receiveInt(t, started); actual != 1 {
		t.Fatalf("最初の実行ID = %d, 1を期待", actual)
	}

	secondContext, cancelSecond := context.WithCancel(context.Background())
	secondResult := make(chan error, 1)
	var secondExecuted atomic.Bool
	go func() {
		_, err := pacing.Execute(secondContext, []rateClass{rateDataGeneral}, func(context.Context) (APIResponse, error) {
			secondExecuted.Store(true)
			return APIResponse{}, nil
		})
		secondResult <- err
	}()
	waitForPacingQueueLength(t, pacing, 1)
	go func() {
		_, err := pacing.Execute(context.Background(), []rateClass{rateCLOBGeneral}, func(context.Context) (APIResponse, error) {
			started <- 3
			return APIResponse{}, nil
		})
		thirdResult <- err
	}()
	waitForPacingQueueLength(t, pacing, 2)
	cancelSecond()
	if err := receiveError(t, secondResult); !errors.Is(err, context.Canceled) {
		t.Errorf("取消し要求error = %v, context.Canceledを期待", err)
	}
	waitForPacingQueueLength(t, pacing, 1)
	close(firstRelease)
	if actual := receiveInt(t, started); actual != 3 {
		t.Errorf("取消し後の実行ID = %d, 3を期待", actual)
	}
	if err := receiveError(t, firstResult); err != nil {
		t.Errorf("先頭Execute() error = %v", err)
	}
	if err := receiveError(t, thirdResult); err != nil {
		t.Errorf("後続Execute() error = %v", err)
	}
	if secondExecuted.Load() {
		t.Error("取消し済み要求のexecute関数が呼ばれました")
	}
}

// assertServiceErrorKind は、errorが指定の共通分類を持つことを確認します。
//
// 機能:
//   - errors.Asで*domain.ServiceErrorを取得しKindを照合する
//   - 型または分類が異なる場合は呼出元テストを失敗させる
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - err error: 検証するerror
//   - want domain.ErrorKind: 期待する共通分類
//
// 返り値:
//   - なし
func assertServiceErrorKind(t *testing.T, err error, want domain.ErrorKind) {
	t.Helper()
	var serviceError *domain.ServiceError
	if !errors.As(err, &serviceError) {
		t.Fatalf("error = %v, *domain.ServiceErrorを期待", err)
	}
	if serviceError.Kind != want {
		t.Fatalf("ServiceError.Kind = %q, %qを期待: %v", serviceError.Kind, want, err)
	}
}

// minimalValidParameters は、指定datasetを1回取得できる最小入力を生成します。
//
// 機能:
//   - 必須項目の型とvalidatorに応じた有効値を生成する
//   - 両方optionalのentity、tag、related-tags routeに1つだけselectorを与える
//
// 引数:
//   - spec endpointSpec: 必須入力を生成するdataset仕様
//
// 返り値:
//   - map[string]any: Collectへ渡せる最小入力
func minimalValidParameters(spec endpointSpec) map[string]any {
	wallet := "0x" + repeatText("1", 40)
	condition := "0x" + repeatText("a", 64)
	result := make(map[string]any)
	for _, parameter := range spec.Parameters {
		if !parameter.Required {
			continue
		}
		switch parameter.Type {
		case typeString:
			switch parameter.Validator {
			case validatorWallet:
				result[parameter.Name] = wallet
			case validatorCondition:
				result[parameter.Name] = condition
			case validatorToken:
				result[parameter.Name] = "1"
			default:
				result[parameter.Name] = "test"
			}
		case typeInteger, typeNumber:
			result[parameter.Name] = 1
		case typeBoolean:
			result[parameter.Name] = false
		case typeStringArray:
			if parameter.Validator == validatorCondition {
				result[parameter.Name] = []string{condition}
			} else {
				result[parameter.Name] = []string{"test"}
			}
		case typeIntegerArray:
			result[parameter.Name] = []int{1}
		}
	}
	switch spec.Dataset {
	case "event", "market":
		result["slug"] = "test"
	case "tag", "related_tags":
		result["id"] = 1
	}
	return result
}

// repeatText は、指定文字列をcount回結合した識別子フィクスチャを返します。
//
// 機能:
//   - テスト用walletとcondition IDを読みやすく組み立てる
//
// 引数:
//   - value string: 反復する文字列
//   - count int: 反復回数
//
// 返り値:
//   - string: valueをcount回結合した文字列
func repeatText(value string, count int) string {
	result := ""
	for index := 0; index < count; index++ {
		result += value
	}
	return result
}

// waitForPacingQueueLength は、FIFOが指定件数に達するまで短時間待機します。
//
// 機能:
//   - pacing mutex下でqueue長を読み取る
//   - テスト全体のハングを防ぐ期限を設ける
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - pacing *pacingState: 確認対象のFIFO状態
//   - want int: 期待するqueue件数
//
// 返り値:
//   - なし
func waitForPacingQueueLength(t *testing.T, pacing *pacingState, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pacing.mu.Lock()
		actual := len(pacing.queue)
		pacing.mu.Unlock()
		if actual == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pacing queueが%d件に達しません", want)
}

// receiveInt は、期限内にint channelから1件受信します。
//
// 機能:
//   - 並行テストのハングを2秒で検出する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - values <-chan int: 受信対象channel
//
// 返り値:
//   - int: 受信した値
func receiveInt(t *testing.T, values <-chan int) int {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("int channelの受信が時間切れになりました")
		return 0
	}
}

// receiveError は、期限内にerror channelから1件受信します。
//
// 機能:
//   - 並行テストのハングを2秒で検出する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - values <-chan error: 受信対象channel
//
// 返り値:
//   - error: 受信した値
func receiveError(t *testing.T, values <-chan error) error {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("error channelの受信が時間切れになりました")
		return context.DeadlineExceeded
	}
}
