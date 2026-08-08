package jquants

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

// Options は、契約に応じたdataset公開範囲を指定します。
type Options struct {
	Plan   string
	Addons []string
}

// Collector は、J-Quants固有入力を検証してAPI V2を収集します。
type Collector struct {
	client    APIClient
	plan      Plan
	addons    map[Addon]struct{}
	endpoints map[string]endpointSpec
	pacing    *pacingState
	now       func() time.Time
}

var japanStandardTime = time.FixedZone("Asia/Tokyo", 9*60*60)

type pacingState struct {
	mu           sync.Mutex
	base         *rateQuota
	fins         *rateQuota
	minute       *rateQuota
	tdnet        *rateQuota
	queue        []*pacedRequest
	running      bool
	nextSequence uint64
	queueChanged chan struct{}
	now          func() time.Time
	waitInterval func(time.Duration, <-chan struct{}) bool
}

type rateQuota struct {
	interval time.Duration
	next     time.Time
}

type pacedRequest struct {
	sequence uint64
	ctx      context.Context
	quotas   []*rateQuota
	execute  func(context.Context) (APIResponse, error)
	result   chan pacedResult
	queued   bool
}

type pacedResult struct {
	response APIResponse
	err      error
}

// ----------------------------------------

/*
NewCollector は、J-Quants API clientと契約内容からcollectorを生成します。

機能:
  - APIClientのnilとOptionsのプラン・Add-onを検証する
  - 現在の契約で利用可能なendpointだけを固定dataset表へ登録する
  - 全要求共通FIFOキューと基本プラン・Add-on別quotaを初期化する

引数:
  - client APIClient: 固定endpointへHTTP要求を送るclient
  - options Options: planとAdd-onsを含む契約設定

返り値:
  - *Collector: provider.Collectorとして登録できるJ-Quants collector
  - error: clientまたは契約設定が不正な場合のエラー
*/
func NewCollector(client APIClient, options Options) (*Collector, error) {
	if isNilAPIClient(client) {
		return nil, errors.New("J-Quants API clientがありません")
	}
	plan, err := normalizePlan(Plan(options.Plan))
	if err != nil {
		return nil, err
	}
	addonValues := make([]Addon, len(options.Addons))
	for index, addon := range options.Addons {
		addonValues[index] = Addon(addon)
	}
	addons, err := normalizeAddons(plan, addonValues)
	if err != nil {
		return nil, err
	}

	available := make(map[string]endpointSpec, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if endpointAvailable(spec, plan, addons) {
			available[spec.Dataset] = spec
		}
	}
	return &Collector{
		client: client, plan: plan, addons: addons, endpoints: available,
		pacing: newPacingState(plan),
		now:    time.Now,
	}, nil
}

// ----------------------------------------

/*
Descriptor は、現在の契約で利用できるJ-Quants datasetを返します。

機能:
  - endpoint固定定義の順序を保って利用可能なdatasetだけを掲載する
  - Standardでは17 APIとBulk 2 APIの計19件を公開する
  - Premium限定queryと未契約Add-onを一覧から除外する

引数:
  - なし

返り値:
  - domain.ProviderDescriptor: 通信せずに返せる現在契約の入力仕様
*/
func (c *Collector) Descriptor() domain.ProviderDescriptor {
	datasets := make([]domain.DatasetDescriptor, 0, len(c.endpoints))
	for _, spec := range endpointSpecs {
		if _, exists := c.endpoints[spec.Dataset]; !exists {
			continue
		}
		dataset := datasetDescriptor(spec, c.plan)
		if spec.Dataset == "bulk_list" || spec.Dataset == "bulk_get" {
			setBulkEndpointAllowList(&dataset, c.availableBulkEndpoints())
		}
		datasets = append(datasets, dataset)
	}
	return domain.ProviderDescriptor{
		Name:        "jquants",
		DisplayName: "J-Quants API",
		Description: fmt.Sprintf("J-Quants API V2の%sプランで利用可能な情報を取得します。", c.plan),
		Datasets:    datasets,
	}
}

// ----------------------------------------

