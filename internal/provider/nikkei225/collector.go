// Package nikkei225 は、225225.jpの数値配信を共通収集サービスへ接続します。
package nikkei225

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider/nikkei225jp"
)

const (
	maximumListLimit       = 1000
	maximumSafeJSONInteger = int64(9007199254740991)
)

// Client は、225225.jpから取得できる全データセットの機能を表します。
//
// 主な特徴:
//   - 実運用ではnikkei225jp.Clientを受け付ける
//   - テストでは外部通信しない差し替え実装を受け付ける
type Client interface {
	FetchMarketCurrent(context.Context, nikkei225jp.MarketSection, []string) (nikkei225jp.MarketCurrentData, error)
	FetchMarketChart(context.Context, nikkei225jp.MarketSection, nikkei225jp.MarketChartRange, []string) (nikkei225jp.MarketChartData, error)
	FetchJapanComponents(context.Context) (nikkei225jp.JapanComponentData, nikkei225jp.ResponseMetadata, error)
	FetchJapanContributions(context.Context) (nikkei225jp.JapanContributionData, nikkei225jp.ResponseMetadata, error)
	FetchJapanIndustries(context.Context) (nikkei225jp.JapanIndustryData, nikkei225jp.ResponseMetadata, error)
	FetchMarketRankings(context.Context, nikkei225jp.MarketSection) (nikkei225jp.MarketRankingData, nikkei225jp.ResponseMetadata, error)
	FetchUSEquities(context.Context, nikkei225jp.USMarketSession) (nikkei225jp.USEquityData, nikkei225jp.ResponseMetadata, error)
	FetchUSIndustries(context.Context) (nikkei225jp.USIndustryData, nikkei225jp.ResponseMetadata, error)
	FetchADR(context.Context) (nikkei225jp.ADRData, nikkei225jp.ResponseMetadata, error)
	FetchFXRates(context.Context, []string) ([]nikkei225jp.CurrentQuote, nikkei225jp.ResponseMetadata, error)
	FetchCryptoAssets(context.Context) (nikkei225jp.CryptoAssetData, nikkei225jp.ResponseMetadata, error)
}

// Collector は、225225.jp固有入力を検証して低負荷取得を実行します。
type Collector struct {
	client Client
}

// New は、225225.jpコレクターを生成します。
//
// 引数:
//   - client Client: 低負荷HTTP取得と厳格パースを行うクライアント。
//
// 返り値:
//   - *Collector: 共通サービスへ登録できるコレクター。
//   - error: クライアントがnilの場合のエラー。
func New(client Client) (*Collector, error) {
	if isNilClient(client) {
		return nil, errors.New("225225.jpクライアントがありません")
	}
	return &Collector{client: client}, nil
}

// Descriptor は、225225.jpで取得できる13種類の情報を返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - domain.ProviderDescriptor: 通信せずに返せる固定仕様。
func (c *Collector) Descriptor() domain.ProviderDescriptor {
	return domain.ProviderDescriptor{
		Name:        "225225jp",
		DisplayName: "225225.jp",
		Description: "225225.jpの画面用内部数値配信から、日本・米国を中心とする指数・株式、日経225構成・寄与度、為替、商品、暗号資産を取得します。",
		Datasets:    datasetDescriptors(),
	}
}

