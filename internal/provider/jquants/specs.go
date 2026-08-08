// Package jquants は、J-Quants API V2を共通収集サービスへ接続します。
package jquants

import (
	"fmt"
	"slices"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

const (
	// APIVersion は、接続対象として固定するJ-Quants APIの版です。
	APIVersion = "v2"
	// DefaultBaseURL は、J-Quants APIの既定オリジンです。
	DefaultBaseURL = "https://api.jquants.com"
	// DefaultUserAgent は、上流へ送信する既定のUser-Agentです。
	DefaultUserAgent = "MarketDataCollector/0.1"
	// DefaultMaxResponseBytes は、展開後JSON本文の既定上限です。
	DefaultMaxResponseBytes int64 = 16 * 1024 * 1024
	// SpecificationURL は、J-Quants API公式仕様の基点URLです。
	SpecificationURL = "https://jpx-jquants.com/ja/spec"
	// SpecificationReviewedDate は、実装が公式仕様を確認した日です。
	SpecificationReviewedDate = "2026-08-08"
	// SpecificationReleaseDate は、実装へ反映した公式リリース履歴の最終日です。
	SpecificationReleaseDate = "2026-08-03"
)

// Plan は、J-Quants APIの契約プランを表します。
type Plan string

const (
	PlanFree     Plan = "free"
	PlanLight    Plan = "light"
	PlanStandard Plan = "standard"
	PlanPremium  Plan = "premium"
)

// Addon は、J-Quants APIの追加契約を表します。
type Addon string

const (
	AddonMinute Addon = "minute"
	AddonTDNet  Addon = "tdnet"
)

type requestRateClass string

const (
	rateClassBase   requestRateClass = "base"
	rateClassMinute requestRateClass = "minute"
	rateClassTDNet  requestRateClass = "tdnet"
)

type queryParameterSpec struct {
	Name         string
	UpstreamName string
	Description  string
	Required     bool
	Allowed      []string
	MinimumPlan  Plan
}

type endpointSpec struct {
	Dataset       string
	Description   string
	Path          string
	Specification string
	MinimumPlan   Plan
	Addon         Addon
	RateClass     requestRateClass
	BulkCapable   bool
	Parameters    []queryParameterSpec
	ForcedQuery   map[string]string
}

var endpointSpecs = buildEndpointSpecs()

// ----------------------------------------

/*
buildEndpointSpecs は、J-Quants API V2の固定endpoint仕様を生成します。

機能:
  - 30件のdatasetと固定HTTP pathを対応付ける
  - 最低契約プラン、Add-on、query項目、Bulk対象を定義する
  - equities_tradesだけはBulk一覧へ固定endpoint条件を付ける

引数:
  - なし

返り値:
  - []endpointSpec: collectorとHTTP clientが共有する固定許可リスト
*/
func buildEndpointSpecs() []endpointSpec {
	pagination := parameter("pagination_key", "", "後続ページを1ページ取得するためのキーです。", false)
	cursor := parameter("cursor", "", "前回取得位置以降の差分を1ページ取得するためのカーソルです。", false)
	code := parameter("code", "", "銘柄または指数のコードです。", false)
	date := parameter("date", "", "対象日です。YYYYMMDDまたはYYYY-MM-DDを指定します。", false)
	from := parameter("from", "", "取得開始日です。", false)
	to := parameter("to", "", "取得終了日です。", false)

	return []endpointSpec{
		{
			Dataset: "equities_master", Description: "上場銘柄一覧を取得します。",
			Path: "/v2/equities/master", Specification: "eq-master", MinimumPlan: PlanFree,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date},
		},
		{
			Dataset: "equities_bars_daily", Description: "株価四本値を取得します。",
			Path: "/v2/equities/bars/daily", Specification: "eq-bars-daily", MinimumPlan: PlanFree,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "equities_bars_daily_am", Description: "前場四本値を取得します。",
			Path: "/v2/equities/bars/daily/am", Specification: "eq-bars-daily-am", MinimumPlan: PlanPremium,
			Parameters: []queryParameterSpec{
				parameter("code", "", "銘柄コードです。", false), pagination,
			},
		},
		{
			Dataset: "equities_investor_types", Description: "投資部門別情報を取得します。",
			Path: "/v2/equities/investor-types", Specification: "eq-investor-types", MinimumPlan: PlanLight,
			BulkCapable: true, Parameters: []queryParameterSpec{
				parameter("section", "", "市場区分です。", false), from, to, pagination,
			},
		},
		{
			Dataset: "markets_margin_interest", Description: "信用取引週末残高を取得します。",
			Path: "/v2/markets/margin-interest", Specification: "mkt-margin-int", MinimumPlan: PlanStandard,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "markets_short_ratio", Description: "業種別空売り比率を取得します。",
			Path: "/v2/markets/short-ratio", Specification: "mkt-short-ratio", MinimumPlan: PlanStandard,
			BulkCapable: true, Parameters: []queryParameterSpec{
				parameter("s33", "", "33業種コードです。", false), date, from, to, pagination,
			},
		},
		{
			Dataset: "markets_short_sale_report", Description: "空売り残高報告を取得します。",
			Path: "/v2/markets/short-sale-report", Specification: "mkt-short-sale", MinimumPlan: PlanStandard,
			BulkCapable: true, Parameters: []queryParameterSpec{
				code,
				parameter("disc_date", "", "公表日です。", false),
				parameter("disc_date_from", "", "公表日の取得開始日です。", false),
				parameter("disc_date_to", "", "公表日の取得終了日です。", false),
				parameter("calc_date", "", "計算日です。", false),
				pagination,
			},
		},
		{
			Dataset: "markets_margin_alert", Description: "日々公表信用取引残高を取得します。",
			Path: "/v2/markets/margin-alert", Specification: "mkt-margin-alert", MinimumPlan: PlanStandard,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "markets_breakdown", Description: "売買内訳データを取得します。",
			Path: "/v2/markets/breakdown", Specification: "mkt-breakdown", MinimumPlan: PlanPremium,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "markets_calendar", Description: "取引カレンダーを取得します。",
			Path: "/v2/markets/calendar", Specification: "mkt-cal", MinimumPlan: PlanFree,
			BulkCapable: true, Parameters: []queryParameterSpec{
				parameter("hol_div", "", "休日区分です。", false), from, to,
			},
		},
		{
			Dataset: "indices_bars_daily", Description: "指数四本値を取得します。",
			Path: "/v2/indices/bars/daily", Specification: "idx-bars-daily", MinimumPlan: PlanStandard,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "indices_bars_daily_topix", Description: "TOPIX指数四本値を取得します。",
			Path: "/v2/indices/bars/daily/topix", Specification: "idx-bars-daily-topix", MinimumPlan: PlanLight,
			BulkCapable: true, Parameters: []queryParameterSpec{from, to, pagination},
		},
		{
			Dataset: "fins_summary", Description: "財務情報を取得します。",
			Path: "/v2/fins/summary", Specification: "fin-summary", MinimumPlan: PlanFree,
			BulkCapable: true, Parameters: []queryParameterSpec{
				code, date, pagination,
				withMinimumPlan(cursor, PlanPremium),
			},
		},
		{
			Dataset: "fins_details", Description: "財務諸表（BS・PL・CF）を取得します。",
			Path: "/v2/fins/details", Specification: "fin-details", MinimumPlan: PlanPremium,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, pagination, cursor},
		},
		{
			Dataset: "fins_dividend", Description: "配当金情報を取得します。",
			Path: "/v2/fins/dividend", Specification: "fin-dividend", MinimumPlan: PlanPremium,
			BulkCapable: true, Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "fins_earnings_date", Description: "決算発表予定日を取得します。",
			Path: "/v2/fins/earnings-date", Specification: "fin-earnings-date", MinimumPlan: PlanFree,
			BulkCapable: true, Parameters: []queryParameterSpec{
				code, date,
				parameter("scheduled_date", "", "決算発表予定日です。", false),
				pagination,
			},
		},
		{
			Dataset: "equities_earnings_calendar", Description: "3月期・9月期決算会社の決算発表予定日を取得します。",
			Path: "/v2/equities/earnings-calendar", Specification: "eq-earnings-cal", MinimumPlan: PlanFree,
			Parameters: []queryParameterSpec{pagination},
		},
		{
			Dataset: "derivatives_bars_daily_options_225", Description: "日経225オプション四本値を取得します。",
			Path: "/v2/derivatives/bars/daily/options/225", Specification: "drv-bars-daily-opt-225", MinimumPlan: PlanStandard,
			BulkCapable: true, Parameters: []queryParameterSpec{
				parameter("date", "", "対象日です。", true), pagination,
			},
		},
		{
			Dataset: "derivatives_bars_daily_futures", Description: "先物四本値を取得します。",
			Path: "/v2/derivatives/bars/daily/futures", Specification: "drv-bars-daily-fut", MinimumPlan: PlanPremium,
			BulkCapable: true, Parameters: []queryParameterSpec{
				parameter("date", "", "対象日です。", true),
				parameter("category", "", "先物商品区分コードです。", false),
				parameter("contract_flag", "", "限月区分です。", false),
				pagination,
			},
		},
		{
			Dataset: "derivatives_bars_daily_options", Description: "オプション四本値を取得します。",
			Path: "/v2/derivatives/bars/daily/options", Specification: "drv-bars-daily-opt", MinimumPlan: PlanPremium,
			BulkCapable: true, Parameters: []queryParameterSpec{
				parameter("date", "", "対象日です。", true),
				parameter("category", "", "オプション商品区分コードです。", false),
				code,
				parameter("contract_flag", "", "限月区分です。", false),
				pagination,
			},
		},
		{
			Dataset: "edinet_major_shareholders", Description: "EDINETの大株主状況を取得します。",
			Path: "/v2/edinet/major-shareholders", Specification: "edinet-major-shareholders", MinimumPlan: PlanStandard,
			Parameters: edinetParameters(code, date, pagination),
		},
		{
			Dataset: "edinet_cross_shareholdings", Description: "EDINETの政策保有株式を取得します。",
			Path: "/v2/edinet/cross-shareholdings", Specification: "edinet-cross-shareholdings", MinimumPlan: PlanStandard,
			Parameters: edinetParameters(code, date, pagination),
		},
		{
			Dataset: "edinet_large_volume_shareholders", Description: "EDINETの大量保有報告書を取得します。",
			Path: "/v2/edinet/large-volume-shareholders", Specification: "edinet-large-volume-shareholders", MinimumPlan: PlanStandard,
			Parameters: edinetParameters(code, date, pagination),
		},
		{
			Dataset: "bulk_list", Description: "ダウンロード可能なBulk CSVファイル一覧を取得します。",
			Path: "/v2/bulk/list", Specification: "bulk-list", MinimumPlan: PlanLight,
			Parameters: []queryParameterSpec{
				parameter("endpoint", "", "対象APIの先頭スラッシュ付きendpointです。", false),
				date, from, to,
			},
		},
		{
			Dataset: "bulk_get", Description: "Bulk CSVファイルの一時ダウンロードURLを取得します。",
			Path: "/v2/bulk/get", Specification: "bulk-get", MinimumPlan: PlanLight,
			Parameters: []queryParameterSpec{
				parameter("key", "", "bulk_listが返したファイルキーです。", false),
				parameter("endpoint", "", "対象APIの先頭スラッシュ付きendpointです。", false),
				date,
			},
		},
		{
			Dataset: "equities_bars_minute", Description: "株価分足を取得します。",
			Path: "/v2/equities/bars/minute", Specification: "eq-bars-minute", Addon: AddonMinute,
			RateClass: rateClassMinute, BulkCapable: true,
			Parameters: []queryParameterSpec{code, date, from, to, pagination},
		},
		{
			Dataset: "equities_trades", Description: "株価ティックのBulk CSVファイル一覧を取得します。",
			Path: "/v2/bulk/list", Specification: "bulk-list", Addon: AddonMinute,
			RateClass:   rateClassMinute,
			Parameters:  []queryParameterSpec{date, from, to},
			ForcedQuery: map[string]string{"endpoint": "/equities/trades"},
		},
		{
			Dataset: "td_list", Description: "TDnet適時開示インデックス一覧を取得します。",
			Path: "/v2/td/list", Specification: "td-list", Addon: AddonTDNet,
			RateClass: rateClassTDNet,
			Parameters: []queryParameterSpec{
				date, code, from, to,
				parameter("disc_items", "discItems", "開示事項コードをカンマ区切りで指定します。", false),
				cursor, pagination,
			},
		},
		{
			Dataset: "td_files", Description: "TDnet適時開示ファイルの一時URLを取得します。",
			Path: "/v2/td/files", Specification: "td-files", Addon: AddonTDNet,
			RateClass: rateClassTDNet,
			Parameters: []queryParameterSpec{
				parameter("disc_no", "discNo", "開示番号です。", true),
				parameter("docs", "", "取得対象文書をg、s、xのカンマ区切りで指定します。", false),
			},
		},
		{
			Dataset: "td_bulk", Description: "TDnet適時開示インデックス一括ダウンロードURLを取得します。",
			Path: "/v2/td/bulk", Specification: "td-bulk", Addon: AddonTDNet,
			RateClass: rateClassTDNet, Parameters: []queryParameterSpec{},
		},
	}
}

