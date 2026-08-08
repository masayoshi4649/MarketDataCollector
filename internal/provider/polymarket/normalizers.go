package polymarket

import (
	"encoding/json"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const eventMarketPreviewLimit = 5

var decimalPricePattern = regexp.MustCompile(`^[-+]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][-+]?[0-9]+)?$`)

// ----------------------------------------

// normalizeResponse は、endpoint仕様に応じてPolymarket応答を正規化します。
//
// 機能:
//   - Gamma APIのイベント、市場、検索応答を要約する
//   - CLOBの注文板とトークン価格を安定した共通形式へ変換する
//   - 個別正規化を行わない応答もJSON標準値として再帰的に複製する
//
// 引数:
//   - kind normalizerKind: endpoint仕様で選択された正規化種別
//   - value any: APIからdecodeしたJSON応答
//   - values map[string]any: 検証済みの収集パラメーター
//
// 返り値:
//   - any: 正規化済みのJSON互換値
func normalizeResponse(kind normalizerKind, value any, values map[string]any) any {
	switch kind {
	case normalizeSearch:
		return normalizeSearchResponse(value)
	case normalizeEvents:
		return normalizeEventListResponse(value)
	case normalizeEvent:
		return normalizeEventDetails(value)
	case normalizeMarkets:
		return normalizeMarketListResponse(value)
	case normalizeMarket:
		return normalizeMarketDetails(value)
	case normalizeBook:
		return normalizeOrderBook(value)
	case normalizeTokenQuote:
		return normalizeTokenPrice(value, values)
	case normalizeRaw:
		return normalizeJSONValue(value)
	default:
		return normalizeJSONValue(value)
	}
}

// ----------------------------------------

// normalizeSearchResponse は、Gamma APIの検索応答を要約します。
//
// 機能:
//   - イベントとタグをモデルが扱いやすい主要項目へ縮約する
//   - プロフィールと公式ページ情報を欠落させず保持する
//
// 引数:
//   - value any: Gamma APIの検索応答
//
// 返り値:
//   - any: イベント、タグ、プロフィール、ページ情報を含む正規化値
func normalizeSearchResponse(value any) any {
	normalized := normalizeJSONValue(value)
	response, ok := asRecord(normalized)
	if !ok {
		return normalized
	}

	result := map[string]any{
		"events": normalizeRecordArray(response["events"], summarizeEvent),
		"tags":   normalizeRecordArray(response["tags"], summarizeTag),
	}
	copyExistingField(result, "profiles", response, "profiles")
	copyExistingField(result, "pagination", response, "pagination")
	return result
}

// ----------------------------------------

// normalizeEventListResponse は、Gamma APIのイベント一覧応答を要約します。
//
// 機能:
//   - 一覧内の各イベントを主要項目へ縮約する
//   - 公式のnext_cursorを存在する場合だけ保持する
//
// 引数:
//   - value any: events keyset endpointの応答
//
// 返り値:
//   - any: 要約済みイベントと任意のnext_cursorを含む正規化値
func normalizeEventListResponse(value any) any {
	normalized := normalizeJSONValue(value)
	response, ok := asRecord(normalized)
	if !ok {
		return normalized
	}

	result := map[string]any{
		"events": normalizeRecordArray(response["events"], summarizeEvent),
	}
	copyExistingField(result, "next_cursor", response, "next_cursor")
	return result
}

// ----------------------------------------

// normalizeEventDetails は、Gamma APIのイベント詳細を正規化します。
//
// 機能:
//   - 元のイベント項目を保持したまま市場とタグを要約する
//   - 市場総数をmarketCountとして付加する
//
// 引数:
//   - value any: event詳細endpointの応答
//
// 返り値:
//   - any: 市場配列、タグ配列、市場総数を正規化したイベント詳細
func normalizeEventDetails(value any) any {
	normalized := normalizeJSONValue(value)
	event, ok := asRecord(normalized)
	if !ok {
		return normalized
	}

	result := cloneRecord(event)
	markets := normalizeRecordArray(event["markets"], summarizeMarket)
	result["marketCount"] = len(markets)
	result["markets"] = markets
	result["tags"] = normalizeRecordArray(event["tags"], summarizeTag)
	return result
}

// ----------------------------------------

// normalizeMarketListResponse は、Gamma APIの市場一覧応答を要約します。
//
// 機能:
//   - 一覧内の各市場を結果別トークンを含む主要項目へ縮約する
//   - 公式のnext_cursorを存在する場合だけ保持する
//
// 引数:
//   - value any: markets keyset endpointの応答
//
// 返り値:
//   - any: 要約済み市場と任意のnext_cursorを含む正規化値
func normalizeMarketListResponse(value any) any {
	normalized := normalizeJSONValue(value)
	response, ok := asRecord(normalized)
	if !ok {
		return normalized
	}

	result := map[string]any{
		"markets": normalizeRecordArray(response["markets"], summarizeMarket),
	}
	copyExistingField(result, "next_cursor", response, "next_cursor")
	return result
}

// ----------------------------------------

// normalizeMarketDetails は、Gamma APIの市場詳細にある配列文字列を展開します。
//
// 機能:
//   - outcomes、outcomePrices、clobTokenIdsのJSON文字列配列を展開する
//   - 同じ添字の結果名、価格、CLOBトークンIDをoutcomeTokensへ結合する
//   - トークンIDを数値化せず文字列として保持する
//
// 引数:
//   - value any: market詳細endpointの応答
//
// 返り値:
//   - any: 結果別配列とoutcomeTokensを正規化した市場詳細
func normalizeMarketDetails(value any) any {
	normalized := normalizeJSONValue(value)
	market, ok := asRecord(normalized)
	if !ok {
		return normalized
	}

	outcomes := parseStringArray(market["outcomes"])
	outcomePrices := parseStringArray(market["outcomePrices"])
	clobTokenIDs := parseStringArray(market["clobTokenIds"])
	result := cloneRecord(market)
	result["outcomes"] = outcomes
	result["outcomePrices"] = outcomePrices
	result["clobTokenIds"] = clobTokenIDs
	result["outcomeTokens"] = buildOutcomeTokens(outcomes, outcomePrices, clobTokenIDs)
	return result
}

// ----------------------------------------

// normalizeOrderBook は、CLOB注文板を価格順へ並べて最良気配を付加します。
//
// 機能:
//   - 上流の配列順に依存せずbidsを価格降順、asksを価格昇順へ並べる
//   - 10進価格をmath/big.Ratで比較し、float64への丸めを避ける
//   - 空でない各板の先頭をbest_bidまたはbest_askとして付加する
//
// 引数:
//   - value any: CLOB book endpointの応答
//
// 返り値:
//   - any: 並び替え済み板と最良気配を含む正規化値
func normalizeOrderBook(value any) any {
	normalized := normalizeJSONValue(value)
	orderBook, ok := asRecord(normalized)
	if !ok {
		return normalized
	}

	bids := normalizeOrderRows(orderBook["bids"])
	asks := normalizeOrderRows(orderBook["asks"])
	sort.SliceStable(bids, func(left, right int) bool {
		return compareDecimalStrings(bids[left]["price"].(string), bids[right]["price"].(string)) > 0
	})
	sort.SliceStable(asks, func(left, right int) bool {
		return compareDecimalStrings(asks[left]["price"].(string), asks[right]["price"].(string)) < 0
	})

	result := cloneRecord(orderBook)
	result["bids"] = bids
	result["asks"] = asks
	delete(result, "best_bid")
	delete(result, "best_ask")
	if len(bids) > 0 {
		result["best_bid"] = bids[0]
	}
	if len(asks) > 0 {
		result["best_ask"] = asks[0]
	}
	return result
}

// ----------------------------------------

// normalizeTokenPrice は、CLOBの価格応答を共通形式へ変換します。
//
// 機能:
//   - midpointではmid、mid_price、priceの順に利用可能な値を選ぶ
//   - string、json.Number、有限floatの価格を文字列へ統一する
//   - CLOBトークンIDを数値化せず文字列として保持する
//
// 引数:
//   - value any: price、midpoint、last-trade-price endpointの応答
//   - values map[string]any: token_idとprice_typeを含む検証済み入力
//
// 返り値:
//   - map[string]any: tokenId、priceType、任意のpriceとsideを含む価格情報
func normalizeTokenPrice(value any, values map[string]any) map[string]any {
	normalized := normalizeJSONValue(value)
	response, ok := asRecord(normalized)
	if !ok {
		response = map[string]any{}
	}
	tokenID, _ := values["token_id"].(string)
	priceType, _ := values["price_type"].(string)
	result := map[string]any{
		"tokenId":   tokenID,
		"priceType": priceType,
	}

	var rawPrice any
	var priceExists bool
	if priceType == "midpoint" {
		for _, key := range []string{"mid", "mid_price", "price"} {
			candidate, exists := response[key]
			if exists && candidate != nil {
				rawPrice = candidate
				priceExists = true
				break
			}
		}
	} else {
		rawPrice, priceExists = response["price"]
	}
	if price, valid := normalizeDecimalString(rawPrice); priceExists && valid {
		result["price"] = price
	}
	copyExistingField(result, "side", response, "side")
	return result
}

// ----------------------------------------

// summarizeEvent は、一覧表示用にイベントの主要項目を抽出します。
//
// 機能:
//   - 市場総数を保持しつつ市場プレビューを先頭5件へ制限する
//   - 市場プレビューが省略されたかをmarketsTruncatedで明示する
//   - タグを識別用の主要項目へ縮約する
//
// 引数:
//   - event map[string]any: Gamma APIのイベントオブジェクト
//
// 返り値:
//   - map[string]any: 一覧向けに縮約したイベント
func summarizeEvent(event map[string]any) map[string]any {
	markets := normalizeRecordArray(event["markets"], summarizeMarket)
	previewLength := len(markets)
	if previewLength > eventMarketPreviewLimit {
		previewLength = eventMarketPreviewLimit
	}
	preview := make([]map[string]any, previewLength)
	copy(preview, markets[:previewLength])

	result := make(map[string]any)
	for _, field := range []string{"id", "slug", "title", "startDate", "endDate", "active", "closed", "featured", "restricted", "liquidity", "volume", "volume24hr", "openInterest"} {
		copyExistingField(result, field, event, field)
	}
	result["marketCount"] = len(markets)
	result["markets"] = preview
	result["marketsTruncated"] = len(markets) > eventMarketPreviewLimit
	result["tags"] = normalizeRecordArray(event["tags"], summarizeTag)
	return result
}

// ----------------------------------------

// summarizeMarket は、一覧表示用に市場の主要項目を抽出します。
//
// 機能:
//   - 一覧に必要な状態、流動性、出来高、気配値を抽出する
//   - 結果名、価格、CLOBトークンIDを同じ添字で結合する
//   - liquidityNumとvolumeNumを従来項目より優先する
//
// 引数:
//   - market map[string]any: Gamma APIの市場オブジェクト
//
// 返り値:
//   - map[string]any: 一覧向けに縮約した市場
func summarizeMarket(market map[string]any) map[string]any {
	outcomes := parseStringArray(market["outcomes"])
	outcomePrices := parseStringArray(market["outcomePrices"])
	clobTokenIDs := parseStringArray(market["clobTokenIds"])
	result := make(map[string]any)

	for _, field := range []string{"id", "slug", "question", "conditionId", "startDate", "endDate", "active", "closed", "acceptingOrders", "restricted", "volume24hr", "bestBid", "bestAsk", "lastTradePrice", "spread"} {
		copyExistingField(result, field, market, field)
	}
	copyFirstNonNilField(result, "liquidity", market, "liquidityNum", "liquidity")
	copyFirstNonNilField(result, "volume", market, "volumeNum", "volume")
	copyExistingField(result, "minimumOrderSize", market, "orderMinSize")
	copyExistingField(result, "minimumTickSize", market, "orderPriceMinTickSize")
	result["outcomeTokens"] = buildOutcomeTokens(outcomes, outcomePrices, clobTokenIDs)
	return result
}

// ----------------------------------------

// summarizeTag は、タグの識別に必要な主要項目を抽出します。
//
// 機能:
//   - ID、表示ラベル、slugだけを存在状態ごと保持する
//
// 引数:
//   - tag map[string]any: Gamma APIのタグオブジェクト
//
// 返り値:
//   - map[string]any: 識別用に縮約したタグ
func summarizeTag(tag map[string]any) map[string]any {
	result := make(map[string]any)
	for _, field := range []string{"id", "label", "slug"} {
		copyExistingField(result, field, tag, field)
	}
	return result
}

// ----------------------------------------

// buildOutcomeTokens は、市場の結果別情報を同じ添字で結合します。
//
// 機能:
//   - 3配列の最長要素数まで結果別オブジェクトを生成する
//   - 存在しない添字の項目を補完せず、CLOBトークンIDを文字列で保持する
//
// 引数:
//   - outcomes []string: 市場の結果名
//   - prices []string: 各結果の価格
//   - tokenIDs []string: 各結果のCLOBトークンID
//
// 返り値:
//   - []map[string]any: 結果名、価格、トークンIDを結合した配列
func buildOutcomeTokens(outcomes, prices, tokenIDs []string) []map[string]any {
	itemCount := len(outcomes)
	if len(prices) > itemCount {
		itemCount = len(prices)
	}
	if len(tokenIDs) > itemCount {
		itemCount = len(tokenIDs)
	}
	items := make([]map[string]any, 0, itemCount)
	for index := 0; index < itemCount; index++ {
		item := make(map[string]any)
		if index < len(outcomes) {
			item["outcome"] = outcomes[index]
		}
		if index < len(prices) {
			item["price"] = prices[index]
		}
		if index < len(tokenIDs) {
			item["tokenId"] = tokenIDs[index]
		}
		items = append(items, item)
	}
	return items
}

// ----------------------------------------

// normalizeJSONValue は、JSON標準値を再帰的に複製して安定した形へ揃えます。
//
// 機能:
//   - mapと配列を再帰的に複製し、呼び出し元の応答を変更しない
//   - json.Number、文字列、bool、nilなどのスカラー値を精度を保って維持する
//   - テストや直接呼び出しで使われる型付きJSON配列も[]anyへ揃える
//
// 引数:
//   - value any: 正規化対象のJSON互換値
//
// 返り値:
//   - any: 再帰的に複製したJSON互換値
func normalizeJSONValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, item := range current {
			result[key] = normalizeJSONValue(item)
		}
		return result
	case []any:
		result := make([]any, len(current))
		for index, item := range current {
			result[index] = normalizeJSONValue(item)
		}
		return result
	case []map[string]any:
		result := make([]any, len(current))
		for index, item := range current {
			result[index] = normalizeJSONValue(item)
		}
		return result
	case []string:
		result := make([]any, len(current))
		for index, item := range current {
			result[index] = item
		}
		return result
	case []json.Number:
		result := make([]any, len(current))
		for index, item := range current {
			result[index] = item
		}
		return result
	default:
		return current
	}
}

