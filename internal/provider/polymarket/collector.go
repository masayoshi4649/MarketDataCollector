package polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

// Collector は、Polymarketの公開API要求を検証、直列化、正規化します。
type Collector struct {
	client    APIClient
	endpoints map[string]endpointSpec
	pacing    *pacingState
}

type queuedResponse struct {
	response APIResponse
	err      error
}

type queuedRequest struct {
	ctx     context.Context
	classes []rateClass
	execute func(context.Context) (APIResponse, error)
	result  chan queuedResponse
	started bool
}

type pacingState struct {
	mu           sync.Mutex
	queue        []*queuedRequest
	running      bool
	nextStart    map[rateClass]time.Time
	intervals    map[rateClass]time.Duration
	queueChanged chan struct{}
	now          func() time.Time
	waitInterval func(time.Duration, <-chan struct{}) bool
}

// ----------------------------------------

// NewCollector は、Polymarket collectorを生成します。
//
// 機能:
//   - nil clientと37件のdataset重複を起動時に検証する
//   - FIFO workerは生成せず、最初のCollect要求時だけ遅延起動できる状態を作る
//
// 引数:
//   - client APIClient: 1回の公開GETを行うclient
//
// 返り値:
//   - *Collector: provider.Collectorを満たす収集器
//   - error: clientまたは固定dataset仕様が不正な場合のエラー
func NewCollector(client APIClient) (*Collector, error) {
	if isNilAPIClient(client) {
		return nil, errors.New("Polymarket API clientがありません")
	}
	endpoints := make(map[string]endpointSpec, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if _, exists := endpoints[spec.Dataset]; exists {
			return nil, fmt.Errorf("Polymarket dataset %qが重複しています", spec.Dataset)
		}
		endpoints[spec.Dataset] = spec
	}
	return &Collector{client: client, endpoints: endpoints, pacing: newPacingState()}, nil
}

// ----------------------------------------

// Descriptor は、対応する37件の公開読取datasetを返します。
//
// 機能:
//   - endpointSpecsの安定順を維持し、入力型、許容値、既定値を公開する
//   - providerの認証不要、読取専用という性質を説明する
//
// 引数:
//   - なし
//
// 返り値:
//   - domain.ProviderDescriptor: datalistへ掲載するprovider仕様
func (c *Collector) Descriptor() domain.ProviderDescriptor {
	datasets := make([]domain.DatasetDescriptor, 0, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		datasets = append(datasets, datasetDescriptor(spec))
	}
	return domain.ProviderDescriptor{
		Name: "polymarket", DisplayName: "Polymarket",
		Description: "認証不要のGamma、CLOB、Data公開APIから、予測市場・イベント、注文板・価格履歴、公開ウォレットのポジション等を読み取り専用収集します。",
		Datasets:    datasets,
	}
}

// ----------------------------------------

// Collect は、指定datasetを検証して単一FIFO経由で1回だけ取得します。
//
// 機能:
//   - 未知項目、型、範囲、相互排他、時刻関係を通信前に検証する
//   - 全dataset共通FIFOと公式quotaの50パーセント以下の開始間隔を適用する
//   - 正規化結果と取得元、仕様、ページング情報をmetadata付きで返す
//
// 引数:
//   - ctx context.Context: キュー待機と上流GETの期限、キャンセル
//   - dataset string: Descriptorに掲載されたdataset識別子
//   - parameters map[string]any: dataset固有の公開入力
//
// 返り値:
//   - domain.ProviderResult: 正規化済みJSONとmetadata
//   - error: 入力、待機、通信、上流状態、正規化の共通分類エラー
func (c *Collector) Collect(ctx context.Context, dataset string, parameters map[string]any) (domain.ProviderResult, error) {
	spec, exists := c.endpoints[dataset]
	if !exists {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorNotFound, fmt.Sprintf("未対応のPolymarket datasetです: %q", dataset), nil)
	}
	values, err := validateParameters(spec, parameters)
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorInvalidArgument, err.Error(), err)
	}
	query, err := buildUpstreamQuery(spec, values, parameters)
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorInvalidArgument, err.Error(), err)
	}
	response, err := c.pacing.Execute(ctx, requestRateClasses(spec), func(requestContext context.Context) (APIResponse, error) {
		return c.client.Fetch(requestContext, dataset, query)
	})
	if err != nil {
		return domain.ProviderResult{}, classifyCollectError(err)
	}
	data := normalizeResponse(spec.Normalizer, response.Body, values)
	metadata := buildMetadata(spec, response, values, data)
	return domain.ProviderResult{Data: data, Metadata: metadata}, nil
}

// ----------------------------------------

