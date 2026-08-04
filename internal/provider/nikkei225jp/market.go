package nikkei225jp

import (
	"fmt"
	"slices"
)

// MarketSection は、225225.jp内の数値データ分類を表します。
//
// 主な特徴:
//   - 値はMCPとJSON出力で利用する安定した識別子
//   - 対応ページのURLパスとは分離して保持する
//   - ニュースや画像だけの分類は含めない
type MarketSection string

const (
	MarketSectionTop           MarketSection = "top"
	MarketSectionNikkeiFutures MarketSection = "nikkei_futures"
	MarketSectionJapan         MarketSection = "japan"
	MarketSectionUS            MarketSection = "us"
	MarketSectionADR           MarketSection = "adr"
	MarketSectionEurope        MarketSection = "europe"
	MarketSectionAsia          MarketSection = "asia"
	MarketSectionCommodities   MarketSection = "commodities"
	MarketSectionFX            MarketSection = "fx"
	MarketSectionBitcoin       MarketSection = "bitcoin"
)

// MarketChartRange は、市場別チャートで取得するデータ範囲を表します。
//
// 主な特徴:
//   - intradayはページの短期複合または個別チャート配信を表す
//   - historyは日足全履歴配信を表す
//   - 配信点は画面の表示期間より広い場合がある
type MarketChartRange string

const (
	MarketChartRangeIntraday MarketChartRange = "intraday"
	MarketChartRangeHistory  MarketChartRange = "history"
)

// MarketSectionInfo は、MCPから参照できる市場分類の機能一覧を表します。
//
// 主な特徴:
//   - PageURLは人が確認する元ページ
//   - IntradayCodesとHistoryCodesは取得前に検証する許可リスト
//   - Datasetsは追加の数値表を取得する識別子
type MarketSectionInfo struct {
	Section                 MarketSection `json:"section"`
	Name                    string        `json:"name"`
	PageURL                 string        `json:"page_url"`
	CurrentAvailable        bool          `json:"current_available"`
	IntradayCodes           []string      `json:"intraday_codes"`
	IntradayCompositeCodes  []string      `json:"intraday_composite_codes"`
	IntradaySingleOnlyCodes []string      `json:"intraday_single_only_codes"`
	HistoryCodes            []string      `json:"history_codes"`
	Datasets                []string      `json:"datasets"`
}

// MarketCurrentData は、分類別の現在値と取得元情報を表します。
//
// 主な特徴:
//   - Quotesは配信元の全件または指定コードだけを含む
//   - Metadataは直接取得した内部配信URLを示す
//   - PageURLは対応する人向けページを示す
type MarketCurrentData struct {
	Section  MarketSection    `json:"section"`
	Name     string           `json:"name"`
	PageURL  string           `json:"page_url"`
	Metadata ResponseMetadata `json:"metadata"`
	Quotes   []CurrentQuote   `json:"quotes"`
}

// MarketChartData は、市場分類別のチャート系列と取得元情報を表します。
//
// 主な特徴:
//   - Rangeは短期配信または日足全履歴を示す
//   - Sourcesは実際に取得した内部配信URLごとの付帯情報
//   - Metadataは商品先物の限月など配信付帯値を保持する
type MarketChartData struct {
	Section  MarketSection      `json:"section"`
	Name     string             `json:"name"`
	PageURL  string             `json:"page_url"`
	Range    MarketChartRange   `json:"range"`
	Sources  []ResponseMetadata `json:"sources"`
	Metadata map[string]string  `json:"metadata,omitempty"`
	Series   []ChartSeries      `json:"series"`
}

type marketSectionConfig struct {
	info                MarketSectionInfo
	currentPath         string
	intradayPath        string
	intradayCodes       []string
	intradayNames       map[string]string
	intradaySinglePaths map[string]string
	historyCodes        map[string]struct{}
}

// ----------------------------------------