// ----------------------------------------

// asRecord は、JSON値を文字列キーのオブジェクトへ安全に絞り込みます。
//
// 機能:
//   - map[string]anyだけをJSONオブジェクトとして受け付ける
//
// 引数:
//   - value any: 検査対象のJSON互換値
//
// 返り値:
//   - map[string]any: JSONオブジェクト
//   - bool: JSONオブジェクトである場合はtrue
func asRecord(value any) (map[string]any, bool) {
	record, ok := value.(map[string]any)
	return record, ok
}

// ----------------------------------------

// normalizeRecordArray は、配列内のJSONオブジェクトを指定関数で変換します。
//
// 機能:
//   - オブジェクト以外の配列要素を除外する
//   - 各オブジェクトを再帰複製してから個別の要約関数へ渡す
//
// 引数:
//   - value any: JSON配列候補
//   - normalizer func(map[string]any) map[string]any: 各オブジェクトの正規化関数
//
// 返り値:
//   - []map[string]any: 正規化済みJSONオブジェクト配列
func normalizeRecordArray(value any, normalizer func(map[string]any) map[string]any) []map[string]any {
	items, ok := arrayValues(value)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record, valid := asRecord(normalizeJSONValue(item))
		if valid {
			result = append(result, normalizer(record))
		}
	}
	return result
}

