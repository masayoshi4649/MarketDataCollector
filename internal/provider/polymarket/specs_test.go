package polymarket

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type expectedEndpointSpec struct {
	service    apiService
	path       string
	route      routeKind
	pagination paginationMode
	normalizer normalizerKind
	rateClass  rateClass
	queries    []string
}

// TestEndpointSpecsMatchPublicReadOnlyAllowlist は、37件の公開GET仕様を固定します。
//
// 機能:
//   - dataset名の重複と欠落を検出する
//   - service、固定path、route、ページング、正規化、rate classを照合する
//   - 上流へ送信可能なquery名を公式OpenAPIに沿った許可リストとして照合する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestEndpointSpecsMatchPublicReadOnlyAllowlist(t *testing.T) {
	expected := map[string]expectedEndpointSpec{
		"search":               {serviceGamma, "/public-search", routeFixed, paginationPage, normalizeSearch, rateGammaSearch, []string{"q", "limit_per_type", "page", "events_status", "keep_closed_markets", "search_profiles"}},
		"events":               {serviceGamma, "/events/keyset", routeFixed, paginationKeyset, normalizeEvents, rateGammaEvents, []string{"limit", "after_cursor", "closed", "live", "title_search", "tag_slug", "order", "ascending"}},
		"event":                {serviceGamma, "/events", routeEntity, paginationNone, normalizeEvent, rateGammaEvents, nil},
		"markets":              {serviceGamma, "/markets/keyset", routeFixed, paginationKeyset, normalizeMarkets, rateGammaMarkets, []string{"limit", "after_cursor", "closed", "tag_id", "liquidity_num_min", "volume_num_min", "order", "ascending"}},
		"market":               {serviceGamma, "/markets", routeEntity, paginationNone, normalizeMarket, rateGammaMarkets, nil},
		"order_book":           {serviceCLOB, "/book", routeFixed, paginationNone, normalizeBook, rateCLOBQuote, []string{"token_id"}},
		"token_price":          {serviceCLOB, "/price", routeTokenPrice, paginationNone, normalizeTokenQuote, rateCLOBQuote, []string{"token_id", "side"}},
		"price_history":        {serviceCLOB, "/prices-history", routeFixed, paginationNone, normalizeRaw, rateCLOBHistory, []string{"market", "interval", "fidelity", "startTs", "endTs"}},
		"user_positions":       {serviceData, "/positions", routeFixed, paginationOffset, normalizeRaw, rateDataPositions, []string{"user", "market", "eventId", "sizeThreshold", "redeemable", "mergeable", "limit", "offset", "sortBy", "sortDirection", "title"}},
		"user_activity":        {serviceData, "/activity", routeFixed, paginationOffset, normalizeRaw, rateDataPositions, []string{"user", "market", "eventId", "limit", "offset", "type", "start", "end", "sortBy", "sortDirection", "side", "excludeDepositsWithdrawals"}},
		"trades":               {serviceData, "/trades", routeFixed, paginationOffset, normalizeRaw, rateDataTrades, []string{"user", "market", "eventId", "limit", "offset", "takerOnly", "filterType", "filterAmount", "side", "start", "end"}},
		"closed_positions":     {serviceData, "/closed-positions", routeFixed, paginationOffset, normalizeRaw, rateDataClosed, []string{"user", "market", "eventId", "title", "limit", "offset", "sortBy", "sortDirection"}},
		"holders":              {serviceData, "/holders", routeFixed, paginationNone, normalizeRaw, rateDataGeneral, []string{"market", "limit", "minBalance"}},
		"market_positions":     {serviceData, "/v1/market-positions", routeFixed, paginationOffset, normalizeRaw, rateDataPositions, []string{"market", "user", "status", "sortBy", "sortDirection", "limit", "offset"}},
		"position_value":       {serviceData, "/value", routeFixed, paginationNone, normalizeRaw, rateDataGeneral, []string{"user", "market"}},
		"traded_markets_count": {serviceData, "/traded", routeFixed, paginationNone, normalizeRaw, rateDataGeneral, []string{"user"}},
		"open_interest":        {serviceData, "/oi", routeFixed, paginationNone, normalizeRaw, rateDataGeneral, []string{"market"}},
		"live_volume":          {serviceData, "/live-volume", routeFixed, paginationNone, normalizeRaw, rateDataGeneral, []string{"id"}},
		"leaderboard":          {serviceData, "/v1/leaderboard", routeFixed, paginationOffset, normalizeRaw, rateDataGeneral, []string{"category", "timePeriod", "orderBy", "limit", "offset", "user", "userName"}},
		"tags":                 {serviceGamma, "/tags", routeFixed, paginationOffset, normalizeRaw, rateGammaTags, []string{"limit", "offset", "order", "ascending", "include_template", "is_carousel"}},
		"tag":                  {serviceGamma, "/tags", routeTag, paginationNone, normalizeRaw, rateGammaTags, []string{"include_template"}},
		"related_tags":         {serviceGamma, "/tags", routeRelatedTags, paginationNone, normalizeRaw, rateGammaTags, []string{"omit_empty", "status"}},
		"series":               {serviceGamma, "/series", routeFixed, paginationOffset, normalizeRaw, rateGammaGeneral, []string{"limit", "offset", "order", "ascending", "slug", "categories_ids", "categories_labels", "closed", "include_chat", "recurrence", "exclude_events"}},
		"series_item":          {serviceGamma, "/series", routeSeriesItem, paginationNone, normalizeRaw, rateGammaGeneral, []string{"include_chat"}},
		"sports":               {serviceGamma, "/sports", routeFixed, paginationNone, normalizeRaw, rateGammaGeneral, nil},
		"sports_market_types":  {serviceGamma, "/sports/market-types", routeFixed, paginationNone, normalizeRaw, rateGammaGeneral, nil},
		"teams":                {serviceGamma, "/teams", routeFixed, paginationOffset, normalizeRaw, rateGammaGeneral, []string{"limit", "offset", "order", "ascending", "league", "name", "abbreviation"}},
		"comments":             {serviceGamma, "/comments", routeComments, paginationOffset, normalizeRaw, rateGammaComments, []string{"limit", "offset", "order", "ascending", "parent_entity_type", "parent_entity_id", "get_positions", "holders_only"}},
		"public_profile":       {serviceGamma, "/public-profile", routeFixed, paginationNone, normalizeRaw, rateGammaGeneral, []string{"address"}},
		"server_time":          {serviceCLOB, "/time", routeFixed, paginationNone, normalizeRaw, rateCLOBGeneral, nil},
		"spread":               {serviceCLOB, "/spread", routeFixed, paginationNone, normalizeRaw, rateCLOBQuote, []string{"token_id"}},
		"tick_size":            {serviceCLOB, "/tick-size", routeFixed, paginationNone, normalizeRaw, rateCLOBTick, []string{"token_id"}},
		"fee_rate":             {serviceCLOB, "/fee-rate", routeFixed, paginationNone, normalizeRaw, rateCLOBGeneral, []string{"token_id"}},
		"negative_risk":        {serviceCLOB, "/neg-risk", routeFixed, paginationNone, normalizeRaw, rateCLOBGeneral, []string{"token_id"}},
		"clob_markets":         {serviceCLOB, "/simplified-markets", routeCLOBMarkets, paginationKeyset, normalizeRaw, rateCLOBGeneral, []string{"next_cursor"}},
		"clob_market":          {serviceCLOB, "/clob-markets", routeCondition, paginationNone, normalizeRaw, rateCLOBGeneral, nil},
		"market_by_token":      {serviceCLOB, "/markets-by-token", routeTokenPath, paginationNone, normalizeRaw, rateCLOBGeneral, nil},
	}

	if len(expected) != 37 {
		t.Fatalf("テスト定義数 = %d, 37を期待", len(expected))
	}
	if len(endpointSpecs) != len(expected) {
		t.Fatalf("endpointSpecs件数 = %d, %dを期待", len(endpointSpecs), len(expected))
	}

	seen := make(map[string]struct{}, len(endpointSpecs))
	for _, actual := range endpointSpecs {
		if _, duplicated := seen[actual.Dataset]; duplicated {
			t.Errorf("dataset %q が重複しています", actual.Dataset)
			continue
		}
		seen[actual.Dataset] = struct{}{}

		want, found := expected[actual.Dataset]
		if !found {
			t.Errorf("未許可dataset %q が含まれています", actual.Dataset)
			continue
		}
		if actual.Service != want.service || actual.Path != want.path || actual.Route != want.route ||
			actual.Pagination != want.pagination || actual.Normalizer != want.normalizer || actual.RateClass != want.rateClass {
			t.Errorf("dataset %q のroute仕様 = service:%q path:%q route:%q pagination:%q normalizer:%q rate:%q, 期待 = %+v", actual.Dataset, actual.Service, actual.Path, actual.Route, actual.Pagination, actual.Normalizer, actual.RateClass, want)
		}
		if !slices.Equal(actual.QueryNames, want.queries) {
			t.Errorf("dataset %q のquery許可リスト = %v, %vを期待", actual.Dataset, actual.QueryNames, want.queries)
		}
	}

	for dataset := range expected {
		if _, found := seen[dataset]; !found {
			t.Errorf("dataset %q がendpointSpecsにありません", dataset)
		}
	}
}