// validateParameters は、公開入力を仕様どおりの安定型へ変換します。
//
// 機能:
//   - 未知項目を拒否し、既定値を補い、必須項目を確認する
//   - JSON arrayだけを配列入力として扱い、配列内重複を拒否する
//   - dataset横断では表現できない相互関係を追加検証する
//
// 引数:
//   - spec endpointSpec: dataset入力仕様
//   - parameters map[string]any: 利用者が指定した公開入力
//
// 返り値:
//   - map[string]any: string、json.Number、bool、[]stringへ正規化した値
//   - error: 未知項目、型、範囲、関係が不正な場合のエラー
func validateParameters(spec endpointSpec, parameters map[string]any) (map[string]any, error) {
	parameterMap := make(map[string]parameterSpec, len(spec.Parameters))
	for _, item := range spec.Parameters {
		parameterMap[item.Name] = item
	}
	for name := range parameters {
		if _, exists := parameterMap[name]; !exists {
			return nil, fmt.Errorf("Polymarket dataset %qに未知の入力項目があります: %q", spec.Dataset, name)
		}
	}
	result := make(map[string]any, len(spec.Parameters))
	for _, item := range spec.Parameters {
		raw, exists := parameters[item.Name]
		if !exists && item.Default != nil {
			raw, exists = item.Default, true
		}
		if !exists {
			if item.Required {
				return nil, fmt.Errorf("Polymarket dataset %qには入力項目 %q が必要です", spec.Dataset, item.Name)
			}
			continue
		}
		value, err := validateParameterValue(item, raw)
		if err != nil {
			return nil, fmt.Errorf("Polymarket dataset %qの入力項目 %q が不正です: %w", spec.Dataset, item.Name, err)
		}
		result[item.Name] = value
	}
	if err := validateDatasetRules(spec, parameters, result); err != nil {
		return nil, err
	}
	return result, nil
}

// ----------------------------------------

// validateParameterValue は、1項目の型、範囲、列挙、形式を検証します。
//
// 機能:
//   - JSON数値を文字列精度のjson.Numberへ揃える
//   - 配列をquery変換用[]stringへ揃え、重複と空配列を拒否する
//
// 引数:
//   - spec parameterSpec: 項目仕様
//   - raw any: 入力された値
//
// 返り値:
//   - any: 安定型へ正規化した値
//   - error: 型、範囲、列挙、形式が不正な場合のエラー
func validateParameterValue(spec parameterSpec, raw any) (any, error) {
	switch spec.Type {
	case typeString:
		value, ok := raw.(string)
		if !ok {
			return nil, errors.New("stringが必要です")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("空でないstringが必要です")
		}
		if spec.MaxLength > 0 && len([]rune(value)) > spec.MaxLength {
			return nil, fmt.Errorf("%d文字以内で指定してください", spec.MaxLength)
		}
		if !containsAllowed(spec.Allowed, value) {
			return nil, fmt.Errorf("許容値は%vです", spec.Allowed)
		}
		if err := validateStringKind(spec.Validator, value); err != nil {
			return nil, err
		}
		return value, nil
	case typeInteger, typeNumber:
		return validateNumberValue(spec, raw)
	case typeBoolean:
		value, ok := raw.(bool)
		if !ok {
			return nil, errors.New("booleanが必要です")
		}
		return value, nil
	case typeStringArray, typeIntegerArray:
		return validateArrayValue(spec, raw)
	default:
		return nil, fmt.Errorf("未対応の入力型です: %q", spec.Type)
	}
}

// ----------------------------------------

// validateNumberValue は、整数または数値をjson.Numberへ正規化します。
//
// 機能:
//   - Goの整数、有限float、json.Numberを受け付ける
//   - integerでは小数と指数表現を拒否し、仕様範囲を確認する
//
// 引数:
//   - spec parameterSpec: 数値項目仕様
//   - raw any: 入力数値
//
// 返り値:
//   - json.Number: queryへ精度を保って変換できる数値
//   - error: 型、有限性、整数性、範囲が不正な場合のエラー
func validateNumberValue(spec parameterSpec, raw any) (json.Number, error) {
	text, err := numberText(raw, spec.Type == typeInteger)
	if err != nil {
		return "", err
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return "", errors.New("有限の数値が必要です")
	}
	if spec.Minimum != nil && number < *spec.Minimum {
		return "", fmt.Errorf("%v以上で指定してください", *spec.Minimum)
	}
	if spec.Maximum != nil && number > *spec.Maximum {
		return "", fmt.Errorf("%v以下で指定してください", *spec.Maximum)
	}
	return json.Number(text), nil
}

// ----------------------------------------