// Collect は、dataset固有入力を検証して要求時収集を実行します。
//
// 引数:
//   - ctx context.Context: 上流通信の期限とキャンセルを伝えるコンテキスト。
//   - dataset string: datalistに掲載されたデータセット識別子。
//   - parameters map[string]any: データセット固有の入力項目。
//
// 返り値:
//   - domain.ProviderResult: 正規化済み数値と取得元付帯情報。
//   - error: 入力検証、通信、配信形式のいずれかに失敗した場合のエラー。
func (c *Collector) Collect(
	ctx context.Context,
	dataset string,
	parameters map[string]any,
) (domain.ProviderResult, error) {
	var data any
	var err error
	switch dataset {
	case "catalog":
		data, err = c.collectCatalog(parameters)
	case "current":
		data, err = c.collectCurrent(ctx, parameters)
	case "chart":
		data, err = c.collectChart(ctx, parameters)
	case "japan_components":
		data, err = c.collectJapanComponents(ctx, parameters)
	case "japan_contributors":
		data, err = c.collectJapanContributors(ctx, parameters)
	case "japan_industries":
		data, err = c.collectJapanIndustries(ctx, parameters)
	case "japan_ranking":
		data, err = c.collectRanking(ctx, parameters, nikkei225jp.MarketSectionJapan)
	case "us_equities":
		data, err = c.collectUSEquities(ctx, parameters)
	case "us_industries":
		data, err = c.collectUSIndustries(ctx, parameters)
	case "us_ranking":
		data, err = c.collectRanking(ctx, parameters, nikkei225jp.MarketSectionUS)
	case "adr":
		data, err = c.collectADR(ctx, parameters)
	case "fx_rates":
		data, err = c.collectFXRates(ctx, parameters)
	case "crypto_assets":
		data, err = c.collectCryptoAssets(ctx, parameters)
	default:
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorNotFound,
			fmt.Sprintf("225225.jpにdataset %qはありません", dataset),
			nil,
		)
	}
	if err != nil {
		return domain.ProviderResult{}, err
	}
	return domain.ProviderResult{
		Data: data,
		Metadata: map[string]any{
			"source":    "https://225225.jp/",
			"read_only": true,
			"on_demand": true,
		},
	}, nil
}

// ----------------------------------------

type emptyParameters struct{}

type currentParameters struct {
	Section nikkei225jp.MarketSection `json:"section"`
	Codes   []string                  `json:"codes"`
}

type chartParameters struct {
	Section            nikkei225jp.MarketSection    `json:"section"`
	Range              nikkei225jp.MarketChartRange `json:"range"`
	Codes              []string                     `json:"codes"`
	FromMillis         int64                        `json:"from_millis"`
	ToMillis           int64                        `json:"to_millis"`
	MaxPointsPerSeries int                          `json:"max_points_per_series"`
}

type listParameters struct {
	Codes []string `json:"codes"`
	Limit int      `json:"limit"`
}

type limitParameters struct {
	Limit int `json:"limit"`
}

type rankingParameters struct {
	Kind  string `json:"kind"`
	Limit int    `json:"limit"`
}

type usEquitiesParameters struct {
	Session  nikkei225jp.USMarketSession `json:"session"`
	Universe string                      `json:"universe"`
	Symbols  []string                    `json:"symbols"`
	Limit    int                         `json:"limit"`
}

type adrParameters struct {
	Codes    []string `json:"codes"`
	Symbols  []string `json:"symbols"`
	MainOnly bool     `json:"main_only"`
	Limit    int      `json:"limit"`
}

type cryptoParameters struct {
	Codes            []string `json:"codes"`
	Symbols          []string `json:"symbols"`
	AvailableInJapan *bool    `json:"available_in_japan"`
	Limit            int      `json:"limit"`
}

type datasetResult[T any] struct {
	Metadata nikkei225jp.ResponseMetadata `json:"metadata"`
	Data     T                            `json:"data"`
}

type fxRatesData struct {
	Quotes []nikkei225jp.CurrentQuote `json:"quotes"`
}

// ----------------------------------------

// collectCatalog は、通信せず対応市場とチャート範囲を返します。
//
// 引数:
//   - parameters map[string]any: 空である必要がある入力。
//
// 返り値:
//   - any: 対応市場と固定機能一覧。
//   - error: 未知の入力項目がある場合のエラー。
func (c *Collector) collectCatalog(parameters map[string]any) (any, error) {
	if err := decodeParameters(parameters, &emptyParameters{}); err != nil {
		return nil, err
	}
	return map[string]any{
		"provider":      "https://225225.jp/",
		"read_only":     true,
		"news_included": false,
		"chart_ranges": []nikkei225jp.MarketChartRange{
			nikkei225jp.MarketChartRangeIntraday,
			nikkei225jp.MarketChartRangeHistory,
		},
		"sections": nikkei225jp.MarketSections(),
	}, nil
}

// collectCurrent は、市場分類別の現在値を取得します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: sectionと任意codes。
//
// 返り値:
//   - any: 市場分類別の現在値。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectCurrent(ctx context.Context, parameters map[string]any) (any, error) {
	var input currentParameters
	if err := decodeParameters(parameters, &input); err != nil {
		return nil, err
	}
	if input.Section == "" {
		input.Section = nikkei225jp.MarketSectionTop
	}
	if err := validateCurrentSection(input.Section); err != nil {
		return nil, invalidArgument(err.Error(), nil)
	}
	data, err := c.client.FetchMarketCurrent(ctx, input.Section, input.Codes)
	if err != nil {
		var inputErr *nikkei225jp.InputError
		if errors.As(err, &inputErr) {
			return nil, invalidArgument(inputErr.Error(), err)
		}
		return nil, err
	}
	return data, nil
}

