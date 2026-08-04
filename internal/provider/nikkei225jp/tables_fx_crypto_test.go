package nikkei225jp

import (
	"strings"
	"testing"
)

// TestParseCompactCurrent は、5項目の簡易現在値をコード順に正規化できることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseCompactCurrent(t *testing.T) {
	t.Parallel()

	body := []byte("A[523]=\"1.15251_-0.00008_-0.01_05:59_0\";\n" +
		"A[501]=\"99.8372_-0.1609_-0.16_05:59_0\";\n" +
		"A[511]=\"___07/31_\";\n")
	quotes, err := parseCompactCurrent(body)
	if err != nil {
		t.Fatalf("parseCompactCurrent() error = %v", err)
	}
	if len(quotes) != 3 {
		t.Fatalf("len(quotes) = %d", len(quotes))
	}
	if quotes[0].Code != "501" || quotes[0].Name != "ドルインデックス" {
		t.Errorf("quotes[0] = %+v", quotes[0])
	}
	if quotes[1].Code != "511" || quotes[1].Value != nil || quotes[1].DisplayStatus != nil {
		t.Errorf("quotes[1] = %+v", quotes[1])
	}
	if quotes[1].High != nil || quotes[1].Low != nil {
		t.Errorf("簡易現在値の高値または安値がnilではありません: %+v", quotes[1])
	}
	if quotes[2].Value == nil || *quotes[2].Value != 1.15251 {
		t.Errorf("quotes[2].Value = %v", quotes[2].Value)
	}
}

// TestParseCompactCurrentRejectsInvalidData は、未知のJavaScriptや不正な列・数値・重複を拒否することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseCompactCurrentRejectsInvalidData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "代入なし", body: "", want: "代入行"},
		{name: "未知のJavaScript", body: "A[511]=\"1_2_3_04:00_0\";\nalert(1);", want: "未対応"},
		{name: "列不足", body: "A[511]=\"1_2_3_04:00\";", want: "列数"},
		{name: "非有限値", body: "A[511]=\"Inf_2_3_04:00_0\";", want: "有限値"},
		{name: "表示状態", body: "A[511]=\"1_2_3_04:00_open\";", want: "表示状態"},
		{name: "コード重複", body: "A[511]=\"1_2_3_04:00_0\";\nA[511]=\"2_3_4_05:00_1\";", want: "重複"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseCompactCurrent([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseCompactCurrent() error = %v, want %q", err, test.want)
			}
		})
	}
}

// ----------------------------------------

// TestParseCryptoAssets は、仮想通貨一覧の文字列、任意値、件数を正規化できることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseCryptoAssets(t *testing.T) {
	t.Parallel()

	body := []byte(`
var CO=[];
CO[1041]="ETC_イーサリアムクラシック_Ethereum Classic__1647_1044.13_+0.17_-2.79_-23.49_-63.14_";
CO[1001]="BTC_ビットコイン_Bitcoin_1_1992571_9934257_-0.16_-5.54_-20.82_-40.26_";
var LastModCoin="22:17";
var Coincount=2;
`)
	data, err := parseCryptoAssets(body)
	if err != nil {
		t.Fatalf("parseCryptoAssets() error = %v", err)
	}
	if data.LastModified != "22:17" || data.CoinCount != 2 || len(data.Assets) != 2 {
		t.Fatalf("data = %+v", data)
	}
	if data.Assets[0].Code != "1001" || data.Assets[0].Symbol != "BTC" {
		t.Errorf("Assets[0] = %+v", data.Assets[0])
	}
	if data.Assets[0].AvailableInJapan == nil || !*data.Assets[0].AvailableInJapan {
		t.Errorf("Assets[0].AvailableInJapan = %v", data.Assets[0].AvailableInJapan)
	}
	if data.Assets[0].MarketCapHundredMillionJPY == nil || *data.Assets[0].MarketCapHundredMillionJPY != 1992571 {
		t.Errorf("Assets[0].MarketCapHundredMillionJPY = %v", data.Assets[0].MarketCapHundredMillionJPY)
	}
	if data.Assets[1].Code != "1041" || data.Assets[1].AvailableInJapan != nil {
		t.Errorf("Assets[1] = %+v", data.Assets[1])
	}
	if data.Assets[1].PriceJPY == nil || *data.Assets[1].PriceJPY != 1044.13 {
		t.Errorf("Assets[1].PriceJPY = %v", data.Assets[1].PriceJPY)
	}
}

// TestParseCryptoAssetsAllowsOptionalNumbers は、空の数値項目をnilとして保持できることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseCryptoAssetsAllowsOptionalNumbers(t *testing.T) {
	t.Parallel()

	body := []byte("var CO=[];\n" +
		"CO[1881]=\"FLOW_FLOW_FLOW____-5.19_+6.83_-31.56_-91.62_\";\n" +
		"var LastModCoin=\"22:17\";\n" +
		"var Coincount=1;\n")
	data, err := parseCryptoAssets(body)
	if err != nil {
		t.Fatalf("parseCryptoAssets() error = %v", err)
	}
	asset := data.Assets[0]
	if asset.AvailableInJapan != nil || asset.MarketCapHundredMillionJPY != nil || asset.PriceJPY != nil {
		t.Errorf("空欄がnilではありません: %+v", asset)
	}
	if asset.Change24HoursPercent == nil || *asset.Change24HoursPercent != -5.19 {
		t.Errorf("Change24HoursPercent = %v", asset.Change24HoursPercent)
	}
}

// TestParseCryptoAssetsRejectsInvalidData は、文書、列、数値、フラグ、重複、件数の異常を拒否することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseCryptoAssetsRejectsInvalidData(t *testing.T) {
	t.Parallel()

	validAssignment := `CO[1001]="BTC_ビットコイン_Bitcoin_1_1992571_9934257_-0.16_-5.54_-20.82_-40.26_";`
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "文書構造", body: validAssignment, want: "文書構造"},
		{name: "代入なし", body: `var CO=[]; var LastModCoin="22:17"; var Coincount=0;`, want: "代入行"},
		{name: "未知のJavaScript", body: `var CO=[]; ` + validAssignment + ` alert(1); var LastModCoin="22:17"; var Coincount=1;`, want: "未対応"},
		{name: "末尾区切りなし", body: `var CO=[]; CO[1001]="BTC_ビットコイン_Bitcoin_1_1_1_1_1_1_1"; var LastModCoin="22:17"; var Coincount=1;`, want: "列数"},
		{name: "必須名称なし", body: `var CO=[]; CO[1001]="_ビットコイン_Bitcoin_1_1_1_1_1_1_1_"; var LastModCoin="22:17"; var Coincount=1;`, want: "名称"},
		{name: "国内取扱フラグ", body: `var CO=[]; CO[1001]="BTC_ビットコイン_Bitcoin_2_1_1_1_1_1_1_"; var LastModCoin="22:17"; var Coincount=1;`, want: "国内取扱"},
		{name: "非有限値", body: `var CO=[]; CO[1001]="BTC_ビットコイン_Bitcoin_1_1_NaN_1_1_1_1_"; var LastModCoin="22:17"; var Coincount=1;`, want: "有限値"},
		{name: "コード重複", body: `var CO=[]; ` + validAssignment + validAssignment + ` var LastModCoin="22:17"; var Coincount=2;`, want: "重複"},
		{name: "件数不一致", body: `var CO=[]; ` + validAssignment + ` var LastModCoin="22:17"; var Coincount=2;`, want: "一致しません"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseCryptoAssets([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseCryptoAssets() error = %v, want %q", err, test.want)
			}
		})
	}
}