// numberText は、対応するGo数値をquery用の10進文字列へ変換します。
//
// 機能:
//   - integer指定時は小数、指数形式、範囲外uint64を拒否する
//   - number指定時は有限floatだけを許可する
//
// 引数:
//   - raw any: Go数値またはjson.Number
//   - integer bool: 整数だけを許可する場合はtrue
//
// 返り値:
//   - string: 正規化した数値文字列
//   - error: 数値として扱えない場合のエラー
func numberText(raw any, integer bool) (string, error) {
	var text string
	switch value := raw.(type) {
	case json.Number:
		text = value.String()
	case int:
		text = strconv.FormatInt(int64(value), 10)
	case int8:
		text = strconv.FormatInt(int64(value), 10)
	case int16:
		text = strconv.FormatInt(int64(value), 10)
	case int32:
		text = strconv.FormatInt(int64(value), 10)
	case int64:
		text = strconv.FormatInt(value, 10)
	case uint:
		text = strconv.FormatUint(uint64(value), 10)
	case uint8:
		text = strconv.FormatUint(uint64(value), 10)
	case uint16:
		text = strconv.FormatUint(uint64(value), 10)
	case uint32:
		text = strconv.FormatUint(uint64(value), 10)
	case uint64:
		text = strconv.FormatUint(value, 10)
	case float32:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", errors.New("有限の数値が必要です")
		}
		if integer && math.Trunc(float64(value)) != float64(value) {
			return "", errors.New("integerが必要です")
		}
		text = strconv.FormatFloat(float64(value), 'f', -1, 32)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", errors.New("有限の数値が必要です")
		}
		if integer && math.Trunc(value) != value {
			return "", errors.New("integerが必要です")
		}
		text = strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return "", errors.New("numberが必要です")
	}
	if integer {
		if _, err := strconv.ParseInt(text, 10, 64); err != nil {
			return "", errors.New("10進integerが必要です")
		}
	} else if _, err := strconv.ParseFloat(text, 64); err != nil {
		return "", errors.New("numberが必要です")
	}
	return text, nil
}

// ----------------------------------------