// collectChart は、チャートを取得してローカル出力条件を適用します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: 市場、範囲、コード、出力点条件。
//
// 返り値:
//   - any: 絞り込み済みチャート。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectChart(ctx context.Context, parameters map[string]any) (any, error) {
	var input chartParameters
	if err := decodeParameters(parameters, &input); err != nil {
		return nil, err
	}
	if input.Section == "" {
		input.Section = nikkei225jp.MarketSectionTop
	}
	if input.Range == "" {
		input.Range = nikkei225jp.MarketChartRangeIntraday
	}
	if err := validateChartSource(input); err != nil {
		return nil, invalidArgument(err.Error(), nil)
	}
	if err := validateChartFilter(input); err != nil {
		return nil, invalidArgument(err.Error(), nil)
	}
	data, err := c.client.FetchMarketChart(ctx, input.Section, input.Range, input.Codes)
	if err != nil {
		var inputErr *nikkei225jp.InputError
		if errors.As(err, &inputErr) {
			return nil, invalidArgument(inputErr.Error(), err)
		}
		return nil, err
	}
	applyChartFilter(&data, input)
	return data, nil
}

// collectJapanComponents は、日経225構成銘柄を取得して絞り込みます。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: codesとlimit。
//
// 返り値:
//   - any: 取得元情報と構成銘柄。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectJapanComponents(ctx context.Context, parameters map[string]any) (any, error) {
	var input listParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	data, metadata, err := c.client.FetchJapanComponents(ctx)
	if err != nil {
		return nil, err
	}
	codes := makeStringSet(input.Codes, false)
	data.Components = filterAndLimit(data.Components, input.Limit, func(item nikkei225jp.JapanComponent) bool {
		return matchesSet(item.Code, codes, false)
	})
	return datasetResult[nikkei225jp.JapanComponentData]{Metadata: metadata, Data: data}, nil
}

// collectJapanContributors は、日経225寄与度を取得して件数制限します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: limit。
//
// 返り値:
//   - any: 取得元情報と寄与度上位・下位。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectJapanContributors(ctx context.Context, parameters map[string]any) (any, error) {
	var input limitParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	data, metadata, err := c.client.FetchJapanContributions(ctx)
	if err != nil {
		return nil, err
	}
	data.Top = limitSlice(data.Top, input.Limit)
	data.Bottom = limitSlice(data.Bottom, input.Limit)
	return datasetResult[nikkei225jp.JapanContributionData]{Metadata: metadata, Data: data}, nil
}

// collectJapanIndustries は、東証33業種を取得して件数制限します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: limit。
//
// 返り値:
//   - any: 取得元情報と業種一覧。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectJapanIndustries(ctx context.Context, parameters map[string]any) (any, error) {
	var input limitParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	data, metadata, err := c.client.FetchJapanIndustries(ctx)
	if err != nil {
		return nil, err
	}
	data.Industries = limitSlice(data.Industries, input.Limit)
	return datasetResult[nikkei225jp.JapanIndustryData]{Metadata: metadata, Data: data}, nil
}

// collectRanking は、市場別ランキングを取得して分類と件数で絞ります。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: kindとlimit。
//   - section nikkei225jp.MarketSection: japanまたはus。
//
// 返り値:
//   - any: 取得元情報と絞り込み済みランキング。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectRanking(
	ctx context.Context,
	parameters map[string]any,
	section nikkei225jp.MarketSection,
) (any, error) {
	var input rankingParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	if input.Kind == "" {
		input.Kind = "all"
	}
	if input.Kind != "all" && input.Kind != "gainers" && input.Kind != "losers" && input.Kind != "active" {
		return nil, invalidArgument("kindはall、gainers、losers、activeのいずれかにしてください", nil)
	}
	data, metadata, err := c.client.FetchMarketRankings(ctx, section)
	if err != nil {
		return nil, err
	}
	if input.Kind != "all" && input.Kind != "gainers" {
		data.Gainers = nil
	} else {
		data.Gainers = limitSlice(data.Gainers, input.Limit)
	}
	if input.Kind != "all" && input.Kind != "losers" {
		data.Losers = nil
	} else {
		data.Losers = limitSlice(data.Losers, input.Limit)
	}
	if input.Kind != "all" && input.Kind != "active" {
		data.Active = nil
	} else {
		data.Active = limitSlice(data.Active, input.Limit)
	}
	return datasetResult[nikkei225jp.MarketRankingData]{Metadata: metadata, Data: data}, nil
}