var marketSectionOrder = []MarketSection{
	MarketSectionTop,
	MarketSectionNikkeiFutures,
	MarketSectionJapan,
	MarketSectionUS,
	MarketSectionADR,
	MarketSectionEurope,
	MarketSectionAsia,
	MarketSectionCommodities,
	MarketSectionFX,
	MarketSectionBitcoin,
}

var marketSectionConfigs = map[MarketSection]marketSectionConfig{
	MarketSectionTop: newMarketSectionConfig(
		MarketSectionTop,
		"世界主要市場",
		"https://225225.jp/",
		DefaultCurrentPath,
		chart1DayPath,
		chart1DayColumnCodes,
		chart1DaySinglePaths,
		keysOfCodeSet(chart6MonthsCodes),
		nil,
		nil,
	),
	MarketSectionNikkeiFutures: newMarketSectionConfig(
		MarketSectionNikkeiFutures,
		"日経先物・CFD",
		"https://225225.jp/2nk/",
		"/_data/_nfsDATA/ajaxindex/ajax_cme.js",
		"/_data/_nfsDATA/hs_data/hs_CME5.json",
		[]string{"511", "111", "136", "191", "cme_nikkei", "413", "211", "731", "cme_dow", "732"},
		nil,
		nil,
		map[string]string{
			"cme_nikkei": "CME日経先物",
			"cme_dow":    "CME NYダウ先物",
		},
		nil,
	),
	MarketSectionJapan: newMarketSectionConfig(
		MarketSectionJapan,
		"日本市場",
		"https://225225.jp/1jp/",
		"/_data/_nfsDATA/ajaxindex/ajax_nikkei.js",
		"/_data/_nfsDATA/hs_data/hs_CHART3.json",
		[]string{"111", "112", "121", "136", "141", "161", "191", "511"},
		nil,
		[]string{"111", "112", "121", "141", "161", "511"},
		nil,
		[]string{"japan_components", "japan_contributors", "japan_industries", "japan_ranking"},
	),
	MarketSectionUS: newMarketSectionConfig(
		MarketSectionUS,
		"米国市場",
		"https://225225.jp/3ny/",
		"/_data/_nfsDATA/ajaxindex/ajax_dow.js",
		"/_data/_nfsDATA/hs_data/hs_NASDAQ4.json",
		[]string{"211", "212", "213", "413", "191", "731", "811", "621", "511", "501"},
		nil,
		[]string{"111", "211", "212", "213", "511", "811"},
		nil,
		[]string{"us_equities", "us_industries", "us_ranking"},
	),
	MarketSectionADR: newMarketSectionConfig(
		MarketSectionADR,
		"日本株ADR・PTS",
		"https://225225.jp/3ny/adr.php",
		"",
		"",
		nil,
		nil,
		nil,
		nil,
		[]string{"adr"},
	),
	MarketSectionEurope: newMarketSectionConfig(
		MarketSectionEurope,
		"欧州市場",
		"https://225225.jp/6ec/",
		"/_data/_nfsDATA/ajaxindex/ajax_euro.js",
		"/_data/_nfsDATA/hs_data/hs_EURO.json",
		[]string{"412", "413", "411", "441", "514", "511", "523"},
		singlePaths("24min", "411", "412", "413", "441", "511", "514"),
		[]string{"411", "412", "413", "441", "511", "514"},
		nil,
		nil,
	),
	MarketSectionAsia: newMarketSectionConfig(
		MarketSectionAsia,
		"アジア市場",
		"https://225225.jp/7as/",
		"/_data/_nfsDATA/ajaxindex/ajax_asia.js",
		"/_data/_nfsDATA/hs_data/hs_CHINA2.json",
		[]string{"111", "313", "321", "331", "352", "511"},
		singlePaths("24min", "111", "313", "321", "331", "352", "511"),
		[]string{"111", "313", "321", "331", "352", "511"},
		nil,
		nil,
	),
	MarketSectionCommodities: newMarketSectionConfig(
		MarketSectionCommodities,
		"商品先物",
		"https://225225.jp/5cx/",
		"/_data/_nfsDATA/ajaxindex/ajax_oil.js",
		"/_data/_nfsDATA/hs_data/hs_OIL_3M2.json",
		[]string{"511", "191", "921_m1", "921_m2", "931_m1", "931_m2", "932", "933"},
		singlePaths("24min", "921", "931", "932", "933", "111", "511"),
		[]string{"921", "931", "932", "933", "111", "511"},
		map[string]string{
			"921_m1": "WTI原油 期近",
			"921_m2": "WTI原油 第2限月",
			"931_m1": "NY金 期近",
			"931_m2": "NY金 第2限月",
		},
		nil,
	),
	MarketSectionFX: newMarketSectionConfig(
		MarketSectionFX,
		"為替",
		"https://225225.jp/4fx/",
		"/_data/_nfsDATA/ajaxindex/ajax_fx.js",
		"/_data/_nfsDATA/hs_data/hs_FX3.json",
		[]string{"501", "511", "514", "523", "515", "516", "510"},
		singlePaths("24", "1251", "501", "509", "510", "511", "513", "514", "515", "516", "517", "523", "541", "548", "555", "556", "734", "735", "742", "743"),
		[]string{"511", "514", "515", "516", "523", "501", "510"},
		nil,
		[]string{"fx_rates"},
	),
	MarketSectionBitcoin: newMarketSectionConfig(
		MarketSectionBitcoin,
		"暗号資産",
		"https://225225.jp/bitcoin/",
		"/_data/_nfsDATA/ajaxindex/ajax_bitcoin.js",
		"/_data/_nfsDATA/hs_data/hs_COIN3.json",
		[]string{"1001", "1011", "1021", "1691", "1631"},
		singlePaths("24", "1001", "1002", "1011", "1021", "1031", "1101", "1121", "1151", "1191", "1381", "1631", "1691", "511"),
		[]string{"1001", "1011", "1021", "1691", "1631"},
		nil,
		[]string{"crypto_assets"},
	),
}