/*
Collect は、dataset固有queryを検証してJ-Quants APIを1ページ取得します。

機能:
  - 現在契約で非公開のdatasetを外部通信前に拒否する
  - 全queryを文字列として厳密に検証し上流名へ変換する
  - pagination_keyとcursorを自動追跡せず指定された1ページだけを取得する
  - API状態を共通エラーへ分類し、成功応答全体と安全なmetadataを返す

引数:
  - ctx context.Context: 待機とHTTP要求の期限・キャンセルを伝える値
  - dataset string: datalistに掲載されたdataset識別子
  - parameters map[string]any: dataset固有のquery項目

返り値:
  - domain.ProviderResult: J-Quantsレスポンス全体と取得metadata
  - error: dataset、query、ペーシング待機、HTTP、API状態のエラー
*/
func (c *Collector) Collect(
	ctx context.Context,
	dataset string,
	parameters map[string]any,
) (domain.ProviderResult, error) {
	spec, exists := c.endpoints[dataset]
	if !exists {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorNotFound,
			fmt.Sprintf("J-Quants APIに利用可能なdataset %qはありません", dataset),
			nil,
		)
	}
	query, err := c.validateAndTranslateQuery(spec, parameters)
	if err != nil {
		return domain.ProviderResult{}, err
	}
	response, err := c.executeRequest(ctx, spec, query)
	if err != nil {
		return domain.ProviderResult{}, classifyAPIError(spec, err)
	}

	return domain.ProviderResult{
		Data: response.Body,
		Metadata: map[string]any{
			"source_name":                 "J-Quants API",
			"source_url":                  response.SourceURL,
			"spec_url":                    SpecificationURL + "/" + spec.Specification + ".md",
			"api_version":                 APIVersion,
			"plan":                        string(c.plan),
			"endpoint":                    spec.Path,
			"upstream_status":             response.StatusCode,
			"read_only":                   true,
			"on_demand":                   true,
			"specification_reviewed_date": SpecificationReviewedDate,
			"specification_release_date":  SpecificationReleaseDate,
		},
	}, nil
}

// ----------------------------------------

/*
validateAndTranslateQuery は、公開parametersを上流queryへ変換します。

機能:
  - 未知項目、現在プランで使えない項目、string以外、空文字を拒否する
  - dataset固有の必須、排他、依存条件とBulk endpoint許可範囲を検証する
  - disc_itemsとdisc_noを上流のcamelCaseへ変換し固定queryを追加する

引数:
  - spec endpointSpec: 収集対象の固定endpoint仕様
  - parameters map[string]any: RESTまたはMCPから受けた公開query項目

返り値:
  - map[string]string: APIClientへ渡す検証済み上流query
  - error: 入力条件に違反した場合のINVALID_ARGUMENTエラー
*/
func (c *Collector) validateAndTranslateQuery(
	spec endpointSpec,
	parameters map[string]any,
) (map[string]string, error) {
	publicValues := make(map[string]string, len(parameters))
	upstreamNames := make(map[string]string, len(spec.Parameters))
	unavailable := make(map[string]struct{})
	for _, item := range spec.Parameters {
		if !parameterAvailable(item, c.plan) {
			unavailable[item.Name] = struct{}{}
			continue
		}
		upstreamName := item.UpstreamName
		if upstreamName == "" {
			upstreamName = item.Name
		}
		upstreamNames[item.Name] = upstreamName
	}

	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, exists := unavailable[key]; exists {
			return nil, invalidArgument(
				fmt.Sprintf("parameters.%sは%sプランでは利用できません", key, c.plan), nil,
			)
		}
		if _, exists := upstreamNames[key]; !exists {
			return nil, invalidArgument(fmt.Sprintf("parametersに未知の項目があります: %q", key), nil)
		}
		value, ok := parameters[key].(string)
		if !ok {
			return nil, invalidArgument(fmt.Sprintf("parameters.%sはstringで指定してください", key), nil)
		}
		if strings.TrimSpace(value) == "" {
			return nil, invalidArgument(fmt.Sprintf("parameters.%sは空文字にできません", key), nil)
		}
		publicValues[key] = value
	}

	if err := c.validateQueryRules(spec, publicValues); err != nil {
		return nil, err
	}
	query := make(map[string]string, len(publicValues)+len(spec.ForcedQuery))
	for name, value := range publicValues {
		query[upstreamNames[name]] = value
	}
	applyDatasetQueryTransform(spec, publicValues, query)
	for name, value := range spec.ForcedQuery {
		query[name] = value
	}
	return query, nil
}

// ----------------------------------------