// collectUSEquities は、米国主要銘柄を取引セッション別に取得します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: session、universe、symbols、limit。
//
// 返り値:
//   - any: 取得元情報と絞り込み済み米国主要銘柄。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectUSEquities(ctx context.Context, parameters map[string]any) (any, error) {
	var input usEquitiesParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	if input.Session == "" {
		input.Session = nikkei225jp.USMarketSessionRegular
	}
	switch input.Session {
	case nikkei225jp.USMarketSessionRegular, nikkei225jp.USMarketSessionPre, nikkei225jp.USMarketSessionAfter:
	default:
		return nil, invalidArgument("sessionはregular、pre、afterのいずれかにしてください", nil)
	}
	if input.Universe != "" && input.Universe != "fang_plus" && input.Universe != "dow30" &&
		input.Universe != "nasdaq100" && input.Universe != "other" {
		return nil, invalidArgument("universeはfang_plus、dow30、nasdaq100、otherのいずれかにしてください", nil)
	}
	data, metadata, err := c.client.FetchUSEquities(ctx, input.Session)
	if err != nil {
		return nil, err
	}
	symbols := makeStringSet(input.Symbols, true)
	data.Equities = filterAndLimit(data.Equities, input.Limit, func(item nikkei225jp.USEquity) bool {
		return (input.Universe == "" || item.Universe == input.Universe) && matchesSet(item.Symbol, symbols, true)
	})
	return datasetResult[nikkei225jp.USEquityData]{Metadata: metadata, Data: data}, nil
}

// collectUSIndustries は、米国業種指数を取得して件数制限します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: limit。
//
// 返り値:
//   - any: 取得元情報と米国業種指数。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectUSIndustries(ctx context.Context, parameters map[string]any) (any, error) {
	var input limitParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	data, metadata, err := c.client.FetchUSIndustries(ctx)
	if err != nil {
		return nil, err
	}
	data.Industries = limitSlice(data.Industries, input.Limit)
	return datasetResult[nikkei225jp.USIndustryData]{Metadata: metadata, Data: data}, nil
}

// collectADR は、ADR・PTS一覧を取得して条件で絞ります。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: codes、symbols、main_only、limit。
//
// 返り値:
//   - any: 取得元情報と絞り込み済みADR・PTS一覧。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectADR(ctx context.Context, parameters map[string]any) (any, error) {
	var input adrParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	data, metadata, err := c.client.FetchADR(ctx)
	if err != nil {
		return nil, err
	}
	codes := makeStringSet(input.Codes, false)
	symbols := makeStringSet(input.Symbols, true)
	data.Quotes = filterAndLimit(data.Quotes, input.Limit, func(item nikkei225jp.ADRQuote) bool {
		return matchesSet(item.Code, codes, false) &&
			matchesSet(item.ADRSymbol, symbols, true) &&
			(!input.MainOnly || item.Main)
	})
	return datasetResult[nikkei225jp.ADRData]{Metadata: metadata, Data: data}, nil
}

// collectFXRates は、為替レート表を取得して件数制限します。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: codesとlimit。
//
// 返り値:
//   - any: 取得元情報と為替レート一覧。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectFXRates(ctx context.Context, parameters map[string]any) (any, error) {
	var input listParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	quotes, metadata, err := c.client.FetchFXRates(ctx, input.Codes)
	if err != nil {
		var inputErr *nikkei225jp.InputError
		if errors.As(err, &inputErr) {
			return nil, invalidArgument(inputErr.Error(), err)
		}
		return nil, err
	}
	quotes = limitSlice(quotes, input.Limit)
	return datasetResult[fxRatesData]{Metadata: metadata, Data: fxRatesData{Quotes: quotes}}, nil
}

