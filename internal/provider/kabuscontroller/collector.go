package kabuscontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

var (
	securitySymbolPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	contractMonthPattern  = regexp.MustCompile(`(\d{2})/(\d{2})`)
)

const bidAskInterpretationWarning = "上流のBidPriceは最良売気配、AskPriceは最良買気配です。売買方向はSell1とBuy1を正として解釈してください。"

// Collector は、KabusControllerのdataset入力を検証して固定GETまたは安全な複合収集を実行します。
type Collector struct {
	client    APIClient
	endpoints map[string]endpointSpec
}

// ----------------------------------------

/*
NewCollector は、KabusController API clientからcollectorを生成します。

機能:
  - 型付きnilを含むAPIClientのnilを拒否する
  - 全固定datasetの重複を起動時に検証する

引数:
  - client APIClient: 検証済み入力から読取GETを行うclient

返り値:
  - *Collector: provider.Collectorを満たす収集器
  - error: clientまたは固定dataset仕様が不正な場合のエラー
*/
func NewCollector(client APIClient) (*Collector, error) {
	if isNilAPIClient(client) {
		return nil, errors.New("KabusController API clientがありません")
	}
	endpoints := make(map[string]endpointSpec, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if _, exists := endpoints[spec.Dataset]; exists {
			return nil, fmt.Errorf("KabusController dataset %qが重複しています", spec.Dataset)
		}
		endpoints[spec.Dataset] = spec
	}
	return &Collector{client: client, endpoints: endpoints}, nil
}

// ----------------------------------------

/*
Descriptor は、KabusControllerとkabuステーション情報APIの公開datasetを返します。

機能:
  - 固定endpoint定義の順序を維持する
  - 標準情報APIのGETがAPI登録銘柄リストを変更し得ることをprovider説明へ明記する

引数:
  - なし

返り値:
  - domain.ProviderDescriptor: datalistへ掲載するprovider仕様
*/
func (c *Collector) Descriptor() domain.ProviderDescriptor {
	datasets := make([]domain.DatasetDescriptor, 0, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		datasets = append(datasets, datasetDescriptor(spec))
	}
	return domain.ProviderDescriptor{
		Name:        "kabus-controller",
		DisplayName: "KabusController",
		Description: "KabusControllerの登録一覧・板とkabuステーション標準情報APIを要求時に収集します。標準情報APIの一部GETは上流のAPI登録銘柄リストへ銘柄を自動登録する場合があります。発注・取消・明示的な登録解除は行いません。",
		Datasets:    datasets,
	}
}

// ----------------------------------------

/*
Collect は、公開入力を検証・正規化して単一GETまたは複数要求の複合収集を実行します。

機能:
  - 未知入力、型、範囲、列挙値、銘柄コード、dataset固有条件を通信前に検証する
  - 通常datasetは上流JSONを変更せず返す
  - NT同限月解決と登録済みオプションチェーンを2要求、API容量情報を3要求で合成する
  - 日付のない時刻からsource_atを推測せず、共通鮮度metadataを付ける

引数:
  - ctx context.Context: 上流GETの期限とキャンセルを伝える値
  - dataset string: Descriptorに掲載されたdataset識別子
  - parameters map[string]any: dataset固有の公開入力

返り値:
  - domain.ProviderResult: 上流JSONまたは合成結果と取得metadata
  - error: dataset、入力、通信、上流状態、JSON、合成の共通分類エラー
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
			fmt.Sprintf("未対応のKabusController datasetです: %q", dataset),
			nil,
		)
	}
	normalized, err := validateParameters(spec, parameters)
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorInvalidArgument, err.Error(), err)
	}

	switch dataset {
	case "nt_pair_symbol_resolver":
		return c.collectNTPair(ctx, spec, normalized)
	case "option_chain_snapshot":
		return c.collectOptionChain(ctx, spec, normalized)
	case "kabus_api_capacity":
		return c.collectAPICapacity(ctx, spec, normalized)
	default:
		return c.collectSingle(ctx, spec, normalized)
	}
}

// ----------------------------------------

/*
collectSingle は、通常datasetを1回取得して上流JSONを保持したまま返します。

機能:
  - APIClientを1回だけ呼び出す
  - endpointの副作用可能性、板方向、鮮度をmetadataへ反映する

引数:
  - ctx context.Context: 上流GETの期限とキャンセルを伝える値
  - spec endpointSpec: 収集する固定dataset仕様
  - parameters map[string]string: 検証・正規化済み入力

返り値:
  - domain.ProviderResult: 上流JSON全体とmetadata
  - error: 上流取得を共通分類したエラー
*/
func (c *Collector) collectSingle(
	ctx context.Context,
	spec endpointSpec,
	parameters map[string]string,
) (domain.ProviderResult, error) {
	response, err := c.client.Fetch(ctx, spec.Dataset, cloneStringMap(parameters))
	if err != nil {
		return domain.ProviderResult{}, classifyCollectError(spec, err)
	}
	return domain.ProviderResult{
		Data:     response.Body,
		Metadata: singleResponseMetadata(spec, parameters, response),
	}, nil
}

// ----------------------------------------