/*
applyDatasetQueryTransform は、公開queryを固定上流endpointの組み合わせへ補正します。

機能:
  - CSV専用のequities_tradesで公開dateを同日のfrom・toへ変換する
  - Bulk一覧で禁止されるendpointとdateの同時送信を防ぐ
  - その他のdatasetではqueryを変更しない

引数:
  - spec endpointSpec: 収集対象の固定endpoint仕様
  - publicValues map[string]string: 検証済みの公開query
  - query map[string]string: 上流名へ変換中のquery

返り値:
  - なし。queryを直接更新する
*/
func applyDatasetQueryTransform(
	spec endpointSpec,
	publicValues map[string]string,
	query map[string]string,
) {
	if spec.Dataset != "equities_trades" || !hasValue(publicValues, "date") {
		return
	}
	date := publicValues["date"]
	delete(query, "date")
	query["from"] = date
	query["to"] = date
}

// ----------------------------------------

/*
validateQueryRules は、datasetごとの必須・排他・依存条件を検証します。

機能:
  - 単純必須項目を共通検証する
  - code/date系、財務、デリバティブ、Bulk、TDnet、EDINETの条件を検証する
  - pagination_keyとcursorの同時指定をすべての対応APIで拒否する

引数:
  - spec endpointSpec: 検証対象の固定endpoint仕様
  - values map[string]string: 型と空文字を検証済みの公開query

返り値:
  - error: 条件違反の場合のINVALID_ARGUMENTエラー。正常な場合はnil
*/
func (c *Collector) validateQueryRules(spec endpointSpec, values map[string]string) error {
	for _, item := range spec.Parameters {
		if item.Required && !hasValue(values, item.Name) {
			return invalidArgument(fmt.Sprintf("parameters.%sを指定してください", item.Name), nil)
		}
	}
	if hasValue(values, "pagination_key") && hasValue(values, "cursor") {
		return invalidArgument("pagination_keyとcursorは同時に指定できません", nil)
	}

	switch spec.Dataset {
	case "equities_bars_daily", "markets_margin_interest", "markets_margin_alert",
		"markets_breakdown", "indices_bars_daily", "fins_dividend", "equities_bars_minute":
		return validateCodeDateRange(values, "code")
	case "markets_short_ratio":
		return validateCodeDateRange(values, "s33")
	case "markets_short_sale_report":
		if !hasAnyValue(values, "code", "disc_date", "calc_date") {
			return invalidArgument("code、disc_date、calc_dateのいずれかを指定してください", nil)
		}
		if hasValue(values, "disc_date") && hasAnyValue(values, "disc_date_from", "disc_date_to") {
			return invalidArgument("disc_dateとdisc_date_from・disc_date_toは同時に指定できません", nil)
		}
	case "fins_summary", "fins_details":
		if !hasAnyValue(values, "code", "date") {
			return invalidArgument("codeまたはdateを指定してください", nil)
		}
		if hasValue(values, "cursor") {
			if !hasValue(values, "date") {
				return invalidArgument("cursorを指定する場合はdateも指定してください", nil)
			}
			if hasValue(values, "code") {
				return invalidArgument("cursorとcodeは同時に指定できません", nil)
			}
			if err := c.validateCursorDate(values["date"]); err != nil {
				return err
			}
		}
	case "fins_earnings_date":
		if countValues(values, "code", "date", "scheduled_date") != 1 {
			return invalidArgument("code、date、scheduled_dateのいずれか1つだけを指定してください", nil)
		}
	case "edinet_major_shareholders", "edinet_cross_shareholdings", "edinet_large_volume_shareholders":
		if hasValue(values, "edinet_code") && hasValue(values, "code") {
			return invalidArgument("edinet_codeとcodeは同時に指定できません", nil)
		}
	case "bulk_list":
		if countValues(values, "endpoint", "date") != 1 {
			return invalidArgument("endpointまたはdateのどちらか一方を指定してください", nil)
		}
		if hasAnyValue(values, "from", "to") && !hasValue(values, "endpoint") {
			return invalidArgument("fromまたはtoを指定する場合はendpointも指定してください", nil)
		}
		if hasValue(values, "date") && hasAnyValue(values, "from", "to") {
			return invalidArgument("dateとfrom・toは同時に指定できません", nil)
		}
		if err := c.validateBulkEndpoint(values["endpoint"]); err != nil {
			return err
		}
	case "bulk_get":
		keySpecified := hasValue(values, "key")
		endpointSpecified := hasValue(values, "endpoint")
		dateSpecified := hasValue(values, "date")
		if (keySpecified && (endpointSpecified || dateSpecified)) ||
			(!keySpecified && (!endpointSpecified || !dateSpecified)) {
			return invalidArgument("key、またはendpointとdateの組み合わせのどちらか一方を指定してください", nil)
		}
		if err := c.validateBulkEndpoint(values["endpoint"]); err != nil {
			return err
		}
	case "equities_trades":
		if hasValue(values, "date") && hasAnyValue(values, "from", "to") {
			return invalidArgument("dateとfrom・toは同時に指定できません", nil)
		}
	case "td_list":
		if !hasAnyValue(values, "date", "code") {
			return invalidArgument("dateまたはcodeを指定してください", nil)
		}
		if hasValue(values, "date") && hasValue(values, "code") {
			return invalidArgument("dateとcodeは同時に指定できません", nil)
		}
		if hasValue(values, "from") != hasValue(values, "to") {
			return invalidArgument("fromとtoは両方を指定してください", nil)
		}
		if hasAnyValue(values, "from", "to") && !hasValue(values, "code") {
			return invalidArgument("fromまたはtoを指定する場合はcodeも指定してください", nil)
		}
		if hasValue(values, "date") && hasAnyValue(values, "from", "to") {
			return invalidArgument("dateとfrom・toは同時に指定できません", nil)
		}
		if hasValue(values, "cursor") && !hasValue(values, "date") {
			return invalidArgument("cursorを指定する場合はdateも指定してください", nil)
		}
		if hasValue(values, "cursor") {
			return c.validateCursorDate(values["date"])
		}
	}
	return nil
}