// validateArrayValue は、JSON互換配列を重複のない[]stringへ変換します。
//
// 機能:
//   - string/integer要素をscalarと同じ規則で検証する
//   - 空配列、最大件数超過、重複要素を拒否する
//
// 引数:
//   - spec parameterSpec: 配列項目仕様
//   - raw any: JSON互換sliceまたはarray
//
// 返り値:
//   - []string: CSVまたは同名反復queryへ変換できる要素
//   - error: 配列型、要素、件数、重複が不正な場合のエラー
func validateArrayValue(spec parameterSpec, raw any) ([]string, error) {
	value := reflect.ValueOf(raw)
	if !value.IsValid() || (value.Kind() != reflect.Slice && value.Kind() != reflect.Array) {
		return nil, errors.New("JSON arrayが必要です")
	}
	if value.Len() == 0 {
		return nil, errors.New("空でないJSON arrayが必要です")
	}
	if spec.MaxItems > 0 && value.Len() > spec.MaxItems {
		return nil, fmt.Errorf("要素数は%d件以下にしてください", spec.MaxItems)
	}
	itemSpec := spec
	itemSpec.Default = nil
	itemSpec.Required = true
	itemSpec.MaxItems = 0
	if spec.Type == typeStringArray {
		itemSpec.Type = typeString
	} else {
		itemSpec.Type = typeInteger
	}
	result := make([]string, 0, value.Len())
	seen := make(map[string]struct{}, value.Len())
	for index := 0; index < value.Len(); index++ {
		item, err := validateParameterValue(itemSpec, value.Index(index).Interface())
		if err != nil {
			return nil, fmt.Errorf("要素%dが不正です: %w", index, err)
		}
		var text string
		switch normalized := item.(type) {
		case string:
			text = normalized
		case json.Number:
			text = normalized.String()
		default:
			return nil, errors.New("配列要素をqueryへ変換できません")
		}
		if _, exists := seen[text]; exists {
			return nil, fmt.Errorf("配列要素 %q が重複しています", text)
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result, nil
}

// ----------------------------------------

// containsAllowed は、列挙制約へ文字列が含まれるか確認します。
//
// 機能:
//   - 許容値未設定を制約なしとして扱う
//
// 引数:
//   - allowed []string: 許容値一覧
//   - value string: 検査する値
//
// 返り値:
//   - bool: 制約なしまたは一致する場合はtrue
func containsAllowed(allowed []string, value string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

// ----------------------------------------

// validateStringKind は、公開識別子の固定形式を確認します。
//
// 機能:
//   - slug、token ID、wallet、condition hashをclientと同じ形式で検証する
//
// 引数:
//   - kind validatorKind: 形式種別
//   - value string: 検査する文字列
//
// 返り値:
//   - error: 形式が不正な場合のエラー。有効または指定なしの場合はnil
func validateStringKind(kind validatorKind, value string) error {
	switch kind {
	case validatorNone:
		return nil
	case validatorSlug:
		if !slugPattern.MatchString(value) {
			return errors.New("slugは英数字、ハイフン、アンダースコアだけで指定してください")
		}
	case validatorToken:
		if len(value) > 100 || !tokenPattern.MatchString(value) {
			return errors.New("token IDは10進数または0x付き16進数で指定してください")
		}
	case validatorWallet:
		if !walletPattern.MatchString(value) {
			return errors.New("walletは0x付き40桁16進数で指定してください")
		}
	case validatorCondition:
		if !conditionPattern.MatchString(value) {
			return errors.New("condition IDは0x付き64桁16進数で指定してください")
		}
	default:
		return fmt.Errorf("未対応の文字列検証種別です: %q", kind)
	}
	return nil
}

// ----------------------------------------

// validateDatasetRules は、複数項目間のdataset固有規則を確認します。
//
// 機能:
//   - selectorの排他必須、配列filterの排他、時刻範囲、filter pairを検証する
//   - 絶対価格期間ではローカル既定intervalを削除する
//   - commentsの3 routeを排他的にし、routeで無効な明示入力を拒否する
//
// 引数:
//   - spec endpointSpec: dataset仕様
//   - original map[string]any: 既定値適用前の公開入力
//   - values map[string]any: 型検証と既定値適用後の値
//
// 返り値:
//   - error: 項目関係が不正な場合のエラー。有効な場合はnil
func validateDatasetRules(spec endpointSpec, original map[string]any, values map[string]any) error {
	switch spec.Dataset {
	case "event", "market", "tag", "related_tags":
		if hasValue(values, "id") == hasValue(values, "slug") {
			return fmt.Errorf("Polymarket dataset %qはidまたはslugを1つだけ指定してください", spec.Dataset)
		}
	case "user_positions", "user_activity", "trades", "closed_positions":
		if hasValue(values, "markets") && hasValue(values, "event_ids") {
			return fmt.Errorf("Polymarket dataset %qのmarketsとevent_idsは同時指定できません", spec.Dataset)
		}
	}
	if hasValue(values, "event_ids") {
		if err := requirePositiveArray(values["event_ids"], "event_ids"); err != nil {
			return err
		}
	}
	if hasValue(values, "tag_ids") {
		if err := requirePositiveArray(values["tag_ids"], "tag_ids"); err != nil {
			return err
		}
		if hasValue(values, "tag_id") {
			return errors.New("marketsのtag_idとtag_idsは同時指定できません")
		}
	}
	if hasValue(values, "category_ids") {
		if err := requirePositiveArray(values["category_ids"], "category_ids"); err != nil {
			return err
		}
	}
	if spec.Dataset == "price_history" {
		hasStart, hasEnd := hasValue(values, "start_timestamp"), hasValue(values, "end_timestamp")
		if hasStart != hasEnd {
			return errors.New("price_historyはstart_timestampとend_timestampを両方指定してください")
		}
		if hasStart {
			if _, explicit := original["interval"]; explicit {
				return errors.New("price_historyのintervalと絶対時刻範囲は同時指定できません")
			}
			delete(values, "interval")
			if !numberLess(values["start_timestamp"], values["end_timestamp"]) {
				return errors.New("start_timestampはend_timestampより前にしてください")
			}
		}
	}
	if spec.Dataset == "user_activity" || spec.Dataset == "trades" {
		if hasValue(values, "start_timestamp") && hasValue(values, "end_timestamp") && !numberLess(values["start_timestamp"], values["end_timestamp"]) {
			return errors.New("start_timestampはend_timestampより前にしてください")
		}
	}
	if spec.Dataset == "trades" && hasValue(values, "filter_type") != hasValue(values, "filter_amount") {
		return errors.New("tradesのfilter_typeとfilter_amountは両方指定してください")
	}
	if spec.Dataset == "comments" {
		if hasValue(values, "comment_id") && hasValue(values, "user_address") {
			return errors.New("commentsのcomment_idとuser_addressは同時指定できません")
		}
		if hasValue(values, "parent_entity_type") != hasValue(values, "parent_entity_id") {
			return errors.New("commentsのparent_entity_typeとparent_entity_idは両方指定してください")
		}
		if hasValue(values, "comment_id") {
			for name := range original {
				if name != "comment_id" && name != "get_positions" {
					return fmt.Errorf("commentsのcomment_id routeでは入力項目 %q を指定できません", name)
				}
			}
		}
		if hasValue(values, "user_address") {
			for _, name := range []string{"parent_entity_type", "parent_entity_id", "get_positions", "holders_only"} {
				if _, explicit := original[name]; explicit {
					return fmt.Errorf("commentsのuser_address routeでは入力項目 %q を指定できません", name)
				}
			}
		}
	}
	return nil
}

// ----------------------------------------

// requirePositiveArray は、整数配列の全要素が正整数か確認します。
//
// 機能:
//   - OpenAPIでpositive IDと定義された配列の0と負数を拒否する
//
// 引数:
//   - value any: validateArrayValue後の[]string
//   - name string: エラーへ掲載する公開項目名
//
// 返り値:
//   - error: 正整数以外を含む場合のエラー。有効な場合はnil
func requirePositiveArray(value any, name string) error {
	items, ok := value.([]string)
	if !ok {
		return fmt.Errorf("%sは整数配列で指定してください", name)
	}
	for _, item := range items {
		number, err := strconv.ParseInt(item, 10, 64)
		if err != nil || number < 1 {
			return fmt.Errorf("%sの各要素は正整数で指定してください", name)
		}
	}
	return nil
}

// ----------------------------------------

// hasValue は、正規化後の値が存在するか確認します。
//
// 機能:
//   - nil mapとnil値を未指定として扱う
//
// 引数:
//   - values map[string]any: 正規化済み入力
//   - name string: 確認する公開項目名
//
// 返り値:
//   - bool: 非nil値が存在する場合はtrue
func hasValue(values map[string]any, name string) bool {
	value, exists := values[name]
	return exists && value != nil
}

// ----------------------------------------

// numberLess は、正規化済み数値2つの大小を比較します。
//
// 機能:
//   - 仕様上安全な範囲のjson.Numberをfloat64へ変換して順序だけを比較する
//
// 引数:
//   - left any: 左側のjson.Number
//   - right any: 右側のjson.Number
//
// 返り値:
//   - bool: leftがrightより小さい場合はtrue
func numberLess(left, right any) bool {
	leftNumber, leftOK := left.(json.Number)
	rightNumber, rightOK := right.(json.Number)
	if !leftOK || !rightOK {
		return false
	}
	leftFloat, leftErr := strconv.ParseFloat(leftNumber.String(), 64)
	rightFloat, rightErr := strconv.ParseFloat(rightNumber.String(), 64)
	return leftErr == nil && rightErr == nil && leftFloat < rightFloat
}

// ----------------------------------------

// buildUpstreamQuery は、公開入力を公式query名とencodingへ変換します。
//
// 機能:
//   - DataのarrayをCSV、Gammaのarrayを同名query反復として符号化する
//   - search、activity、動的routeの制御項目を公式形式へ変換する
//   - commentsのrouteごとに関係ないローカル既定値を上流へ送らない
//
// 引数:
//   - spec endpointSpec: dataset仕様
//   - values map[string]any: 型検証と既定値適用後の値
//   - original map[string]any: 明示入力を判定するための元入力
//
// 返り値:
//   - url.Values: Clientへ渡すqueryと内部selector
//   - error: 正規化済み値をqueryへ変換できない場合のエラー
func buildUpstreamQuery(spec endpointSpec, values map[string]any, original map[string]any) (url.Values, error) {
	result := make(url.Values)
	commentMode := "list"
	if spec.Dataset == "comments" {
		if hasValue(values, "comment_id") {
			commentMode = "item"
		} else if hasValue(values, "user_address") {
			commentMode = "user"
		}
	}
	for _, parameter := range spec.Parameters {
		value, exists := values[parameter.Name]
		if !exists {
			continue
		}
		if spec.Dataset == "search" && parameter.Name == "include_closed_markets" {
			continue
		}
		if spec.Dataset == "user_activity" && parameter.Name == "include_deposits_and_withdrawals" {
			continue
		}
		if spec.Dataset == "comments" && !commentParameterAllowed(commentMode, parameter.Name) {
			continue
		}
		name := parameter.UpstreamName
		if name == "" {
			name = parameter.Name
		}
		if items, ok := value.([]string); ok {
			if parameter.Encoding == encodingRepeat {
				for _, item := range items {
					result.Add(name, item)
				}
			} else {
				result.Set(name, strings.Join(items, ","))
			}
			continue
		}
		text, err := queryScalar(value)
		if err != nil {
			return nil, fmt.Errorf("入力項目 %q をqueryへ変換できません: %w", parameter.Name, err)
		}
		result.Set(name, text)
	}
	if spec.Dataset == "search" {
		includeClosed, _ := values["include_closed_markets"].(bool)
		if includeClosed {
			result.Set("keep_closed_markets", "1")
			result.Del("events_status")
		} else {
			result.Set("events_status", "active")
			result.Set("keep_closed_markets", "0")
		}
		result.Set("search_profiles", "false")
	}
	if spec.Dataset == "user_activity" {
		include, _ := values["include_deposits_and_withdrawals"].(bool)
		result.Set("excludeDepositsWithdrawals", strconv.FormatBool(!include))
	}
	_ = original
	return result, nil
}

// ----------------------------------------

// commentParameterAllowed は、comments routeで送信可能な公開項目か確認します。
//
// 機能:
//   - item、user、listの公式query集合をcollector境界で固定する
//
// 引数:
//   - mode string: list、item、userのroute種別
//   - name string: 公開入力項目名
//
// 返り値:
//   - bool: routeで送信可能な場合はtrue
func commentParameterAllowed(mode, name string) bool {
	switch mode {
	case "item":
		return name == "comment_id" || name == "get_positions"
	case "user":
		return name == "user_address" || name == "limit" || name == "offset" || name == "order" || name == "ascending"
	default:
		return name != "comment_id" && name != "user_address"
	}
}

// ----------------------------------------

// queryScalar は、正規化済みscalarをquery文字列へ変換します。
//
// 機能:
//   - string、json.Number、boolだけを明示的に受け付ける
//
// 引数:
//   - value any: validateParameterValue後のscalar
//
// 返り値:
//   - string: URL queryへ設定する値
//   - error: 未対応型の場合のエラー
func queryScalar(value any) (string, error) {
	switch item := value.(type) {
	case string:
		return item, nil
	case json.Number:
		return item.String(), nil
	case bool:
		return strconv.FormatBool(item), nil
	default:
		return "", fmt.Errorf("未対応型です: %T", value)
	}
}

// ----------------------------------------

// buildMetadata は、収集結果へ公開APIとページングの付帯情報を追加します。
//
// 機能:
//   - service、実endpoint、公開source URL、仕様確認日、規約URLを常に返す
//   - paged datasetでは総ページ既知状態を常に明示し、未提供値を推測しない
//   - Data offset応答からhas_moreやnext_offsetを推測しない
//
// 引数:
//   - spec endpointSpec: dataset仕様
//   - response APIResponse: clientが返した応答と取得情報
//   - values map[string]any: 正規化済みrequest値
//   - data any: 正規化後JSON値
//
// 返り値:
//   - map[string]any: ProviderResultへ付加するmetadata
func buildMetadata(spec endpointSpec, response APIResponse, values map[string]any, data any) map[string]any {
	endpoint := response.Endpoint
	if endpoint == "" {
		endpoint = spec.Path
	}
	metadata := map[string]any{
		"api_service": string(spec.Service), "endpoint": endpoint, "source_url": response.SourceURL,
		"public": true, "authentication_required": false, "read_only": true,
		"specification_reviewed_date": SpecificationReviewDate,
		"specification_url":           SpecificationURL, "terms_url": TermsURL,
		"http_status": response.StatusCode, "fetched_at": response.FetchedAt,
		"response_bytes": response.ResponseBytes,
	}
	mode := spec.Pagination
	if spec.Dataset == "comments" && hasValue(values, "comment_id") {
		mode = paginationNone
	}
	if mode != paginationNone {
		metadata["pagination"] = buildPaginationMetadata(mode, spec.Dataset, values, data)
	}
	return metadata
}

// ----------------------------------------

// buildPaginationMetadata は、上流が根拠を返した値だけをページ情報へ追加します。
//
// 機能:
//   - page、keyset、offsetのrequest位置を明示する
//   - keysetのnext_cursorとsearchのpagination値だけから継続状態を確定する
//   - total_pagesは上流提供時だけ返し、常にtotal_pages_knownを返す
//
// 引数:
//   - mode paginationMode: page、keyset、offset方式
//   - dataset string: 入力cursor名を決めるdataset識別子
//   - values map[string]any: 正規化済みrequest値
//   - data any: 上流または正規化済み応答
//
// 返り値:
//   - map[string]any: 推測を含まないページングmetadata
func buildPaginationMetadata(mode paginationMode, dataset string, values map[string]any, data any) map[string]any {
	result := map[string]any{"mode": string(mode), "total_pages_known": false}
	switch mode {
	case paginationPage:
		if value, exists := values["page"]; exists {
			result["request_page"] = value
		}
		result["has_more_known"] = false
		if response, ok := data.(map[string]any); ok {
			if pagination, ok := response["pagination"].(map[string]any); ok {
				copyPaginationValue(result, pagination, "total_results", "totalResults", "total_results")
				if copyPaginationValue(result, pagination, "total_pages", "totalPages", "total_pages") {
					result["total_pages_known"] = true
				}
				if copyPaginationValue(result, pagination, "has_more", "hasMore", "has_more") {
					result["has_more_known"] = true
				}
				copyPaginationValue(result, pagination, "next_page", "nextPage", "next_page")
			}
		}
	case paginationKeyset:
		inputName := "after_cursor"
		if dataset == "clob_markets" {
			inputName = "next_cursor"
		}
		if value, exists := values[inputName]; exists {
			result["request_cursor"] = value
		} else {
			result["request_cursor"] = ""
		}
		nextCursor, cursorKnown := objectString(data, "next_cursor", "nextCursor")
		if !cursorKnown && dataset != "clob_markets" {
			cursorKnown = true
		}
		if cursorKnown {
			if nextCursor != "" {
				result["next_cursor"] = nextCursor
			}
			result["has_more"] = nextCursor != "" && nextCursor != "LTE="
		}
		result["has_more_known"] = cursorKnown
	case paginationOffset:
		if value, exists := values["offset"]; exists {
			result["request_offset"] = value
		}
		if value, exists := values["limit"]; exists {
			result["request_limit"] = value
		}
		result["has_more_known"] = false
	}
	return result
}

// ----------------------------------------

// copyPaginationValue は、候補名で見つかった上流値をmetadataへ複製します。
//
// 機能:
//   - camelCaseとsnake_caseの公式応答差分を吸収する
//
// 引数:
//   - destination map[string]any: コピー先metadata
//   - source map[string]any: 上流pagination object
//   - destinationName string: 公開metadata名
//   - candidates ...string: 上流候補名
//
// 返り値:
//   - bool: 値を1件コピーした場合はtrue
func copyPaginationValue(destination, source map[string]any, destinationName string, candidates ...string) bool {
	for _, name := range candidates {
		if value, exists := source[name]; exists && value != nil {
			destination[destinationName] = value
			return true
		}
	}
	return false
}

// ----------------------------------------

// objectString は、JSON objectの候補fieldから文字列を取得します。
//
// 機能:
//   - keyset応答のsnake_caseとcamelCaseを同じ処理へ統一する
//
// 引数:
//   - value any: JSON object候補
//   - names ...string: 確認するfield名
//
// 返り値:
//   - string: 最初に見つかった文字列。存在しない場合は空文字
//   - bool: 候補fieldに文字列値が実際に存在した場合はtrue
func objectString(value any, names ...string) (string, bool) {
	record, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	for _, name := range names {
		if text, ok := record[name].(string); ok {
			return text, true
		}
	}
	return "", false
}

// ----------------------------------------

// classifyCollectError は、clientとcontextエラーを共通ServiceErrorへ分類します。
//
// 機能:
//   - 400/422、404、401/403、408/425/429/504、5xxを指定された分類へ対応付ける
//   - Retry-AfterをAPIError内に保持したままcauseへ接続し、自動再試行しない
//
// 引数:
//   - err error: pacingまたはAPIClientが返したエラー
//
// 返り値:
//   - error: transport共通分類を持つ*domain.ServiceError
func classifyCollectError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewServiceError(domain.ErrorTimeout, "Polymarket API要求が時間切れになりました", err)
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewServiceError(domain.ErrorProviderUnavailable, "Polymarket API要求がキャンセルされました", err)
	}
	var apiError *APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return domain.NewServiceError(domain.ErrorInvalidArgument, apiError.Message, apiError)
		case http.StatusNotFound:
			return domain.NewServiceError(domain.ErrorNotFound, apiError.Message, apiError)
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooEarly, http.StatusTooManyRequests:
			return domain.NewServiceError(domain.ErrorProviderUnavailable, apiError.Message, apiError)
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return domain.NewServiceError(domain.ErrorTimeout, apiError.Message, apiError)
		default:
			return domain.NewServiceError(domain.ErrorUpstream, apiError.Message, apiError)
		}
	}
	return domain.NewServiceError(domain.ErrorUpstream, "Polymarket APIからデータを取得できません", err)
}