// TestEndpointSpecsStayInsidePublicGETBoundary は、固定仕様が公開読取境界から外れないことを検証します。
//
// 機能:
//   - 任意URLを許可せず相対固定pathだけを保持することを確認する
//   - 認証、注文、取消、残高、通知、資産移動系pathの混入を検出する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestEndpointSpecsStayInsidePublicGETBoundary(t *testing.T) {
	blockedFragments := []string{"/auth", "/order", "/cancel", "/balance", "/allowance", "/notification", "/relayer", "/bridge", "/data/orders", "/data/trades"}
	for _, spec := range endpointSpecs {
		if !strings.HasPrefix(spec.Path, "/") || strings.Contains(spec.Path, "://") || strings.ContainsAny(spec.Path, "?#") {
			t.Errorf("dataset %q の固定pathが不正です: %q", spec.Dataset, spec.Path)
		}
		for _, blocked := range blockedFragments {
			if strings.Contains(spec.Path, blocked) {
				t.Errorf("dataset %q に非公開または更新系path %q が含まれています", spec.Dataset, spec.Path)
			}
		}
		if spec.Service != serviceGamma && spec.Service != serviceCLOB && spec.Service != serviceData {
			t.Errorf("dataset %q のservice = %q, 公開3 APIだけを期待", spec.Dataset, spec.Service)
		}
	}
}