// ----------------------------------------

/*
validateCursorDate は、cursorと組み合わせるdateがJSTの当日か検証します。

機能:
  - YYYYMMDDとYYYY-MM-DDの2書式を受け付ける
  - 注入された現在時刻をAsia/Tokyo固定オフセットへ変換する
  - 書式不正またはJST当日以外をINVALID_ARGUMENTとして拒否する

引数:
  - value string: cursorと組み合わせるdate文字列

返り値:
  - error: dateがJST当日の場合はnil、それ以外はINVALID_ARGUMENTエラー
*/
func (c *Collector) validateCursorDate(value string) error {
	var parsed time.Time
	var parseErr error
	for _, layout := range []string{"20060102", "2006-01-02"} {
		parsed, parseErr = time.ParseInLocation(layout, value, japanStandardTime)
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil {
		return invalidArgument("cursorと組み合わせるdateはYYYYMMDDまたはYYYY-MM-DDで指定してください", parseErr)
	}

	today := c.now().In(japanStandardTime)
	if parsed.Year() != today.Year() || parsed.YearDay() != today.YearDay() {
		return invalidArgument("cursorと組み合わせるdateはJSTの当日を指定してください", nil)
	}
	return nil
}

// ----------------------------------------

/*
validateCodeDateRange は、銘柄等の識別子と日付・期間の共通条件を検証します。

機能:
  - 識別子またはdateの少なくとも一方を必須にする
  - from・toは識別子と組み合わせた期間検索だけに許可する
  - 単一dateと期間条件の混在を拒否する

引数:
  - values map[string]string: 検証済み公開query
  - identifier string: codeまたはs33などの識別子項目名

返り値:
  - error: 組み合わせが不正な場合のINVALID_ARGUMENTエラー。正常な場合はnil
*/
func validateCodeDateRange(values map[string]string, identifier string) error {
	if !hasAnyValue(values, identifier, "date") {
		return invalidArgument(fmt.Sprintf("%sまたはdateを指定してください", identifier), nil)
	}
	if hasAnyValue(values, "from", "to") && !hasValue(values, identifier) {
		return invalidArgument(fmt.Sprintf("fromまたはtoを指定する場合は%sも指定してください", identifier), nil)
	}
	if hasValue(values, "date") && hasAnyValue(values, "from", "to") {
		return invalidArgument("dateとfrom・toは同時に指定できません", nil)
	}
	return nil
}

// ----------------------------------------

/*
validateBulkEndpoint は、Bulk queryのendpointが現在契約の固定許可対象か確認します。

機能:
  - endpoint未指定を許容する
  - `/v2`を除いた先頭スラッシュ付き固定pathだけを許可する
  - Premium専用または未契約Add-onのCSV指定を外部通信前に拒否する

引数:
  - endpoint string: Bulk APIへ渡すendpoint条件

返り値:
  - error: 現在契約でダウンロードできないendpointの場合のINVALID_ARGUMENTエラー
*/
func (c *Collector) validateBulkEndpoint(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	for _, allowed := range c.availableBulkEndpoints() {
		if endpoint == allowed {
			return nil
		}
	}
	return invalidArgument(fmt.Sprintf("endpoint %qは現在の契約でBulk取得できません", endpoint), nil)
}

// ----------------------------------------

/*
availableBulkEndpoints は、現在契約でCSV取得できる固定endpoint一覧を返します。

機能:
  - API利用可能かつBulk対応と定義したendpointだけを抽出する
  - API pathの固定`/v2`接頭辞をBulk query用に除去する
  - 株価ティックAdd-on契約時はCSV専用endpointを追加する

引数:
  - なし

返り値:
  - []string: 重複のない先頭スラッシュ付きBulk endpoint一覧
*/
func (c *Collector) availableBulkEndpoints() []string {
	values := make([]string, 0, len(c.endpoints))
	seen := make(map[string]struct{})
	for _, spec := range endpointSpecs {
		if !spec.BulkCapable || !endpointAvailable(spec, c.plan, c.addons) {
			continue
		}
		endpoint := strings.TrimPrefix(spec.Path, "/"+APIVersion)
		if _, exists := seen[endpoint]; exists {
			continue
		}
		seen[endpoint] = struct{}{}
		values = append(values, endpoint)
	}
	if _, enabled := c.addons[AddonMinute]; enabled {
		values = append(values, "/equities/trades")
	}
	return values
}

// ----------------------------------------

/*
setBulkEndpointAllowList は、Bulk datasetのendpoint項目へ許可値を設定します。

機能:
  - Descriptor内のendpoint項目だけへ現在契約の固定許可値を複製する

引数:
  - descriptor *domain.DatasetDescriptor: 書き換えるBulk dataset仕様
  - allowed []string: 現在契約で利用できるBulk endpoint一覧

返り値:
  - なし。descriptorのParameterDescriptorを直接更新する
*/
func setBulkEndpointAllowList(descriptor *domain.DatasetDescriptor, allowed []string) {
	for index := range descriptor.Parameters {
		if descriptor.Parameters[index].Name == "endpoint" {
			descriptor.Parameters[index].Allowed = append([]string(nil), allowed...)
			return
		}
	}
}

// ----------------------------------------

/*
classifyAPIError は、J-Quants HTTP状態を共通ServiceErrorへ変換します。

機能:
  - 400をINVALID_ARGUMENT、403と429をPROVIDER_UNAVAILABLEへ分類する
  - 5xxとその他の非成功状態をUPSTREAM_ERRORへ分類する
  - APIError以外とcontextエラーはserviceの共通分類へ委ねる

引数:
  - spec endpointSpec: 失敗した固定endpoint仕様
  - err error: APIClientが返した内部原因

返り値:
  - error: transport間で共有できる分類済みエラーまたは元のエラー
*/
func classifyAPIError(spec endpointSpec, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.StatusCode {
	case 400:
		return domain.NewServiceError(
			domain.ErrorInvalidArgument,
			fmt.Sprintf("dataset %qのqueryがJ-Quants APIに拒否されました", spec.Dataset),
			err,
		)
	case 401, 403:
		return domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			"J-Quants APIのAPIキーまたは契約プランではこのデータを利用できません",
			err,
		)
	case 429:
		return domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			"J-Quants APIのレートリミットを超過しました。時間を空けて再実行してください",
			err,
		)
	default:
		return domain.NewServiceError(
			domain.ErrorUpstream,
			fmt.Sprintf("J-Quants APIからdataset %qを取得できません", spec.Dataset),
			err,
		)
	}
}

