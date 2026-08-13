package kabuscontroller

import (
	"reflect"
	"strings"
	"testing"
)

/*
TestEndpointSpecsPublishEighteenDatasetsInStableOrder は、公開datasetの完全な順序を検証します。

機能:
  - 既存6 datasetの互換順序を維持する
  - 新規12 datasetを要求された順序で追加していることを確認する
  - dataset名の重複と説明不足を検出する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEndpointSpecsPublishEighteenDatasetsInStableOrder(t *testing.T) {
	wantNames := []string{
		"future_registrations",
		"option_registrations",
		"market_data",
		"future_market_data",
		"option_market_data",
		"symbol_market_data",
		"kabus_ranking",
		"kabus_regulations",
		"derivative_symbol_resolver",
		"nt_pair_symbol_resolver",
		"arbitrary_board_snapshot",
		"option_chain_snapshot",
		"kabus_symbol_info",
		"kabus_primary_exchange",
		"kabus_fx_snapshot",
		"kabus_margin_premium",
		"kabus_api_soft_limits",
		"kabus_api_capacity",
	}
	if len(endpointSpecs) != len(wantNames) {
		t.Fatalf("endpoint件数 = %d, %dを期待", len(endpointSpecs), len(wantNames))
	}

	seen := make(map[string]struct{}, len(endpointSpecs))
	for index, spec := range endpointSpecs {
		if spec.Dataset != wantNames[index] {
			t.Errorf("endpointSpecs[%d].Dataset = %q, %qを期待", index, spec.Dataset, wantNames[index])
		}
		if strings.TrimSpace(spec.Description) == "" {
			t.Errorf("dataset %qのDescriptionが空です", spec.Dataset)
		}
		if _, exists := seen[spec.Dataset]; exists {
			t.Errorf("dataset %qが重複しています", spec.Dataset)
		}
		seen[spec.Dataset] = struct{}{}
	}
}

// ----------------------------------------

/*
TestEndpointSpecsPreserveControllerAPIContracts は、既存KabusController APIの公開契約を検証します。

機能:
  - 既存6 datasetの固定pathとendpoint種別を確認する
  - 板datasetにBid・Ask逆転注意を付ける
  - 個別板だけがcontroller用symbolを要求してNOT_FOUND対象になることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEndpointSpecsPreserveControllerAPIContracts(t *testing.T) {
	want := []struct {
		name            string
		path            string
		kind            endpointKind
		bidAskReversed  bool
		parameterLength int
	}{
		{name: "future_registrations", path: "/api/trade/registrations/future", kind: kindFixed},
		{name: "option_registrations", path: "/api/trade/registrations/option", kind: kindFixed},
		{name: "market_data", path: "/api/trade/market-data", kind: kindFixed, bidAskReversed: true},
		{name: "future_market_data", path: "/api/trade/market-data/future", kind: kindFixed, bidAskReversed: true},
		{name: "option_market_data", path: "/api/trade/market-data/option", kind: kindFixed, bidAskReversed: true},
		{name: "symbol_market_data", path: "/api/trade/market-data/:symbol", kind: kindControllerSymbol, bidAskReversed: true, parameterLength: 1},
	}
	for _, expected := range want {
		spec := requireEndpointSpec(t, expected.name)
		if spec.Path != expected.path || spec.Kind != expected.kind ||
			spec.BidAskReversed != expected.bidAskReversed || len(spec.Parameters) != expected.parameterLength {
			t.Errorf("dataset %q = %+v, path=%q kind=%q bidAsk=%t parameters=%dを期待", expected.name, spec, expected.path, expected.kind, expected.bidAskReversed, expected.parameterLength)
		}
		if spec.StandardInfo || spec.MayRegisterSymbol {
			t.Errorf("既存Controller dataset %qの標準情報APIフラグ = %+v, falseを期待", expected.name, spec)
		}
	}

	symbolDataset := requireEndpointSpec(t, "symbol_market_data")
	parameter := requireParameterSpec(t, symbolDataset, "symbol")
	if !parameter.Required || parameter.Type != typeString || parameter.Validator != validatorControllerSymbol || !symbolDataset.NotFound {
		t.Errorf("symbol_market_data仕様 = endpoint:%+v parameter:%+v", symbolDataset, parameter)
	}
}

// ----------------------------------------

/*
TestEndpointSpecsPublishKabusStandardInformationInputs は、標準情報APIの入力と安全フラグを検証します。

機能:
  - ランキング、規制、板、銘柄、優先市場、為替、プレミアム料、ソフトリミットの固定仕様を確認する
  - 銘柄指定APIの自動登録可能性と板のBid・Ask逆転注意を公開する
  - 列挙値、既定値、上流query名を固定する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEndpointSpecsPublishKabusStandardInformationInputs(t *testing.T) {
	ranking := requireEndpointSpec(t, "kabus_ranking")
	if ranking.Path != "/kabusapi/ranking" || ranking.Kind != kindFixed || !ranking.StandardInfo || ranking.MayRegisterSymbol {
		t.Errorf("kabus_ranking = %+v", ranking)
	}
	rankingType := requireParameterSpec(t, ranking, "ranking_type")
	if !rankingType.Required || rankingType.QueryName != "Type" ||
		!reflect.DeepEqual(rankingType.Allowed, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}) {
		t.Errorf("ranking_type = %+v", rankingType)
	}
	exchangeDivision := requireParameterSpec(t, ranking, "exchange_division")
	if exchangeDivision.Default != "ALL" || exchangeDivision.QueryName != "ExchangeDivision" ||
		!reflect.DeepEqual(exchangeDivision.Allowed, []string{"ALL", "T", "TP", "TS", "TG", "M", "FK", "S"}) {
		t.Errorf("exchange_division = %+v", exchangeDivision)
	}

	regulations := requireEndpointSpec(t, "kabus_regulations")
	if regulations.Path != "/kabusapi/regulations/:symbol" || regulations.Kind != kindSymbolExchange ||
		!regulations.NotFound || !regulations.MayRegisterSymbol || !regulations.StandardInfo {
		t.Errorf("kabus_regulations = %+v", regulations)
	}
	assertSymbolAndExchange(t, regulations, []string{"1", "3", "5", "6"}, false, "1")

	board := requireEndpointSpec(t, "arbitrary_board_snapshot")
	if board.Path != "/kabusapi/board/:symbol" || board.Kind != kindSymbolExchange ||
		!board.NotFound || !board.MayRegisterSymbol || !board.StandardInfo || !board.BidAskReversed {
		t.Errorf("arbitrary_board_snapshot = %+v", board)
	}
	assertSymbolAndExchange(t, board, []string{"1", "3", "5", "6", "2", "23", "24"}, true, nil)

	symbolInfo := requireEndpointSpec(t, "kabus_symbol_info")
	if symbolInfo.Path != "/kabusapi/symbol/:symbol" || symbolInfo.Kind != kindSymbolExchange ||
		!symbolInfo.NotFound || !symbolInfo.MayRegisterSymbol || !symbolInfo.StandardInfo {
		t.Errorf("kabus_symbol_info = %+v", symbolInfo)
	}
	assertSymbolAndExchange(t, symbolInfo, []string{"1", "3", "5", "6", "2", "23", "24"}, true, nil)
	addInfo := requireParameterSpec(t, symbolInfo, "add_info")
	if addInfo.Type != typeBoolean || addInfo.Required || addInfo.Default != true || addInfo.QueryName != "addinfo" {
		t.Errorf("add_info = %+v", addInfo)
	}

	primary := requireEndpointSpec(t, "kabus_primary_exchange")
	margin := requireEndpointSpec(t, "kabus_margin_premium")
	for _, spec := range []endpointSpec{primary, margin} {
		if spec.Kind != kindPlainSymbol || !spec.NotFound || !spec.MayRegisterSymbol || !spec.StandardInfo {
			t.Errorf("plain symbol dataset %q = %+v", spec.Dataset, spec)
		}
		parameter := requireParameterSpec(t, spec, "symbol")
		if parameter.Validator != validatorSecuritySymbol || !parameter.Required {
			t.Errorf("dataset %qのsymbol = %+v", spec.Dataset, parameter)
		}
	}
	if primary.Path != "/kabusapi/primaryexchange/:symbol" || margin.Path != "/kabusapi/margin/marginpremium/:symbol" {
		t.Errorf("plain symbol path = primary:%q margin:%q", primary.Path, margin.Path)
	}

	fx := requireEndpointSpec(t, "kabus_fx_snapshot")
	if fx.Path != "/kabusapi/exchange/:pair" || fx.Kind != kindPair || !fx.StandardInfo || fx.MayRegisterSymbol {
		t.Errorf("kabus_fx_snapshot = %+v", fx)
	}
	pair := requireParameterSpec(t, fx, "pair")
	if pair.Default != "usdjpy" || !reflect.DeepEqual(pair.Allowed, []string{
		"usdjpy", "eurjpy", "gbpjpy", "audjpy", "chfjpy", "cadjpy",
		"nzdjpy", "zarjpy", "eurusd", "gbpusd", "audusd",
	}) {
		t.Errorf("pair = %+v", pair)
	}

	softLimits := requireEndpointSpec(t, "kabus_api_soft_limits")
	if softLimits.Path != "/kabusapi/apisoftlimit" || softLimits.Kind != kindFixed ||
		!softLimits.StandardInfo || softLimits.MayRegisterSymbol || len(softLimits.Parameters) != 0 {
		t.Errorf("kabus_api_soft_limits = %+v", softLimits)
	}
	if !strings.Contains(softLimits.Description, "1注文") || !strings.Contains(softLimits.Description, "登録残枠ではありません") {
		t.Errorf("kabus_api_soft_limitsの説明 = %q", softLimits.Description)
	}

	capacity := requireEndpointSpec(t, "kabus_api_capacity")
	if capacity.Path != "" || capacity.Kind != kindComposite || capacity.StandardInfo ||
		capacity.MayRegisterSymbol || capacity.NotFound || len(capacity.Parameters) != 0 {
		t.Errorf("kabus_api_capacity = %+v", capacity)
	}
	for _, expectedText := range []string{"登録残枠の上限", "株式", "PUSH", "他クライアント", "正確な残枠ではありません"} {
		if !strings.Contains(capacity.Description, expectedText) {
			t.Errorf("kabus_api_capacityの説明 = %q, %qを含むことを期待", capacity.Description, expectedText)
		}
	}
}

// ----------------------------------------

/*
TestEndpointSpecsPublishResolverAndCompositeInputs は、resolverと複合datasetの入力を検証します。

機能:
  - 単一デリバティブの条件付き入力と上流query名を確認する
  - NT両脚で明示限月を必須とし、日経商品に安全な既定値を設定する
  - オプションチェーンの中心価格と上下本数の範囲を固定する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEndpointSpecsPublishResolverAndCompositeInputs(t *testing.T) {
	resolver := requireEndpointSpec(t, "derivative_symbol_resolver")
	if resolver.Path != "/kabusapi/symbolname" || resolver.Kind != kindResolver ||
		!resolver.NotFound || !resolver.MayRegisterSymbol || !resolver.StandardInfo {
		t.Errorf("derivative_symbol_resolver = %+v", resolver)
	}
	kind := requireParameterSpec(t, resolver, "kind")
	if !kind.Required || !reflect.DeepEqual(kind.Allowed, []string{"future", "option", "mini_option_weekly"}) {
		t.Errorf("resolver kind = %+v", kind)
	}
	productCode := requireParameterSpec(t, resolver, "product_code")
	if productCode.Required || !reflect.DeepEqual(productCode.Allowed, []string{
		"NK225", "NK225mini", "TOPIX", "TOPIXmini", "GROWTH", "JPX400",
		"DOW", "VI", "Core30", "REIT", "NK225micro", "NK225op", "NK225miniop",
	}) {
		t.Errorf("product_code = %+v", productCode)
	}
	derivMonth := requireParameterSpec(t, resolver, "deriv_month")
	if !derivMonth.Required || derivMonth.QueryName != "DerivMonth" || derivMonth.Minimum == nil ||
		minimumValue(derivMonth) != 0 || derivMonth.Maximum == nil || maximumValue(derivMonth) != 999999 {
		t.Errorf("resolver deriv_month = %+v", derivMonth)
	}
	putOrCall := requireParameterSpec(t, resolver, "put_or_call")
	if putOrCall.QueryName != "PutOrCall" || !reflect.DeepEqual(putOrCall.Allowed, []string{"P", "C"}) {
		t.Errorf("put_or_call = %+v", putOrCall)
	}
	strikePrice := requireParameterSpec(t, resolver, "strike_price")
	if strikePrice.QueryName != "StrikePrice" || strikePrice.Minimum == nil || minimumValue(strikePrice) != 0 {
		t.Errorf("strike_price = %+v", strikePrice)
	}
	derivWeekly := requireParameterSpec(t, resolver, "deriv_weekly")
	if derivWeekly.QueryName != "DerivWeekly" || !reflect.DeepEqual(derivWeekly.Allowed, []string{"0", "1", "3", "4", "5"}) {
		t.Errorf("deriv_weekly = %+v", derivWeekly)
	}

	ntPair := requireEndpointSpec(t, "nt_pair_symbol_resolver")
	if ntPair.Path != "" || ntPair.Kind != kindComposite || !ntPair.NotFound ||
		!ntPair.MayRegisterSymbol || !ntPair.StandardInfo {
		t.Errorf("nt_pair_symbol_resolver = %+v", ntPair)
	}
	ntMonth := requireParameterSpec(t, ntPair, "deriv_month")
	if !ntMonth.Required || ntMonth.Minimum == nil || minimumValue(ntMonth) <= 0 || ntMonth.QueryName != "DerivMonth" {
		t.Errorf("NT deriv_month = %+v", ntMonth)
	}
	nikkeiProduct := requireParameterSpec(t, ntPair, "nikkei_product_code")
	if nikkeiProduct.Default != "NK225mini" ||
		!reflect.DeepEqual(nikkeiProduct.Allowed, []string{"NK225mini", "NK225micro"}) {
		t.Errorf("nikkei_product_code = %+v", nikkeiProduct)
	}

	chain := requireEndpointSpec(t, "option_chain_snapshot")
	if chain.Path != "" || chain.Kind != kindComposite || !chain.NotFound ||
		chain.MayRegisterSymbol || chain.StandardInfo || !chain.BidAskReversed {
		t.Errorf("option_chain_snapshot = %+v", chain)
	}
	optionCode := requireParameterSpec(t, chain, "option_code")
	chainMonth := requireParameterSpec(t, chain, "deriv_month")
	centerStrike := requireParameterSpec(t, chain, "center_strike")
	strikesEachSide := requireParameterSpec(t, chain, "strikes_each_side")
	if !optionCode.Required || !reflect.DeepEqual(optionCode.Allowed, []string{"NK225op", "NK225miniop"}) ||
		!chainMonth.Required || !centerStrike.Required || centerStrike.Minimum == nil || minimumValue(centerStrike) != 1 ||
		!strings.Contains(centerStrike.Description, "自動判定しません") {
		t.Errorf("option chain必須入力 = code:%+v month:%+v center:%+v", optionCode, chainMonth, centerStrike)
	}
	if strikesEachSide.Required || strikesEachSide.Default != 5 || strikesEachSide.Minimum == nil ||
		minimumValue(strikesEachSide) != 0 || strikesEachSide.Maximum == nil || maximumValue(strikesEachSide) != 20 {
		t.Errorf("strikes_each_side = %+v", strikesEachSide)
	}
}

// ----------------------------------------

/*
TestDatasetDescriptorCopiesAllPublicParametersAndAllowedValues は、datalist変換とスライス分離を検証します。

機能:
  - endpoint仕様の全parameter属性を公開Descriptorへ写す
  - DescriptorのAllowedを書き換えても固定endpoint仕様を変更しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestDatasetDescriptorCopiesAllPublicParametersAndAllowedValues(t *testing.T) {
	spec := requireEndpointSpec(t, "kabus_ranking")
	descriptor := datasetDescriptor(spec)
	if descriptor.Name != spec.Dataset || descriptor.Description != spec.Description ||
		len(descriptor.Parameters) != len(spec.Parameters) {
		t.Fatalf("datasetDescriptor() = %+v, endpoint仕様全体を期待", descriptor)
	}
	for index, parameter := range descriptor.Parameters {
		source := spec.Parameters[index]
		if parameter.Name != source.Name || parameter.Type != string(source.Type) ||
			parameter.Required != source.Required || parameter.Description != source.Description ||
			!reflect.DeepEqual(parameter.Allowed, source.Allowed) || parameter.Default != source.Default ||
			!numberPointersEqual(parameter.Minimum, source.Minimum) ||
			!numberPointersEqual(parameter.Maximum, source.Maximum) {
			t.Errorf("Parameters[%d] = %+v, %+vからの変換を期待", index, parameter, source)
		}
	}

	descriptor.Parameters[0].Allowed[0] = "変更済み"
	if endpointSpecs[6].Parameters[0].Allowed[0] != "1" {
		t.Errorf("DescriptorのAllowed変更がendpointSpecsへ波及しました: %+v", endpointSpecs[6].Parameters[0].Allowed)
	}
	chainSpec := requireEndpointSpec(t, "option_chain_snapshot")
	chainDescriptor := datasetDescriptor(chainSpec)
	for index := range chainDescriptor.Parameters {
		if chainDescriptor.Parameters[index].Name == "strikes_each_side" {
			if chainDescriptor.Parameters[index].Minimum == nil || *chainDescriptor.Parameters[index].Minimum != 0 ||
				chainDescriptor.Parameters[index].Maximum == nil || *chainDescriptor.Parameters[index].Maximum != 20 {
				t.Errorf("strikes_each_sideの公開境界 = %+v, 0..20を期待", chainDescriptor.Parameters[index])
			}
		}
	}
}

// ----------------------------------------

// numberPointersEqual は、数値境界ポインターの値を比較します。
//
// 主な特徴:
//   - 双方nilまたは双方が同値の場合だけtrueを返す
//
// 引数:
//   - left *float64: 左辺の境界
//   - right *float64: 右辺の境界
//
// 返り値:
//   - bool: nil状態と値が一致する場合はtrue
func numberPointersEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// ----------------------------------------

/*
assertSymbolAndExchange は、symbol_exchange datasetの共通入力を検証します。

機能:
  - symbolがsecurity_symbol validator付きの必須文字列であることを確認する
  - exchangeが期待した市場コードだけを許可することを確認する

引数:
  - t *testing.T: テスト状態を管理する値
  - spec endpointSpec: 検証するdataset仕様
  - exchanges []string: 期待する市場コード一覧
  - required bool: exchangeを必須とする場合はtrue
  - defaultValue any: exchangeの既定値

返り値:
  - なし
*/
func assertSymbolAndExchange(
	t *testing.T,
	spec endpointSpec,
	exchanges []string,
	required bool,
	defaultValue any,
) {
	t.Helper()
	symbol := requireParameterSpec(t, spec, "symbol")
	exchange := requireParameterSpec(t, spec, "exchange")
	if symbol.Type != typeString || !symbol.Required || symbol.Validator != validatorSecuritySymbol {
		t.Errorf("dataset %qのsymbol = %+v", spec.Dataset, symbol)
	}
	if exchange.Type != typeString || exchange.Required != required || exchange.Default != defaultValue ||
		!reflect.DeepEqual(exchange.Allowed, exchanges) {
		t.Errorf("dataset %qのexchange = %+v, Required=%t Default=%v Allowed=%vを期待", spec.Dataset, exchange, required, defaultValue, exchanges)
	}
}

