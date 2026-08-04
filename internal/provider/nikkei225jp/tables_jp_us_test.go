package nikkei225jp

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

const validJapanComponentsBody = `var N2 = new Array();
N2[0]="1332__10__ニッスイ__1262__1262__0.07%__-2.59__-33.5__-1.12__Nissui Corporation";
N2[1]="9984__250__ソフトバンクグループ__5260__126240__6.58%__13.8__638__+513.29__Softbank";
LastTime="15:30"; CntUp=1; CntDwn=1; CntEvn=0;
N225total=127502; N225josuu=2; N225kabuka=63751;`

const validJapanContributionsBody = `var LastTime2="15:30";
var top10 = new Array(); var las10 = new Array();
top10[0]="6857__アドバンテスト__+1101.80__32500__4565__16.34";
las10[0]="9766__コナミ__-40.23__20640__-1200__-5.49";`

const validJapanRankingBody = `var UPDATE_TIME="07/31";
var RANK_up=[]; var RANK_dw=[]; var RANK_bi=[];
RANK_up[0]="4052_フィーチャ_1_-_-_342_+30.53%_80_GRT_07/31";
RANK_dw[0]="4307_野村総合研究所_1_2_-_4686_-15.96%_-890_PRM_07/31";
RANK_bi[0]="8411_みずほフィナンシャルグループ_1_19_19_8167_+4.46%_349_PRM_07/31";`

const validUSRankingBody = `var UPDATE_TIME="05:02";
var RANK_up=[]; var RANK_dw=[]; var RANK_bi=[];
RANK_up[0]="IESC_IES Holdings, Inc._1_1_50_744.54_30.27_173.00_485145_05:00";
RANK_dw[0]="AAPL_Apple Inc._1_2_-_308.91_-7.35_-24.50_132825635_05:00";
RANK_bi[0]="MSFT_Microsoft Corporation_1_15_4_464.72_3.02_13.62_56468734_05:00";`

const validUSEquityBody = `var stockDataTime="07/31";
var stockDataMarket="late_trading";
var stockDataDivisor=0.5;
var stockData=[
{"F":10,"S":"AAPL","J":"アップル","E":"Apple","G":10,"V":308.91,"Z":2,"P":0.65,"D":1000,"K":0},
{"F":30,"S":"AAPL","J":"アップル","E":"Apple","G":10,"V":308.91,"Z":2,"P":0.65,"D":1000,"K":3.5},
{"F":100,"S":"MSFT","J":"マイクロソフト","E":"Microsoft","G":45,"V":464.72,"Z":13.62,"P":3.02,"D":2000,"K":8.2}
];`

const validUSPreEquityBody = `var stockDataTime_pre="07/31 22:29";
var stockData_pre=[
{"F":30,"S":"AAPL","J":"アップル","G":10,"V":305.54,"Z":-27.90,"P":-8.37,"K":3.5}
];`

const validUSIndustryBody = `var GYO1=[],GYO2=[];
GYO1[0]="241_627.22_-17.46_-2.71_07/31";
GYO2[0]="261_5404.35_+7.41_+0.14_07/31";
for (Y in GYO1) GYO1[Y] = GYO1[Y].split('_');
for (X in GYO2) GYO2[X] = GYO2[X].split('_');
var GyoModTime="07/31";`

const validADRBody = `var Shu="1605";
var A0=new Array();q=0;
A0[q]="1605_IPXHY_ＩＮＰＥＸ_2_INPEX_OTC_1_07/31_3627_-1_-0.03_0_07/31_22.97_0.34_+1.50_36791_3623_157.711_23:54_3627_4,400_U_1";q++;
A0[q]="4755_RKUNY_楽天_33_Rakuten_OTC_1_07/31_833_-4.1_-0.49_0_07/31_5.29_0.00_+0.03_36796_834_157.619_23:54_828_2,800_S_1";q++;`

// TestParseJapanComponents は、N2の10列と集計値を正規化できることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseJapanComponents(t *testing.T) {
	t.Parallel()
	data, err := parseJapanComponents([]byte(validJapanComponentsBody))
	if err != nil {
		t.Fatalf("parseJapanComponents() error = %v", err)
	}
	if data.UpdatedAt != "15:30" || data.UpCount != 1 || data.DownCount != 1 {
		t.Errorf("集計値 = %+v", data)
	}
	if len(data.Components) != 2 {
		t.Fatalf("len(Components) = %d", len(data.Components))
	}
	first := data.Components[0]
	if first.Code != "1332" || first.DeemedPrice != 1262 || first.WeightPercent != 0.07 || first.ContributionYen != -1.12 {
		t.Errorf("Components[0] = %+v", first)
	}
}