// ----------------------------------------

/*
newPacingState は、全要求共通のFIFOキューと独立quotaを生成します。

機能:
  - 公式上限の50%になるよう、2分間あたり公式1分間上限数の均等間隔で流す
  - Free 2.5、Light 30、Standard 60、Premium 250要求/分を基本キューに設定する
  - 財務と分足・ティックは30、TDnetは50要求/分の独立キューに設定する

引数:
  - plan Plan: 検証済み基本契約プラン

返り値:
  - *pacingState: collector内の全要求で共有するFIFOキューとquota状態
*/
func newPacingState(plan Plan) *pacingState {
	baseOfficialRequestsPerMinute := 120
	switch plan {
	case PlanFree:
		baseOfficialRequestsPerMinute = 5
	case PlanLight:
		baseOfficialRequestsPerMinute = 60
	case PlanStandard:
		baseOfficialRequestsPerMinute = 120
	case PlanPremium:
		baseOfficialRequestsPerMinute = 500
	}
	return &pacingState{
		base:         newHalfRateQuota(baseOfficialRequestsPerMinute),
		fins:         newHalfRateQuota(60),
		minute:       newHalfRateQuota(60),
		tdnet:        newHalfRateQuota(100),
		queueChanged: make(chan struct{}, 1),
		now:          time.Now,
		waitInterval: waitForPacerInterval,
	}
}