/*
validateParameters は、endpoint仕様に従って公開入力を正規化します。

機能:
  - 既定値を適用し、未知項目と必須不足を拒否する
  - string、integer、booleanを通信時の正規文字列へ変換する
  - Allowed、最小値、最大値、validator、dataset固有条件を検証する

引数:
  - spec endpointSpec: 収集対象の固定endpoint仕様
  - parameters map[string]any: RESTまたはMCPから受けた入力項目

返り値:
  - map[string]string: clientへ渡す検証・正規化済み入力
  - error: 未知項目、必須不足、型、範囲、条件違反のエラー
*/
func validateParameters(spec endpointSpec, parameters map[string]any) (map[string]string, error) {
	if parameters == nil {
		parameters = map[string]any{}
	}
	allowedSpecs := make(map[string]parameterSpec, len(spec.Parameters))
	for _, item := range spec.Parameters {
		allowedSpecs[item.Name] = item
	}
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, exists := allowedSpecs[key]; !exists {
			return nil, fmt.Errorf("KabusController dataset %qに未知の入力項目があります: %q", spec.Dataset, key)
		}
	}

	normalized := make(map[string]string, len(spec.Parameters))
	for _, item := range spec.Parameters {
		raw, exists := parameters[item.Name]
		if !exists && item.Default != nil {
			raw = item.Default
			exists = true
		}
		if !exists {
			if item.Required {
				return nil, fmt.Errorf("KabusController dataset %qには入力項目 %q が必要です", spec.Dataset, item.Name)
			}
			continue
		}
		value, numeric, err := normalizeParameterValue(item, raw)
		if err != nil {
			return nil, fmt.Errorf("KabusControllerの入力項目 %s が不正です: %w", item.Name, err)
		}
		if err := validateNormalizedParameter(item, value, numeric); err != nil {
			return nil, fmt.Errorf("KabusControllerの入力項目 %s が不正です: %w", item.Name, err)
		}
		normalized[item.Name] = value
	}
	if err := validateDatasetConditions(spec.Dataset, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// ----------------------------------------

/*
normalizeParameterValue は、公開入力を上流要求用の正規文字列へ変換します。

機能:
  - stringは前後空白と空文字を拒否する
  - integerはjson.Number、全整数型、整数値のfloat32・float64を受け付ける
  - booleanはboolだけを受け付ける

引数:
  - spec parameterSpec: 入力項目の型仕様
  - raw any: 公開APIから受けた値

返り値:
  - string: 上流要求用の正規文字列
  - *float64: 範囲検証用数値。非数値型ではnil
  - error: 型、空白、非整数、オーバーフローのエラー
*/
func normalizeParameterValue(spec parameterSpec, raw any) (string, *float64, error) {
	switch spec.Type {
	case typeString:
		value, ok := raw.(string)
		if !ok {
			return "", nil, errors.New("stringで指定してください")
		}
		if value == "" || strings.TrimSpace(value) != value {
			return "", nil, errors.New("空文字または前後空白を含む文字列は指定できません")
		}
		return value, nil, nil
	case typeInteger:
		value, err := normalizeInteger(raw)
		if err != nil {
			return "", nil, err
		}
		numeric, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsInf(numeric, 0) {
			return "", nil, errors.New("範囲外の整数です")
		}
		return value, &numeric, nil
	case typeBoolean:
		value, ok := raw.(bool)
		if !ok {
			return "", nil, errors.New("booleanで指定してください")
		}
		return strconv.FormatBool(value), nil, nil
	default:
		return "", nil, fmt.Errorf("未対応の型 %q です", spec.Type)
	}
}

// ----------------------------------------

/*
normalizeInteger は、JSONまたはGoの整数表現を10進整数文字列へ変換します。

機能:
  - json.Numberの指数・小数表現は数学的に整数の場合だけ受け付ける
  - 符号付き・符号なし整数を桁落ちなく変換する
  - float32・float64は有限かつ整数値の場合だけ受け付ける

引数:
  - raw any: 整数として正規化する値

返り値:
  - string: 10進整数の正規文字列
  - error: 非数値、非整数、NaN、無限大、表現範囲外のエラー
*/
func normalizeInteger(raw any) (string, error) {
	switch value := raw.(type) {
	case json.Number:
		return normalizeNumberString(value.String())
	case int:
		return strconv.FormatInt(int64(value), 10), nil
	case int8:
		return strconv.FormatInt(int64(value), 10), nil
	case int16:
		return strconv.FormatInt(int64(value), 10), nil
	case int32:
		return strconv.FormatInt(int64(value), 10), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case uint:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint64:
		return strconv.FormatUint(value, 10), nil
	case float32:
		return normalizeFloatInteger(float64(value))
	case float64:
		return normalizeFloatInteger(value)
	default:
		return "", errors.New("integerで指定してください")
	}
}

// ----------------------------------------

/*
normalizeNumberString は、json.Numberの文字列表現を整数文字列へ変換します。

機能:
  - まずint64またはuint64として精度を保って解析する
  - 小数点または指数を含む場合はfloat64の安全な整数範囲内だけを許可する

引数:
  - value string: json.Numberが保持する数値文字列

返り値:
  - string: 正規化した10進整数文字列
  - error: 非整数または表現範囲外のエラー
*/
func normalizeNumberString(value string) (string, error) {
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return strconv.FormatInt(parsed, 10), nil
	}
	if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
		return strconv.FormatUint(parsed, 10), nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Trunc(parsed) != parsed || math.Abs(parsed) > 1<<53 {
		return "", errors.New("有限な整数で指定してください")
	}
	return strconv.FormatFloat(parsed, 'f', 0, 64), nil
}

// ----------------------------------------

/*
normalizeFloatInteger は、浮動小数値を安全な整数文字列へ変換します。

機能:
  - NaN、無限大、非整数、IEEE 754で整数精度を保証できない値を拒否する

引数:
  - value float64: 整数として確認する浮動小数値

返り値:
  - string: 正規化した10進整数文字列
  - error: 安全な整数として扱えない場合のエラー
*/
func normalizeFloatInteger(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || math.Abs(value) > 1<<53 {
		return "", errors.New("有限な整数で指定してください")
	}
	return strconv.FormatFloat(value, 'f', 0, 64), nil
}

// ----------------------------------------

