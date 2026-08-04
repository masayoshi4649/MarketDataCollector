package nikkei225jp

import (
	"context"
	"fmt"
)

const (
	japanComponentsPath    = "/_data/_nfsDATA/min/country_jp_nk225N.js"
	japanContributionsPath = "/_data/_nfsDATA/min/country_jp_kiyo10N.js"
	japanIndustriesPath    = "/_data/_nfsDATA/min/country_jp_gyo.js"
	japanRankingPath       = "/_data/_nfsDATA/min/country_jp_ranking.js"
	usEquitiesRegularPath  = "/_data/_nfsDATA/min/country_ny.js"
	usEquitiesPrePath      = "/_data/_nfsDATA/min/country_ny_pre.js"
	usEquitiesAfterPath    = "/_data/_nfsDATA/min/country_ny_after.js"
	usIndustriesPath       = "/_data/_nfsDATA/min/country_ny_gyo.js"
	usRankingPath          = "/_data/_nfsDATA/min/country_ny_ranking.js"
	adrPath                = "/_data/_nfsDATA/adr/_adr_all.js"
	fxRatesPath            = "/_data/_nfsDATA/ajaxindex/ajax_fx_table.js"
	cryptoAssetsPath       = "/_data/_nfsDATA/min/coin_table_DWMY.js"
)

// USMarketSession は、米国主要銘柄表の取引セッションを表します。
//
// 主な特徴:
//   - regularは通常取引を表す
//   - preはプレマーケットを表す
//   - afterはアフターマーケットを表す
type USMarketSession string

const (
	USMarketSessionRegular USMarketSession = "regular"
	USMarketSessionPre     USMarketSession = "pre"
	USMarketSessionAfter   USMarketSession = "after"
)

// ----------------------------------------

// FetchJapanComponents は、日経225構成銘柄表を小容量の内部配信から取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - JapanComponentData: 日経225構成銘柄と配信集計値。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 通信または本文形式に異常がある場合のエラー。
func (c *Client) FetchJapanComponents(
	ctx context.Context,
) (JapanComponentData, ResponseMetadata, error) {
	return fetchMarketTable(
		ctx,
		c,
		japanComponentsPath,
		marketSectionConfigs[MarketSectionJapan].info.PageURL,
		"日経225構成銘柄",
		parseJapanComponents,
	)
}

// FetchJapanContributions は、日経225寄与度上位・下位表を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - JapanContributionData: 寄与度上位・下位の配信値。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 通信または本文形式に異常がある場合のエラー。
func (c *Client) FetchJapanContributions(
	ctx context.Context,
) (JapanContributionData, ResponseMetadata, error) {
	return fetchMarketTable(
		ctx,
		c,
		japanContributionsPath,
		marketSectionConfigs[MarketSectionJapan].info.PageURL,
		"日経225寄与度",
		parseJapanContributions,
	)
}

// FetchJapanIndustries は、東証33業種の数値表を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - JapanIndustryData: 東証33業種の数値一覧。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 通信または本文形式に異常がある場合のエラー。
func (c *Client) FetchJapanIndustries(
	ctx context.Context,
) (JapanIndustryData, ResponseMetadata, error) {
	return fetchMarketTable(
		ctx,
		c,
		japanIndustriesPath,
		marketSectionConfigs[MarketSectionJapan].info.PageURL,
		"東証33業種",
		parseJapanIndustries,
	)
}

// FetchMarketRankings は、日本株または米国株の騰落・出来高ランキングを取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - section MarketSection: japanまたはus。
//
// 返り値:
//   - MarketRankingData: 値上がり、値下がり、出来高ランキング。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 市場指定、通信、本文形式に異常がある場合のエラー。
func (c *Client) FetchMarketRankings(
	ctx context.Context,
	section MarketSection,
) (MarketRankingData, ResponseMetadata, error) {
	switch section {
	case MarketSectionJapan:
		return fetchMarketTable(
			ctx,
			c,
			japanRankingPath,
			marketSectionConfigs[section].info.PageURL,
			"日本株ランキング",
			parseJapanRankings,
		)
	case MarketSectionUS:
		return fetchMarketTable(
			ctx,
			c,
			usRankingPath,
			marketSectionConfigs[section].info.PageURL,
			"米国株ランキング",
			parseUSRankings,
		)
	default:
		return MarketRankingData{}, ResponseMetadata{}, fmt.Errorf(
			"ランキングの市場はjapanまたはusにしてください: %q",
			section,
		)
	}
}

// ----------------------------------------