// ----------------------------------------

/*
newHalfRateQuota は、公式の1分間上限の50%に対応するquotaを生成します。

機能:
  - 浮動小数を使わず、公式上限数を2分間に均等配置する
  - Freeの5要求/分も24秒間隔として2.5要求/分を正確に表現する

引数:
  - officialRequestsPerMinute int: 公式の1分間上限数

返り値:
  - *rateQuota: 公式上限の50%間隔を保持するレート枠
*/
func newHalfRateQuota(officialRequestsPerMinute int) *rateQuota {
	return newRateQuota(officialRequestsPerMinute, 2*time.Minute)
}

// ----------------------------------------

/*
newRateQuota は、指定期間の要求数からレート枠を生成します。

機能:
  - 整数の期間除算で均等な要求開始間隔を計算する
  - FIFOキューが参照する要求開始間隔を保持する

引数:
  - requestsPerPeriod int: period内に開始する要求数
  - period time.Duration: requestsPerPeriodを配置する期間

返り値:
  - *rateQuota: 要求開始間隔を保持するレート枠
*/
func newRateQuota(requestsPerPeriod int, period time.Duration) *rateQuota {
	if requestsPerPeriod <= 0 || period <= 0 {
		panic("J-Quantsレートキューの要求数と期間は正数で指定してください")
	}
	return &rateQuota{interval: period / time.Duration(requestsPerPeriod)}
}

// ----------------------------------------

/*
executeRequest は、全J-Quants要求共通のFIFOキュー経由でAPI通信を実行します。

機能:
  - 受付順に単一キューへ登録し、後続要求の追い越しを防ぐ
  - endpointとBulk queryに必要な独立quotaを選択する
  - ペーシング無効時はAPIClientを直接呼び出す

引数:
  - ctx context.Context: 待機とHTTP要求のキャンセル・期限を制御する値
  - spec endpointSpec: rate classとpathを持つ対象endpoint
  - query map[string]string: Bulk対象を含む検証済み上流query

返り値:
  - APIResponse: APIClientが返したJ-Quants応答
  - error: キュー待機、contextまたはAPIClientのエラー
*/
func (c *Collector) executeRequest(
	ctx context.Context,
	spec endpointSpec,
	query map[string]string,
) (APIResponse, error) {
	if c.pacing == nil {
		return c.client.Fetch(ctx, spec.Dataset, query)
	}
	quotas := c.requestQuotas(spec, query)
	return c.pacing.Execute(ctx, quotas, func(requestContext context.Context) (APIResponse, error) {
		return c.client.Fetch(requestContext, spec.Dataset, query)
	})
}

// ----------------------------------------

/*
requestQuotas は、endpointとqueryに適用する独立レート枠を返します。

機能:
  - 通常APIへ基本プラン枠を適用する
  - fins summary/detailsへ基本枠と財務専用枠を二重適用する
  - Add-on APIへ基本プランとは独立した専用枠だけを適用する
  - Bulk経由の分足・ティック取得に株価Add-on専用枠を適用する

引数:
  - spec endpointSpec: rate classとpathを持つ対象endpoint
  - query map[string]string: Bulk対象を含む検証済み上流query

返り値:
  - []*rateQuota: 要求開始前にすべて満たす必要があるquota一覧
*/
func (c *Collector) requestQuotas(spec endpointSpec, query map[string]string) []*rateQuota {
	if isMinuteBulkRequest(spec, query) {
		return []*rateQuota{c.pacing.minute}
	}
	switch spec.RateClass {
	case rateClassMinute:
		return []*rateQuota{c.pacing.minute}
	case rateClassTDNet:
		return []*rateQuota{c.pacing.tdnet}
	default:
		quotas := []*rateQuota{c.pacing.base}
		if spec.Path == "/v2/fins/summary" || spec.Path == "/v2/fins/details" {
			quotas = append(quotas, c.pacing.fins)
		}
		return quotas
	}
}