// TestParseJapanContributions は、寄与度上位と下位を別々に保持することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseJapanContributions(t *testing.T) {
	t.Parallel()
	data, err := parseJapanContributions([]byte(validJapanContributionsBody))
	if err != nil {
		t.Fatalf("parseJapanContributions() error = %v", err)
	}
	if len(data.Top) != 1 || data.Top[0].Direction != "top" || data.Top[0].Rank != 1 {
		t.Errorf("Top = %+v", data.Top)
	}
	if len(data.Bottom) != 1 || data.Bottom[0].ContributionYen != -40.23 {
		t.Errorf("Bottom = %+v", data.Bottom)
	}
}

// TestParseJapanIndustries は、33業種の固定順と有限数値を検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseJapanIndustries(t *testing.T) {
	t.Parallel()
	body := buildJapanIndustryBody("0")
	data, err := parseJapanIndustries([]byte(body))
	if err != nil {
		t.Fatalf("parseJapanIndustries() error = %v", err)
	}
	if len(data.Industries) != 33 {
		t.Fatalf("len(Industries) = %d", len(data.Industries))
	}
	if data.Industries[0].Code != "0050" || data.Industries[0].Name != "水産・農林業" {
		t.Errorf("Industries[0] = %+v", data.Industries[0])
	}
	if data.Industries[32].Code != "9050" || data.Industries[32].Value != 33 {
		t.Errorf("Industries[32] = %+v", data.Industries[32])
	}
}

// TestParseMarketRankings は、日本株と米国株の異なる第9列を正規化します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseMarketRankings(t *testing.T) {
	t.Parallel()
	japan, err := parseJapanRankings([]byte(validJapanRankingBody))
	if err != nil {
		t.Fatalf("parseJapanRankings() error = %v", err)
	}
	if japan.Gainers[0].Exchange != "GRT" || japan.Gainers[0].Volume != nil {
		t.Errorf("日本ランキング = %+v", japan.Gainers[0])
	}
	us, err := parseUSRankings([]byte(validUSRankingBody))
	if err != nil {
		t.Fatalf("parseUSRankings() error = %v", err)
	}
	if us.Gainers[0].Volume == nil || *us.Gainers[0].Volume != 485145 || us.Gainers[0].Exchange != "" {
		t.Errorf("米国ランキング = %+v", us.Gainers[0])
	}
}

// TestParseUSEquities は、指数間重複を保持してDOW寄与度だけを算出することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseUSEquities(t *testing.T) {
	t.Parallel()
	data, err := parseUSEquities([]byte(validUSEquityBody))
	if err != nil {
		t.Fatalf("parseUSEquities() error = %v", err)
	}
	if len(data.Equities) != 3 || data.Equities[0].Symbol != data.Equities[1].Symbol {
		t.Fatalf("Equities = %+v", data.Equities)
	}
	if data.Equities[0].DowContribution != nil {
		t.Errorf("FANG+寄与度 = %v", data.Equities[0].DowContribution)
	}
	if data.Equities[1].DowContribution == nil || *data.Equities[1].DowContribution != 4 {
		t.Errorf("DOW寄与度 = %v", data.Equities[1].DowContribution)
	}
	pre, err := parseUSEquities([]byte(validUSPreEquityBody))
	if err != nil {
		t.Fatalf("プレ市場parseUSEquities() error = %v", err)
	}
	if pre.Session != "pre" || pre.Divisor != nil || pre.Equities[0].DowContribution != nil {
		t.Errorf("プレ市場 = %+v", pre)
	}
	afterBody := strings.ReplaceAll(validUSPreEquityBody, "_pre", "_after")
	after, err := parseUSEquities([]byte(afterBody))
	if err != nil {
		t.Fatalf("アフター市場parseUSEquities() error = %v", err)
	}
	if after.Session != "after" || len(after.Equities) != 1 {
		t.Errorf("アフター市場 = %+v", after)
	}
}

// TestParseUSIndustries は、GYO1とGYO2の配信順を保持することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseUSIndustries(t *testing.T) {
	t.Parallel()
	data, err := parseUSIndustries([]byte(validUSIndustryBody))
	if err != nil {
		t.Fatalf("parseUSIndustries() error = %v", err)
	}
	if len(data.Industries) != 2 || data.Industries[0].Group != "GYO1" || data.Industries[1].Code != "261" {
		t.Errorf("Industries = %+v", data.Industries)
	}
}

// TestParseADRData は、ADRとPTSの東証比較率を元値がある場合だけ算出することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseADRData(t *testing.T) {
	t.Parallel()
	data, err := parseADRData([]byte(validADRBody))
	if err != nil {
		t.Fatalf("parseADRData() error = %v", err)
	}
	if len(data.Quotes) != 2 || !data.Quotes[0].Main || data.Quotes[1].Main {
		t.Fatalf("Quotes = %+v", data.Quotes)
	}
	first := data.Quotes[0]
	wantADRPercent := (3623.0 - 3627.0) / 3627.0 * 100
	if first.ADRVsTokyoPercent == nil || math.Abs(*first.ADRVsTokyoPercent-wantADRPercent) > 1e-12 {
		t.Errorf("ADRVsTokyoPercent = %v", first.ADRVsTokyoPercent)
	}
	if first.PTSVsTokyoPercent == nil || *first.PTSVsTokyoPercent != 0 {
		t.Errorf("PTSVsTokyoPercent = %v", first.PTSVsTokyoPercent)
	}
}