// FetchUSEquities は、米国主要銘柄表を指定取引セッションで取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - session USMarketSession: regular、pre、afterのいずれか。
//
// 返り値:
//   - USEquityData: 指数別の米国主要銘柄一覧。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: セッション、通信、本文形式に異常がある場合のエラー。
func (c *Client) FetchUSEquities(
	ctx context.Context,
	session USMarketSession,
) (USEquityData, ResponseMetadata, error) {
	resourcePath := ""
	switch session {
	case USMarketSessionRegular:
		resourcePath = usEquitiesRegularPath
	case USMarketSessionPre:
		resourcePath = usEquitiesPrePath
	case USMarketSessionAfter:
		resourcePath = usEquitiesAfterPath
	default:
		return USEquityData{}, ResponseMetadata{}, fmt.Errorf(
			"米国株セッションはregular、pre、afterのいずれかにしてください: %q",
			session,
		)
	}
	return fetchMarketTable(
		ctx,
		c,
		resourcePath,
		marketSectionConfigs[MarketSectionUS].info.PageURL,
		"米国主要銘柄",
		parseUSEquities,
	)
}

// FetchUSIndustries は、米国業種指数表を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - USIndustryData: 米国業種指数一覧。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 通信または本文形式に異常がある場合のエラー。
func (c *Client) FetchUSIndustries(
	ctx context.Context,
) (USIndustryData, ResponseMetadata, error) {
	return fetchMarketTable(
		ctx,
		c,
		usIndustriesPath,
		marketSectionConfigs[MarketSectionUS].info.PageURL,
		"米国業種指数",
		parseUSIndustries,
	)
}

// FetchADR は、日本株ADR・PTS・東証価格の一覧を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - ADRData: ADR、PTS、東証価格を正規化した一覧。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 通信または本文形式に異常がある場合のエラー。
func (c *Client) FetchADR(ctx context.Context) (ADRData, ResponseMetadata, error) {
	return fetchMarketTable(
		ctx,
		c,
		adrPath,
		marketSectionConfigs[MarketSectionADR].info.PageURL,
		"日本株ADR・PTS",
		parseADRData,
	)
}

// ----------------------------------------

// FetchFXRates は、為替レート一覧を小容量の表配信から取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - codes []string: 出力対象の数値コード。空の場合は配信全件。
//
// 返り値:
//   - []CurrentQuote: 指定コードへ絞り込んだ為替現在値一覧。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: コード、通信、本文形式に異常がある場合のエラー。
func (c *Client) FetchFXRates(
	ctx context.Context,
	codes []string,
) ([]CurrentQuote, ResponseMetadata, error) {
	selectedCodes, err := normalizeChartCodes(codes)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	quotes, metadata, err := fetchMarketTable(
		ctx,
		c,
		fxRatesPath,
		marketSectionConfigs[MarketSectionFX].info.PageURL,
		"為替レート表",
		parseCompactCurrent,
	)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	quotes, err = filterCurrentQuotes(quotes, selectedCodes)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	for index := range quotes {
		quotes[index].Name = InstrumentName(quotes[index].Code)
	}
	return quotes, metadata, nil
}

// FetchCryptoAssets は、暗号資産一覧と期間別騰落率を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - CryptoAssetData: 暗号資産一覧、銘柄数、更新表示。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 通信または本文形式に異常がある場合のエラー。
func (c *Client) FetchCryptoAssets(
	ctx context.Context,
) (CryptoAssetData, ResponseMetadata, error) {
	return fetchMarketTable(
		ctx,
		c,
		cryptoAssetsPath,
		marketSectionConfigs[MarketSectionBitcoin].info.PageURL,
		"暗号資産一覧",
		parseCryptoAssets,
	)
}

// ----------------------------------------

// fetchMarketTable は、固定パスの数値表を要求ごとに直列取得して解析します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - client *Client: 同一ホスト限定で取得を直列化するクライアント。
//   - resourcePath string: 取得する確認済み内部配信の絶対パス。
//   - referer string: 配信へ送る対応ページURL。
//   - resourceName string: エラー表示に用いる数値表名。
//   - parser func([]byte) (T, error): 本文をJavaScript実行なしで解析する関数。
//
// 返り値:
//   - T: 解析済みの数値表。
//   - ResponseMetadata: 実際に取得した内部配信のHTTP付帯情報。
//   - error: 直列化、通信、URL、本文解析に異常がある場合のエラー。
func fetchMarketTable[T any](
	ctx context.Context,
	client *Client,
	resourcePath string,
	referer string,
	resourceName string,
	parser func([]byte) (T, error),
) (T, ResponseMetadata, error) {
	var zero T
	release, err := client.acquireRequestSlot(ctx)
	if err != nil {
		return zero, ResponseMetadata{}, err
	}
	defer release()

	requestURL, err := client.resolveResourceURL(resourcePath)
	if err != nil {
		return zero, ResponseMetadata{}, err
	}
	body, metadata, err := client.fetchWithRefererLocked(
		ctx,
		requestURL,
		resourceName,
		client.maxChartResponseBytes,
		referer,
	)
	if err != nil {
		return zero, ResponseMetadata{}, err
	}
	data, err := parser(body)
	if err != nil {
		return zero, ResponseMetadata{}, fmt.Errorf("%s本文を解析できません: %w", resourceName, err)
	}
	return data, metadata, nil
}
