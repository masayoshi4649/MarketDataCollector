package nikkei225jp

import (
	"reflect"
	"strings"
	"testing"
)

// ----------------------------------------

// TestParseMarketIntradayChartEuropeFixture は、欧州市場の絶対時刻と疎列を検証します。
//
// 引数:
//   - t *testing.T: 検証失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。系列順、表示名、絶対時刻、疎列、空の末尾情報を検証する。
func TestParseMarketIntradayChartEuropeFixture(t *testing.T) {
	t.Parallel()

	body := []byte(`var DATAm = [
		[1785099600000,25405.81,10820.70,8442.75,871.19,186.371,163.558,1.13926],
		[1785101400000,null,10822.50,null,null,186.409,163.611,1.13930]
	];`)
	codes := []string{"412", "413", "411", "441", "514", "511", "523"}

	series, suffixes, err := parseMarketIntradayChart(body, codes, nil)
	if err != nil {
		t.Fatalf("parseMarketIntradayChart() error = %v", err)
	}
	if len(suffixes) != 0 {
		t.Fatalf("suffixes = %#v, want empty", suffixes)
	}
	if len(series) != len(codes) {
		t.Fatalf("len(series) = %d, want %d", len(series), len(codes))
	}
	for index, code := range codes {
		if series[index].Code != code {
			t.Fatalf("series[%d].Code = %q, want %q", index, series[index].Code, code)
		}
	}
	if series[0].Name != "ドイツDAX" {
		t.Fatalf("series[0].Name = %q, want %q", series[0].Name, "ドイツDAX")
	}
	if len(series[0].Points) != 1 {
		t.Fatalf("len(series[0].Points) = %d, want 1", len(series[0].Points))
	}
	if got := series[1].Points[1].TimestampMillis; got != 1785101400000 {
		t.Fatalf("series[1].Points[1].TimestampMillis = %d, want 1785101400000", got)
	}
	if got := series[1].Points[1].Value; got != 10822.50 {
		t.Fatalf("series[1].Points[1].Value = %v, want 10822.50", got)
	}
}

// TestParseMarketIntradayChartAsiaFixture は、アジア市場の絶対時刻と差分時刻の混在を検証します。
//
// 2行目の18は直前時刻へ18×10,000ミリ秒を加算し、3行目は
// 1,000,000,000,000以上なので新しい絶対Unixミリ秒として扱われます。
//
// 引数:
//   - t *testing.T: 検証失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。混在時刻の復元、疎列、系列順を検証する。
func TestParseMarketIntradayChartAsiaFixture(t *testing.T) {
	t.Parallel()

	body := []byte(`var DATAm=[
		[1784251800000,null,null,3867.72,null,null,null],
		[18,65191.78,6796.06,null,24993.77,23947.60,163.558],
		[1784255400000,65200.00,null,3828.44,null,null,163.611]
	];`)
	codes := []string{"111", "313", "321", "331", "352", "511"}

	series, suffixes, err := parseMarketIntradayChart(body, codes, nil)
	if err != nil {
		t.Fatalf("parseMarketIntradayChart() error = %v", err)
	}
	if len(suffixes) != 0 {
		t.Fatalf("suffixes = %#v, want empty", suffixes)
	}
	if got := series[0].Points[0].TimestampMillis; got != 1784251980000 {
		t.Fatalf("series[0].Points[0].TimestampMillis = %d, want 1784251980000", got)
	}
	if got := series[0].Points[1].TimestampMillis; got != 1784255400000 {
		t.Fatalf("series[0].Points[1].TimestampMillis = %d, want 1784255400000", got)
	}
	if len(series[2].Points) != 2 {
		t.Fatalf("len(series[2].Points) = %d, want 2", len(series[2].Points))
	}
	if series[5].Code != "511" || series[5].Name != "USD/JPY" {
		t.Fatalf("series[5] = %#v, want code 511 and name USD/JPY", series[5])
	}
}