// TestEndpointParameterBoundsAndArrayEncoding は、公式上限と配列query方式を検証します。
//
// 機能:
//   - Gamma keysetのevents最大500件とmarkets最大100件を区別する
//   - Data APIのexplode=false配列をCSV、Gammaの既定explode=true配列を反復queryとして固定する
//   - marketsの単一tag_idと公式反復tag_idsが同じ上流名を使うことを固定する
//   - eventsとmarketsのorderをローカルenumで狭めないことを確認する
//   - user_activityのtype配列の公式上限12件を固定する
//   - market-positionsが複数配列ではなく単一condition hashを要求することを確認する
//   - query形式だけを使うCLOB補助endpointでtoken IDを必須にする
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestEndpointParameterBoundsAndArrayEncoding(t *testing.T) {
	eventsLimit := mustParameter(t, mustEndpoint(t, "events"), "limit")
	marketsLimit := mustParameter(t, mustEndpoint(t, "markets"), "limit")
	if eventsLimit.Maximum == nil || *eventsLimit.Maximum != 500 {
		t.Errorf("events.limit maximum = %v, 500を期待", eventsLimit.Maximum)
	}
	if marketsLimit.Maximum == nil || *marketsLimit.Maximum != 100 {
		t.Errorf("markets.limit maximum = %v, 100を期待", marketsLimit.Maximum)
	}

	for _, dataset := range []string{"user_positions", "user_activity", "trades", "closed_positions", "holders", "position_value", "open_interest"} {
		spec := mustEndpoint(t, dataset)
		for _, parameter := range spec.Parameters {
			if parameter.Type != typeStringArray && parameter.Type != typeIntegerArray {
				continue
			}
			if parameter.Encoding != encodingCSV {
				t.Errorf("dataset %q の配列 %q encoding = %q, CSVを期待", dataset, parameter.Name, parameter.Encoding)
			}
		}
	}

	for _, dataset := range []string{"series", "teams"} {
		spec := mustEndpoint(t, dataset)
		for _, parameter := range spec.Parameters {
			if parameter.Type != typeStringArray && parameter.Type != typeIntegerArray {
				continue
			}
			if parameter.Encoding != encodingRepeat {
				t.Errorf("dataset %q の配列 %q encoding = %q, query反復を期待", dataset, parameter.Name, parameter.Encoding)
			}
		}
	}

	marketsSpec := mustEndpoint(t, "markets")
	tagID := mustParameter(t, marketsSpec, "tag_id")
	tagIDs := mustParameter(t, marketsSpec, "tag_ids")
	if tagID.Type != typeInteger || tagID.UpstreamName != "tag_id" {
		t.Errorf("markets.tag_id = %+v, 単一integerの上流tag_idを期待", tagID)
	}
	if tagIDs.Type != typeIntegerArray || tagIDs.UpstreamName != "tag_id" || tagIDs.Encoding != encodingRepeat || tagIDs.MaxItems != 100 {
		t.Errorf("markets.tag_ids = %+v, 最大100件の反復tag_idを期待", tagIDs)
	}

	for _, dataset := range []string{"events", "markets"} {
		order := mustParameter(t, mustEndpoint(t, dataset), "order")
		if len(order.Allowed) != 0 || order.Default != "volume24hr" || order.MaxLength != 100 {
			t.Errorf("dataset %qのorder = %+v, 任意文字列・最大100・既定volume24hrを期待", dataset, order)
		}
	}

	activityTypes := mustParameter(t, mustEndpoint(t, "user_activity"), "types")
	if activityTypes.MaxItems != 12 || len(activityTypes.Allowed) != 12 {
		t.Errorf("user_activity.types = %+v, 公式12種類・最大12件を期待", activityTypes)
	}

	market := mustParameter(t, mustEndpoint(t, "market_positions"), "market")
	if !market.Required || market.Type != typeString || market.Validator != validatorCondition {
		t.Errorf("market_positions.market = %+v, 必須の単一condition hashを期待", market)
	}

	for _, dataset := range []string{"tick_size", "fee_rate", "negative_risk"} {
		token := mustParameter(t, mustEndpoint(t, dataset), "token_id")
		if !token.Required || token.Validator != validatorToken {
			t.Errorf("dataset %q のtoken_id = %+v, 必須tokenを期待", dataset, token)
		}
	}
}

