package jquants

import (
	"strings"
	"testing"
)

/*
TestEndpointSpecsAreFixedAndUnique は、API V2の固定許可リストを検証します。

機能:
  - endpoint仕様が対象30 datasetを重複なく定義することを確認する
  - dataset識別子と固定pathが共通serviceおよびAPI V2の形式に従うことを確認する
  - 各datasetに公式仕様slugと日本語説明があることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEndpointSpecsAreFixedAndUnique(t *testing.T) {
	if len(endpointSpecs) != 30 {
		t.Fatalf("endpointSpecs件数 = %d, 30を期待", len(endpointSpecs))
	}
	seen := make(map[string]struct{}, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if _, exists := seen[spec.Dataset]; exists {
			t.Errorf("dataset %qが重複しています", spec.Dataset)
		}
		seen[spec.Dataset] = struct{}{}
		if !validDatasetIdentifier(spec.Dataset) {
			t.Errorf("dataset %qが公開識別子の形式に従っていません", spec.Dataset)
		}
		if !strings.HasPrefix(spec.Path, "/v2/") || strings.ContainsAny(spec.Path, "?#") {
			t.Errorf("dataset %qの固定pathが不正です: %q", spec.Dataset, spec.Path)
		}
		if spec.Specification == "" || spec.Description == "" {
			t.Errorf("dataset %qに仕様slugまたは説明がありません", spec.Dataset)
		}
	}
}

// ----------------------------------------

/*
TestEquitiesTradesUsesOnlyBulkEndpoint は、CSV専用ティック仕様を検証します。

機能:
  - equities_tradesが存在しないRESTデータendpointを呼ばないことを確認する
  - bulk/listへendpoint=/equities/tradesを必ず付ける定義を確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEquitiesTradesUsesOnlyBulkEndpoint(t *testing.T) {
	spec, exists := endpointSpecByDataset("equities_trades")
	if !exists {
		t.Fatal("equities_tradesの固定仕様がありません")
	}
	if spec.Path != "/v2/bulk/list" || spec.ForcedQuery["endpoint"] != "/equities/trades" {
		t.Errorf("equities_trades仕様 = %+v, Bulk一覧への固定変換を期待", spec)
	}
}

// ----------------------------------------

/*
endpointSpecByDataset は、テスト対象の固定endpoint仕様を取得します。

機能:
  - dataset名の完全一致で30件の固定許可リストを検索する

引数:
  - dataset string: 検索するdataset識別子

返り値:
  - endpointSpec: 一致した固定endpoint仕様
  - bool: 一致する仕様が存在する場合はtrue
*/
func endpointSpecByDataset(dataset string) (endpointSpec, bool) {
	for _, spec := range endpointSpecs {
		if spec.Dataset == dataset {
			return spec, true
		}
	}
	return endpointSpec{}, false
}

// ----------------------------------------

/*
validDatasetIdentifier は、dataset名が共通serviceのASCII形式に従うか確認します。

機能:
  - 英小文字、数字、アンダースコア、ハイフンだけを許可する

引数:
  - value string: 検証するdataset名

返り値:
  - bool: 共通serviceから到達可能な形式の場合はtrue
*/
func validDatasetIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