/*
validateNormalizedParameter は、正規化後の列挙値、範囲、銘柄形式を検証します。

機能:
  - Allowedに候補がある場合は完全一致を要求する
  - integerの最小値と最大値を確認する
  - controller用と標準情報API用の銘柄コードを用途別に検証する

引数:
  - spec parameterSpec: 入力項目仕様
  - value string: 正規化済み文字列
  - numeric *float64: 範囲確認用数値

返り値:
  - error: 列挙値、範囲、銘柄形式に違反した場合のエラー
*/
func validateNormalizedParameter(spec parameterSpec, value string, numeric *float64) error {
	if len(spec.Allowed) > 0 {
		matched := false
		for _, allowed := range spec.Allowed {
			if value == allowed {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("許可値 %v のいずれかで指定してください", spec.Allowed)
		}
	}
	if numeric != nil {
		if spec.Minimum != nil && *numeric < *spec.Minimum {
			return fmt.Errorf("%v以上で指定してください", *spec.Minimum)
		}
		if spec.Maximum != nil && *numeric > *spec.Maximum {
			return fmt.Errorf("%v以下で指定してください", *spec.Maximum)
		}
	}
	switch spec.Validator {
	case validatorNone:
		return nil
	case validatorControllerSymbol:
		return validateSymbol(value)
	case validatorSecuritySymbol:
		return validateSecuritySymbol(value)
	default:
		return fmt.Errorf("未対応のvalidator %q です", spec.Validator)
	}
}

// ----------------------------------------

/*
validateSecuritySymbol は、標準情報APIへ渡す銘柄コードを安全なpath要素として検証します。

機能:
  - 1～100文字のASCII英数字、ピリオド、アンダースコア、ハイフンだけを許可する
  - pathの特殊要素を拒否する

引数:
  - symbol string: 検証する現物・先物・オプション銘柄コード

返り値:
  - error: 空、長過ぎる、不正文字、特殊要素の場合のエラー
*/
func validateSecuritySymbol(symbol string) error {
	if symbol == "" || strings.TrimSpace(symbol) != symbol || len(symbol) > 100 || !securitySymbolPattern.MatchString(symbol) {
		return errors.New("symbolは100文字以内の英数字、ピリオド、アンダースコア、ハイフンで指定してください")
	}
	if symbol == "." || symbol == ".." {
		return errors.New("symbolにpathの特殊要素は指定できません")
	}
	return nil
}

// ----------------------------------------

/*
validateDatasetConditions は、parameter単体では表せない入力間条件を検証します。

機能:
  - resolverのkindごとの必須項目と禁止項目を確認する
  - 限月0または実在月のYYYYMM形式だけを許可する
  - NTとオプションチェーンでは明示限月を必須とする

引数:
  - dataset string: 条件を適用するdataset識別子
  - parameters map[string]string: 正規化済み入力

返り値:
  - error: 条件付き必須、禁止入力、限月形式のエラー
*/
func validateDatasetConditions(dataset string, parameters map[string]string) error {
	if month, exists := parameters["deriv_month"]; exists {
		allowZero := dataset == "derivative_symbol_resolver"
		if err := validateContractMonth(month, allowZero); err != nil {
			return fmt.Errorf("KabusControllerの入力項目 deriv_month が不正です: %w", err)
		}
	}
	switch dataset {
	case "derivative_symbol_resolver":
		return validateResolverConditions(parameters)
	case "nt_pair_symbol_resolver":
		if parameters["deriv_month"] == "0" {
			return errors.New("nt_pair_symbol_resolverではderiv_month=0を指定できません")
		}
	case "option_chain_snapshot":
		if _, exists := parameters["center_strike"]; !exists {
			return errors.New("option_chain_snapshotには入力項目 \"center_strike\" が必要です")
		}
	}
	return nil
}

// ----------------------------------------

/*
validateContractMonth は、限月の0またはYYYYMM形式を検証します。

機能:
  - 許可された場合だけ0を直近限月指定として受け付ける
  - 明示限月では6桁かつ月部分01～12を要求する

引数:
  - value string: 正規化済み限月
  - allowZero bool: 0を許可する場合はtrue

返り値:
  - error: 0の禁止またはYYYYMM形式違反のエラー
*/
func validateContractMonth(value string, allowZero bool) error {
	if value == "0" {
		if allowZero {
			return nil
		}
		return errors.New("明示的なYYYYMM形式で指定してください")
	}
	if len(value) != 6 {
		return errors.New("6桁のYYYYMM形式で指定してください")
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed/100 < 1000 || parsed%100 < 1 || parsed%100 > 12 {
		return errors.New("有効な年月をYYYYMM形式で指定してください")
	}
	return nil
}

// ----------------------------------------

/*
validateResolverConditions は、resolverのkind別入力条件を検証します。

機能:
  - future、option、mini_option_weeklyの必須項目を確認する
  - kindで使用しない項目を禁止し、誤った上流query送信を防ぐ

引数:
  - parameters map[string]string: 正規化済みresolver入力

返り値:
  - error: 条件付き必須または禁止入力がある場合のエラー
*/
func validateResolverConditions(parameters map[string]string) error {
	require := func(names ...string) error {
		for _, name := range names {
			if _, exists := parameters[name]; !exists {
				return fmt.Errorf("derivative_symbol_resolverのkind=%sには入力項目 %q が必要です", parameters["kind"], name)
			}
		}
		return nil
	}
	forbid := func(names ...string) error {
		for _, name := range names {
			if _, exists := parameters[name]; exists {
				return fmt.Errorf("derivative_symbol_resolverのkind=%sには入力項目 %q を指定できません", parameters["kind"], name)
			}
		}
		return nil
	}

	switch parameters["kind"] {
	case "future":
		if err := require("product_code", "deriv_month"); err != nil {
			return err
		}
		if parameters["product_code"] == "NK225op" || parameters["product_code"] == "NK225miniop" {
			return errors.New("derivative_symbol_resolverのkind=futureでは先物商品コードを指定してください")
		}
		return forbid("put_or_call", "strike_price", "deriv_weekly")
	case "option":
		if err := require("product_code", "deriv_month", "put_or_call", "strike_price"); err != nil {
			return err
		}
		if parameters["product_code"] != "NK225op" && parameters["product_code"] != "NK225miniop" {
			return errors.New("derivative_symbol_resolverのkind=optionではoption商品コードを指定してください")
		}
		return forbid("deriv_weekly")
	case "mini_option_weekly":
		if err := require("deriv_month", "put_or_call", "strike_price", "deriv_weekly"); err != nil {
			return err
		}
		return forbid("product_code")
	default:
		return fmt.Errorf("derivative_symbol_resolverのkindが不正です: %q", parameters["kind"])
	}
}

// ----------------------------------------

/*
collectNTPair は、ミニTOPIXと指定した日経225先物を同一明示限月で解決します。

機能:
  - derivative_symbol_resolverをTOPIXminiとNK225miniまたはmicroについて各1回呼び出す
  - SymbolName末尾のyy/MMを解析し、要求限月との一致を脚ごとに検証する
  - 2要求が成功した場合だけ合成結果を返す

引数:
  - ctx context.Context: 2要求の期限とキャンセルを共有する値
  - spec endpointSpec: nt_pair_symbol_resolver仕様
  - parameters map[string]string: 検証・正規化済み入力

返り値:
  - domain.ProviderResult: 2脚の解決結果と検証フラグ、複合metadata
  - error: いずれかの取得失敗を共通分類したエラー
*/
func (c *Collector) collectNTPair(
	ctx context.Context,
	spec endpointSpec,
	parameters map[string]string,
) (domain.ProviderResult, error) {
	month := parameters["deriv_month"]
	requestSets := []map[string]string{
		{"kind": "future", "product_code": "TOPIXmini", "deriv_month": month},
		{"kind": "future", "product_code": parameters["nikkei_product_code"], "deriv_month": month},
	}
	responses := make([]APIResponse, 0, 2)
	for _, request := range requestSets {
		response, err := c.client.Fetch(ctx, "derivative_symbol_resolver", cloneStringMap(request))
		if err != nil {
			return domain.ProviderResult{}, classifyCollectError(c.endpoints["derivative_symbol_resolver"], err)
		}
		responses = append(responses, response)
	}

	topix := derivativeLeg("topix", "TOPIXmini", month, responses[0].Body)
	nikkei := derivativeLeg("nikkei", parameters["nikkei_product_code"], month, responses[1].Body)
	if topix["symbol"] == "" || topix["symbol_name"] == "" || nikkei["symbol"] == "" || nikkei["symbol_name"] == "" {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorUpstream,
			"KabusControllerの派生商品コード解決応答形式が不正です",
			errors.New("SymbolまたはSymbolNameがありません"),
		)
	}
	sameMonth := topix["contract_month_verified"] == true && nikkei["contract_month_verified"] == true &&
		topix["contract_month"] == nikkei["contract_month"]
	warnings := make([]string, 0, 2)
	if topix["contract_month_verified"] != true {
		warnings = append(warnings, "ミニTOPIXのSymbolNameから要求限月との一致を確認できませんでした。")
	}
	if nikkei["contract_month_verified"] != true {
		warnings = append(warnings, "日経225側のSymbolNameから要求限月との一致を確認できませんでした。")
	}
	requestedMonth, _ := strconv.Atoi(month)
	data := map[string]any{
		"kind":                     "nt_pair",
		"requested_contract_month": requestedMonth,
		"same_contract_month":      sameMonth,
		"all_contracts_verified":   sameMonth,
		"usable_for_nt":            sameMonth,
		"execution_blocked":        !sameMonth,
		"contracts":                []any{topix, nikkei},
		"warnings":                 warnings,
	}
	return domain.ProviderResult{
		Data:     data,
		Metadata: compositeResponseMetadata(spec, parameters, responses),
	}, nil
}

// ----------------------------------------

/*
derivativeLeg は、銘柄コード解決応答をNT脚の検証済み形式へ変換します。

機能:
  - SymbolとSymbolNameを上流オブジェクトから取り出す
  - SymbolNameのyy/MMを要求したYYYYMMと照合する

引数:
  - leg string: topixまたはnikkeiの脚識別子
  - productCode string: 解決要求に使った商品コード
  - requestedMonth string: 明示指定したYYYYMM
  - body any: 上流の銘柄コード解決JSON

返り値:
  - map[string]any: 銘柄、解析限月、検証フラグを持つ脚情報
*/
func derivativeLeg(leg, productCode, requestedMonth string, body any) map[string]any {
	object, _ := body.(map[string]any)
	symbol, _ := stringField(object, "Symbol", "symbol")
	symbolName, _ := stringField(object, "SymbolName", "symbolName", "symbol_name")
	parsedMonth, parsed := parseContractMonthFromName(symbolName, requestedMonth)
	requested, _ := strconv.Atoi(requestedMonth)
	verified := parsed && parsedMonth == requested
	var contractMonth any
	if parsed {
		contractMonth = parsedMonth
	}
	return map[string]any{
		"leg":                     leg,
		"product_code":            productCode,
		"symbol":                  symbol,
		"symbol_name":             symbolName,
		"exchange":                2,
		"contract_month":          contractMonth,
		"contract_month_parsed":   parsed,
		"contract_month_verified": verified,
	}
}

// ----------------------------------------

/*
parseContractMonthFromName は、上流銘柄名に含まれるyy/MMをYYYYMMへ変換します。

機能:
  - 2桁年を要求限月と同じ世紀へ展開する
  - 月が01～12のときだけ解析成功とする

引数:
  - name string: 上流が返したSymbolName
  - requestedMonth string: 世紀決定に使うYYYYMM

返り値:
  - int: 解析したYYYYMM
  - bool: 有効なyy/MMを解析できた場合はtrue
*/
func parseContractMonthFromName(name, requestedMonth string) (int, bool) {
	matches := contractMonthPattern.FindAllStringSubmatch(name, -1)
	if len(matches) == 0 {
		return 0, false
	}
	match := matches[len(matches)-1]
	year2, yearErr := strconv.Atoi(match[1])
	month, monthErr := strconv.Atoi(match[2])
	requested, requestErr := strconv.Atoi(requestedMonth)
	if yearErr != nil || monthErr != nil || requestErr != nil || month < 1 || month > 12 {
		return 0, false
	}
	century := requested / 10000 * 100
	return (century+year2)*100 + month, true
}

// ----------------------------------------

/*
collectOptionChain は、登録済みオプションと板をsymbolで結合して周辺チェーンを返します。

機能:
  - option_registrationsとoption_market_dataを各1回取得する
  - 商品・明示限月で絞り、中心に最も近い権利行使価格の上下N本を選ぶ
  - Call・Putの登録と板欠損を明示し、建玉を提供しないことを宣言する

引数:
  - ctx context.Context: 2要求の期限とキャンセルを共有する値
  - spec endpointSpec: option_chain_snapshot仕様
  - parameters map[string]string: 検証・正規化済み入力

返り値:
  - domain.ProviderResult: 選択チェーン、欠損、範囲、複合metadata
  - error: 上流取得、応答形式、該当登録なしのエラー
*/
func (c *Collector) collectOptionChain(
	ctx context.Context,
	spec endpointSpec,
	parameters map[string]string,
) (domain.ProviderResult, error) {
	requests := []string{"option_registrations", "option_market_data"}
	responses := make([]APIResponse, 0, 2)
	for _, dataset := range requests {
		response, err := c.client.Fetch(ctx, dataset, map[string]string{})
		if err != nil {
			return domain.ProviderResult{}, classifyCollectError(c.endpoints[dataset], err)
		}
		responses = append(responses, response)
	}

	registrations, err := optionRegistrations(responses[0].Body)
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorUpstream, "KabusControllerのオプション登録一覧形式が不正です", err)
	}
	boards, err := optionBoards(responses[1].Body)
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorUpstream, "KabusControllerのオプション板一覧形式が不正です", err)
	}

	month, _ := strconv.ParseInt(parameters["deriv_month"], 10, 64)
	center, _ := strconv.ParseInt(parameters["center_strike"], 10, 64)
	eachSide, _ := strconv.Atoi(parameters["strikes_each_side"])
	chain := buildOptionChain(registrations, boards, parameters["option_code"], month, center, eachSide)
	if chain == nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorNotFound,
			"指定商品・限月の登録済みオプションがKabusControllerにありません",
			nil,
		)
	}
	chain["registration_snapshot"] = registrationSnapshot(responses[0].Body)
	metadata := compositeResponseMetadata(spec, parameters, responses)
	replaceFreshnessMetadata(metadata, spec.Dataset, chain["strikes"], responses[1].FetchedAt)
	return domain.ProviderResult{
		Data:     chain,
		Metadata: metadata,
	}, nil
}