// ----------------------------------------

// newPacingState は、遅延起動する単一FIFOのrate状態を生成します。
//
// 機能:
//   - 公式10秒quotaの50パーセント以下となるclass別開始間隔を設定する
//   - goroutineを生成せず、最初のExecuteまで通信とworkerを発生させない
//
// 引数:
//   - なし
//
// 返り値:
//   - *pacingState: 空queueとclass別開始時刻を持つ状態
func newPacingState() *pacingState {
	window := 10 * time.Second
	return &pacingState{
		nextStart: make(map[rateClass]time.Time),
		intervals: map[rateClass]time.Duration{
			rateGammaGeneral: parameterDuration(4000, window), rateGammaSearch: parameterDuration(350, window),
			rateGammaEvents: parameterDuration(500, window), rateGammaMarkets: parameterDuration(300, window),
			rateGammaTags: parameterDuration(200, window), rateGammaComments: parameterDuration(200, window),
			rateDataGeneral: parameterDuration(1000, window), rateDataTrades: parameterDuration(200, window),
			rateDataPositions: parameterDuration(150, window), rateDataClosed: parameterDuration(150, window),
			rateCLOBGeneral: parameterDuration(9000, window), rateCLOBQuote: parameterDuration(1500, window),
			rateCLOBHistory: parameterDuration(1000, window), rateCLOBTick: parameterDuration(200, window),
		},
		queueChanged: make(chan struct{}), now: time.Now, waitInterval: waitForPacerInterval,
	}
}