// ----------------------------------------

// parseStringArray は、Gamma APIのJSON文字列配列を通常の文字列配列へ変換します。
//
// 機能:
//   - JSON文字列または既にdecode済みの配列を受け付ける
//   - 文字列要素だけを保持し、巨大なCLOBトークンIDを数値化しない
//   - 不正なJSON、非配列、非文字列要素を安全に除外する
//
// 引数:
//   - value any: JSON文字列または配列候補
//
// 返り値:
//   - []string: 正規化済み文字列配列
func parseStringArray(value any) []string {
	if encoded, ok := value.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			return []string{}
		}
		value = decoded
	}
	if stringsValue, ok := value.([]string); ok {
		result := make([]string, len(stringsValue))
		copy(result, stringsValue)
		return result
	}
	items, ok := arrayValues(value)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, valid := item.(string); valid {
			result = append(result, text)
		}
	}
	return result
}

// ----------------------------------------

// normalizeDecimalString は、対応する価格値を精度保持可能な文字列へ変換します。
//
// 機能:
//   - stringとjson.Numberは元の10進表現をそのまま保持する
//   - float32とfloat64は有限値だけを最短の10進表現へ変換する
//
// 引数:
//   - value any: APIが返した価格候補
//
// 返り値:
//   - string: 正規化した価格文字列
//   - bool: 対応する型かつ有効な価格値の場合はtrue
func normalizeDecimalString(value any) (string, bool) {
	switch price := value.(type) {
	case string:
		return price, true
	case json.Number:
		if price.String() == "" {
			return "", false
		}
		return price.String(), true
	case float32:
		if math.IsNaN(float64(price)) || math.IsInf(float64(price), 0) {
			return "", false
		}
		if price == 0 {
			return "0", true
		}
		return strconv.FormatFloat(float64(price), 'g', -1, 32), true
	case float64:
		if math.IsNaN(price) || math.IsInf(price, 0) {
			return "", false
		}
		if price == 0 {
			return "0", true
		}
		return strconv.FormatFloat(price, 'g', -1, 64), true
	default:
		return "", false
	}
}

