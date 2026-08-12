package kabuscontroller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

// Collector は、KabusControllerのdataset入力を検証して固定GETを実行します。
type Collector struct {
	client    APIClient
	endpoints map[string]endpointSpec
}

// ----------------------------------------

/*
NewCollector は、KabusController API clientからcollectorを生成します。

機能:
  - 型付きnilを含むAPIClientのnilを拒否する
  - 6件の固定dataset重複を起動時に検証する

引数:
  - client APIClient: 1回の読取専用GETを行うclient

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
Descriptor は、KabusControllerの6件の読取専用datasetを返します。

機能:
  - 固定endpoint定義の順序を維持する
  - 個別銘柄datasetだけに必須symbol入力を掲載する

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
		Description: "KabusControllerに登録された先物・オプションの一覧と板情報を、固定GETで読み取り専用収集します。",
		Datasets:    datasets,
	}
}

// ----------------------------------------

/*
Collect は、指定datasetの入力を検証してKabusControllerから1回だけ取得します。

機能:
  - 未知dataset、未知入力、symbolの型と形式を通信前に検証する
  - 上流HTTP状態とcontext終了を共通エラー分類へ変換する
  - 上流JSON全体を数値精度を保ったままmetadata付きで返す

引数:
  - ctx context.Context: 上流GETの期限とキャンセルを伝える値
  - dataset string: Descriptorに掲載されたdataset識別子
  - parameters map[string]any: dataset固有の公開入力

返り値:
  - domain.ProviderResult: 上流JSON全体と取得metadata
  - error: dataset、入力、通信、上流状態、JSONの共通分類エラー
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
	symbol, err := validateParameters(spec, parameters)
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(domain.ErrorInvalidArgument, err.Error(), err)
	}
	response, err := c.client.Fetch(ctx, dataset, symbol)
	if err != nil {
		return domain.ProviderResult{}, classifyCollectError(spec, err)
	}
	return domain.ProviderResult{
		Data: response.Body,
		Metadata: map[string]any{
			"source_name":      "KabusController",
			"source_url":       response.SourceURL,
			"endpoint":         spec.Path,
			"upstream_status":  response.StatusCode,
			"upstream_fetched": response.FetchedAt,
			"response_bytes":   response.ResponseBytes,
			"read_only":        true,
			"on_demand":        true,
		},
	}, nil
}

// ----------------------------------------

/*
validateParameters は、dataset固有の入力項目を検証します。

機能:
  - symbolを持たない5 datasetでは全入力を拒否する
  - symbol_market_dataではsymbolだけを必須stringとして許可する

引数:
  - spec endpointSpec: 収集対象の固定endpoint仕様
  - parameters map[string]any: RESTまたはMCPから受けた入力項目

返り値:
  - string: 個別銘柄datasetの検証済みsymbol。その他は空文字
  - error: 未知項目、必須不足、型、symbol形式のエラー
*/
func validateParameters(spec endpointSpec, parameters map[string]any) (string, error) {
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !spec.RequiresSymbol || key != "symbol" {
			return "", fmt.Errorf("KabusController dataset %qに未知の入力項目があります: %q", spec.Dataset, key)
		}
	}
	if !spec.RequiresSymbol {
		return "", nil
	}
	raw, exists := parameters["symbol"]
	if !exists {
		return "", fmt.Errorf("KabusController dataset %qには入力項目 %q が必要です", spec.Dataset, "symbol")
	}
	symbol, ok := raw.(string)
	if !ok {
		return "", errors.New("KabusControllerの入力項目 symbol はstringで指定してください")
	}
	if err := validateSymbol(symbol); err != nil {
		return "", err
	}
	return symbol, nil
}

// ----------------------------------------

/*
classifyCollectError は、clientとcontextエラーを共通ServiceErrorへ分類します。

機能:
  - 404を個別銘柄のNOT_FOUND、408と504をTIMEOUTへ分類する
  - 401、403、425、429、503をPROVIDER_UNAVAILABLEへ分類する
  - その他の上流HTTP・通信・JSONエラーをUPSTREAM_ERRORへ分類する

引数:
  - spec endpointSpec: 失敗した固定endpoint仕様
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
			if spec.RequiresSymbol {
				return domain.NewServiceError(domain.ErrorNotFound, "指定銘柄はKabusControllerに登録されていません", apiError)
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