// ----------------------------------------

// requestRateClasses は、1要求へ適用するservice generalとendpoint classを返します。
//
// 機能:
//   - endpoint classがgeneralと同じ場合は重複を除く
//   - すべての要求をservice全体quotaと個別quotaの両方へ数える
//
// 引数:
//   - spec endpointSpec: 要求するdataset仕様
//
// 返り値:
//   - []rateClass: 同時に満たす必要があるrate class一覧
func requestRateClasses(spec endpointSpec) []rateClass {
	var general rateClass
	switch spec.Service {
	case serviceGamma:
		general = rateGammaGeneral
	case serviceData:
		general = rateDataGeneral
	case serviceCLOB:
		general = rateCLOBGeneral
	}
	if spec.RateClass == "" || spec.RateClass == general {
		return []rateClass{general}
	}
	return []rateClass{general, spec.RateClass}
}

// ----------------------------------------

// Execute は、要求を単一FIFOへ追加して実行結果を待ちます。
//
// 機能:
//   - 最初のenqueue時だけworkerを開始し、queueが空になるとworkerを終了する
//   - queued中のcontext終了では要求を削除し、HTTP関数を呼ばず後続を進める
//   - 1件の通信開始から完了まで次の通信を開始しない
//
// 引数:
//   - ctx context.Context: queue待機と通信の期限、キャンセル
//   - classes []rateClass: 同時に適用するquota class
//   - execute func(context.Context) (APIResponse, error): 1回だけ呼ぶHTTP関数
//
// 返り値:
//   - APIResponse: HTTP関数の成功応答
//   - error: contextまたはHTTP関数のエラー
func (p *pacingState) Execute(ctx context.Context, classes []rateClass, execute func(context.Context) (APIResponse, error)) (APIResponse, error) {
	if err := ctx.Err(); err != nil {
		return APIResponse{}, err
	}
	request := &queuedRequest{ctx: ctx, classes: append([]rateClass(nil), classes...), execute: execute, result: make(chan queuedResponse, 1)}
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return APIResponse{}, err
	}
	p.queue = append(p.queue, request)
	p.signalQueueLocked()
	if !p.running {
		p.running = true
		go p.runQueue()
	}
	p.mu.Unlock()
	select {
	case result := <-request.result:
		return result.response, result.err
	case <-ctx.Done():
		p.cancelQueued(request)
		return APIResponse{}, ctx.Err()
	}
}