// ----------------------------------------

/*
parameter は、文字列query項目の固定仕様を生成します。

機能:
  - 公開名、上流名、説明、単純必須状態を1つの値へまとめる
  - 上流名が空の場合は公開名をそのまま利用する

引数:
  - name string: collectで受け付けるquery項目名
  - upstreamName string: J-Quants APIへ送る項目名。空文字はnameと同じ
  - description string: datalistへ掲載する日本語説明
  - required bool: 単独で常に必須となる場合はtrue

返り値:
  - queryParameterSpec: endpoint仕様へ格納するquery項目
*/
func parameter(name string, upstreamName string, description string, required bool) queryParameterSpec {
	return queryParameterSpec{
		Name: name, UpstreamName: upstreamName, Description: description, Required: required,
	}
}

// ----------------------------------------

/*
withMinimumPlan は、query項目へ最低契約プランを設定します。

機能:
  - endpointは利用可能でも上位プランだけが使えるqueryを表現する

引数:
  - value queryParameterSpec: 元のquery項目仕様
  - plan Plan: query項目を利用できる最低契約プラン

返り値:
  - queryParameterSpec: 最低契約プランを設定した複製
*/
func withMinimumPlan(value queryParameterSpec, plan Plan) queryParameterSpec {
	value.MinimumPlan = plan
	return value
}

