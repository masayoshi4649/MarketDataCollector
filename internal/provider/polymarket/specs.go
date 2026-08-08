// Package polymarket は、Polymarketの公開読み取りAPIを収集します。
package polymarket

import (
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

const (
	DefaultGammaBaseURL           = "https://gamma-api.polymarket.com"
	DefaultCLOBBaseURL            = "https://clob.polymarket.com"
	DefaultDataBaseURL            = "https://data-api.polymarket.com"
	DefaultUserAgent              = "MarketDataCollector/0.1"
	DefaultMaxResponseBytes int64 = 16 * 1024 * 1024
	SpecificationURL              = "https://docs.polymarket.com/llms.txt"
	TermsURL                      = "https://polymarket.com/tos"
	SpecificationReviewDate       = "2026-08-08"
)

type apiService string

const (
	serviceGamma apiService = "gamma"
	serviceCLOB  apiService = "clob"
	serviceData  apiService = "data"
)

type parameterType string

const (
	typeString       parameterType = "string"
	typeInteger      parameterType = "integer"
	typeNumber       parameterType = "number"
	typeBoolean      parameterType = "boolean"
	typeStringArray  parameterType = "array<string>"
	typeIntegerArray parameterType = "array<integer>"
)

type queryEncoding string

const (
	encodingCSV    queryEncoding = "csv"
	encodingRepeat queryEncoding = "repeat"
)

type validatorKind string

const (
	validatorNone      validatorKind = ""
	validatorSlug      validatorKind = "slug"
	validatorToken     validatorKind = "token"
	validatorWallet    validatorKind = "wallet"
	validatorCondition validatorKind = "condition"
)

type routeKind string

const (
	routeFixed       routeKind = "fixed"
	routeEntity      routeKind = "entity"
	routeTag         routeKind = "tag"
	routeRelatedTags routeKind = "related_tags"
	routeSeriesItem  routeKind = "series_item"
	routeComments    routeKind = "comments"
	routeTokenPrice  routeKind = "token_price"
	routeCLOBMarkets routeKind = "clob_markets"
	routeCondition   routeKind = "condition"
	routeTokenPath   routeKind = "token_path"
)

type paginationMode string

const (
	paginationNone   paginationMode = ""
	paginationPage   paginationMode = "page"
	paginationKeyset paginationMode = "keyset"
	paginationOffset paginationMode = "offset"
)

type normalizerKind string

const (
	normalizeRaw        normalizerKind = "raw"
	normalizeSearch     normalizerKind = "search"
	normalizeEvents     normalizerKind = "events"
	normalizeEvent      normalizerKind = "event"
	normalizeMarkets    normalizerKind = "markets"
	normalizeMarket     normalizerKind = "market"
	normalizeBook       normalizerKind = "book"
	normalizeTokenQuote normalizerKind = "token_quote"
)

type rateClass string

const (
	rateGammaGeneral  rateClass = "gamma_general"
	rateGammaSearch   rateClass = "gamma_search"
	rateGammaEvents   rateClass = "gamma_events"
	rateGammaMarkets  rateClass = "gamma_markets"
	rateGammaTags     rateClass = "gamma_tags"
	rateGammaComments rateClass = "gamma_comments"
	rateDataGeneral   rateClass = "data_general"
	rateDataTrades    rateClass = "data_trades"
	rateDataPositions rateClass = "data_positions"
	rateDataClosed    rateClass = "data_closed_positions"
	rateCLOBGeneral   rateClass = "clob_general"
	rateCLOBQuote     rateClass = "clob_quote"
	rateCLOBHistory   rateClass = "clob_history"
	rateCLOBTick      rateClass = "clob_tick"
)

type parameterSpec struct {
	Name         string
	UpstreamName string
	Description  string
	Type         parameterType
	Required     bool
	Allowed      []string
	Default      any
	Minimum      *float64
	Maximum      *float64
	MaxLength    int
	MaxItems     int
	Validator    validatorKind
	PathOnly     bool
	Encoding     queryEncoding
}

type endpointSpec struct {
	Dataset     string
	Description string
	Service     apiService
	Path        string
	Route       routeKind
	Parameters  []parameterSpec
	QueryNames  []string
	Pagination  paginationMode
	Normalizer  normalizerKind
	RateClass   rateClass
}

var endpointSpecs = buildEndpointSpecs()

// ----------------------------------------

// buildEndpointSpecs は、37件の公開Polymarket dataset仕様を生成します。
//
// 機能:
//   - 固定path、query、正規化、ページング、rate classを一元定義する
//
// 引数:
//   - なし
//
// 返り値:
//   - []endpointSpec: client、collector、Descriptorが共有する固定仕様
func buildEndpointSpecs() []endpointSpec {
	eventsLimit := integerParameter("limit", "limit", "1回に取得する最大件数です。ローカル既定は10件、上限は500件です。", false, 1, 500, 10)
	marketsLimit := integerParameter("limit", "limit", "1回に取得する最大件数です。ローカル既定は10件、上限は100件です。", false, 1, 100, 10)
	offset100k := integerParameter("offset", "offset", "取得開始位置です。", false, 0, 100000, 0)
	afterCursor := stringParameter("after_cursor", "after_cursor", "次ページのkeyset cursorです。", false, 1000)
	wallet := validatedStringParameter("address", "user", "公開ウォレットアドレスです。", true, 42, validatorWallet)
	markets := arrayParameter("markets", "market", "condition IDのJSON文字列配列です。event_idsとは同時指定できません。", typeStringArray, 100, validatorCondition)
	eventIDs := arrayParameter("event_ids", "eventId", "イベントIDのJSON整数配列です。marketsとは同時指定できません。", typeIntegerArray, 100, validatorNone)
	condition := validatedStringParameter("condition_id", "condition_id", "市場のcondition IDです。", true, 100, validatorCondition)
	token := validatedStringParameter("token_id", "token_id", "CLOBトークンIDです。", true, 100, validatorToken)

	result := []endpointSpec{
		{Dataset: "search", Description: "イベント、市場、タグをキーワード検索します。", Service: serviceGamma, Path: "/public-search", Route: routeFixed, Pagination: paginationPage, Normalizer: normalizeSearch, RateClass: rateGammaSearch, QueryNames: []string{"q", "limit_per_type", "page", "events_status", "keep_closed_markets", "search_profiles"}, Parameters: []parameterSpec{
			stringParameter("query", "q", "検索語です。", true, 200), integerParameter("limit_per_type", "limit_per_type", "種類ごとの取得件数です。", false, 1, 20, 5), integerParameter("page", "page", "取得ページです。", false, 1, 100, 1), booleanParameter("include_closed_markets", "", "終了市場も検索対象へ含めます。", false, false),
		}},
		{Dataset: "events", Description: "イベント一覧をkeyset方式で取得します。", Service: serviceGamma, Path: "/events/keyset", Route: routeFixed, Pagination: paginationKeyset, Normalizer: normalizeEvents, RateClass: rateGammaEvents, QueryNames: []string{"limit", "after_cursor", "closed", "live", "title_search", "tag_slug", "order", "ascending"}, Parameters: []parameterSpec{
			eventsLimit, afterCursor, booleanParameter("closed", "closed", "終了イベントを取得します。", false, false), booleanOptionalParameter("live", "live", "ライブイベントに絞ります。"), stringParameter("title_search", "title_search", "タイトル検索語です。", false, 200), validatedStringParameter("tag_slug", "tag_slug", "タグslugです。", false, 100, validatorSlug), enumParameter("order", "order", "並び順の項目です。", false, []string{"volume24hr", "volume", "liquidity", "startDate", "endDate"}, "volume24hr"), booleanParameter("ascending", "ascending", "昇順の場合はtrueです。", false, false),
		}},
		{Dataset: "event", Description: "slugまたはIDでイベント詳細を取得します。", Service: serviceGamma, Path: "/events", Route: routeEntity, Normalizer: normalizeEvent, RateClass: rateGammaEvents, Parameters: []parameterSpec{
			validatedStringParameter("slug", "slug", "イベントslugです。idとは排他です。", false, 200, validatorSlug), integerOptionalParameter("id", "id", "イベントIDです。slugとは排他です。", 1, 1e15),
		}},
		{Dataset: "markets", Description: "市場一覧をkeyset方式で取得します。", Service: serviceGamma, Path: "/markets/keyset", Route: routeFixed, Pagination: paginationKeyset, Normalizer: normalizeMarkets, RateClass: rateGammaMarkets, QueryNames: []string{"limit", "after_cursor", "closed", "tag_id", "liquidity_num_min", "volume_num_min", "order", "ascending"}, Parameters: []parameterSpec{
			marketsLimit, afterCursor, booleanParameter("closed", "closed", "終了市場を取得します。", false, false), integerOptionalParameter("tag_id", "tag_id", "タグIDです。", 1, 1e15), numberOptionalParameter("minimum_liquidity", "liquidity_num_min", "最低流動性です。", 0, 1e18), numberOptionalParameter("minimum_volume", "volume_num_min", "最低出来高です。", 0, 1e18), enumParameter("order", "order", "並び順の項目です。", false, []string{"volume24hr", "volume", "liquidity", "startDate", "endDate"}, "volume24hr"), booleanParameter("ascending", "ascending", "昇順の場合はtrueです。", false, false),
		}},
		{Dataset: "market", Description: "slugまたはIDで市場詳細を取得します。", Service: serviceGamma, Path: "/markets", Route: routeEntity, Normalizer: normalizeMarket, RateClass: rateGammaMarkets, Parameters: []parameterSpec{
			validatedStringParameter("slug", "slug", "市場slugです。idとは排他です。", false, 200, validatorSlug), integerOptionalParameter("id", "id", "市場IDです。slugとは排他です。", 1, 1e15),
		}},
		{Dataset: "order_book", Description: "CLOB注文板を取得し、価格順と最良気配を正規化します。", Service: serviceCLOB, Path: "/book", Route: routeFixed, Normalizer: normalizeBook, RateClass: rateCLOBQuote, QueryNames: []string{"token_id"}, Parameters: []parameterSpec{token}},
		{Dataset: "token_price", Description: "best bid、best ask、midpoint、最終約定価格を取得します。", Service: serviceCLOB, Path: "/price", Route: routeTokenPrice, Normalizer: normalizeTokenQuote, RateClass: rateCLOBQuote, QueryNames: []string{"token_id", "side"}, Parameters: []parameterSpec{token, enumParameter("price_type", "", "価格種別です。BUYはbest bid、SELLはbest askとして取得します。", false, []string{"best_bid", "best_ask", "midpoint", "last_trade"}, "midpoint")}},
		{Dataset: "price_history", Description: "CLOBトークンの価格履歴を取得します。", Service: serviceCLOB, Path: "/prices-history", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateCLOBHistory, QueryNames: []string{"market", "interval", "fidelity", "startTs", "endTs"}, Parameters: []parameterSpec{
			validatedStringParameter("token_id", "market", "CLOBトークンIDです。", true, 100, validatorToken), enumParameter("interval", "interval", "相対取得期間です。絶対期間とは排他です。", false, []string{"max", "all", "1m", "1w", "1d", "6h", "1h"}, "1w"), integerParameter("fidelity", "fidelity", "分単位の粒度です。", false, 1, 1440, 60), integerOptionalParameter("start_timestamp", "startTs", "取得開始Unix秒です。", 0, 9e15), integerOptionalParameter("end_timestamp", "endTs", "取得終了Unix秒です。", 0, 9e15),
		}},
		{Dataset: "user_positions", Description: "公開ウォレットの現在ポジションを取得します。", Service: serviceData, Path: "/positions", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateDataPositions, QueryNames: []string{"user", "market", "eventId", "sizeThreshold", "redeemable", "mergeable", "limit", "offset", "sortBy", "sortDirection", "title"}, Parameters: []parameterSpec{wallet, markets, eventIDs, numberParameter("minimum_size", "sizeThreshold", "最低トークン数です。", false, 0, 1e18, 1), booleanParameter("redeemable", "redeemable", "償還可能なポジションに絞ります。", false, false), booleanParameter("mergeable", "mergeable", "統合可能なポジションに絞ります。", false, false), integerParameter("limit", "limit", "取得件数です。", false, 1, 500, 100), integerParameter("offset", "offset", "取得開始位置です。", false, 0, 10000, 0), enumParameter("sort_by", "sortBy", "並び替え項目です。", false, []string{"CURRENT", "INITIAL", "TOKENS", "CASHPNL", "PERCENTPNL", "TITLE", "RESOLVING", "PRICE", "AVGPRICE"}, "TOKENS"), enumParameter("sort_direction", "sortDirection", "並び順です。", false, []string{"ASC", "DESC"}, "DESC"), stringParameter("title", "title", "タイトル検索語です。", false, 100)}},
		{Dataset: "user_activity", Description: "公開ウォレットのアクティビティを取得します。", Service: serviceData, Path: "/activity", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateDataPositions, QueryNames: []string{"user", "market", "eventId", "limit", "offset", "type", "start", "end", "sortBy", "sortDirection", "side", "excludeDepositsWithdrawals"}, Parameters: []parameterSpec{wallet, markets, eventIDs, integerParameter("limit", "limit", "取得件数です。", false, 1, 500, 100), integerParameter("offset", "offset", "取得開始位置です。", false, 0, 5000, 0), enumArrayParameter("types", "type", "活動種別のJSON文字列配列です。", []string{"TRADE", "SPLIT", "MERGE", "REDEEM", "REWARD", "CONVERSION", "DEPOSIT", "WITHDRAWAL", "YIELD", "MAKER_REBATE", "TAKER_REBATE", "REFERRAL_REWARD"}, 20), integerOptionalParameter("start_timestamp", "start", "開始Unix秒です。", 0, 9e15), integerOptionalParameter("end_timestamp", "end", "終了Unix秒です。", 0, 9e15), enumParameter("sort_by", "sortBy", "並び替え項目です。", false, []string{"TIMESTAMP", "TOKENS", "CASH"}, "TIMESTAMP"), enumParameter("sort_direction", "sortDirection", "並び順です。", false, []string{"ASC", "DESC"}, "DESC"), enumOptionalParameter("side", "side", "売買方向です。", []string{"BUY", "SELL"}), booleanParameter("include_deposits_and_withdrawals", "", "入出金活動も含めます。", false, false)}},
	}
	for specIndex := range result {
		spec := &result[specIndex]
		if spec.Dataset == "events" || spec.Dataset == "markets" {
			for parameterIndex := range spec.Parameters {
				if spec.Parameters[parameterIndex].Name == "order" {
					spec.Parameters[parameterIndex].Allowed = nil
					spec.Parameters[parameterIndex].MaxLength = 100
					spec.Parameters[parameterIndex].Default = "volume24hr"
				}
				if spec.Parameters[parameterIndex].Name == "ascending" {
					spec.Parameters[parameterIndex].Description += " ローカル既定はfalseです。"
				}
			}
		}
		if spec.Dataset == "markets" {
			tagIDs := arrayParameter("tag_ids", "tag_id", "複数タグIDのJSON整数配列です。tag_idとは排他で、同名query反復へ変換します。", typeIntegerArray, 100, validatorNone)
			tagIDs.Encoding = encodingRepeat
			spec.Parameters = append(spec.Parameters, tagIDs)
		}
		if spec.Dataset == "user_activity" {
			for parameterIndex := range spec.Parameters {
				if spec.Parameters[parameterIndex].Name == "types" {
					spec.Parameters[parameterIndex].MaxItems = 12
				}
			}
		}
		if spec.Dataset == "price_history" {
			for parameterIndex := range spec.Parameters {
				switch spec.Parameters[parameterIndex].Name {
				case "interval":
					spec.Parameters[parameterIndex].Description += " ローカル既定は1wです。"
				case "fidelity":
					spec.Parameters[parameterIndex].Description += " ローカル既定は60分です。"
				}
			}
		}
	}
	result = append(result, dataAdditionalSpecs(wallet, markets, eventIDs)...)
	result = append(result, gammaAdditionalSpecs(offset100k)...)
	result = append(result, clobAdditionalSpecs(token, condition)...)
	return result
}

// ----------------------------------------

// dataAdditionalSpecs は、Data APIの追加9 dataset仕様を生成します。
//
// 機能:
//   - Data API固有の公開queryと安全なローカル上限を定義する
//
// 引数:
//   - wallet parameterSpec: 公開ウォレット項目
//   - markets parameterSpec: condition ID配列項目
//   - eventIDs parameterSpec: イベントID配列項目
//
// 返り値:
//   - []endpointSpec: Data API追加仕様
func dataAdditionalSpecs(wallet, markets, eventIDs parameterSpec) []endpointSpec {
	return []endpointSpec{
		{Dataset: "trades", Description: "公開取引履歴を取得します。", Service: serviceData, Path: "/trades", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateDataTrades, QueryNames: []string{"user", "market", "eventId", "limit", "offset", "takerOnly", "filterType", "filterAmount", "side", "start", "end"}, Parameters: []parameterSpec{optional(wallet), markets, eventIDs, integerParameter("limit", "limit", "取得件数です。", false, 1, 10000, 100), integerParameter("offset", "offset", "取得開始位置です。", false, 0, 10000, 0), booleanParameter("taker_only", "takerOnly", "taker取引だけを取得します。", false, true), enumOptionalParameter("filter_type", "filterType", "絞り込みの単位です。filter_amountと組で指定します。", []string{"CASH", "TOKENS"}), numberOptionalParameter("filter_amount", "filterAmount", "絞り込み下限です。filter_typeと組で指定します。", 0, 1e18), enumOptionalParameter("side", "side", "売買方向です。", []string{"BUY", "SELL"}), integerOptionalParameter("start_timestamp", "start", "開始Unix秒です。", 0, 9e15), integerOptionalParameter("end_timestamp", "end", "終了Unix秒です。", 0, 9e15)}},
		{Dataset: "closed_positions", Description: "公開ウォレットの決済済みポジションを取得します。", Service: serviceData, Path: "/closed-positions", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateDataClosed, QueryNames: []string{"user", "market", "eventId", "title", "limit", "offset", "sortBy", "sortDirection"}, Parameters: []parameterSpec{wallet, markets, eventIDs, stringParameter("title", "title", "タイトル検索語です。", false, 100), integerParameter("limit", "limit", "取得件数です。", false, 1, 50, 10), integerParameter("offset", "offset", "取得開始位置です。", false, 0, 100000, 0), enumParameter("sort_by", "sortBy", "並び替え項目です。", false, []string{"REALIZEDPNL", "TITLE", "PRICE", "AVGPRICE", "TIMESTAMP"}, "REALIZEDPNL"), enumParameter("sort_direction", "sortDirection", "並び順です。", false, []string{"ASC", "DESC"}, "DESC")}},
		{Dataset: "holders", Description: "市場トークンの保有者を取得します。", Service: serviceData, Path: "/holders", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateDataGeneral, QueryNames: []string{"market", "limit", "minBalance"}, Parameters: []parameterSpec{required(markets), integerParameter("limit", "limit", "市場ごとの取得件数です。", false, 1, 20, 20), integerParameter("minimum_balance", "minBalance", "最低残高です。", false, 0, 999999, 1)}},
		{Dataset: "market_positions", Description: "市場単位のポジションを取得します。", Service: serviceData, Path: "/v1/market-positions", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateDataPositions, QueryNames: []string{"market", "user", "status", "sortBy", "sortDirection", "limit", "offset"}, Parameters: []parameterSpec{validatedStringParameter("market", "market", "単一の64桁condition hashです。", true, 66, validatorCondition), optional(wallet), enumParameter("status", "status", "ポジション状態です。", false, []string{"OPEN", "CLOSED", "ALL"}, "ALL"), enumParameter("sort_by", "sortBy", "並び替え項目です。", false, []string{"TOKENS", "CASH_PNL", "REALIZED_PNL", "TOTAL_PNL"}, "TOTAL_PNL"), enumParameter("sort_direction", "sortDirection", "並び順です。", false, []string{"ASC", "DESC"}, "DESC"), integerParameter("limit", "limit", "取得件数です。", false, 1, 500, 50), integerParameter("offset", "offset", "取得開始位置です。", false, 0, 10000, 0)}},
		{Dataset: "position_value", Description: "公開ウォレットのポジション評価額を取得します。", Service: serviceData, Path: "/value", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateDataGeneral, QueryNames: []string{"user", "market"}, Parameters: []parameterSpec{wallet, markets}},
		{Dataset: "traded_markets_count", Description: "公開ウォレットが取引した市場数を取得します。", Service: serviceData, Path: "/traded", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateDataGeneral, QueryNames: []string{"user"}, Parameters: []parameterSpec{wallet}},
		{Dataset: "open_interest", Description: "市場の未決済建玉を取得します。", Service: serviceData, Path: "/oi", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateDataGeneral, QueryNames: []string{"market"}, Parameters: []parameterSpec{markets}},
		{Dataset: "live_volume", Description: "イベントのライブ出来高を取得します。", Service: serviceData, Path: "/live-volume", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateDataGeneral, QueryNames: []string{"id"}, Parameters: []parameterSpec{integerParameter("id", "id", "イベントIDです。", true, 1, 1e15, nil)}},
		{Dataset: "leaderboard", Description: "公開リーダーボードを取得します。", Service: serviceData, Path: "/v1/leaderboard", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateDataGeneral, QueryNames: []string{"category", "timePeriod", "orderBy", "limit", "offset", "user", "userName"}, Parameters: []parameterSpec{enumParameter("category", "category", "カテゴリです。", false, []string{"OVERALL", "POLITICS", "SPORTS", "ESPORTS", "CRYPTO", "CULTURE", "MENTIONS", "WEATHER", "ECONOMICS", "TECH", "FINANCE"}, "OVERALL"), enumParameter("time_period", "timePeriod", "集計期間です。", false, []string{"DAY", "WEEK", "MONTH", "ALL"}, "DAY"), enumParameter("order_by", "orderBy", "順位項目です。", false, []string{"PNL", "VOL"}, "PNL"), integerParameter("limit", "limit", "取得件数です。", false, 1, 50, 25), integerParameter("offset", "offset", "取得開始位置です。", false, 0, 1000, 0), optional(wallet), stringParameter("user_name", "userName", "公開ユーザー名です。", false, 100)}},
	}
}

// ----------------------------------------

// gammaAdditionalSpecs は、Gamma APIの追加10 dataset仕様を生成します。
//
// 機能:
//   - Gamma API固有のタグ、シリーズ、スポーツ、コメント仕様を定義する
//
// 引数:
//   - offset parameterSpec: offset項目の雛形
//
// 返り値:
//   - []endpointSpec: Gamma API追加仕様
func gammaAdditionalSpecs(offset parameterSpec) []endpointSpec {
	list := func(max int) []parameterSpec {
		return []parameterSpec{integerParameter("limit", "limit", "取得件数です。ローカル安全上限を適用します。", false, 1, float64(max), 100), offset, stringParameter("order", "order", "並び替え項目です。", false, 100), booleanParameter("ascending", "ascending", "昇順の場合はtrueです。", false, false)}
	}
	result := []endpointSpec{
		{Dataset: "tags", Description: "タグ一覧を取得します。", Service: serviceGamma, Path: "/tags", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateGammaTags, QueryNames: []string{"limit", "offset", "order", "ascending", "include_template", "is_carousel"}, Parameters: append(list(500), booleanParameter("include_template", "include_template", "テンプレート情報を含めます。", false, false), booleanParameter("is_carousel", "is_carousel", "カルーセル用タグに絞ります。", false, false))},
		{Dataset: "tag", Description: "IDまたはslugでタグ詳細を取得します。", Service: serviceGamma, Path: "/tags", Route: routeTag, Normalizer: normalizeRaw, RateClass: rateGammaTags, QueryNames: []string{"include_template"}, Parameters: []parameterSpec{integerOptionalParameter("id", "id", "タグIDです。slugとは排他です。", 1, 1e15), validatedStringParameter("slug", "slug", "タグslugです。idとは排他です。", false, 200, validatorSlug), booleanParameter("include_template", "include_template", "テンプレート情報を含めます。", false, false)}},
		{Dataset: "related_tags", Description: "IDまたはslugに関連するタグを取得します。resolved_tagsで解決済みタグ形式へ切り替えます。", Service: serviceGamma, Path: "/tags", Route: routeRelatedTags, Normalizer: normalizeRaw, RateClass: rateGammaTags, QueryNames: []string{"omit_empty", "status"}, Parameters: []parameterSpec{integerOptionalParameter("id", "id", "タグIDです。slugとは排他です。", 1, 1e15), validatedStringParameter("slug", "slug", "タグslugです。idとは排他です。", false, 200, validatorSlug), booleanParameter("resolved_tags", "", "末尾/tagsの解決済みタグendpointを利用します。", false, false), booleanParameter("omit_empty", "omit_empty", "空の関連を除外します。", false, false), enumParameter("status", "status", "状態です。", false, []string{"active", "closed", "all"}, "all")}},
		{Dataset: "series", Description: "シリーズ一覧を取得します。", Service: serviceGamma, Path: "/series", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateGammaGeneral, QueryNames: []string{"limit", "offset", "order", "ascending", "slug", "categories_ids", "categories_labels", "closed", "include_chat", "recurrence", "exclude_events"}, Parameters: append(list(500), arrayParameter("slugs", "slug", "slugのJSON文字列配列です。", typeStringArray, 100, validatorSlug), arrayParameter("category_ids", "categories_ids", "カテゴリIDのJSON整数配列です。", typeIntegerArray, 100, validatorNone), arrayParameter("category_labels", "categories_labels", "カテゴリlabelのJSON文字列配列です。", typeStringArray, 100, validatorNone), booleanOptionalParameter("closed", "closed", "終了状態で絞ります。"), booleanParameter("include_chat", "include_chat", "チャット情報を含めます。", false, false), stringParameter("recurrence", "recurrence", "繰り返し条件です。", false, 100), booleanParameter("exclude_events", "exclude_events", "イベント詳細を除外します。", false, false))},
		{Dataset: "series_item", Description: "シリーズ詳細を取得します。", Service: serviceGamma, Path: "/series", Route: routeSeriesItem, Normalizer: normalizeRaw, RateClass: rateGammaGeneral, QueryNames: []string{"include_chat"}, Parameters: []parameterSpec{integerParameter("id", "id", "シリーズIDです。", true, 1, 1e15, nil), booleanParameter("include_chat", "include_chat", "チャット情報を含めます。", false, false)}},
		{Dataset: "sports", Description: "スポーツ設定一覧を取得します。", Service: serviceGamma, Path: "/sports", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateGammaGeneral},
		{Dataset: "sports_market_types", Description: "スポーツ市場タイプ一覧を取得します。", Service: serviceGamma, Path: "/sports/market-types", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateGammaGeneral},
		{Dataset: "teams", Description: "チーム一覧を取得します。", Service: serviceGamma, Path: "/teams", Route: routeFixed, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateGammaGeneral, QueryNames: []string{"limit", "offset", "order", "ascending", "league", "name", "abbreviation"}, Parameters: append(list(500), arrayParameter("leagues", "league", "リーグのJSON文字列配列です。", typeStringArray, 100, validatorNone), arrayParameter("names", "name", "チーム名のJSON文字列配列です。", typeStringArray, 100, validatorNone), arrayParameter("abbreviations", "abbreviation", "略称のJSON文字列配列です。", typeStringArray, 100, validatorNone))},
		{Dataset: "comments", Description: "コメント一覧、コメントID、公開ウォレットのコメントを排他的に取得します。", Service: serviceGamma, Path: "/comments", Route: routeComments, Pagination: paginationOffset, Normalizer: normalizeRaw, RateClass: rateGammaComments, QueryNames: []string{"limit", "offset", "order", "ascending", "parent_entity_type", "parent_entity_id", "get_positions", "holders_only"}, Parameters: append(list(500), integerOptionalParameter("comment_id", "comment_id", "単一コメントIDです。list条件やuser_addressとは排他です。", 1, 1e15), validatedStringParameter("user_address", "user_address", "公開ウォレットのコメントを取得します。comment_idやlist親条件とは排他です。", false, 42, validatorWallet), enumOptionalParameter("parent_entity_type", "parent_entity_type", "親要素種別です。parent_entity_idと組で指定します。", []string{"Event", "Series", "market"}), integerOptionalParameter("parent_entity_id", "parent_entity_id", "親要素IDです。parent_entity_typeと組で指定します。", 1, 1e15), booleanParameter("get_positions", "get_positions", "コメント投稿者のポジションを含めます。", false, false), booleanParameter("holders_only", "holders_only", "保有者のコメントに絞ります。", false, false))},
		{Dataset: "public_profile", Description: "公開プロフィールを取得します。", Service: serviceGamma, Path: "/public-profile", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateGammaGeneral, QueryNames: []string{"address"}, Parameters: []parameterSpec{validatedStringParameter("address", "address", "公開ウォレットアドレスです。", true, 42, validatorWallet)}},
	}
	for specIndex := range result {
		for parameterIndex := range result[specIndex].Parameters {
			parameter := &result[specIndex].Parameters[parameterIndex]
			if parameter.Type == typeStringArray || parameter.Type == typeIntegerArray {
				parameter.Encoding = encodingRepeat
			}
		}
	}
	return result
}

// ----------------------------------------

// clobAdditionalSpecs は、CLOB APIの追加8 dataset仕様を生成します。
//
// 機能:
//   - CLOB API固有の時刻、quote補助、市場参照仕様を定義する
//
// 引数:
//   - token parameterSpec: 必須token_id項目
//   - condition parameterSpec: 必須condition_id項目
//
// 返り値:
//   - []endpointSpec: CLOB API追加仕様
func clobAdditionalSpecs(token, condition parameterSpec) []endpointSpec {
	return []endpointSpec{
		{Dataset: "server_time", Description: "CLOBサーバー時刻を取得します。", Service: serviceCLOB, Path: "/time", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateCLOBGeneral},
		{Dataset: "spread", Description: "トークンのspreadを取得します。", Service: serviceCLOB, Path: "/spread", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateCLOBQuote, QueryNames: []string{"token_id"}, Parameters: []parameterSpec{token}},
		{Dataset: "tick_size", Description: "トークンのtick sizeを取得します。", Service: serviceCLOB, Path: "/tick-size", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateCLOBTick, QueryNames: []string{"token_id"}, Parameters: []parameterSpec{token}},
		{Dataset: "fee_rate", Description: "トークンのfee rateを取得します。", Service: serviceCLOB, Path: "/fee-rate", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateCLOBGeneral, QueryNames: []string{"token_id"}, Parameters: []parameterSpec{token}},
		{Dataset: "negative_risk", Description: "トークンのnegative-risk状態を取得します。", Service: serviceCLOB, Path: "/neg-risk", Route: routeFixed, Normalizer: normalizeRaw, RateClass: rateCLOBGeneral, QueryNames: []string{"token_id"}, Parameters: []parameterSpec{token}},
		{Dataset: "clob_markets", Description: "CLOB市場一覧を種類別に取得します。", Service: serviceCLOB, Path: "/simplified-markets", Route: routeCLOBMarkets, Pagination: paginationKeyset, Normalizer: normalizeRaw, RateClass: rateCLOBGeneral, QueryNames: []string{"next_cursor"}, Parameters: []parameterSpec{enumParameter("kind", "", "一覧形式です。", false, []string{"simplified", "sampling", "sampling_simplified"}, "simplified"), stringParameter("next_cursor", "next_cursor", "次ページcursorです。", false, 1000)}},
		{Dataset: "clob_market", Description: "condition IDでCLOB市場詳細を取得します。", Service: serviceCLOB, Path: "/clob-markets", Route: routeCondition, Normalizer: normalizeRaw, RateClass: rateCLOBGeneral, Parameters: []parameterSpec{condition}},
		{Dataset: "market_by_token", Description: "token IDでCLOB市場詳細を取得します。", Service: serviceCLOB, Path: "/markets-by-token", Route: routeTokenPath, Normalizer: normalizeRaw, RateClass: rateCLOBGeneral, Parameters: []parameterSpec{token}},
	}
}

// ----------------------------------------

// datasetDescriptor は、内部仕様を公開Descriptorへ変換します。
//
// 機能:
//   - 型、必須状態、許容値、既定値を共通domain形式へ複製する
//
// 引数:
//   - spec endpointSpec: 変換元dataset仕様
//
// 返り値:
//   - domain.DatasetDescriptor: 公開可能なdataset仕様
func datasetDescriptor(spec endpointSpec) domain.DatasetDescriptor {
	parameters := make([]domain.ParameterDescriptor, 0, len(spec.Parameters))
	for _, item := range spec.Parameters {
		allowed := append([]string(nil), item.Allowed...)
		parameters = append(parameters, domain.ParameterDescriptor{Name: item.Name, Type: string(item.Type), Required: item.Required, Description: item.Description, Allowed: allowed, Default: item.Default})
	}
	return domain.DatasetDescriptor{Name: spec.Dataset, Description: spec.Description, Parameters: parameters}
}

// ----------------------------------------

// parameterDuration は、公式rateの50パーセント以下となる開始間隔を返します。
//
// 機能:
//   - durationの端数を切り上げて公式上限の半分を超えない間隔を計算する
//
// 引数:
//   - requests int: 公式window内の最大要求数
//   - window time.Duration: 公式rate window
//
// 返り値:
//   - time.Duration: 公式rateの半分に抑える最小開始間隔
func parameterDuration(requests int, window time.Duration) time.Duration {
	return time.Duration((int64(window)*2 + int64(requests) - 1) / int64(requests))
}

// ----------------------------------------

// stringParameter は、文字列項目仕様を生成します。
//
// 機能:
//   - 公開名、上流名、説明、必須状態、最大文字数を1つの仕様へまとめる
//
// 引数:
//   - name string: 公開名
//   - upstream string: 上流query名
//   - description string: 説明
//   - required bool: 必須状態
//   - maxLength int: 最大文字数
//
// 返り値:
//   - parameterSpec: 文字列項目仕様
func stringParameter(name, upstream, description string, required bool, maxLength int) parameterSpec {
	return parameterSpec{
		Name: name, UpstreamName: upstream, Description: description,
		Type: typeString, Required: required, MaxLength: maxLength,
	}
}

// ----------------------------------------

// validatedStringParameter は、形式検証付き文字列項目仕様を生成します。
//
// 機能:
//   - 通常の文字列仕様へslug、token、wallet、condition形式検証を追加する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - required bool: 必須状態
//   - maxLength int: 最大文字数
//   - validator validatorKind: 形式検証種別
//
// 返り値:
//   - parameterSpec: 形式検証付き文字列項目仕様
func validatedStringParameter(name, upstream, description string, required bool, maxLength int, validator validatorKind) parameterSpec {
	value := stringParameter(name, upstream, description, required, maxLength)
	value.Validator = validator
	return value
}

// ----------------------------------------

// integerParameter は、整数項目仕様を生成します。
//
// 機能:
//   - integer型、必須状態、範囲、既定値を1つの仕様へまとめる
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - required bool: 必須状態
//   - minimum, maximum float64: 許容範囲
//   - defaultValue any: 既定値
//
// 返り値:
//   - parameterSpec: 整数項目仕様
func integerParameter(name, upstream, description string, required bool, minimum, maximum float64, defaultValue any) parameterSpec {
	return parameterSpec{
		Name: name, UpstreamName: upstream, Description: description,
		Type: typeInteger, Required: required, Minimum: numberPointer(minimum),
		Maximum: numberPointer(maximum), Default: defaultValue,
	}
}

// ----------------------------------------

// integerOptionalParameter は、既定値なし整数項目仕様を生成します。
//
// 機能:
//   - integerParameterを任意かつ既定値なしで生成する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - minimum, maximum float64: 許容範囲
//
// 返り値:
//   - parameterSpec: 整数項目仕様
func integerOptionalParameter(name, upstream, description string, minimum, maximum float64) parameterSpec {
	return integerParameter(name, upstream, description, false, minimum, maximum, nil)
}

// ----------------------------------------

// numberParameter は、数値項目仕様を生成します。
//
// 機能:
//   - number型、必須状態、範囲、既定値を1つの仕様へまとめる
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - required bool: 必須状態
//   - minimum, maximum float64: 許容範囲
//   - defaultValue any: 既定値
//
// 返り値:
//   - parameterSpec: 数値項目仕様
func numberParameter(name, upstream, description string, required bool, minimum, maximum float64, defaultValue any) parameterSpec {
	return parameterSpec{
		Name: name, UpstreamName: upstream, Description: description,
		Type: typeNumber, Required: required, Minimum: numberPointer(minimum),
		Maximum: numberPointer(maximum), Default: defaultValue,
	}
}

// ----------------------------------------

// numberOptionalParameter は、既定値なし数値項目仕様を生成します。
//
// 機能:
//   - numberParameterを任意かつ既定値なしで生成する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - minimum, maximum float64: 許容範囲
//
// 返り値:
//   - parameterSpec: 数値項目仕様
func numberOptionalParameter(name, upstream, description string, minimum, maximum float64) parameterSpec {
	return numberParameter(name, upstream, description, false, minimum, maximum, nil)
}

// ----------------------------------------

// booleanParameter は、bool項目仕様を生成します。
//
// 機能:
//   - boolean型、必須状態、既定値を1つの仕様へまとめる
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - required bool: 必須状態
//   - defaultValue any: 既定値
//
// 返り値:
//   - parameterSpec: bool項目仕様
func booleanParameter(name, upstream, description string, required bool, defaultValue any) parameterSpec {
	return parameterSpec{
		Name: name, UpstreamName: upstream, Description: description,
		Type: typeBoolean, Required: required, Default: defaultValue,
	}
}

// ----------------------------------------

// booleanOptionalParameter は、既定値なしbool項目仕様を生成します。
//
// 機能:
//   - booleanParameterを任意かつ既定値なしで生成する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//
// 返り値:
//   - parameterSpec: bool項目仕様
func booleanOptionalParameter(name, upstream, description string) parameterSpec {
	return booleanParameter(name, upstream, description, false, nil)
}

// ----------------------------------------

// enumParameter は、列挙文字列項目仕様を生成します。
//
// 機能:
//   - string型仕様へ許容値一覧と既定値を追加する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - required bool: 必須状態
//   - allowed []string: 許容値
//   - defaultValue any: 既定値
//
// 返り値:
//   - parameterSpec: 列挙文字列項目仕様
func enumParameter(name, upstream, description string, required bool, allowed []string, defaultValue any) parameterSpec {
	value := stringParameter(name, upstream, description, required, 100)
	value.Allowed = allowed
	value.Default = defaultValue
	return value
}

// ----------------------------------------

// enumOptionalParameter は、既定値なし列挙文字列項目仕様を生成します。
//
// 機能:
//   - enumParameterを任意かつ既定値なしで生成する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - allowed []string: 許容値
//
// 返り値:
//   - parameterSpec: 列挙文字列項目仕様
func enumOptionalParameter(name, upstream, description string, allowed []string) parameterSpec {
	return enumParameter(name, upstream, description, false, allowed, nil)
}

// ----------------------------------------

// arrayParameter は、JSON配列項目仕様を生成します。
//
// 機能:
//   - 配列要素型、最大件数、要素形式検証を1つの仕様へまとめる
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - kind parameterType: 配列要素型
//   - maxItems int: 最大要素数
//   - validator validatorKind: 文字列要素の形式検証
//
// 返り値:
//   - parameterSpec: 配列項目仕様
func arrayParameter(name, upstream, description string, kind parameterType, maxItems int, validator validatorKind) parameterSpec {
	return parameterSpec{
		Name: name, UpstreamName: upstream, Description: description,
		Type: kind, MaxItems: maxItems, Validator: validator, Encoding: encodingCSV,
	}
}

// ----------------------------------------

// enumArrayParameter は、列挙文字列配列項目仕様を生成します。
//
// 機能:
//   - 文字列配列仕様へ要素の許容値一覧を追加する
//
// 引数:
//   - name, upstream, description string: 公開名、上流名、説明
//   - allowed []string: 許容要素
//   - maxItems int: 最大要素数
//
// 返り値:
//   - parameterSpec: 列挙文字列配列項目仕様
func enumArrayParameter(name, upstream, description string, allowed []string, maxItems int) parameterSpec {
	value := arrayParameter(name, upstream, description, typeStringArray, maxItems, validatorNone)
	value.Allowed = allowed
	return value
}

// ----------------------------------------

// optional は、項目仕様を任意へ複製します。
//
// 機能:
//   - 元仕様を変更せずRequiredだけをfalseへ設定する
//
// 引数:
//   - value parameterSpec: 元の項目仕様
//
// 返り値:
//   - parameterSpec: Requiredをfalseにした複製
func optional(value parameterSpec) parameterSpec {
	value.Required = false
	return value
}

// ----------------------------------------

// required は、項目仕様を必須へ複製します。
//
// 機能:
//   - 元仕様を変更せずRequiredだけをtrueへ設定する
//
// 引数:
//   - value parameterSpec: 元の項目仕様
//
// 返り値:
//   - parameterSpec: Requiredをtrueにした複製
func required(value parameterSpec) parameterSpec {
	value.Required = true
	return value
}

// ----------------------------------------

// numberPointer は、範囲値のポインターを生成します。
//
// 機能:
//   - parameterSpecの任意範囲境界へ設定できるポインターを返す
//
// 引数:
//   - value float64: 数値
//
// 返り値:
//   - *float64: 指定値へのポインター
func numberPointer(value float64) *float64 {
	return &value
}
