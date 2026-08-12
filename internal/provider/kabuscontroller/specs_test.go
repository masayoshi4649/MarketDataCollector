package kabuscontroller

import (
	"reflect"
	"testing"
)

/*
TestEndpointSpecsMatchKabusControllerReadOnlyAPI は、公開する6件の固定endpoint仕様を検証します。

機能:
  - dataset、GET path、symbol必須状態がKabusController読取APIと完全一致することを確認する
  - datasetとpathが重複せず、client用endpoint表を構築できることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestEndpointSpecsMatchKabusControllerReadOnlyAPI(t *testing.T) {
	want := []endpointSpec{
		{
			Dataset:     "future_registrations",
			Description: "KabusControllerへ登録されている先物一覧を取得します。",
			Path:        "/api/trade/registrations/future",
		},
		{
			Dataset:     "option_registrations",
			Description: "KabusControllerへ登録されているオプション一覧を取得します。",
			Path:        "/api/trade/registrations/option",
		},
		{
			Dataset:     "market_data",
			Description: "登録中の先物・オプションすべての板情報を取得します。",
			Path:        "/api/trade/market-data",
		},
		{
			Dataset:     "future_market_data",
			Description: "登録中の先物だけの板情報を取得します。",
			Path:        "/api/trade/market-data/future",
		},
		{
			Dataset:     "option_market_data",
			Description: "登録中のオプションだけの板情報を取得します。",
			Path:        "/api/trade/market-data/option",
		},
		{
			Dataset:        "symbol_market_data",
			Description:    "指定した先物またはオプション1銘柄の板情報を取得します。",
			Path:           "/api/trade/market-data/:symbol",
			RequiresSymbol: true,
		},
	}

	if !reflect.DeepEqual(endpointSpecs, want) {
		t.Fatalf("endpointSpecs = %#v, 期待値 = %#v", endpointSpecs, want)
	}
	endpoints, err := buildClientEndpoints()
	if err != nil {
		t.Fatalf("buildClientEndpoints() error = %v", err)
	}
	if len(endpoints) != len(want) {
		t.Errorf("client endpoint件数 = %d, %dを期待", len(endpoints), len(want))
	}
}