// ----------------------------------------

/*
edinetParameters は、EDINET系3 APIで共有するquery仕様を生成します。

機能:
  - EDINETコード、銘柄コード、日付、ページングキーを同じ順序で定義する

引数:
  - code queryParameterSpec: 銘柄コード項目
  - date queryParameterSpec: 日付項目
  - pagination queryParameterSpec: ページング項目

返り値:
  - []queryParameterSpec: EDINET endpoint用query仕様
*/
func edinetParameters(
	code queryParameterSpec,
	date queryParameterSpec,
	pagination queryParameterSpec,
) []queryParameterSpec {
	return []queryParameterSpec{
		parameter("edinet_code", "", "EDINETコードです。", false), code, date, pagination,
	}
}

// ----------------------------------------

/*
normalizePlan は、未指定プランへStandardを適用して有効性を確認します。

機能:
  - 空のPlanを現在の標準運用プランへ補正する
  - Free、Light、Standard、Premium以外を拒否する

引数:
  - plan Plan: Optionsで指定された契約プラン

返り値:
  - Plan: 検証済み契約プラン
  - error: 未対応プランの場合のエラー
*/
func normalizePlan(plan Plan) (Plan, error) {
	if plan == "" {
		return PlanStandard, nil
	}
	switch plan {
	case PlanFree, PlanLight, PlanStandard, PlanPremium:
		return plan, nil
	default:
		return "", fmt.Errorf("未対応のJ-Quants契約プランです: %q", plan)
	}
}