// ----------------------------------------

/*
collectAPICapacity は、注文ソフトリミットとcontroller既知登録数を合成します。

機能:
  - kabus_api_soft_limits、future_registrations、option_registrationsを各1回取得する
  - API登録銘柄上限50件に対するcontroller既知件数と残枠上限を計算する
  - 現物登録と他clientの登録が不明なため残枠を確定値として扱わない

引数:
  - ctx context.Context: 3要求の期限とキャンセルを共有する値
  - spec endpointSpec: kabus_api_capacity仕様
  - parameters map[string]string: 検証・正規化済み入力

返り値:
  - domain.ProviderResult: ソフトリミット生値、登録件数、3要求の複合metadata
  - error: 上流取得または登録一覧形式を共通分類したエラー
*/
func (c *Collector) collectAPICapacity(
	ctx context.Context,
	spec endpointSpec,
	parameters map[string]string,
) (domain.ProviderResult, error) {
	datasets := []string{"kabus_api_soft_limits", "future_registrations", "option_registrations"}
	responses := make([]APIResponse, 0, len(datasets))
	for _, dataset := range datasets {
		response, err := c.client.Fetch(ctx, dataset, map[string]string{})
		if err != nil {
			return domain.ProviderResult{}, classifyCollectError(c.endpoints[dataset], err)
		}
		responses = append(responses, response)
	}
	if _, ok := responses[0].Body.(map[string]any); !ok {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorUpstream,
			"KabusControllerの注文ソフトリミット形式が不正です",
			errors.New("ルートがobjectではありません"),
		)
	}
	futures, err := registrationArray(responses[1].Body, "futures")
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorUpstream,
			"KabusControllerの先物登録一覧形式が不正です",
			err,
		)
	}
	options, err := registrationArray(responses[2].Body, "options")
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorUpstream,
			"KabusControllerのオプション登録一覧形式が不正です",
			err,
		)
	}
	known := len(futures) + len(options)
	uniqueSymbols := make(map[string]struct{}, known)
	missingSymbolCount := 0
	for _, registration := range append(append([]map[string]any{}, futures...), options...) {
		symbol, exists := stringField(registration, "symbol", "Symbol")
		if !exists || symbol == "" {
			missingSymbolCount++
			continue
		}
		uniqueSymbols[symbol] = struct{}{}
	}
	uniqueKnown := len(uniqueSymbols)
	remaining := 50 - uniqueKnown
	if remaining < 0 {
		remaining = 0
	}
	data := map[string]any{
		"soft_limits":                                  responses[0].Body,
		"registration_limit":                           50,
		"controller_known_future_count":                len(futures),
		"controller_known_option_count":                len(options),
		"controller_known_count":                       known,
		"controller_known_unique_symbol_count":         uniqueKnown,
		"controller_registration_missing_symbol_count": missingSymbolCount,
		"controller_registration_duplicate_count":      known - uniqueKnown - missingSymbolCount,
		"remaining_upper_bound":                        remaining,
		"remaining_upper_bound_calculation":            "50 - controller_known_unique_symbol_count",
		"remaining_is_exact":                           false,
		"other_clients_or_stock_registrations_unknown": true,
		"shared_limit_membership_verified":             false,
		"calculation_assumption":                       "controllerの先物・オプション登録がREST/PUSH共通の50銘柄上限に含まれる前提で計算しています。",
		"registration_snapshots": map[string]any{
			"future": registrationSnapshot(responses[1].Body),
			"option": registrationSnapshot(responses[2].Body),
		},
	}
	return domain.ProviderResult{
		Data:     data,
		Metadata: compositeResponseMetadata(spec, parameters, responses),
	}, nil
}

// ----------------------------------------