// ----------------------------------------

// normalizeOrderRows は、注文板配列から価格文字列を持つ行だけを抽出します。
//
// 機能:
//   - 各注文行を再帰的に複製して入力応答の変更を防ぐ
//   - 価格が有効な10進文字列でない行を除外し、CLOBの10進表現を維持する
//
// 引数:
//   - value any: bidsまたはasksの配列候補
//
// 返り値:
//   - []map[string]any: 有効な価格文字列を持つ注文行
func normalizeOrderRows(value any) []map[string]any {
	items, ok := arrayValues(value)
	if !ok {
		return []map[string]any{}
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, valid := asRecord(normalizeJSONValue(item))
		if !valid {
			continue
		}
		price, valid := row["price"].(string)
		if !valid {
			continue
		}
		if _, valid = parseDecimalString(price); !valid {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// ----------------------------------------

// compareDecimalStrings は、2つの10進価格文字列を任意精度で比較します。
//
// 機能:
//   - math/big.Ratを使って小数桁を丸めず大小を判定する
//   - 解釈できない価格は基礎検証版と同様に0として扱う
//
// 引数:
//   - left string: 左側の価格文字列
//   - right string: 右側の価格文字列
//
// 返り値:
//   - int: leftが小さい場合は負、等しい場合は0、大きい場合は正
func compareDecimalStrings(left, right string) int {
	leftValue, valid := parseDecimalString(left)
	if !valid {
		leftValue = new(big.Rat)
	}
	rightValue, valid := parseDecimalString(right)
	if !valid {
		rightValue = new(big.Rat)
	}
	return leftValue.Cmp(rightValue)
}

// ----------------------------------------

// parseDecimalString は、10進価格文字列を任意精度の有理数へ変換します。
//
// 機能:
//   - 10進整数、小数、指数表現だけを許可し、分数や非10進表現を拒否する
//   - math/big.Ratで全桁を保持したまま比較可能な値を生成する
//
// 引数:
//   - value string: 検査して変換する価格文字列
//
// 返り値:
//   - *big.Rat: 任意精度の価格値
//   - bool: 有効な10進価格文字列の場合はtrue
func parseDecimalString(value string) (*big.Rat, bool) {
	text := strings.TrimSpace(value)
	if !decimalPricePattern.MatchString(text) {
		return nil, false
	}
	return new(big.Rat).SetString(text)
}

// ----------------------------------------

// arrayValues は、対応するJSON配列表現を[]anyへ揃えます。
//
// 機能:
//   - JSON decoderの[]anyとテストで扱いやすい型付き配列を共通化する
//   - 元の配列を変更しない新しい配列を返す
//
// 引数:
//   - value any: JSON配列候補
//
// 返り値:
//   - []any: 配列要素を格納した複製
//   - bool: 対応する配列表現の場合はtrue
func arrayValues(value any) ([]any, bool) {
	switch items := value.(type) {
	case []any:
		result := make([]any, len(items))
		copy(result, items)
		return result, true
	case []map[string]any:
		result := make([]any, len(items))
		for index, item := range items {
			result[index] = item
		}
		return result, true
	case []string:
		result := make([]any, len(items))
		for index, item := range items {
			result[index] = item
		}
		return result, true
	case []json.Number:
		result := make([]any, len(items))
		for index, item := range items {
			result[index] = item
		}
		return result, true
	default:
		return nil, false
	}
}

// ----------------------------------------

// cloneRecord は、JSONオブジェクトの最上位mapを複製します。
//
// 機能:
//   - 既に再帰正規化された項目を保持しつつ上書き可能なmapを生成する
//
// 引数:
//   - source map[string]any: 複製元のJSONオブジェクト
//
// 返り値:
//   - map[string]any: 最上位を複製したJSONオブジェクト
func cloneRecord(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// ----------------------------------------

// copyExistingField は、存在するJSON項目を別名指定可能な形で複製します。
//
// 機能:
//   - 未指定項目と明示的なJSON nullを区別し、存在する項目だけを保持する
//
// 引数:
//   - target map[string]any: 複製先のJSONオブジェクト
//   - targetKey string: 複製先の項目名
//   - source map[string]any: 複製元のJSONオブジェクト
//   - sourceKey string: 複製元の項目名
//
// 返り値:
//   - なし
func copyExistingField(target map[string]any, targetKey string, source map[string]any, sourceKey string) {
	if value, exists := source[sourceKey]; exists {
		target[targetKey] = value
	}
}

// ----------------------------------------

// copyFirstNonNilField は、候補から最初の非nil項目を複製します。
//
// 機能:
//   - JavaScriptのnull合体演算と同様にnilを飛ばしてfallback項目を選ぶ
//
// 引数:
//   - target map[string]any: 複製先のJSONオブジェクト
//   - targetKey string: 複製先の項目名
//   - source map[string]any: 複製元のJSONオブジェクト
//   - sourceKeys ...string: 優先順に並べた複製元の項目名
//
// 返り値:
//   - なし
func copyFirstNonNilField(target map[string]any, targetKey string, source map[string]any, sourceKeys ...string) {
	for _, sourceKey := range sourceKeys {
		if value, exists := source[sourceKey]; exists && value != nil {
			target[targetKey] = value
			return
		}
	}
	if len(sourceKeys) > 0 {
		if value, exists := source[sourceKeys[len(sourceKeys)-1]]; exists {
			target[targetKey] = value
		}
	}
}