// TestParseMarketIntradayChartOilFixture は、商品市場の合成コードと限月文字列を検証します。
//
// 2行目の18は差分時刻として復元します。単一・二重引用符を混在させ、
// suffixNamesで許可した限月変数だけが返されることを確認します。
//
// 引数:
//   - t *testing.T: 検証失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。合成コード、系列値、差分時刻、限月情報を検証する。
func TestParseMarketIntradayChartOilFixture(t *testing.T) {
	t.Parallel()

	body := []byte(`var DATAm=[
		[1785099600000,163.558,64214.80,null,null,null,null,null,null],
		[18,null,64214.80,84.30,80.90,4117.20,4148.10,59.923,6.3678]
	];
	var oilM1='09',oilM2="10",oilM3='11',gldM1="10",gldM2='12';`)
	codes := []string{"511", "191", "921_m1", "921_m2", "931_m1", "931_m2", "932", "933"}
	allowedSuffixes := []string{"oilM1", "oilM2", "oilM3", "gldM1", "gldM2"}
	wantSuffixes := map[string]string{
		"oilM1": "09",
		"oilM2": "10",
		"oilM3": "11",
		"gldM1": "10",
		"gldM2": "12",
	}

	series, suffixes, err := parseMarketIntradayChart(body, codes, allowedSuffixes)
	if err != nil {
		t.Fatalf("parseMarketIntradayChart() error = %v", err)
	}
	if !reflect.DeepEqual(suffixes, wantSuffixes) {
		t.Fatalf("suffixes = %#v, want %#v", suffixes, wantSuffixes)
	}
	if len(series) != len(codes) || series[2].Code != "921_m1" {
		t.Fatalf("series = %#v, want %d ordered series", series, len(codes))
	}
	if series[2].Name != "" {
		t.Fatalf("series[2].Name = %q, want empty", series[2].Name)
	}
	if got := series[2].Points[0]; got.TimestampMillis != 1785099780000 || got.Value != 84.30 {
		t.Fatalf("series[2].Points[0] = %#v, want restored timestamp and value 84.30", got)
	}
}

// TestParseMarketIntradayChartRejectsUnsafeInput は、未知式と不正な市場データを拒否します。
//
// 引数:
//   - t *testing.T: 検証失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。未知変数、式、余分なJavaScript、列超過、時刻逆転、非有限値を検証する。
func TestParseMarketIntradayChartRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		body        string
		codes       []string
		suffixNames []string
		errorPart   string
	}{
		{
			name:        "未知の末尾変数",
			body:        `var DATAm=[[1700000000000,1]];var unknown='09';`,
			codes:       []string{"921_m1"},
			suffixNames: []string{"oilM1"},
			errorPart:   "想定外の末尾変数名",
		},
		{
			name:        "文字列式",
			body:        `var DATAm=[[1700000000000,1]];var oilM1='0'+'9';`,
			codes:       []string{"921_m1"},
			suffixNames: []string{"oilM1"},
			errorPart:   "カンマまたはセミコロン",
		},
		{
			name:      "余分なJavaScript",
			body:      `var DATAm=[[1700000000000,1]];alert(1);`,
			codes:     []string{"111"},
			errorPart: "余分なJavaScript",
		},
		{
			name:      "列超過",
			body:      `var DATAm=[[1700000000000,1,2]];`,
			codes:     []string{"111"},
			errorPart: "列数",
		},
		{
			name:      "絶対時刻の逆転",
			body:      `var DATAm=[[1700000001000,1],[1700000000000,2]];`,
			codes:     []string{"111"},
			errorPart: "非降順",
		},
		{
			name:      "非有限値",
			body:      `var DATAm=[[1700000000000,1e999]];`,
			codes:     []string{"111"},
			errorPart: "数値",
		},
		{
			name:        "末尾変数の重複代入",
			body:        `var DATAm=[[1700000000000,1]];var oilM1='09',oilM1='10';`,
			codes:       []string{"921_m1"},
			suffixNames: []string{"oilM1"},
			errorPart:   "重複",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := parseMarketIntradayChart(
				[]byte(testCase.body),
				testCase.codes,
				testCase.suffixNames,
			)
			if err == nil {
				t.Fatal("parseMarketIntradayChart() error = nil, want error")
			}
			if !strings.Contains(err.Error(), testCase.errorPart) {
				t.Fatalf("error = %q, want containing %q", err, testCase.errorPart)
			}
		})
	}
}

// TestParseMarketIntradayChartRejectsInvalidDefinitions は、呼び出し側の不正な定義を拒否します。
//
// 引数:
//   - t *testing.T: 検証失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。空・重複コードと不正・重複末尾変数名を検証する。
func TestParseMarketIntradayChartRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		codes       []string
		suffixNames []string
	}{
		{name: "列コードなし"},
		{name: "空の列コード", codes: []string{""}},
		{name: "列コード重複", codes: []string{"111", "111"}},
		{name: "不正な末尾変数名", codes: []string{"111"}, suffixNames: []string{"oil-M1"}},
		{name: "末尾変数名重複", codes: []string{"111"}, suffixNames: []string{"oilM1", "oilM1"}},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := parseMarketIntradayChart(
				[]byte(`var DATAm=[[1700000000000,1]];`),
				testCase.codes,
				testCase.suffixNames,
			)
			if err == nil {
				t.Fatal("parseMarketIntradayChart() error = nil, want error")
			}
		})
	}
}