// ----------------------------------------

/*
normalizeAddons は、Add-on一覧を重複のない集合へ変換します。

機能:
  - minuteとtdnetだけを許可する
  - 重複指定とFreeプランへのAdd-on指定を拒否する

引数:
  - plan Plan: 検証済み契約プラン
  - addons []Addon: Optionsで指定されたAdd-on一覧

返り値:
  - map[Addon]struct{}: 利用可能なAdd-on集合
  - error: 未対応、重複、Freeプランとの不正な組み合わせの場合のエラー
*/
func normalizeAddons(plan Plan, addons []Addon) (map[Addon]struct{}, error) {
	result := make(map[Addon]struct{}, len(addons))
	for _, addon := range addons {
		switch addon {
		case AddonMinute, AddonTDNet:
		default:
			return nil, fmt.Errorf("未対応のJ-Quants Add-onです: %q", addon)
		}
		if _, exists := result[addon]; exists {
			return nil, fmt.Errorf("J-Quants Add-on %qが重複しています", addon)
		}
		result[addon] = struct{}{}
	}
	if plan == PlanFree && len(result) > 0 {
		return nil, fmt.Errorf("FreeプランではJ-Quants Add-onを利用できません")
	}
	return result, nil
}

// ----------------------------------------

/*
planRank は、契約プランを利用可否比較用の整数へ変換します。

機能:
  - FreeからPremiumまでを昇順の数値へ対応付ける

引数:
  - plan Plan: 検証済み契約プラン

返り値:
  - int: 契約プランの比較順位
*/
func planRank(plan Plan) int {
	switch plan {
	case PlanFree:
		return 0
	case PlanLight:
		return 1
	case PlanStandard:
		return 2
	case PlanPremium:
		return 3
	default:
		return -1
	}
}