/*
registrationArray は、KabusController登録一覧応答から指定配列を取り出します。

機能:
  - custom responseのdata.futuresまたはdata.optionsを優先して扱う
  - 直接futuresまたはoptionsを持つ互換形も受け付ける
  - statusとstateが存在する場合は正常完了値を検証する

引数:
  - body any: 登録一覧の上流JSON
  - name string: futuresまたはoptionsの配列名

返り値:
  - []map[string]any: 登録情報のobject配列
  - error: ルート、data、配列、要素型が不正な場合のエラー
*/
func registrationArray(body any, name string) ([]map[string]any, error) {
	root, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New("ルートがobjectではありません")
	}
	if status, exists := root["status"]; exists && status != "ok" {
		return nil, fmt.Errorf("statusがokではありません: %v", status)
	}
	container := root
	if data, exists := root["data"]; exists {
		typed, dataOK := data.(map[string]any)
		if !dataOK {
			return nil, errors.New("dataがobjectではありません")
		}
		container = typed
	}
	if state, exists := container["state"]; exists && state != "succeeded" {
		return nil, fmt.Errorf("stateがsucceededではありません: %v", state)
	}
	return objectSlice(container[name])
}

// ----------------------------------------

/*
registrationSnapshot は、登録一覧応答の状態と基準時刻を監査用に抽出します。

機能:
  - ルートのstatusとdata内のstate、updatedAtを取得する
  - 互換形としてルート直下のstate、updatedAtも扱う
  - 欠損項目を推測せずnilとして返す

引数:
  - body any: 先物またはオプション登録一覧の上流JSON

返り値:
  - map[string]any: status、state、updated_atを持つ登録スナップショット
*/
func registrationSnapshot(body any) map[string]any {
	snapshot := map[string]any{
		"status":     nil,
		"state":      nil,
		"updated_at": nil,
	}
	root, ok := body.(map[string]any)
	if !ok {
		return snapshot
	}
	if status, exists := root["status"]; exists {
		snapshot["status"] = status
	}
	container := root
	if data, exists := root["data"].(map[string]any); exists {
		container = data
	}
	if state, exists := container["state"]; exists {
		snapshot["state"] = state
	}
	if updatedAt, exists := stringField(container, "updatedAt", "updated_at", "UpdatedAt"); exists {
		snapshot["updated_at"] = updatedAt
	}
	return snapshot
}

// ----------------------------------------

/*
optionRegistrations は、KabusControllerの登録一覧応答からoption配列を取り出します。

機能:
  - data.options形式と直接options形式を受け付ける
  - 各要素をmap[string]anyとして検証する

引数:
  - body any: option_registrationsの上流JSON

返り値:
  - []map[string]any: 登録済みオプション配列
  - error: 必須階層または要素型が不正な場合のエラー
*/
func optionRegistrations(body any) ([]map[string]any, error) {
	return registrationArray(body, "options")
}

// ----------------------------------------

/*
optionBoards は、KabusControllerの板応答からoption板配列を取り出します。

機能:
  - data配列と直接配列を受け付ける
  - 各要素をmap[string]anyとして検証する

引数:
  - body any: option_market_dataの上流JSON

返り値:
  - []map[string]any: オプション板配列
  - error: 配列または要素型が不正な場合のエラー
*/
func optionBoards(body any) ([]map[string]any, error) {
	if root, ok := body.(map[string]any); ok {
		return objectSlice(root["data"])
	}
	return objectSlice(body)
}

// ----------------------------------------

/*
objectSlice は、JSON配列をobject配列として検証・変換します。

機能:
  - []anyと[]map[string]anyの双方を受け付ける
  - object以外の要素を拒否する

引数:
  - value any: 変換するJSON配列

返り値:
  - []map[string]any: objectだけを含む配列
  - error: 配列または要素型が不正な場合のエラー
*/
func objectSlice(value any) ([]map[string]any, error) {
	if typed, ok := value.([]map[string]any); ok {
		return typed, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, errors.New("配列ではありません")
	}
	result := make([]map[string]any, 0, len(values))
	for index, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("要素%dがobjectではありません", index)
		}
		result = append(result, object)
	}
	return result, nil
}

// ----------------------------------------

type optionStrike struct {
	strike        int64
	registrations map[string]map[string]any
}

/*
buildOptionChain は、登録配列と板配列を指定範囲のCall・Putチェーンへ変換します。

機能:
  - symbolをjoin keyとして板を登録へ対応付ける
  - 中心に最も近い登録strikeと上下N本を安定した昇順で選ぶ
  - registration_missingとboard_missingをmissingへ列挙する

引数:
  - registrations []map[string]any: 登録済みオプション
  - boards []map[string]any: 登録済みオプションの板
  - optionCode string: 抽出する商品コード
  - month int64: 抽出するYYYYMM
  - requestedCenter int64: 利用者が指定した中心価格
  - eachSide int: 中心の上下に含めるstrike本数

返り値:
  - map[string]any: チェーン、coverage、missing。該当登録がなければnil
*/
func buildOptionChain(
	registrations []map[string]any,
	boards []map[string]any,
	optionCode string,
	month int64,
	requestedCenter int64,
	eachSide int,
) map[string]any {
	boardBySymbol := make(map[string]map[string]any, len(boards))
	for _, board := range boards {
		if symbol, ok := stringField(board, "Symbol", "symbol"); ok {
			boardBySymbol[symbol] = board
		}
	}
	byStrike := make(map[int64]*optionStrike)
	for _, registration := range registrations {
		code, codeOK := stringField(registration, "optionCode", "OptionCode")
		registrationMonth, monthOK := integerField(registration, "derivMonth", "DerivMonth")
		strike, strikeOK := integerField(registration, "strikePrice", "StrikePrice")
		side, sideOK := stringField(registration, "putOrCall", "PutOrCall")
		if !codeOK || !monthOK || !strikeOK || !sideOK || strike < 0 || code != optionCode || registrationMonth != month || (side != "C" && side != "P") {
			continue
		}
		item, exists := byStrike[strike]
		if !exists {
			item = &optionStrike{strike: strike, registrations: map[string]map[string]any{}}
			byStrike[strike] = item
		}
		item.registrations[side] = registration
	}
	if len(byStrike) == 0 {
		return nil
	}

	strikes := make([]int64, 0, len(byStrike))
	for strike := range byStrike {
		strikes = append(strikes, strike)
	}
	sort.Slice(strikes, func(i, j int) bool { return strikes[i] < strikes[j] })
	centerIndex := nearestStrikeIndex(strikes, requestedCenter)
	start := centerIndex - eachSide
	if start < 0 {
		start = 0
	}
	end := centerIndex + eachSide + 1
	if end > len(strikes) {
		end = len(strikes)
	}
	selected := strikes[start:end]

	rows := make([]any, 0, len(selected))
	missing := make([]any, 0)
	for _, strike := range selected {
		row := map[string]any{"strike_price": strike}
		for _, side := range []string{"C", "P"} {
			field := "call"
			if side == "P" {
				field = "put"
			}
			registration, exists := byStrike[strike].registrations[side]
			if !exists {
				row[field] = nil
				missing = append(missing, map[string]any{
					"strike_price": strike, "put_or_call": side, "reason": "registration_missing",
				})
				continue
			}
			symbol, _ := stringField(registration, "symbol", "Symbol")
			leg := map[string]any{
				"symbol":              symbol,
				"registration":        cloneAnyMap(registration),
				"has_board":           false,
				"has_current_price":   false,
				"has_buy_quote":       false,
				"has_sell_quote":      false,
				"has_two_sided_quote": false,
				"board":               nil,
			}
			if board, boardExists := boardBySymbol[symbol]; boardExists {
				leg["has_board"] = true
				hasCurrentPrice, hasBuyQuote, hasSellQuote := optionBoardQuality(board)
				leg["has_current_price"] = hasCurrentPrice
				leg["has_buy_quote"] = hasBuyQuote
				leg["has_sell_quote"] = hasSellQuote
				leg["has_two_sided_quote"] = hasBuyQuote && hasSellQuote
				leg["board"] = cloneAnyMap(board)
			} else {
				missing = append(missing, map[string]any{
					"strike_price": strike, "put_or_call": side, "symbol": symbol, "reason": "board_missing",
				})
			}
			row[field] = leg
		}
		rows = append(rows, row)
	}
	complete := len(missing) == 0 && start == centerIndex-eachSide && end == centerIndex+eachSide+1
	return map[string]any{
		"option_code":              optionCode,
		"contract_month":           month,
		"requested_center_strike":  requestedCenter,
		"center_strike":            strikes[centerIndex],
		"strikes_each_side":        eachSide,
		"strikes":                  rows,
		"missing":                  missing,
		"open_interest_available":  false,
		"volume_availability":      optionChainAvailability(rows, "TradingVolume"),
		"quote_time_availability":  optionChainAvailability(rows, "CurrentPriceTime"),
		"automatic_registration":   false,
		"automatic_unregistration": false,
		"coverage": map[string]any{
			"registered_strike_min":   strikes[0],
			"registered_strike_max":   strikes[len(strikes)-1],
			"registered_strike_count": len(strikes),
			"selected_strike_min":     selected[0],
			"selected_strike_max":     selected[len(selected)-1],
			"selected_strike_count":   len(selected),
			"complete":                complete,
		},
	}
}