// ----------------------------------------

/*
isMinuteBulkRequest は、Bulk API要求が株価分足・ティックを対象とするか確認します。

機能:
  - bulk_listとbulk_getのendpoint条件から株価Add-on対象を識別する
  - bulk_getのkey条件は固定ディレクトリ接頭辞を完全な区切りで照合する

引数:
  - spec endpointSpec: 対象datasetの固定仕様
  - query map[string]string: 検証済み上流query

返り値:
  - bool: 株価Add-on専用レート枠を適用する場合はtrue
*/
func isMinuteBulkRequest(spec endpointSpec, query map[string]string) bool {
	if spec.Dataset != "bulk_list" && spec.Dataset != "bulk_get" {
		return false
	}
	endpoint := query["endpoint"]
	if endpoint == "/equities/bars/minute" || endpoint == "/equities/trades" {
		return true
	}
	key := strings.TrimPrefix(query["key"], "/")
	return strings.HasPrefix(key, "equities/bars/minute/") ||
		strings.HasPrefix(key, "equities/trades/")
}

// ----------------------------------------

/*
Execute は、API要求を全class共通のFIFOキューで実行します。

機能:
  - mutex内で受付連番を付け、全rate classをまたいだ到着順を固定する
  - headが持つすべてのquotaの開始時刻を満たすまで後続を進めない
  - キュー待機中のcontextキャンセルは対象要求を削除する
  - API通信自体をランナーが開始し、goroutineの実行順による呼出順の逆転を防ぐ

引数:
  - ctx context.Context: 待機とAPI通信のキャンセル・期限を制御する値
  - quotas []*rateQuota: 開始前にすべて満たす必要があるレート枠
  - execute func(context.Context) (APIResponse, error): キューが受付順に実行するAPI通信

返り値:
  - APIResponse: executeが返したJ-Quants応答
  - error: contextまたはexecuteが返したエラー
*/
func (p *pacingState) Execute(
	ctx context.Context,
	quotas []*rateQuota,
	execute func(context.Context) (APIResponse, error),
) (APIResponse, error) {
	if err := ctx.Err(); err != nil {
		return APIResponse{}, err
	}

	request := &pacedRequest{
		ctx:     ctx,
		quotas:  append([]*rateQuota(nil), quotas...),
		execute: execute,
		result:  make(chan pacedResult, 1),
		queued:  true,
	}
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return APIResponse{}, err
	}
	p.nextSequence++
	request.sequence = p.nextSequence
	p.queue = append(p.queue, request)
	if !p.running {
		p.running = true
		go p.runQueue()
	}
	p.mu.Unlock()

	select {
	case result := <-request.result:
		return result.response, result.err
	case <-ctx.Done():
		p.mu.Lock()
		if request.queued {
			p.removeRequestLocked(request)
			p.notifyQueueChangedLocked()
		}
		p.mu.Unlock()
		return APIResponse{}, ctx.Err()
	}
}

// ----------------------------------------

/*
runQueue は、待機中のAPI要求を受付連番順に実行します。

機能:
  - headに必要なすべてのquotaから最も遅い開始可能時刻を計算する
  - 開始時に必要quotaの次回時刻を同時に更新する
  - executeをランナー内で呼び出し、完了後に次の受付要求へ進む
  - キューが空になるとgoroutineを終了し、常駐goroutineを残さない

引数:
  - なし

返り値:
  - なし
*/
func (p *pacingState) runQueue() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.running = false
			p.mu.Unlock()
			return
		}
		request := p.queue[0]
		now := p.now()
		next := now
		for _, quota := range request.quotas {
			if quota.next.After(next) {
				next = quota.next
			}
		}
		remaining := next.Sub(now)
		queueChanged := p.queueChanged
		p.mu.Unlock()

		if remaining > 0 && !p.waitInterval(remaining, queueChanged) {
			continue
		}

		p.mu.Lock()
		if len(p.queue) == 0 || p.queue[0] != request {
			p.mu.Unlock()
			continue
		}
		now = p.now()
		ready := true
		for _, quota := range request.quotas {
			if quota.next.After(now) {
				ready = false
				break
			}
		}
		if !ready {
			p.mu.Unlock()
			continue
		}
		p.queue = p.queue[1:]
		request.queued = false
		for _, quota := range request.quotas {
			quota.next = now.Add(quota.interval)
		}
		p.mu.Unlock()

		response, err := request.execute(request.ctx)
		request.result <- pacedResult{response: response, err: err}
	}
}

