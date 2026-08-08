package polymarket

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// TestNormalizeResponseDoesNotMutateInput は、すべての基礎となるJSON複製性を検証します。
//
// 機能:
//   - map、array、その内側のmapを再帰的に複製する
//   - 正規化結果を変更してもAPIClientの元応答を変更しない
//   - json.Numberをfloat64化せず表現と精度を保持する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNormalizeResponseDoesNotMutateInput(t *testing.T) {
	input := map[string]any{
		"large":  json.Number("900719925474099312345678901234567890"),
		"nested": map[string]any{"name": "original"},
		"items":  []any{map[string]any{"value": json.Number("0.12345678901234567890")}},
	}
	normalized, ok := normalizeResponse(normalizeRaw, input, nil).(map[string]any)
	if !ok {
		t.Fatalf("normalizeResponse() = %#v, mapを期待", normalized)
	}
	if normalized["large"] != json.Number("900719925474099312345678901234567890") {
		t.Errorf("large = %#v, json.Numberの保持を期待", normalized["large"])
	}
	normalized["nested"].(map[string]any)["name"] = "changed"
	normalized["items"].([]any)[0].(map[string]any)["value"] = json.Number("1")
	if input["nested"].(map[string]any)["name"] != "original" || input["items"].([]any)[0].(map[string]any)["value"] != json.Number("0.12345678901234567890") {
		t.Errorf("正規化結果の変更が元応答へ伝播しました: %#v", input)
	}
}

// TestNormalizeMarketDetailsExpandsJSONStringArrays は、Gamma市場詳細の二重符号化配列を検証します。
//
// 機能:
//   - outcomes、outcomePrices、clobTokenIdsのJSON文字列を配列へ展開する
//   - 同じ添字の結果、価格、token IDをoutcomeTokensへ結合する
//   - 100桁のtoken IDを数値変換せず文字列で保持する
//   - 元応答のJSON文字列を書き換えない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNormalizeMarketDetailsExpandsJSONStringArrays(t *testing.T) {
	hugeToken := repeatText("9", 100)
	input := map[string]any{
		"id":            "market-1",
		"outcomes":      `["Yes","No"]`,
		"outcomePrices": `["0.004000000000000000001","0.995999999999999999999"]`,
		"clobTokenIds":  `["` + hugeToken + `","2"]`,
	}
	actual, ok := normalizeResponse(normalizeMarket, input, nil).(map[string]any)
	if !ok {
		t.Fatalf("normalizeResponse() = %#v, mapを期待", actual)
	}
	if !reflect.DeepEqual(actual["outcomes"], []string{"Yes", "No"}) || !reflect.DeepEqual(actual["outcomePrices"], []string{"0.004000000000000000001", "0.995999999999999999999"}) || !reflect.DeepEqual(actual["clobTokenIds"], []string{hugeToken, "2"}) {
		t.Errorf("展開配列 = outcomes:%#v prices:%#v tokens:%#v", actual["outcomes"], actual["outcomePrices"], actual["clobTokenIds"])
	}
	tokens, ok := actual["outcomeTokens"].([]map[string]any)
	if !ok || len(tokens) != 2 || tokens[0]["tokenId"] != hugeToken || tokens[0]["outcome"] != "Yes" || tokens[0]["price"] != "0.004000000000000000001" {
		t.Errorf("outcomeTokens = %#v", actual["outcomeTokens"])
	}
	if input["clobTokenIds"] != `["`+hugeToken+`","2"]` {
		t.Errorf("元clobTokenIdsが変更されました: %#v", input["clobTokenIds"])
	}
}