// ----------------------------------------

// runQueue は、FIFO先頭要求をrate開始可能時刻に1件ずつ実行します。
//
// 機能:
//   - queue先頭を追い越さず、開始時刻と通信完了の両方を直列化する
//   - 待機中のqueue変更とcontext終了へ応答する
//   - queueが空になった時点でrunningをfalseへ戻しgoroutineを終了する
//
// 引数:
//   - なし
//
// 返り値:
//   - なし
func (p *pacingState) runQueue() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.running = false
			p.mu.Unlock()
			return
		}
		request := p.queue[0]
		delay := p.startDelayLocked(request.classes)
		changed := p.queueChanged
		p.mu.Unlock()

		if delay > 0 && !p.waitInterval(delay, changed) {
			continue
		}

		p.mu.Lock()
		if len(p.queue) == 0 || p.queue[0] != request {
			p.mu.Unlock()
			continue
		}
		if p.startDelayLocked(request.classes) > 0 {
			p.mu.Unlock()
			continue
		}
		if err := request.ctx.Err(); err != nil {
			p.queue = p.queue[1:]
			p.signalQueueLocked()
			p.mu.Unlock()
			continue
		}
		p.queue = p.queue[1:]
		request.started = true
		startedAt := p.now()
		for _, class := range request.classes {
			p.nextStart[class] = startedAt.Add(p.intervals[class])
		}
		p.signalQueueLocked()
		p.mu.Unlock()

		if err := request.ctx.Err(); err != nil {
			request.result <- queuedResponse{err: err}
			continue
		}
		response, err := request.execute(request.ctx)
		request.result <- queuedResponse{response: response, err: err}
	}
}