// ----------------------------------------

// MarketSections は、対応市場分類を固定順で返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - []MarketSectionInfo: 呼び出し側が変更しても内部定義へ影響しない機能一覧。
func MarketSections() []MarketSectionInfo {
	sections := make([]MarketSectionInfo, 0, len(marketSectionOrder))
	for _, section := range marketSectionOrder {
		config := marketSectionConfigs[section]
		sections = append(sections, cloneMarketSectionInfo(config.info))
	}
	return sections
}

// MarketSectionInformation は、指定市場分類の機能情報を返します。
//
// 引数:
//   - section MarketSection: 検索する市場分類。
//
// 返り値:
//   - MarketSectionInfo: 呼び出し側が変更しても内部定義へ影響しない機能情報。
//   - error: 未対応の市場分類を指定した場合のエラー。
func MarketSectionInformation(section MarketSection) (MarketSectionInfo, error) {
	config, exists := marketSectionConfigs[section]
	if !exists {
		return MarketSectionInfo{}, fmt.Errorf("未対応の市場分類です: %q", section)
	}
	return cloneMarketSectionInfo(config.info), nil
}

// newMarketSectionConfig は、市場分類の公開情報と内部取得設定を生成します。
//
// 引数:
//   - section MarketSection: 市場分類の識別子。
//   - name string: 日本語表示名。
//   - pageURL string: 対応する人向けページURL。
//   - currentPath string: 現在値内部配信パス。空文字なら未対応。
//   - intradayPath string: 短期複合チャート内部配信パス。空文字なら未対応。
//   - intradayCodes []string: 短期複合チャートの列コード。
//   - intradaySinglePaths map[string]string: 低負荷な単一チャートのコード別パス。
//   - historyCodes []string: 長期日足で許可するコード。
//   - intradayNames map[string]string: ページ固有系列の表示名。
//   - datasets []string: 追加数値表の識別子。
//
// 返り値:
//   - marketSectionConfig: 公開情報と内部取得設定をまとめた値。
func newMarketSectionConfig(
	section MarketSection,
	name string,
	pageURL string,
	currentPath string,
	intradayPath string,
	intradayCodes []string,
	intradaySinglePaths map[string]string,
	historyCodes []string,
	intradayNames map[string]string,
	datasets []string,
) marketSectionConfig {
	historySet := make(map[string]struct{}, len(historyCodes))
	for _, code := range historyCodes {
		historySet[code] = struct{}{}
	}
	publicIntradayCodes := slices.Clone(intradayCodes)
	publicSingleOnlyCodes := make([]string, 0, len(intradaySinglePaths))
	for code := range intradaySinglePaths {
		publicIntradayCodes = append(publicIntradayCodes, code)
		if !slices.Contains(intradayCodes, code) {
			publicSingleOnlyCodes = append(publicSingleOnlyCodes, code)
		}
	}
	publicIntradayCodes, _ = normalizeMarketCodes(publicIntradayCodes)
	publicSingleOnlyCodes, _ = normalizeMarketCodes(publicSingleOnlyCodes)
	return marketSectionConfig{
		info: MarketSectionInfo{
			Section:                 section,
			Name:                    name,
			PageURL:                 pageURL,
			CurrentAvailable:        currentPath != "",
			IntradayCodes:           publicIntradayCodes,
			IntradayCompositeCodes:  slices.Clone(intradayCodes),
			IntradaySingleOnlyCodes: publicSingleOnlyCodes,
			HistoryCodes:            slices.Clone(historyCodes),
			Datasets:                slices.Clone(datasets),
		},
		currentPath:         currentPath,
		intradayPath:        intradayPath,
		intradayCodes:       slices.Clone(intradayCodes),
		intradayNames:       intradayNames,
		intradaySinglePaths: intradaySinglePaths,
		historyCodes:        historySet,
	}
}