// ----------------------------------------

// optionChainAvailability は、選択チェーンの板項目品質を集計します。
//
// 主な特徴:
//   - TradingVolumeは正数、CurrentPriceTimeはRFC3339解析成功を利用可能とする
//   - 項目の存在、ゼロ値、不正値、欠損を利用可能件数と分離して返す
//   - Call・Putの登録本数と板あり本数を併記する
//
// 引数:
//   - rows []any: buildOptionChainが生成したstrike行
//   - field string: TradingVolumeまたはCurrentPriceTime等の板項目名
//
// 返り値:
//   - map[string]any: 登録、板、項目の件数と完全性
func optionChainAvailability(rows []any, field string) map[string]any {
	registeredCount := 0
	boardCount := 0
	presentCount := 0
	availableCount := 0
	zeroValueCount := 0
	invalidCount := 0
	for _, rawRow := range rows {
		row, _ := rawRow.(map[string]any)
		for _, side := range []string{"call", "put"} {
			leg, exists := row[side].(map[string]any)
			if !exists {
				continue
			}
			registeredCount++
			board, boardExists := leg["board"].(map[string]any)
			if !boardExists {
				continue
			}
			boardCount++
			value, exists := board[field]
			present, available, zeroValue, invalid := optionFieldAvailability(field, value, exists)
			if present {
				presentCount++
			}
			if available {
				availableCount++
			}
			if zeroValue {
				zeroValueCount++
			}
			if invalid {
				invalidCount++
			}
		}
	}
	availableDefinition := "non_null"
	switch field {
	case "TradingVolume":
		availableDefinition = "positive_numeric"
	case "CurrentPriceTime":
		availableDefinition = "rfc3339"
	}
	return map[string]any{
		"registered_contract_count": registeredCount,
		"board_contract_count":      boardCount,
		"present_contract_count":    presentCount,
		"available_contract_count":  availableCount,
		"zero_value_contract_count": zeroValueCount,
		"invalid_contract_count":    invalidCount,
		"missing_contract_count":    registeredCount - presentCount,
		"available_definition":      availableDefinition,
		"complete":                  registeredCount > 0 && availableCount == registeredCount,
	}
}

// ----------------------------------------

/*
optionFieldAvailability は、オプション板項目の存在と利用可能性を判定します。

機能:
  - TradingVolumeは数値として存在するか、正数か、ゼロかを分ける
  - CurrentPriceTimeは空文字を欠損、非RFC3339文字列を不正として扱う
  - その他の項目は非nil・非空文字を利用可能とする

引数:
  - field string: TradingVolume、CurrentPriceTimeまたはその他の項目名
  - value any: 上流板項目の値
  - exists bool: 上流板objectにキーが存在するか

返り値:
  - bool: 項目がnull以外で存在する場合はtrue
  - bool: 分析に利用可能な値の場合はtrue
  - bool: TradingVolumeが数値0の場合はtrue
  - bool: 型または形式が不正な場合はtrue
*/
func optionFieldAvailability(field string, value any, exists bool) (bool, bool, bool, bool) {
	if !exists || value == nil || value == "" {
		return false, false, false, false
	}
	switch field {
	case "TradingVolume":
		number, valid := numericValue(value)
		if !valid || number < 0 {
			return true, false, false, true
		}
		return true, number > 0, number == 0, false
	case "CurrentPriceTime":
		text, valid := value.(string)
		if !valid {
			return true, false, false, true
		}
		_, err := time.Parse(time.RFC3339, text)
		return true, err == nil, false, err != nil
	default:
		return true, true, false, false
	}
}

// ----------------------------------------

/*
optionBoardQuality は、オプション板の現在値と両側気配の利用可能性を判定します。

機能:
  - CurrentPriceが正の数値の場合だけ現在値ありとする
  - 上流のBid・Ask命名逆転を避け、Buy1.PriceとSell1.Priceを個別に判定する

引数:

  - board map[string]any: 判定するオプション板

    返り値:

  - bool: CurrentPriceが正の数値の場合はtrue

  - bool: Buy1.Priceが0より大きい場合はtrue

  - bool: Sell1.Priceが0より大きい場合はtrue
*/
func optionBoardQuality(board map[string]any) (bool, bool, bool) {
	currentPrice, currentPriceExists := numericValue(board["CurrentPrice"])
	hasCurrentPrice := currentPriceExists && currentPrice > 0
	buyPrice := nestedPositivePrice(board, "Buy1")
	sellPrice := nestedPositivePrice(board, "Sell1")
	return hasCurrentPrice, buyPrice, sellPrice
}

// ----------------------------------------

/*
nestedPositivePrice は、板階層内のPriceが正の数値か確認します。

機能:
  - 指定したBuy1またはSell1のobjectだけを読み取る

引数:
  - board map[string]any: 板情報object
  - level string: Buy1またはSell1の階層名

返り値:
  - bool: Priceが数値かつ0より大きい場合はtrue
*/
func nestedPositivePrice(board map[string]any, level string) bool {
	quote, ok := board[level].(map[string]any)
	if !ok {
		return false
	}
	price, ok := numericValue(quote["Price"])
	return ok && price > 0
}

// ----------------------------------------

/*
numericValue は、JSON数値またはGo数値をfloat64へ変換します。

機能:
  - json.Numberと全整数・浮動小数型を受け付ける
  - nil、非数値、NaN、無限大を拒否する

引数:
  - value any: 数値として読み取る値

返り値:
  - float64: 変換した有限数値
  - bool: 有限な数値の場合はtrue
*/
func numericValue(value any) (float64, bool) {
	var result float64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		result = parsed
	case float64:
		result = typed
	case float32:
		result = float64(typed)
	case int:
		result = float64(typed)
	case int8:
		result = float64(typed)
	case int16:
		result = float64(typed)
	case int32:
		result = float64(typed)
	case int64:
		result = float64(typed)
	case uint:
		result = float64(typed)
	case uint8:
		result = float64(typed)
	case uint16:
		result = float64(typed)
	case uint32:
		result = float64(typed)
	case uint64:
		result = float64(typed)
	default:
		return 0, false
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, false
	}
	return result, true
}