// collectCryptoAssets は、暗号資産一覧を取得して条件で絞ります。
//
// 引数:
//   - ctx context.Context: 上流要求のコンテキスト。
//   - parameters map[string]any: codes、symbols、国内取扱、limit。
//
// 返り値:
//   - any: 取得元情報と絞り込み済み暗号資産一覧。
//   - error: 入力または上流取得のエラー。
func (c *Collector) collectCryptoAssets(ctx context.Context, parameters map[string]any) (any, error) {
	var input cryptoParameters
	if err := decodeAndValidateLimit(parameters, &input, &input.Limit); err != nil {
		return nil, err
	}
	data, metadata, err := c.client.FetchCryptoAssets(ctx)
	if err != nil {
		return nil, err
	}
	codes := makeStringSet(input.Codes, false)
	symbols := makeStringSet(input.Symbols, true)
	data.Assets = filterAndLimit(data.Assets, input.Limit, func(item nikkei225jp.CryptoAsset) bool {
		availabilityMatches := input.AvailableInJapan == nil ||
			(item.AvailableInJapan != nil && *item.AvailableInJapan == *input.AvailableInJapan)
		return matchesSet(item.Code, codes, false) &&
			matchesSet(item.Symbol, symbols, true) && availabilityMatches
	})
	return datasetResult[nikkei225jp.CryptoAssetData]{Metadata: metadata, Data: data}, nil
}

// ----------------------------------------

// validateCurrentSection は、現在値を取得できる市場分類か確認します。
//
// 引数:
//   - section nikkei225jp.MarketSection: 利用者が指定した市場分類。
//
// 返り値:
//   - error: 未対応分類または現在値配信がない分類の場合のエラー。
func validateCurrentSection(section nikkei225jp.MarketSection) error {
	information, err := nikkei225jp.MarketSectionInformation(section)
	if err != nil {
		return err
	}
	if !information.CurrentAvailable {
		return fmt.Errorf("section %qには現在値配信がありません", section)
	}
	return nil
}

// validateChartSource は、市場分類、範囲、コードの組み合わせを確認します。
//
// 引数:
//   - input chartParameters: 市場分類、チャート範囲、任意の対象コード。
//
// 返り値:
//   - error: 未対応分類、範囲、配信なし、許可されていないコードの場合のエラー。
func validateChartSource(input chartParameters) error {
	information, err := nikkei225jp.MarketSectionInformation(input.Section)
	if err != nil {
		return err
	}

	var allowedCodes []string
	switch input.Range {
	case nikkei225jp.MarketChartRangeIntraday:
		allowedCodes = information.IntradayCodes
		if len(input.Codes) > 1 {
			allowedCodes = information.IntradayCompositeCodes
		}
	case nikkei225jp.MarketChartRangeHistory:
		allowedCodes = information.HistoryCodes
	default:
		return fmt.Errorf("rangeはintradayまたはhistoryにしてください: %q", input.Range)
	}
	if len(allowedCodes) == 0 {
		return fmt.Errorf("section %qにはrange %qのチャート配信がありません", input.Section, input.Range)
	}
	if len(input.Codes) == 0 {
		return nil
	}

	allowedSet := makeStringSet(allowedCodes, false)
	for _, code := range input.Codes {
		if !matchesSet(code, allowedSet, false) {
			return fmt.Errorf("section %qのrange %qで許可されていないcodeです: %q", input.Section, input.Range, code)
		}
	}
	return nil
}

// ----------------------------------------