// TestDatasetDescriptorMatchesEndpointSpec は、公開Descriptorが内部仕様を欠落なく写すことを検証します。
//
// 機能:
//   - dataset名、説明、parameter名・型・必須・許可値・既定値を照合する
//   - Allowedのsliceを複製し内部仕様を外部変更から保護することを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestDatasetDescriptorMatchesEndpointSpec(t *testing.T) {
	for _, spec := range endpointSpecs {
		descriptor := datasetDescriptor(spec)
		if descriptor.Name != spec.Dataset || descriptor.Description != spec.Description {
			t.Errorf("dataset %q のDescriptor基本情報 = %+v", spec.Dataset, descriptor)
		}
		if len(descriptor.Parameters) != len(spec.Parameters) {
			t.Errorf("dataset %q のDescriptor parameter数 = %d, %dを期待", spec.Dataset, len(descriptor.Parameters), len(spec.Parameters))
			continue
		}
		for index, parameter := range spec.Parameters {
			actual := descriptor.Parameters[index]
			if actual.Name != parameter.Name || actual.Type != string(parameter.Type) || actual.Required != parameter.Required ||
				actual.Description != parameter.Description || !reflect.DeepEqual(actual.Allowed, parameter.Allowed) || !reflect.DeepEqual(actual.Default, parameter.Default) {
				t.Errorf("dataset %q parameter %q のDescriptor = %+v, spec = %+v", spec.Dataset, parameter.Name, actual, parameter)
			}
		}
	}

	spec := mustEndpoint(t, "token_price")
	descriptor := datasetDescriptor(spec)
	descriptor.Parameters[1].Allowed[0] = "変更値"
	if spec.Parameters[1].Allowed[0] == "変更値" {
		t.Error("DescriptorのAllowed変更が内部endpointSpecへ伝播しました")
	}
}