// ----------------------------------------

/*
nearestStrikeIndex は、中心値に最も近いstrikeの位置を返します。

機能:
  - 同距離の場合は価格が低い側を選ぶ

引数:
  - strikes []int64: 昇順の権利行使価格
  - center int64: 利用者が指定した中心値

返り値:
  - int: 最も近いstrikeの配列位置
*/
func nearestStrikeIndex(strikes []int64, center int64) int {
	if center == 0 {
		return (len(strikes) - 1) / 2
	}
	index := 0
	bestDistance := absoluteDifference(strikes[0], center)
	for candidate := 1; candidate < len(strikes); candidate++ {
		distance := absoluteDifference(strikes[candidate], center)
		if distance < bestDistance {
			index = candidate
			bestDistance = distance
		}
	}
	return index
}

// ----------------------------------------

/*
absoluteDifference は、2つの非負価格の差を符号なしで返します。

機能:
  - 引き算の向きを値の大小に応じて切り替える

引数:
  - left int64: 比較する値
  - right int64: 比較する値

返り値:
  - uint64: 2値間の絶対差
*/
func absoluteDifference(left, right int64) uint64 {
	if left >= right {
		return uint64(left - right)
	}
	return uint64(right - left)
}

// ----------------------------------------

/*
singleResponseMetadata は、単一GETの共通metadataを生成します。

機能:
  - 取得元、応答状態、入力、副作用可能性、板方向を記録する
  - RFC3339のCurrentPriceTimeだけからsource_atとage_secondsを算出する

引数:
  - spec endpointSpec: 収集したdataset仕様
  - parameters map[string]string: 正規化済み入力
  - response APIResponse: 上流応答

返り値:
  - map[string]any: 共通収集metadata
*/
func singleResponseMetadata(
	spec endpointSpec,
	parameters map[string]string,
	response APIResponse,
) map[string]any {
	metadata := baseMetadata(spec, parameters)
	metadata["source_url"] = response.SourceURL
	metadata["endpoint"] = spec.Path
	metadata["upstream_status"] = response.StatusCode
	metadata["upstream_fetched"] = response.FetchedAt
	metadata["response_bytes"] = response.ResponseBytes
	applyFreshnessMetadata(metadata, response.Body, response.FetchedAt)
	return metadata
}

// ----------------------------------------

/*
compositeResponseMetadata は、複数要求で作るdatasetの共通metadataを生成します。

機能:
  - source_urls、upstream_requests、各状態、合計本文サイズを記録する
  - 全応答内の最古CurrentPriceTimeを保守的なsource_atとし、最新値を別項目へ保持する

引数:
  - spec endpointSpec: 複合dataset仕様
  - parameters map[string]string: 正規化済み公開入力
  - responses []APIResponse: 合成に使用した上流応答

返り値:
  - map[string]any: 複合収集metadata
*/
func compositeResponseMetadata(
	spec endpointSpec,
	parameters map[string]string,
	responses []APIResponse,
) map[string]any {
	metadata := baseMetadata(spec, parameters)
	sourceURLs := make([]string, 0, len(responses))
	statuses := make([]int, 0, len(responses))
	var responseBytes int64
	var fetchedAt time.Time
	bodies := make([]any, 0, len(responses))
	for _, response := range responses {
		sourceURLs = append(sourceURLs, response.SourceURL)
		statuses = append(statuses, response.StatusCode)
		responseBytes += response.ResponseBytes
		if response.FetchedAt.After(fetchedAt) {
			fetchedAt = response.FetchedAt
		}
		bodies = append(bodies, response.Body)
	}
	metadata["source_urls"] = sourceURLs
	metadata["upstream_requests"] = len(responses)
	metadata["upstream_statuses"] = statuses
	metadata["upstream_fetched"] = fetchedAt
	metadata["response_bytes"] = responseBytes
	applyFreshnessMetadata(metadata, bodies, fetchedAt)
	return metadata
}

// ----------------------------------------

/*
replaceFreshnessMetadata は、複合応答の鮮度を分析対象データだけで再計算します。

機能:
  - 全上流応答から算出済みの鮮度項目を初期状態へ戻す
  - OPチェーン等で選択対象外の板時刻がsource_atへ混入することを防ぐ
  - 指定した取得時刻を基準にapplyFreshnessMetadataを再適用する

引数:
  - metadata map[string]any: 更新する複合応答metadata
  - dataset string: 既定のstale_reasonを選ぶdataset識別子
  - body any: 鮮度を計算する選択済みデータ
  - fetchedAt time.Time: age_secondsの基準時刻

返り値:
  - なし
*/
func replaceFreshnessMetadata(metadata map[string]any, dataset string, body any, fetchedAt time.Time) {
	metadata["source_at"] = nil
	metadata["age_seconds"] = nil
	metadata["is_stale"] = nil
	metadata["stale_reason"] = defaultStaleReason(dataset)
	delete(metadata, "source_at_latest")
	delete(metadata, "source_time_parsed_count")
	delete(metadata, "source_time_missing_or_unparseable_count")
	applyFreshnessMetadata(metadata, body, fetchedAt)
}

// ----------------------------------------

/*
baseMetadata は、全datasetに共通するmetadata初期値を生成します。

機能:
  - request_parametersを複製して呼び出し元mapとの共有を避ける
  - 鮮度、取引状態、限月、副作用、板方向の共通項目を設定する

引数:
  - spec endpointSpec: 収集対象のdataset仕様
  - parameters map[string]string: 正規化済み公開入力

返り値:
  - map[string]any: 共通metadata初期値
*/
func baseMetadata(spec endpointSpec, parameters map[string]string) map[string]any {
	metadata := map[string]any{
		"source_name":        "KabusController",
		"request_parameters": cloneStringMap(parameters),
		"read_only":          !spec.MayRegisterSymbol,
		"on_demand":          true,
		"source_at":          nil,
		"age_seconds":        nil,
		"market_state":       "unknown",
		"contract_month":     contractMonthMetadata(parameters),
		"is_stale":           nil,
		"stale_reason":       defaultStaleReason(spec.Dataset),
	}
	if spec.MayRegisterSymbol {
		metadata["may_register_symbol"] = true
		metadata["side_effect_notice"] = "この標準情報APIのGETは上流のAPI登録銘柄リストへ銘柄を自動登録する場合があります。"
	}
	if spec.StandardInfo {
		metadata["standard_information_api"] = true
	}
	if spec.BidAskReversed {
		metadata["bid_ask_warning"] = bidAskInterpretationWarning
	}
	if spec.Dataset == "kabus_ranking" {
		metadata["ranking_target_date_available"] = false
		metadata["price_and_industry_clear_window_jst"] = "平日07:53頃から09:00過ぎ頃"
		metadata["margin_ranking_update_schedule_jst"] = "毎週第3営業日07:55頃"
		metadata["empty_response_may_be_clear_window"] = true
	}
	return metadata
}

// ----------------------------------------

// defaultStaleReason は、datasetの上流時刻仕様に応じた鮮度未判定理由を返します。
//
// 主な特徴:
//   - ランキング対象日と為替時刻の日付欠損を区別する
//   - その他は鮮度を確定できる日付付き価格時刻がないことを示す
//
// 引数:
//   - dataset string: 収集対象dataset
//
// 返り値:
//   - string: is_staleを未判定とする理由
func defaultStaleReason(dataset string) string {
	switch dataset {
	case "kabus_ranking":
		return "上流がランキング対象日を返さないため鮮度を判定できません"
	case "kabus_fx_snapshot":
		return "上流の為替時刻に日付がないため鮮度を判定できません"
	default:
		return "上流応答に鮮度を確定できる日付付き価格時刻がないため判定できません"
	}
}