// decodeParameters は、未知項目を拒否して共通マップを型付き入力へ変換します。
//
// 引数:
//   - parameters map[string]any: RESTまたはMCPから受けたJSONオブジェクト。
//   - target any: JSONを格納する構造体ポインター。
//
// 返り値:
//   - error: JSON変換、未知項目、余分なJSON値を検出した場合の入力エラー。
func decodeParameters(parameters map[string]any, target any) error {
	targetType := reflect.TypeOf(target)
	if targetType.Kind() != reflect.Pointer || targetType.Elem().Kind() != reflect.Struct {
		return invalidArgument("parametersの復号先が構造体ポインターではありません", nil)
	}
	allowed := make(map[string]struct{}, targetType.Elem().NumField())
	for index := 0; index < targetType.Elem().NumField(); index++ {
		jsonName := strings.Split(targetType.Elem().Field(index).Tag.Get("json"), ",")[0]
		if jsonName != "" && jsonName != "-" {
			allowed[jsonName] = struct{}{}
		}
	}
	for key := range parameters {
		if _, exists := allowed[key]; !exists {
			return invalidArgument(fmt.Sprintf("parametersに未知の項目があります: %q", key), nil)
		}
	}

	body, err := json.Marshal(parameters)
	if err != nil {
		return invalidArgument("parametersをJSONへ変換できません", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidArgument("parametersが不正です", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalidArgument("parametersに余分なJSON値があります", err)
	}
	return nil
}

// decodeAndValidateLimit は、入力を変換して共通件数上限を検証します。
//
// 引数:
//   - parameters map[string]any: 変換元のJSONオブジェクト。
//   - target any: limitを含む構造体ポインター。
//   - limit *int: 変換後に検証するlimitフィールド。
//
// 返り値:
//   - error: 入力変換またはlimit範囲のエラー。
func decodeAndValidateLimit(parameters map[string]any, target any, limit *int) error {
	if err := decodeParameters(parameters, target); err != nil {
		return err
	}
	if *limit < 0 || *limit > maximumListLimit {
		return invalidArgument(fmt.Sprintf("limitは0から%dの範囲にしてください", maximumListLimit), nil)
	}
	return nil
}

// validateChartFilter は、チャートのローカル出力条件を検証します。
//
// 引数:
//   - input chartParameters: Unixミリ秒範囲と最大点数を含む入力。
//
// 返り値:
//   - error: 負数、逆転範囲、最大点数超過の場合のエラー。
func validateChartFilter(input chartParameters) error {
	if input.FromMillis < 0 || input.ToMillis < 0 {
		return errors.New("from_millisとto_millisは0以上にしてください")
	}
	if input.FromMillis > maximumSafeJSONInteger || input.ToMillis > maximumSafeJSONInteger {
		return fmt.Errorf("from_millisとto_millisは%d以下にしてください", maximumSafeJSONInteger)
	}
	if input.FromMillis > 0 && input.ToMillis > 0 && input.FromMillis > input.ToMillis {
		return errors.New("from_millisはto_millis以下にしてください")
	}
	if input.MaxPointsPerSeries < 0 || input.MaxPointsPerSeries > 250000 {
		return errors.New("max_points_per_seriesは0から250000の範囲にしてください")
	}
	return nil
}

// applyChartFilter は、取得済み点列へ時刻範囲と最大点数を適用します。
//
// 引数:
//   - data *nikkei225jp.MarketChartData: 書き換える取得済みチャート。
//   - input chartParameters: 検証済みのローカル出力条件。
//
// 返り値:
//   - なし。各系列のPointsを直接置き換える。
func applyChartFilter(data *nikkei225jp.MarketChartData, input chartParameters) {
	for index := range data.Series {
		points := data.Series[index].Points
		filtered := make([]nikkei225jp.ChartPoint, 0, len(points))
		for _, point := range points {
			if input.FromMillis > 0 && point.TimestampMillis < input.FromMillis {
				continue
			}
			if input.ToMillis > 0 && point.TimestampMillis > input.ToMillis {
				continue
			}
			filtered = append(filtered, point)
		}
		data.Series[index].Points = evenlyLimitPoints(filtered, input.MaxPointsPerSeries)
	}
}

// evenlyLimitPoints は、先頭と末尾を保ちながら点列を均等に間引きます。
//
// 引数:
//   - points []nikkei225jp.ChartPoint: 時刻順の取得済み点列。
//   - limit int: 0なら全点、それ以外は返す最大点数。
//
// 返り値:
//   - []nikkei225jp.ChartPoint: 元点列を共有しない抽出済み点列。
func evenlyLimitPoints(points []nikkei225jp.ChartPoint, limit int) []nikkei225jp.ChartPoint {
	if limit == 0 || len(points) <= limit {
		return append([]nikkei225jp.ChartPoint(nil), points...)
	}
	if limit == 1 {
		return []nikkei225jp.ChartPoint{points[len(points)-1]}
	}
	result := make([]nikkei225jp.ChartPoint, limit)
	for index := range result {
		result[index] = points[index*(len(points)-1)/(limit-1)]
	}
	return result
}

// makeStringSet は、任意の文字列一覧を照合用集合へ変換します。
//
// 引数:
//   - values []string: 集合へ格納する文字列一覧。
//   - foldCase bool: trueの場合は大文字へ正規化する。
//
// 返り値:
//   - map[string]struct{}: 重複を除いた集合。未指定ならnil。
func makeStringSet(values []string, foldCase bool) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if foldCase {
			value = strings.ToUpper(value)
		}
		result[value] = struct{}{}
	}
	return result
}