// ----------------------------------------

/*
requireEndpointSpec は、dataset名に一致する固定仕様を取得します。

機能:
  - endpointSpecsを検索し、見つからない場合はテストを即時失敗させる

引数:
  - t *testing.T: テスト状態を管理する値
  - dataset string: 検索するdataset識別子

返り値:
  - endpointSpec: 見つかった固定endpoint仕様
*/
func requireEndpointSpec(t *testing.T, dataset string) endpointSpec {
	t.Helper()
	for _, spec := range endpointSpecs {
		if spec.Dataset == dataset {
			return spec
		}
	}
	t.Fatalf("dataset %qがendpointSpecsにありません", dataset)
	return endpointSpec{}
}

// ----------------------------------------

/*
requireParameterSpec は、dataset内の公開parameter仕様を取得します。

機能:
  - parameter名を検索し、見つからない場合はテストを即時失敗させる

引数:
  - t *testing.T: テスト状態を管理する値
  - spec endpointSpec: 検索対象のdataset仕様
  - name string: 検索するparameter名

返り値:
  - parameterSpec: 見つかった公開parameter仕様
*/
func requireParameterSpec(t *testing.T, spec endpointSpec, name string) parameterSpec {
	t.Helper()
	for _, parameter := range spec.Parameters {
		if parameter.Name == name {
			return parameter
		}
	}
	t.Fatalf("dataset %qにparameter %qがありません", spec.Dataset, name)
	return parameterSpec{}
}

// ----------------------------------------

/*
minimumValue は、parameterの最小値をテスト比較用に取得します。

機能:
  - Minimumがnilの場合はテスト比較用の0を返す

引数:
  - parameter parameterSpec: 最小値を確認する入力仕様

返り値:
  - float64: 設定された最小値。未設定時は0
*/
func minimumValue(parameter parameterSpec) float64 {
	if parameter.Minimum == nil {
		return 0
	}
	return *parameter.Minimum
}

// ----------------------------------------

/*
maximumValue は、parameterの最大値をテスト比較用に取得します。

機能:
  - Maximumがnilの場合はテスト比較用の0を返す

引数:
  - parameter parameterSpec: 最大値を確認する入力仕様

返り値:
  - float64: 設定された最大値。未設定時は0
*/
func maximumValue(parameter parameterSpec) float64 {
	if parameter.Maximum == nil {
		return 0
	}
	return *parameter.Maximum
}