// ----------------------------------------

/*
contractMonthMetadata は、正規化済み入力から明示限月をmetadata用整数へ変換します。

機能:
  - deriv_monthが未指定または0の場合はnilを返す

引数:
  - parameters map[string]string: 正規化済み公開入力

返り値:
  - any: YYYYMM整数またはnil
*/
func contractMonthMetadata(parameters map[string]string) any {
	value, exists := parameters["deriv_month"]
	if !exists || value == "0" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return parsed
}

// ----------------------------------------

/*
applyFreshnessMetadata は、応答内のRFC3339 CurrentPriceTimeから鮮度項目を更新します。

機能:
  - objectとarrayを再帰走査し、解析可能なCurrentPriceTimeの最古値を保守的なsource_atにする
  - 最新値、解析件数、欠損または解析不能件数も別metadataへ保持する
  - 時刻だけのランキング・FX値から日付を推測しない
  - dataset固有の鮮度閾値がないためis_staleは未判定のままにする

引数:
  - metadata map[string]any: 更新するmetadata
  - body any: 上流JSONまたは複数応答本文
  - fetchedAt time.Time: age_secondsの基準時刻

返り値:
  - なし
*/
func applyFreshnessMetadata(metadata map[string]any, body any, fetchedAt time.Time) {
	summary := summarizeCurrentPriceTimes(body)
	if summary.parsedCount+summary.unparsedCount > 0 {
		metadata["source_time_parsed_count"] = summary.parsedCount
		metadata["source_time_missing_or_unparseable_count"] = summary.unparsedCount
	}
	if summary.parsedCount == 0 {
		return
	}
	metadata["source_at"] = summary.oldest
	metadata["source_at_latest"] = summary.latest
	age := fetchedAt.Sub(summary.oldest).Seconds()
	if age < 0 {
		age = 0
	}
	metadata["age_seconds"] = age
	if summary.unparsedCount > 0 {
		metadata["stale_reason"] = fmt.Sprintf(
			"日付付き価格時刻を%d件解析しましたが、欠損または解析不能が%d件あり、dataset固有の鮮度閾値も未定義です",
			summary.parsedCount,
			summary.unparsedCount,
		)
	} else {
		metadata["stale_reason"] = "dataset固有の鮮度閾値が定義されていないためis_staleを判定していません"
	}
}

// ----------------------------------------

type currentPriceTimeSummary struct {
	oldest        time.Time
	latest        time.Time
	parsedCount   int
	unparsedCount int
}

/*
summarizeCurrentPriceTimes は、JSON値内のCurrentPriceTimeを保守的に集計します。

機能:
  - mapとsliceを再帰走査する
  - 解析可能な最古・最新時刻と、解析済み・欠損件数を分ける
  - 複数板の一部だけ新しい場合に最新時刻だけで全体鮮度を表さない

引数:
  - value any: 走査するJSON値

返り値:
  - currentPriceTimeSummary: 最古・最新時刻と解析件数
*/
func summarizeCurrentPriceTimes(value any) currentPriceTimeSummary {
	result := currentPriceTimeSummary{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if key == "CurrentPriceTime" {
					text, stringValue := child.(string)
					parsed, err := time.Parse(time.RFC3339Nano, text)
					if !stringValue || err != nil {
						result.unparsedCount++
					} else {
						if result.parsedCount == 0 || parsed.Before(result.oldest) {
							result.oldest = parsed
						}
						if result.parsedCount == 0 || parsed.After(result.latest) {
							result.latest = parsed
						}
						result.parsedCount++
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case []map[string]any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

// ----------------------------------------

/*
classifyCollectError は、clientとcontextエラーを共通ServiceErrorへ分類します。

機能:
  - NotFound対象datasetの404をNOT_FOUNDへ分類する
  - 400・422、408・504、認証・混雑・利用不能状態を共通ErrorKindへ変換する
  - その他の上流HTTP・通信・JSONエラーをUPSTREAM_ERRORへ分類する

引数:
  - spec endpointSpec: 失敗したdataset仕様
  - err error: APIClientが返したエラー

返り値:
  - error: transport共通分類を持つ*domain.ServiceError
*/
func classifyCollectError(spec endpointSpec, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewServiceError(domain.ErrorTimeout, "KabusController API要求が時間切れになりました", err)
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewServiceError(domain.ErrorProviderUnavailable, "KabusController API要求がキャンセルされました", err)
	}
	var apiError *APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return domain.NewServiceError(domain.ErrorInvalidArgument, "KabusController APIが要求を拒否しました", apiError)
		case http.StatusNotFound:
			if spec.NotFound {
				return domain.NewServiceError(domain.ErrorNotFound, "指定したKabusControllerデータが見つかりません", apiError)
			}
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return domain.NewServiceError(domain.ErrorTimeout, "KabusController API要求が時間切れになりました", apiError)
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooEarly,
			http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return domain.NewServiceError(domain.ErrorProviderUnavailable, "KabusController APIを現在利用できません", apiError)
		}
	}
	return domain.NewServiceError(
		domain.ErrorUpstream,
		fmt.Sprintf("KabusController APIからdataset %qを取得できません", spec.Dataset),
		err,
	)
}

// ----------------------------------------

/*
stringField は、候補キーの最初のstring値をJSON objectから取得します。

機能:
  - 上流API間の大文字・小文字命名差を候補順で吸収する

引数:
  - object map[string]any: 検索するJSON object
  - names ...string: 候補キー

返り値:
  - string: 見つかった文字列
  - bool: string値が見つかった場合はtrue
*/
func stringField(object map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := object[name].(string); ok {
			return value, true
		}
	}
	return "", false
}

// ----------------------------------------

/*
integerField は、候補キーの最初の整数値をJSON objectから取得します。

機能:
  - json.NumberまたはGo数値型をnormalizeIntegerで統一して解析する

引数:
  - object map[string]any: 検索するJSON object
  - names ...string: 候補キー

返り値:
  - int64: 見つかった整数
  - bool: int64範囲の整数が見つかった場合はtrue
*/
func integerField(object map[string]any, names ...string) (int64, bool) {
	for _, name := range names {
		raw, exists := object[name]
		if !exists {
			continue
		}
		normalized, err := normalizeInteger(raw)
		if err != nil {
			continue
		}
		parsed, err := strconv.ParseInt(normalized, 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// ----------------------------------------

/*
cloneStringMap は、入力文字列mapを複製します。

機能:
  - APIClientやmetadataとの可変map共有を防ぐ

引数:
  - source map[string]string: 複製元

返り値:
  - map[string]string: 独立した複製
*/
func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// ----------------------------------------

/*
cloneAnyMap は、JSON objectの最上位mapを複製します。

機能:
  - 合成結果へ項目を追加しても上流応答の最上位mapを変更しない

引数:
  - source map[string]any: 複製元JSON object

返り値:
  - map[string]any: 独立した最上位map
*/
func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// ----------------------------------------

/*
isNilAPIClient は、interfaceへ格納された型付きnilを含めて検出します。

機能:
  - constructorでnil clientによる後続panicを防ぐ

引数:
  - client APIClient: nilかどうか確認するclient実装

返り値:
  - bool: clientがnilまたは型付きnilの場合はtrue
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