// matchesSet は、集合未指定または値が集合に含まれるか確認します。
//
// 引数:
//   - value string: 確認する値。
//   - values map[string]struct{}: 許可値集合。nilなら全件許可。
//   - foldCase bool: trueの場合は大文字へ正規化して照合する。
//
// 返り値:
//   - bool: 値を出力対象に含める場合はtrue。
func matchesSet(value string, values map[string]struct{}, foldCase bool) bool {
	if values == nil {
		return true
	}
	if foldCase {
		value = strings.ToUpper(value)
	}
	_, exists := values[value]
	return exists
}

// filterAndLimit は、一覧を述語で絞って指定件数まで複製します。
//
// 引数:
//   - values []T: 配信順を維持する元一覧。
//   - limit int: 0なら全件、それ以外は最大返却件数。
//   - keep func(T) bool: 出力対象ならtrueを返す述語。
//
// 返り値:
//   - []T: 配信順を維持した絞り込み済み一覧。
func filterAndLimit[T any](values []T, limit int, keep func(T) bool) []T {
	result := make([]T, 0, len(values))
	for _, value := range values {
		if !keep(value) {
			continue
		}
		result = append(result, value)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// limitSlice は、一覧を指定件数まで複製します。
//
// 引数:
//   - values []T: 配信順を維持する元一覧。
//   - limit int: 0なら全件、それ以外は最大返却件数。
//
// 返り値:
//   - []T: 元一覧と要素を共有しない指定件数までの一覧。
func limitSlice[T any](values []T, limit int) []T {
	length := len(values)
	if limit > 0 && limit < length {
		length = limit
	}
	return append([]T(nil), values[:length]...)
}

// invalidArgument は、公開可能な入力エラーを生成します。
//
// 引数:
//   - message string: 利用者へ返す日本語メッセージ。
//   - cause error: ログだけに利用する内部原因。
//
// 返り値:
//   - error: INVALID_ARGUMENT分類の共通エラー。
func invalidArgument(message string, cause error) error {
	return domain.NewServiceError(domain.ErrorInvalidArgument, message, cause)
}

// isNilClient は、インターフェース内の型付きnilも含めて確認します。
//
// 引数:
//   - client Client: nilかどうかを確認する取得クライアント。
//
// 返り値:
//   - bool: インターフェースまたは内包値がnilの場合はtrue。
func isNilClient(client Client) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ----------------------------------------

// datasetDescriptors は、225225.jpの固定データセット仕様を生成します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - []domain.DatasetDescriptor: collectの入力仕様を含む13種類の一覧。
func datasetDescriptors() []domain.DatasetDescriptor {
	limit := parameter("limit", "integer", false, "返す最大件数。0は全件、最大1000。", nil, 0)
	codes := parameter("codes", "array<string>", false, "返すコード。省略時は配信全件。", nil, nil)
	chartCodes := parameter(
		"codes",
		"array<string>",
		false,
		"返す系列コード。catalogのintraday_single_only_codesは1件だけ指定でき、historyはhistory_codesだけを指定できます。",
		nil,
		nil,
	)
	return []domain.DatasetDescriptor{
		{Name: "catalog", Description: "対応市場、コード、チャート範囲を通信せず返します。", Parameters: []domain.ParameterDescriptor{}},
		{Name: "current", Description: "分類別の現在値、変化、騰落率、高値、安値、配信時刻を返します。", Parameters: []domain.ParameterDescriptor{
			parameter("section", "string", false, "現在値配信がある市場分類。", currentMarketSections(), "top"), codes,
		}},
		{Name: "chart", Description: "分類別の短期または日足全履歴チャートを返します。", Parameters: []domain.ParameterDescriptor{
			parameter("section", "string", false, "いずれかのチャート配信がある市場分類。range別の対応はcatalogで確認します。", chartMarketSections(), "top"),
			parameter("range", "string", false, "チャート範囲。", []string{"intraday", "history"}, "intraday"), chartCodes,
			parameter("from_millis", "integer", false, "このUnixミリ秒以降の点だけを返します。最大9007199254740991。", nil, 0),
			parameter("to_millis", "integer", false, "このUnixミリ秒以前の点だけを返します。最大9007199254740991。", nil, 0),
			parameter("max_points_per_series", "integer", false, "系列ごとの最大点数。最大250000。", nil, 0),
		}},
		{Name: "japan_components", Description: "日経225構成銘柄、価格、ウェイト、寄与度を返します。", Parameters: []domain.ParameterDescriptor{codes, limit}},
		{Name: "japan_contributors", Description: "日経225への寄与度上位・下位を返します。", Parameters: []domain.ParameterDescriptor{limit}},
		{Name: "japan_industries", Description: "東証33業種の指数値と変化を返します。", Parameters: []domain.ParameterDescriptor{limit}},
		{Name: "japan_ranking", Description: "日本株の値上がり、値下がり、出来高ランキングを返します。", Parameters: []domain.ParameterDescriptor{
			parameter("kind", "string", false, "返すランキング分類。", []string{"all", "gainers", "losers", "active"}, "all"), limit,
		}},
		{Name: "us_equities", Description: "FANG+、DOW30、NASDAQ100等の通常・時間外価格を返します。", Parameters: []domain.ParameterDescriptor{
			parameter("session", "string", false, "米国取引セッション。", []string{"regular", "pre", "after"}, "regular"),
			parameter("universe", "string", false, "指数分類。省略時は全分類。", []string{"fang_plus", "dow30", "nasdaq100", "other"}, nil),
			parameter("symbols", "array<string>", false, "返すティッカー。", nil, nil), limit,
		}},
		{Name: "us_industries", Description: "米国業種指数の値と変化を返します。", Parameters: []domain.ParameterDescriptor{limit}},
		{Name: "us_ranking", Description: "米国株の値上がり、値下がり、出来高ランキングを返します。", Parameters: []domain.ParameterDescriptor{
			parameter("kind", "string", false, "返すランキング分類。", []string{"all", "gainers", "losers", "active"}, "all"), limit,
		}},
		{Name: "adr", Description: "日本株のADR、PTS、東証価格と比較率を返します。", Parameters: []domain.ParameterDescriptor{
			codes, parameter("symbols", "array<string>", false, "返すADRティッカー。", nil, nil),
			parameter("main_only", "boolean", false, "主要銘柄だけを返します。", nil, false), limit,
		}},
		{Name: "fx_rates", Description: "主要・新興国通貨の為替レート、変化、騰落率を返します。", Parameters: []domain.ParameterDescriptor{codes, limit}},
		{Name: "crypto_assets", Description: "暗号資産の円価格、時価総額、期間別騰落率を返します。", Parameters: []domain.ParameterDescriptor{
			codes, parameter("symbols", "array<string>", false, "返す暗号資産シンボル。", nil, nil),
			parameter("available_in_japan", "boolean", false, "国内取扱フラグで絞ります。", nil, nil), limit,
		}},
	}
}

// parameter は、データセット入力項目の説明を生成します。
//
// 引数:
//   - name string: JSON項目名。
//   - valueType string: JSON上の値型。
//   - required bool: 必須入力かどうか。
//   - description string: 利用者向け説明。
//   - allowed []string: 省略可能な許可値一覧。
//   - defaultValue any: 省略時の既定値。
//
// 返り値:
//   - domain.ParameterDescriptor: datalistへ掲載する入力仕様。
func parameter(
	name string,
	valueType string,
	required bool,
	description string,
	allowed []string,
	defaultValue any,
) domain.ParameterDescriptor {
	return domain.ParameterDescriptor{
		Name: name, Type: valueType, Required: required, Description: description,
		Allowed: allowed, Default: defaultValue,
	}
}

// currentMarketSections は、現在値配信がある市場分類を固定順で返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - []string: currentで利用できる市場分類。
func currentMarketSections() []string {
	sections := nikkei225jp.MarketSections()
	result := make([]string, 0, len(sections))
	for _, section := range sections {
		if section.CurrentAvailable {
			result = append(result, string(section.Section))
		}
	}
	return result
}

// chartMarketSections は、短期または履歴チャート配信がある市場分類を返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - []string: chartのsection候補。range別対応はcatalogで確認する。
func chartMarketSections() []string {
	sections := nikkei225jp.MarketSections()
	result := make([]string, 0, len(sections))
	for _, section := range sections {
		if len(section.IntradayCodes) > 0 || len(section.HistoryCodes) > 0 {
			result = append(result, string(section.Section))
		}
	}
	return result
}