// TestParseADRDataMissingSources は、比較元が空欄なら差率を算出しないことを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestParseADRDataMissingSources(t *testing.T) {
	t.Parallel()
	fields := []string{
		"1605", "IPXHY", "ＩＮＰＥＸ", "2", "INPEX", "OTC", "1", "07/31",
		"", "", "", "0", "07/31", "", "", "", "0", "", "", "23:54",
		"", "0", "U", "1",
	}
	body := `var Shu="1605"; var A0=new Array();q=0; A0[q]="` + strings.Join(fields, "_") + `";q++;`
	data, err := parseADRData([]byte(body))
	if err != nil {
		t.Fatalf("parseADRData() error = %v", err)
	}
	quote := data.Quotes[0]
	if quote.ADRVsTokyoPercent != nil || quote.PTSVsTokyoPercent != nil {
		t.Errorf("比較元空欄時の差率 = ADR:%v PTS:%v", quote.ADRVsTokyoPercent, quote.PTSVsTokyoPercent)
	}
}

// TestTableParsersRejectInvalidData は、列不足、重複、非有限値、余分なJSを拒否します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestTableParsersRejectInvalidData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "構成銘柄の余分なJS",
			run: func() error {
				_, err := parseJapanComponents([]byte(validJapanComponentsBody + ` alert(1);`))
				return err
			},
			want: "未知の変数",
		},
		{
			name: "寄与度の添字重複",
			run: func() error {
				body := validJapanContributionsBody + ` top10[0]="9984__SBG__1__1__1__1";`
				_, err := parseJapanContributions([]byte(body))
				return err
			},
			want: "重複",
		},
		{
			name: "業種の非有限値",
			run: func() error {
				_, err := parseJapanIndustries([]byte(buildJapanIndustryBody("NaN")))
				return err
			},
			want: "有限数値",
		},
		{
			name: "ランキングの列不足",
			run: func() error {
				body := strings.Replace(validJapanRankingBody, "4052_フィーチャ_1_-_-_342_+30.53%_80_GRT_07/31", "4052_フィーチャ_1", 1)
				_, err := parseJapanRankings([]byte(body))
				return err
			},
			want: "列数",
		},
		{
			name: "米国同一指数内重複",
			run: func() error {
				duplicate := `,{"F":30,"S":"AAPL","J":"アップル","E":"Apple","G":10,"V":1,"Z":1,"P":1,"D":1,"K":1}`
				body := strings.Replace(validUSEquityBody, "\n];", duplicate+"\n];", 1)
				_, err := parseUSEquities([]byte(body))
				return err
			},
			want: "重複",
		},
		{
			name: "米国オブジェクトの末尾カンマ",
			run: func() error {
				body := strings.Replace(validUSEquityBody, `"K":0}`, `"K":0,}`, 1)
				_, err := parseUSEquities([]byte(body))
				return err
			},
			want: "末尾カンマ",
		},
		{
			name: "米国業種コード重複",
			run: func() error {
				body := strings.Replace(validUSIndustryBody, "261_5404.35", "241_5404.35", 1)
				_, err := parseUSIndustries([]byte(body))
				return err
			},
			want: "重複",
		},
		{
			name: "ADR列不足",
			run: func() error {
				body := strings.Replace(validADRBody, "_U_1\";q++;", "_U\";q++;", 1)
				_, err := parseADRData([]byte(body))
				return err
			},
			want: "列数",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

// buildJapanIndustryBody は、33業種の正常系または指定異常値を含むテスト本文を作ります。
//
// 引数:
//   - firstChange string: G2先頭列へ設定する文字列。
//
// 返り値:
//   - string: country_jp_gyo.js互換のテスト本文。
func buildJapanIndustryBody(firstChange string) string {
	levels := make([]string, len(japanIndustryNames))
	changes := make([]string, len(japanIndustryNames))
	for index := range levels {
		levels[index] = strconv.Itoa(index + 1)
		changes[index] = strconv.Itoa(-index)
	}
	changes[0] = firstChange
	return fmt.Sprintf(
		`ModDate="2026/07/31"; ModTime="15:30"; G1="%s_"; G2="%s_"; G1=G1.split("_"); G2=G2.split("_");`,
		strings.Join(levels, "_"),
		strings.Join(changes, "_"),
	)
}