// TestParameterDurationUsesHalfOfficialRate は、公式quotaの50パーセント以下になる間隔を検証します。
//
// 機能:
//   - 10秒windowの代表quotaを20秒へ均等配置する
//   - 割り切れないdurationを切り上げ、50パーセントを超えないことを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestParameterDurationUsesHalfOfficialRate(t *testing.T) {
	tests := []struct {
		name     string
		requests int
		want     time.Duration
	}{
		{name: "CLOB book・price・midpoint", requests: 1500, want: 13_333_334 * time.Nanosecond},
		{name: "Gamma tags・comments", requests: 200, want: 100 * time.Millisecond},
		{name: "CLOB general", requests: 9000, want: 2_222_223 * time.Nanosecond},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			actual := parameterDuration(testCase.requests, 10*time.Second)
			if actual != testCase.want {
				t.Errorf("parameterDuration(%d, 10s) = %s, %sを期待", testCase.requests, actual, testCase.want)
			}
			if int64(actual)*int64(testCase.requests) < int64(20*time.Second) {
				t.Errorf("要求開始間隔 %s は公式quotaの50パーセントを超えます", actual)
			}
		})
	}
}

// mustEndpoint は、dataset名に一致する固定仕様を返します。
//
// 機能:
//   - endpointSpecsから完全一致するdatasetを検索する
//   - 未定義datasetをテスト失敗として即時終了する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - dataset string: 検索するdataset名
//
// 返り値:
//   - endpointSpec: 一致した固定仕様
func mustEndpoint(t *testing.T, dataset string) endpointSpec {
	t.Helper()
	for _, spec := range endpointSpecs {
		if spec.Dataset == dataset {
			return spec
		}
	}
	t.Fatalf("dataset %q がendpointSpecsにありません", dataset)
	return endpointSpec{}
}

// mustParameter は、公開名に一致するparameter仕様を返します。
//
// 機能:
//   - 指定endpoint内からparameter名を完全一致で検索する
//   - 未定義parameterをテスト失敗として即時終了する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - spec endpointSpec: 検索対象endpoint
//   - name string: 検索する公開parameter名
//
// 返り値:
//   - parameterSpec: 一致したparameter仕様
func mustParameter(t *testing.T, spec endpointSpec, name string) parameterSpec {
	t.Helper()
	for _, parameter := range spec.Parameters {
		if parameter.Name == name {
			return parameter
		}
	}
	t.Fatalf("dataset %q にparameter %q がありません", spec.Dataset, name)
	return parameterSpec{}
}