// TestNormalizeEventListLimitsMarketPreview は、一覧用イベント要約の5件プレビューを検証します。
//
// 機能:
//   - 市場総数はmarketCountに保持する
//   - プレビューを先頭5件に限定しmarketsTruncatedをtrueにする
//   - 上流next_cursorと要約タグを保持する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNormalizeEventListLimitsMarketPreview(t *testing.T) {
	markets := make([]any, 6)
	for index := range markets {
		markets[index] = map[string]any{"id": json.Number(string(rune('1' + index))), "question": "question"}
	}
	input := map[string]any{"events": []any{map[string]any{"id": "event-1", "markets": markets, "tags": []any{map[string]any{"id": "tag-1", "label": "News", "ignored": "value"}}}}, "next_cursor": "cursor-2"}
	actual, ok := normalizeResponse(normalizeEvents, input, nil).(map[string]any)
	if !ok || actual["next_cursor"] != "cursor-2" {
		t.Fatalf("event list = %#v", actual)
	}
	events, ok := actual["events"].([]map[string]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events = %#v", actual["events"])
	}
	event := events[0]
	preview, ok := event["markets"].([]map[string]any)
	if !ok || len(preview) != eventMarketPreviewLimit || event["marketCount"] != 6 || event["marketsTruncated"] != true {
		t.Errorf("イベント要約 = %#v", event)
	}
	if len(input["events"].([]any)[0].(map[string]any)["markets"].([]any)) != 6 {
		t.Error("元イベントの市場配列が短縮されました")
	}
}

// TestNormalizeOrderBookSortsArbitraryPrecisionPrices は、CLOB注文板の価格順と最良気配を検証します。
//
// 機能:
//   - bidsを任意精度の価格降順、asksを価格昇順へ並べる
//   - 上流配列の先頭や末尾に依存せずmax bidとmin askを選ぶ
//   - 行全体をbest_bid、best_askに保持する
//   - 元応答の並びを変更しない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNormalizeOrderBookSortsArbitraryPrecisionPrices(t *testing.T) {
	input := map[string]any{
		"bids": []any{
			map[string]any{"price": "0.0040000000000000000001", "size": "10"},
			map[string]any{"price": "0.0040000000000000000003", "size": "30"},
			map[string]any{"price": "0.0040000000000000000002", "size": "20"},
			map[string]any{"price": json.Number("0.99"), "size": "non-string-price"},
		},
		"asks": []any{
			map[string]any{"price": "0.0150000000000000000003", "size": "30"},
			map[string]any{"price": "0.0150000000000000000001", "size": "10"},
			map[string]any{"price": "0.0150000000000000000002", "size": "20"},
			map[string]any{"price": "invalid", "size": "invalid-decimal"},
			map[string]any{"size": "missing-price"},
		},
	}
	actual, ok := normalizeResponse(normalizeBook, input, nil).(map[string]any)
	if !ok {
		t.Fatalf("order book = %#v", actual)
	}
	bids := actual["bids"].([]map[string]any)
	asks := actual["asks"].([]map[string]any)
	if len(bids) != 3 || len(asks) != 3 {
		t.Fatalf("有効な注文板行数 = bids:%d asks:%d, それぞれ3を期待: %#v", len(bids), len(asks), actual)
	}
	if pricesOfRows(bids)[0] != "0.0040000000000000000003" || pricesOfRows(bids)[2] != "0.0040000000000000000001" {
		t.Errorf("bids = %#v, 降順を期待", bids)
	}
	if pricesOfRows(asks)[0] != "0.0150000000000000000001" || pricesOfRows(asks)[2] != "0.0150000000000000000003" {
		t.Errorf("asks = %#v, 昇順を期待", asks)
	}
	if actual["best_bid"].(map[string]any)["size"] != "30" || actual["best_ask"].(map[string]any)["size"] != "10" {
		t.Errorf("最良気配 = bid:%#v ask:%#v", actual["best_bid"], actual["best_ask"])
	}
	if input["bids"].([]any)[0].(map[string]any)["price"] != "0.0040000000000000000001" || input["asks"].([]any)[0].(map[string]any)["price"] != "0.0150000000000000000003" {
		t.Errorf("元注文板が並べ替えられました: %#v", input)
	}
}