// singlePaths は、コード一覧から単一チャートの固定許可パスを生成します。
//
// 引数:
//   - suffix string: ファイル名のコード後へ付ける24または24min。
//   - codes ...string: 単一チャート取得を許可するコード。
//
// 返り値:
//   - map[string]string: コードをキーにした同一ホスト固定パス。
func singlePaths(suffix string, codes ...string) map[string]string {
	paths := make(map[string]string, len(codes))
	for _, code := range codes {
		paths[code] = fmt.Sprintf("/_data/_nfsDATA/json/%s_%s.json", code, suffix)
	}
	return paths
}

// keysOfCodeSet は、コード集合を数値順のスライスへ変換します。
//
// 引数:
//   - codes map[string]struct{}: 変換するコード集合。
//
// 返り値:
//   - []string: 数値順に並べたコード。
func keysOfCodeSet(codes map[string]struct{}) []string {
	result := make([]string, 0, len(codes))
	for code := range codes {
		result = append(result, code)
	}
	sortCodes(result)
	return result
}

// cloneMarketSectionInfo は、スライスを共有しない公開用コピーを生成します。
//
// 引数:
//   - info MarketSectionInfo: 複製元の機能情報。
//
// 返り値:
//   - MarketSectionInfo: 内部スライスを共有しない複製。
func cloneMarketSectionInfo(info MarketSectionInfo) MarketSectionInfo {
	info.IntradayCodes = slices.Clone(info.IntradayCodes)
	info.IntradayCompositeCodes = slices.Clone(info.IntradayCompositeCodes)
	info.IntradaySingleOnlyCodes = slices.Clone(info.IntradaySingleOnlyCodes)
	info.HistoryCodes = slices.Clone(info.HistoryCodes)
	info.Datasets = slices.Clone(info.Datasets)
	return info
}