// ----------------------------------------

// startDelayLocked は、全classが開始可能になるまでの最大待機時間を返します。
//
// 機能:
//   - service generalとendpoint個別quotaの遅い方を採用する
//
// 引数:
//   - classes []rateClass: 適用するquota class
//
// 返り値:
//   - time.Duration: 現在時刻からの非負待機時間
func (p *pacingState) startDelayLocked(classes []rateClass) time.Duration {
	now := p.now()
	var latest time.Time
	for _, class := range classes {
		if p.nextStart[class].After(latest) {
			latest = p.nextStart[class]
		}
	}
	if !latest.After(now) {
		return 0
	}
	return latest.Sub(now)
}

// ----------------------------------------

// cancelQueued は、未開始要求をFIFOから削除してworkerを起こします。
//
// 機能:
//   - 実行開始済み要求は削除せず、HTTP contextによるキャンセルへ委ねる
//
// 引数:
//   - request *queuedRequest: contextが終了した要求
//
// 返り値:
//   - なし
func (p *pacingState) cancelQueued(request *queuedRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if request.started {
		return
	}
	for index, candidate := range p.queue {
		if candidate == request {
			p.queue = append(p.queue[:index], p.queue[index+1:]...)
			p.signalQueueLocked()
			return
		}
	}
}

// ----------------------------------------

// signalQueueLocked は、queue待機者へ状態変更を通知します。
//
// 機能:
//   - 現channelをcloseし、次の通知用channelへ置き換える
//   - pacingState.mu保持中だけ呼び出す
//
// 引数:
//   - なし
//
// 返り値:
//   - なし
func (p *pacingState) signalQueueLocked() {
	close(p.queueChanged)
	p.queueChanged = make(chan struct{})
}

// ----------------------------------------

// waitForPacerInterval は、開始可能時刻またはqueue変更まで待機します。
//
// 機能:
//   - timer経過時はtrue、queued cancel等のqueue変更時はfalseを返す
//   - queue変更時はtimerを停止し、必要な場合だけchannelをdrainする
//
// 引数:
//   - interval time.Duration: 次の開始可能時刻までの待機時間
//   - queueChanged <-chan struct{}: FIFO構成変更通知
//
// 返り値:
//   - bool: intervalが経過した場合はtrue、queue変更時はfalse
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

// isNilAPIClient は、interface内のtyped nilを含めてclient欠落を確認します。
//
// 機能:
//   - nil可能なreflect kindだけIsNilを呼びpanicを避ける
//
// 引数:
//   - client APIClient: 検査するclient interface
//
// 返り値:
//   - bool: nilまたはtyped nilの場合はtrue
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