// ----------------------------------------

/*
endpointAvailable は、契約内容でendpointを利用できるか確認します。

機能:
  - 通常APIは最低契約プランと現在プランを比較する
  - Add-on APIは有料基本プランと該当Add-on契約を確認する

引数:
  - spec endpointSpec: 確認する固定endpoint仕様
  - plan Plan: 現在の契約プラン
  - addons map[Addon]struct{}: 現在のAdd-on集合

返り値:
  - bool: datalistとcollectへ公開できる場合はtrue
*/
func endpointAvailable(spec endpointSpec, plan Plan, addons map[Addon]struct{}) bool {
	if spec.Addon != "" {
		_, enabled := addons[spec.Addon]
		return plan != PlanFree && enabled
	}
	return planRank(plan) >= planRank(spec.MinimumPlan)
}

// ----------------------------------------

/*
parameterAvailable は、現在プランでquery項目を利用できるか確認します。

機能:
  - 最低プラン未設定の項目をすべてのendpoint利用者へ公開する
  - Premium限定cursor等を下位プランのDescriptorから除外する

引数:
  - parameter queryParameterSpec: 確認するquery項目仕様
  - plan Plan: 現在の契約プラン

返り値:
  - bool: 現在プランで利用できる場合はtrue
*/
func parameterAvailable(parameter queryParameterSpec, plan Plan) bool {
	return parameter.MinimumPlan == "" || planRank(plan) >= planRank(parameter.MinimumPlan)
}

// ----------------------------------------

/*
datasetDescriptor は、内部endpoint仕様を公開用dataset仕様へ変換します。

機能:
  - 現在プランで利用可能なquery項目だけを掲載する
  - Allowedスライスを複製して内部仕様との共有を避ける

引数:
  - spec endpointSpec: 変換元の固定endpoint仕様
  - plan Plan: query項目の公開範囲を決める契約プラン

返り値:
  - domain.DatasetDescriptor: datalistへ掲載できるdataset仕様
*/
func datasetDescriptor(spec endpointSpec, plan Plan) domain.DatasetDescriptor {
	parameters := make([]domain.ParameterDescriptor, 0, len(spec.Parameters))
	for _, item := range spec.Parameters {
		if !parameterAvailable(item, plan) {
			continue
		}
		parameters = append(parameters, domain.ParameterDescriptor{
			Name: item.Name, Type: "string", Required: item.Required,
			Description: item.Description, Allowed: slices.Clone(item.Allowed),
		})
	}
	return domain.DatasetDescriptor{
		Name: spec.Dataset, Description: spec.Description, Parameters: parameters,
	}
}