// ----------------------------------------

/*
removeRequestLocked は、キャンセルされた要求をFIFOキューから除きます。

機能:
  - ポインタ一致する未実行要求だけを削除する
  - 削除後のスライスに不要な参照を残さない

引数:
  - request *pacedRequest: 削除する未実行要求

返り値:
  - なし
*/
func (p *pacingState) removeRequestLocked(request *pacedRequest) {
	for index, queued := range p.queue {
		if queued != request {
			continue
		}
		copy(p.queue[index:], p.queue[index+1:])
		p.queue[len(p.queue)-1] = nil
		p.queue = p.queue[:len(p.queue)-1]
		request.queued = false
		return
	}
}

// ----------------------------------------

/*
notifyQueueChangedLocked は、待機中のランナーへキュー変更を通知します。

機能:
  - バッファ付きchannelへ非ブロッキングで通知する
  - 複数の変更を1回の再評価通知へまとめる

引数:
  - なし

返り値:
  - なし
*/
func (p *pacingState) notifyQueueChangedLocked() {
	select {
	case p.queueChanged <- struct{}{}:
	default:
	}
}

// ----------------------------------------

/*
waitForPacerInterval は、次の要求開始時刻まで、またはキュー変更まで待機します。

機能:
  - time.Timerで指定間隔を待機する
  - キュー変更時はtimerを安全に停止・drainして再評価させる

引数:
  - interval time.Duration: 待機する時間
  - queueChanged <-chan struct{}: キュー変更通知を受け取るchannel

返り値:
  - bool: 指定間隔が経過した場合はtrue、キューが変更された場合はfalse
*/
func waitForPacerInterval(interval time.Duration, queueChanged <-chan struct{}) bool {
	timer := time.NewTimer(interval)
	select {
	case <-timer.C:
		return true
	case <-queueChanged:
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return false
	}
}

// ----------------------------------------

/*
hasValue は、queryに指定項目が存在するか確認します。

機能:
  - 型と空文字検証後のmapにキーが含まれるかだけを判定する

引数:
  - values map[string]string: 検証済みquery
  - name string: 確認する項目名

返り値:
  - bool: 項目が指定されている場合はtrue
*/
func hasValue(values map[string]string, name string) bool {
	_, exists := values[name]
	return exists
}

// ----------------------------------------

/*
hasAnyValue は、候補のうち1項目以上が指定されているか確認します。

機能:
  - 必須選択条件と依存条件を共通処理する

引数:
  - values map[string]string: 検証済みquery
  - names ...string: 確認する項目名一覧

返り値:
  - bool: 1項目以上が指定されている場合はtrue
*/
func hasAnyValue(values map[string]string, names ...string) bool {
	return countValues(values, names...) > 0
}

// ----------------------------------------

/*
countValues は、候補のうち指定されたquery項目数を数えます。

機能:
  - 正確に1項目だけ必要な排他条件へ利用する

引数:
  - values map[string]string: 検証済みquery
  - names ...string: 数える項目名一覧

返り値:
  - int: 指定済み項目数
*/
func countValues(values map[string]string, names ...string) int {
	count := 0
	for _, name := range names {
		if hasValue(values, name) {
			count++
		}
	}
	return count
}

// ----------------------------------------

/*
invalidArgument は、公開可能なJ-Quants入力エラーを生成します。

機能:
  - 全datasetの入力違反をINVALID_ARGUMENTへ統一する

引数:
  - message string: 利用者へ返す日本語メッセージ
  - cause error: ログだけに利用する内部原因

返り値:
  - error: INVALID_ARGUMENT分類の共通エラー
*/
func invalidArgument(message string, cause error) error {
	return domain.NewServiceError(domain.ErrorInvalidArgument, message, cause)
}

// ----------------------------------------

/*
isNilAPIClient は、interfaceに格納された型付きnilを含めて検出します。

機能:
  - constructorでnil clientによる後続panicを防ぐ

引数:
  - client APIClient: nilかどうか確認するclient実装

返り値:
  - bool: interfaceまたは内包値がnilの場合はtrue
*/
func isNilAPIClient(client APIClient) bool {
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