// TestNormalizeTokenPriceSupportsCurrentAndDocumentedFields は、CLOB価格応答の実稼働差分を検証します。
//
// 機能:
//   - midpointでliveのmid、OpenAPIのmid_price、互換priceの順に値を選ぶ
//   - priceのstring、json.Number、有限floatを精度表現した文字列へ揃える
//   - best bidとbest askのBUY、SELL sideを応答から保持する
//   - 巨大token IDを文字列のまま返す
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNormalizeTokenPriceSupportsCurrentAndDocumentedFields(t *testing.T) {
	hugeToken := repeatText("8", 100)
	tests := []struct {
		name      string
		priceType string
		response  map[string]any
		wantPrice string
		wantSide  any
	}{
		{name: "live midpoint", priceType: "midpoint", response: map[string]any{"mid": "0.25", "mid_price": "0.30", "price": "0.40"}, wantPrice: "0.25"},
		{name: "openapi midpoint", priceType: "midpoint", response: map[string]any{"mid_price": json.Number("0.333333333333333333")}, wantPrice: "0.333333333333333333"},
		{name: "compatible midpoint", priceType: "midpoint", response: map[string]any{"price": float64(0.5)}, wantPrice: "0.5"},
		{name: "best bid", priceType: "best_bid", response: map[string]any{"price": json.Number("0.004"), "side": "BUY"}, wantPrice: "0.004", wantSide: "BUY"},
		{name: "best ask", priceType: "best_ask", response: map[string]any{"price": "0.015", "side": "SELL"}, wantPrice: "0.015", wantSide: "SELL"},
		{name: "last trade float32", priceType: "last_trade", response: map[string]any{"price": float32(0.75)}, wantPrice: "0.75"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			actual, ok := normalizeResponse(normalizeTokenQuote, testCase.response, map[string]any{"token_id": hugeToken, "price_type": testCase.priceType}).(map[string]any)
			if !ok {
				t.Fatalf("token quote = %#v", actual)
			}
			if actual["tokenId"] != hugeToken || actual["priceType"] != testCase.priceType || actual["price"] != testCase.wantPrice {
				t.Errorf("token quote = %#v", actual)
			}
			if testCase.wantSide != nil && actual["side"] != testCase.wantSide {
				t.Errorf("side = %#v, %#vを期待", actual["side"], testCase.wantSide)
			}
		})
	}

	for _, invalid := range []any{math.NaN(), math.Inf(1)} {
		actual := normalizeResponse(normalizeTokenQuote, map[string]any{"price": invalid}, map[string]any{"token_id": "1", "price_type": "last_trade"}).(map[string]any)
		if _, exists := actual["price"]; exists {
			t.Errorf("非有限価格 %#v が公開されました: %#v", invalid, actual)
		}
	}
}

// TestNormalizeSearchKeepsOfficialPagination は、検索応答の公式ページ情報保持を検証します。
//
// 機能:
//   - pagination objectを欠落や推測なく保持する
//   - eventsとtagsを要約し、profilesを再帰複製する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestNormalizeSearchKeepsOfficialPagination(t *testing.T) {
	pagination := map[string]any{"totalPages": json.Number("11"), "totalResults": json.Number("101"), "hasMore": true, "nextPage": json.Number("2")}
	input := map[string]any{"events": []any{}, "tags": []any{}, "profiles": []any{map[string]any{"name": "alice"}}, "pagination": pagination}
	actual := normalizeResponse(normalizeSearch, input, nil).(map[string]any)
	if !reflect.DeepEqual(actual["pagination"], pagination) {
		t.Errorf("pagination = %#v, %#vを期待", actual["pagination"], pagination)
	}
	actual["pagination"].(map[string]any)["totalPages"] = json.Number("99")
	if pagination["totalPages"] != json.Number("11") {
		t.Error("正規化後paginationの変更が元応答へ伝播しました")
	}
}

// pricesOfRows は、注文板行から価格文字列だけを取り出します。
//
// 機能:
//   - 並び順テスト用にprice項目をsliceへ変換する
//
// 引数:
//   - rows []map[string]any: 正規化済み注文板行
//
// 返り値:
//   - []string: 行順の価格文字列
func pricesOfRows(rows []map[string]any) []string {
	result := make([]string, len(rows))
	for index, row := range rows {
		result[index], _ = row["price"].(string)
	}
	return result
}
