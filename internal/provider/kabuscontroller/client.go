package kabuscontroller

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	symbolPattern       = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	pathSelectorPattern = regexp.MustCompile(`^[A-Za-z0-9._@-]+$`)
)

// APIClient は、固定したKabusController endpointからJSONを取得する契約です。
type APIClient interface {
	Fetch(context.Context, string, map[string]string) (APIResponse, error)
}

// ClientConfig は、KabusController API clientの接続条件を保持します。
type ClientConfig struct {
	BaseURL          string
	HTTPClient       *http.Client
	UserAgent        string
	MaxResponseBytes int64
}

// APIResponse は、正常なKabusController API応答と取得時の付帯情報を保持します。
type APIResponse struct {
	Body          any
	SourceURL     string
	StatusCode    int
	FetchedAt     time.Time
	ResponseBytes int64
}

// APIError は、KabusController APIが返した非成功HTTP応答を表します。
type APIError struct {
	StatusCode int
	RetryAfter string
	Endpoint   string
}

// Client は、固定許可リストに従ってKabusController APIへHTTP GETを送信します。
type Client struct {
	baseURL          url.URL
	httpClient       *http.Client
	userAgent        string
	maxResponseBytes int64
	endpoints        map[string]endpointSpec
}

// ----------------------------------------

/*
Error は、上流応答本文を含まないKabusController APIエラー文字列を返します。

機能:
  - HTTP状態と固定endpointだけを公開可能な形式へ整形する

引数:
  - なし

返り値:
  - string: 利用者へ公開可能なKabusController APIエラー文字列
*/
func (e *APIError) Error() string {
	if e == nil {
		return "KabusController APIエラー"
	}
	return fmt.Sprintf("KabusController API %s がHTTP %dを返しました", e.Endpoint, e.StatusCode)
}

// ----------------------------------------

/*
NewClient は、KabusController API clientを生成します。

機能:
  - 接続オリジン、User-Agent、本文上限を検証する
  - datasetと固定pathの重複や形式を起動時に検証する
  - 呼び出し元のHTTP clientを複製して共有設定の書き換えを防ぐ
  - HTTPリダイレクトを追跡せず固定GETの接続境界を維持する

引数:
  - config ClientConfig: 接続先、HTTP client、識別値、JSON本文上限

返り値:
  - *Client: 並行利用可能なKabusController API client
  - error: 設定または固定endpoint仕様が不正な場合のエラー
*/
func NewClient(config ClientConfig) (*Client, error) {
	baseURL, err := normalizeBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("KabusController APIのbase URLが不正です: %w", err)
	}

	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	if strings.TrimSpace(userAgent) == "" || !validHTTPHeaderValue(userAgent) {
		return nil, errors.New("KabusController APIのUser-Agentが不正です")
	}

	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = DefaultMaxResponseBytes
	}
	if maxResponseBytes < 1 {
		return nil, errors.New("KabusController APIの応答本文上限は1バイト以上である必要があります")
	}

	endpoints, err := buildClientEndpoints()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{}
	if config.HTTPClient != nil {
		cloned := *config.HTTPClient
		httpClient = &cloned
	}
	httpClient.Jar = nil
	httpClient.CheckRedirect = rejectRedirect

	return &Client{
		baseURL:          baseURL,
		httpClient:       httpClient,
		userAgent:        userAgent,
		maxResponseBytes: maxResponseBytes,
		endpoints:        endpoints,
	}, nil
}

// ----------------------------------------

/*
rejectRedirect は、KabusController APIからのHTTPリダイレクト追跡を停止します。

機能:
  - 最初の3xx応答をClient.Fetchへ返し、固定endpoint以外への後続GETを防ぐ

引数:
  - request *http.Request: 追跡しようとしているリダイレクト先要求
  - via []*http.Request: 直前までに送信した要求履歴

返り値:
  - error: 応答本文を閉じず最初の3xx応答を利用させるhttp.ErrUseLastResponse
*/
func rejectRedirect(request *http.Request, via []*http.Request) error {
	_ = request
	_ = via
	return http.ErrUseLastResponse
}

// ----------------------------------------

/*
Fetch は、datasetに対応する固定endpointからJSONを1件取得します。

機能:
  - datasetを固定許可リストへ再照合する
  - endpoint種別に応じて固定パス、検証済みpath selector、固定query名から要求URLを生成する
  - GET要求へUser-AgentとJSONのAcceptを設定する
  - JSON MIME、本文上限、UTF-8、余分なJSON値を検証し、数値をjson.Numberで保持する

引数:
  - ctx context.Context: HTTP要求の期限とキャンセルを伝えるコンテキスト
  - dataset string: endpointSpecsに存在するdataset識別子
  - parameters map[string]string: collectorが検証・正規化したdataset固有入力

返り値:
  - APIResponse: JSON本文、HTTP状態、取得元URL、取得時刻、本文サイズ
  - error: dataset、parameters、要求作成、通信、HTTP状態、本文、MIME、JSONのエラー
*/
func (c *Client) Fetch(
	ctx context.Context,
	dataset string,
	parameters map[string]string,
) (APIResponse, error) {
	spec, exists := c.endpoints[dataset]
	if !exists {
		return APIResponse{}, fmt.Errorf("未対応のKabusController datasetです: %q", dataset)
	}
	if spec.Kind == kindComposite {
		return APIResponse{}, fmt.Errorf("KabusController dataset %qは複合datasetのため直接Fetchできません", dataset)
	}
	requestURL, err := c.buildRequestURL(spec, parameters)
	if err != nil {
		return APIResponse{}, err
	}
	sourceURL := requestURL.String()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return APIResponse{}, fmt.Errorf("KabusController API要求を作成できません: %w", err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "gzip")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return APIResponse{}, fmt.Errorf("KabusController APIへ接続できません: %w", err)
	}
	defer response.Body.Close()
	sourceURL = responseSourceURL(response, sourceURL)

	fetchedAt := time.Now().UTC()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return APIResponse{}, &APIError{
			StatusCode: response.StatusCode,
			RetryAfter: response.Header.Get("Retry-After"),
			Endpoint:   spec.Path,
		}
	}
	if err := validateJSONContentType(response.Header.Get("Content-Type")); err != nil {
		return APIResponse{}, err
	}
	body, err := readResponseBody(response, c.maxResponseBytes)
	if err != nil {
		return APIResponse{}, err
	}
	decoded, err := decodeJSON(body)
	if err != nil {
		return APIResponse{}, err
	}

	return APIResponse{
		Body:          decoded,
		SourceURL:     sourceURL,
		StatusCode:    response.StatusCode,
		FetchedAt:     fetchedAt,
		ResponseBytes: int64(len(body)),
	}, nil
}

// ----------------------------------------

/*
buildRequestURL は、検証済み入力から固定許可済みURLを生成します。

機能:
  - datasetのroute種別ごとにpath selectorとqueryを組み立てる
  - endpoint仕様にない入力、欠落、path注入、query注入をHTTP通信前に拒否する
  - resolverのkindに応じて固定pathと上流query名を切り替える

引数:
  - spec endpointSpec: 固定endpoint仕様
  - parameters map[string]string: collectorが検証・正規化した入力

返り値:
  - url.URL: 安全に組み立てたGET先URL
  - error: 入力とrouteの契約に違反した場合のエラー
*/
func (c *Client) buildRequestURL(spec endpointSpec, parameters map[string]string) (url.URL, error) {
	if parameters == nil {
		parameters = map[string]string{}
	}
	allowed := make(map[string]parameterSpec, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		allowed[parameter.Name] = parameter
	}
	for name := range parameters {
		if _, exists := allowed[name]; !exists {
			return url.URL{}, fmt.Errorf("KabusController dataset %qに未知の入力項目があります: %q", spec.Dataset, name)
		}
	}
	for _, parameter := range spec.Parameters {
		if parameter.Required && parameters[parameter.Name] == "" {
			return url.URL{}, fmt.Errorf("KabusController dataset %qには入力項目 %q が必要です", spec.Dataset, parameter.Name)
		}
	}
	if err := validateNormalizedParameterValues(spec, parameters); err != nil {
		return url.URL{}, err
	}

	requestURL := c.baseURL
	requestURL.Path = spec.Path
	requestURL.RawPath = ""
	requestURL.RawQuery = ""
	requestURL.Fragment = ""
	values := make(url.Values)

	switch spec.Kind {
	case kindFixed:
	case kindControllerSymbol:
		if err := validateSymbol(parameters["symbol"]); err != nil {
			return url.URL{}, err
		}
		if err := setPathSelector(&requestURL, spec.Path, ":symbol", parameters["symbol"]); err != nil {
			return url.URL{}, err
		}
	case kindSymbolExchange:
		if err := validatePathAtom(parameters["symbol"]); err != nil {
			return url.URL{}, err
		}
		if err := validatePathAtom(parameters["exchange"]); err != nil {
			return url.URL{}, err
		}
		selector := parameters["symbol"] + "@" + parameters["exchange"]
		if err := setPathSelector(&requestURL, spec.Path, ":symbol", selector); err != nil {
			return url.URL{}, err
		}
	case kindPlainSymbol:
		if err := validatePathAtom(parameters["symbol"]); err != nil {
			return url.URL{}, err
		}
		if err := setPathSelector(&requestURL, spec.Path, ":symbol", parameters["symbol"]); err != nil {
			return url.URL{}, err
		}
	case kindPair:
		if err := validatePathAtom(parameters["pair"]); err != nil {
			return url.URL{}, err
		}
		if err := setPathSelector(&requestURL, spec.Path, ":pair", parameters["pair"]); err != nil {
			return url.URL{}, err
		}
	case kindResolver:
		kind := parameters["kind"]
		switch kind {
		case "future":
			if err := requireResolverParameters(parameters, []string{"product_code", "deriv_month"}, []string{"put_or_call", "strike_price", "deriv_weekly"}); err != nil {
				return url.URL{}, err
			}
			if !isFutureProductCode(parameters["product_code"]) {
				return url.URL{}, fmt.Errorf("KabusController resolverの先物商品コードが不正です: %q", parameters["product_code"])
			}
			requestURL.Path = "/kabusapi/symbolname/future"
			values.Set("FutureCode", parameters["product_code"])
		case "option":
			if err := requireResolverParameters(parameters, []string{"product_code", "deriv_month", "put_or_call", "strike_price"}, []string{"deriv_weekly"}); err != nil {
				return url.URL{}, err
			}
			if !isOptionProductCode(parameters["product_code"]) {
				return url.URL{}, fmt.Errorf("KabusController resolverのオプション商品コードが不正です: %q", parameters["product_code"])
			}
			requestURL.Path = "/kabusapi/symbolname/option"
			values.Set("OptionCode", parameters["product_code"])
		case "mini_option_weekly":
			if err := requireResolverParameters(parameters, []string{"deriv_month", "deriv_weekly", "put_or_call", "strike_price"}, []string{"product_code"}); err != nil {
				return url.URL{}, err
			}
			requestURL.Path = "/kabusapi/symbolname/minioptionweekly"
		default:
			return url.URL{}, fmt.Errorf("KabusController resolverのkindが不正です: %q", kind)
		}
	default:
		return url.URL{}, fmt.Errorf("KabusController dataset %qのroute種別が不正です", spec.Dataset)
	}

	if spec.Kind != kindResolver {
		for _, parameter := range spec.Parameters {
			if parameter.QueryName != "" {
				if value, exists := parameters[parameter.Name]; exists && value != "" {
					values.Set(parameter.QueryName, value)
				}
			}
		}
	} else {
		for _, name := range []string{"deriv_month", "put_or_call", "strike_price", "deriv_weekly"} {
			value, exists := parameters[name]
			if !exists || value == "" {
				continue
			}
			parameter, exists := allowed[name]
			if !exists || parameter.QueryName == "" {
				return url.URL{}, fmt.Errorf("KabusController resolverに未定義のquery入力があります: %q", name)
			}
			values.Set(parameter.QueryName, value)
		}
	}
	requestURL.RawQuery = values.Encode()
	return requestURL, nil
}

// ----------------------------------------

/*
isFutureProductCode は、resolverが受け付ける先物商品コードか確認します。

機能:
  - オプション商品コードが先物routeへ混入することを防ぐ

引数:
  - value string: 検証する商品コード

返り値:
  - bool: 対応する先物商品コードの場合はtrue
*/
func isFutureProductCode(value string) bool {
	switch value {
	case "NK225", "NK225mini", "TOPIX", "TOPIXmini", "GROWTH", "JPX400",
		"DOW", "VI", "Core30", "REIT", "NK225micro":
		return true
	default:
		return false
	}
}

// ----------------------------------------

/*
isOptionProductCode は、resolverが受け付けるオプション商品コードか確認します。

機能:
  - 先物商品コードがオプションrouteへ混入することを防ぐ

引数:
  - value string: 検証する商品コード

返り値:
  - bool: 対応するオプション商品コードの場合はtrue
*/
func isOptionProductCode(value string) bool {
	return value == "NK225op" || value == "NK225miniop"
}

// ----------------------------------------

/*
validateNormalizedParameterValues は、Clientに渡された正規化済み文字列を再検証します。

機能:
  - 入力値が上流queryとpathに安全に使える単一行文字列か確認する
  - endpoint仕様のAllowed制約をClient境界でも再確認する

引数:
  - spec endpointSpec: 固定endpoint仕様
  - parameters map[string]string: collectorが正規化した文字列入力

返り値:
  - error: 制御文字、前後空白、許容外の列挙値がある場合のエラー
*/
func validateNormalizedParameterValues(spec endpointSpec, parameters map[string]string) error {
	parameterMap := make(map[string]parameterSpec, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		parameterMap[parameter.Name] = parameter
	}
	for name, value := range parameters {
		parameter := parameterMap[name]
		if value == "" {
			continue
		}
		if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("KabusController dataset %qの入力項目 %q が不正です", spec.Dataset, name)
		}
		if len(parameter.Allowed) > 0 {
			matched := false
			for _, allowed := range parameter.Allowed {
				if value == allowed {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("KabusController dataset %qの入力項目 %q は許容値でありません", spec.Dataset, name)
			}
		}
	}
	return nil
}

// ----------------------------------------

/*
requireResolverParameters は、resolver種別ごとの入力項目構成を検証します。

機能:
  - 必須項目の欠落を拒否する
  - 別種類だけで使う項目の混入を拒否する

引数:
  - parameters map[string]string: resolverの検証対象入力
  - required []string: 必ず空でない値が必要な項目名
  - forbidden []string: 指定を許可しない項目名

返り値:
  - error: 必須不足または禁止項目がある場合のエラー
*/
func requireResolverParameters(parameters map[string]string, required, forbidden []string) error {
	for _, name := range required {
		if parameters[name] == "" {
			return fmt.Errorf("KabusController resolverには入力項目 %q が必要です", name)
		}
	}
	for _, name := range forbidden {
		if parameters[name] != "" {
			return fmt.Errorf("KabusController resolverのこのkindには入力項目 %q を指定できません", name)
		}
	}
	return nil
}

// ----------------------------------------

/*
setPathSelector は、固定path templateの末尾へ安全な1 segmentを設定します。

機能:
  - templateが指定placeholderで終わることを確認する
  - selectorが空でなくASCII英数字と限定記号だけであることを確認する

引数:
  - requestURL *url.URL: pathを更新するURL
  - template string: 固定path template
  - placeholder string: template末尾のplaceholder
  - selector string: 1 path segmentとして設定する値

返り値:
  - error: templateまたはselectorが不正な場合のエラー
*/
func setPathSelector(requestURL *url.URL, template, placeholder, selector string) error {
	if requestURL == nil || !strings.HasSuffix(template, "/"+placeholder) {
		return errors.New("KabusControllerのpath templateが不正です")
	}
	if err := validatePathSelector(selector); err != nil {
		return err
	}
	requestURL.Path = strings.TrimSuffix(template, "/"+placeholder) + "/" + selector
	requestURL.RawPath = ""
	return nil
}

// ----------------------------------------

/*
validatePathSelector は、上流pathの1 segmentに使う値を検証します。

機能:
  - 1～100文字のASCII英数字、ピリオド、アンダースコア、ハイフン、@だけを許可する
  - path区切り、query、fragment、特殊要素の注入を拒否する

引数:
  - selector string: 検証するpath selector

返り値:
  - error: selectorが安全な1 segmentでない場合のエラー
*/
func validatePathSelector(selector string) error {
	if selector == "" || strings.TrimSpace(selector) != selector {
		return errors.New("KabusControllerのpath selectorは空にできず、前後に空白を含めることもできません")
	}
	if utf8.RuneCountInString(selector) > 100 || !pathSelectorPattern.MatchString(selector) {
		return errors.New("KabusControllerのpath selectorは100文字以内の英数字と限定記号で指定してください")
	}
	if selector == "." || selector == ".." {
		return errors.New("KabusControllerのpath selectorに特殊path要素は使用できません")
	}
	return nil
}

// ----------------------------------------

/*
validatePathAtom は、複数入力からpath selectorを組み立てる前の1要素を検証します。

機能:
  - 英数字とピリオド、アンダースコア、ハイフンだけを許可する
  - 区切りに使う@を入力要素自体へ含めることを拒否する

引数:
  - value string: 検証するpath要素

返り値:
  - error: pathの1要素として不正な場合のエラー
*/
func validatePathAtom(value string) error {
	if err := validatePathSelector(value); err != nil {
		return err
	}
	if strings.ContainsRune(value, '@') {
		return errors.New("KabusControllerのpath入力要素に@は使用できません")
	}
	return nil
}

// ----------------------------------------

/*
buildClientEndpoints は、固定datasetをHTTP client用mapへ変換します。

機能:
  - datasetとpathの重複を拒否する
  - 固定pathとsymbol path templateの形式を確認する

引数:
  - なし

返り値:
  - map[string]endpointSpec: datasetをキーとする固定endpoint表
  - error: datasetまたはpath定義が不正な場合のエラー
*/
func buildClientEndpoints() (map[string]endpointSpec, error) {
	result := make(map[string]endpointSpec, len(endpointSpecs))
	paths := make(map[string]struct{}, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if strings.TrimSpace(spec.Dataset) == "" {
			return nil, errors.New("KabusController datasetが空です")
		}
		if _, exists := result[spec.Dataset]; exists {
			return nil, fmt.Errorf("KabusController dataset %qが重複しています", spec.Dataset)
		}
		if spec.Kind == kindComposite {
			if spec.Path != "" {
				return nil, fmt.Errorf("KabusController複合dataset %qに固定pathは指定できません", spec.Dataset)
			}
			result[spec.Dataset] = spec
			continue
		}
		if !strings.HasPrefix(spec.Path, "/") || path.Clean(spec.Path) != spec.Path {
			return nil, fmt.Errorf("KabusController dataset %qのpathが不正です", spec.Dataset)
		}
		switch spec.Kind {
		case kindControllerSymbol, kindSymbolExchange, kindPlainSymbol:
			if !strings.HasSuffix(spec.Path, "/:symbol") {
				return nil, fmt.Errorf("KabusController dataset %qのsymbol pathが不正です", spec.Dataset)
			}
		case kindPair:
			if !strings.HasSuffix(spec.Path, "/:pair") {
				return nil, fmt.Errorf("KabusController dataset %qのpair pathが不正です", spec.Dataset)
			}
		case kindFixed:
			if strings.Contains(spec.Path, ":") {
				return nil, fmt.Errorf("KabusController固定dataset %qのpathにplaceholderがあります", spec.Dataset)
			}
		case kindResolver:
			if spec.Path != "/kabusapi/symbolname" {
				return nil, fmt.Errorf("KabusController resolver dataset %qのpathが不正です", spec.Dataset)
			}
		default:
			return nil, fmt.Errorf("KabusController dataset %qのroute種別が不正です", spec.Dataset)
		}
		if _, exists := paths[spec.Path]; exists {
			return nil, fmt.Errorf("KabusController path %qが重複しています", spec.Path)
		}
		result[spec.Dataset] = spec
		paths[spec.Path] = struct{}{}
	}
	return result, nil
}

// ----------------------------------------

/*
validateSymbol は、個別板情報の銘柄コードを安全な1 path segmentとして検証します。

機能:
  - 1～100文字のASCII英数字、ピリオド、アンダースコア、ハイフンだけを許可する
  - pathの特殊要素と固定future・optionルートとの衝突を拒否する

引数:
  - symbol string: 検証する先物またはオプションの銘柄コード

返り値:
  - error: 銘柄コードが空、長すぎる、不正文字を含む、または固定ルートと衝突する場合のエラー
*/
func validateSymbol(symbol string) error {
	if symbol == "" || strings.TrimSpace(symbol) != symbol {
		return errors.New("KabusControllerのsymbolは空にできず、前後に空白を含めることもできません")
	}
	if utf8.RuneCountInString(symbol) > 100 || !symbolPattern.MatchString(symbol) {
		return errors.New("KabusControllerのsymbolは100文字以内の英数字、ピリオド、アンダースコア、ハイフンで指定してください")
	}
	if symbol == "." || symbol == ".." || symbol == "future" || symbol == "option" {
		return errors.New("KabusControllerのsymbolが固定APIパスと衝突します")
	}
	return nil
}

// ----------------------------------------

/*
normalizeBaseURL は、KabusControllerの接続先をHTTPオリジンとして解析します。

機能:
  - 空値には現在の既定オリジンを適用する
  - httpまたはhttpsの絶対URLだけを許可する
  - userinfo、固定path、query、fragmentを拒否する

引数:
  - value string: 検証するbase URL

返り値:
  - url.URL: path等を除いた検証済みオリジン
  - error: URLが安全なHTTPオリジンでない場合のエラー
*/
func normalizeBaseURL(value string) (url.URL, error) {
	if value == "" {
		value = DefaultBaseURL
	}
	if strings.TrimSpace(value) != value {
		return url.URL{}, errors.New("URLの前後に空白を含めることはできません")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return url.URL{}, fmt.Errorf("URLを解析できません: %w", err)
	}
	if parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return url.URL{}, errors.New("httpまたはhttpsの絶対URLである必要があります")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return url.URL{}, errors.New("ホストが必要です")
	}
	if parsed.User != nil {
		return url.URL{}, errors.New("userinfoを含めることはできません")
	}
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return url.URL{}, errors.New("パスを含めることはできません")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return url.URL{}, errors.New("クエリを含めることはできません")
	}
	if parsed.Fragment != "" {
		return url.URL{}, errors.New("フラグメントを含めることはできません")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return *parsed, nil
}

// ----------------------------------------

/*
validHTTPHeaderValue は、net/httpで送信可能なfield-valueか確認します。

機能:
  - HTABを除く0x20未満の制御文字とDELを拒否する

引数:
  - value string: User-Agentへ設定するHTTPヘッダー値

返り値:
  - bool: net/httpで有効なfield-valueの場合はtrue
*/
func validHTTPHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == '\t' {
			continue
		}
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

// ----------------------------------------

/*
validateJSONContentType は、成功応答がJSON互換MIMEか確認します。

機能:
  - application/jsonとapplication/*+jsonを許可する
  - MIMEパラメーターの構文も検証する

引数:
  - value string: Content-Typeヘッダー値

返り値:
  - error: JSON互換MIMEでない場合のエラー
*/
func validateJSONContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("KabusController APIのContent-Typeを解析できません: %w", err)
	}
	if mediaType != "application/json" &&
		(!strings.HasPrefix(mediaType, "application/") || !strings.HasSuffix(mediaType, "+json")) {
		return fmt.Errorf("KabusController APIがJSON以外のContent-Typeを返しました: %q", mediaType)
	}
	return nil
}

// ----------------------------------------

/*
readResponseBody は、必要に応じてgzipを展開し、上限以内の本文を読み取ります。

機能:
  - Content-Lengthが上限を超える場合は読み取り前に拒否する
  - gzip応答を展開した後の本文にも同じ上限を適用する

引数:
  - response *http.Response: 成功したKabusController API応答
  - maximum int64: 読み取るJSON本文の最大バイト数

返り値:
  - []byte: gzip展開後のJSON本文
  - error: gzip、読み取り、本文上限のエラー
*/
func readResponseBody(response *http.Response, maximum int64) ([]byte, error) {
	if response.ContentLength > maximum {
		return nil, fmt.Errorf("KabusController APIの応答本文が上限%dバイトを超えています", maximum)
	}
	limitedBody := &io.LimitedReader{R: response.Body, N: maximum + 1}
	var reader io.Reader = limitedBody
	if strings.EqualFold(strings.TrimSpace(response.Header.Get("Content-Encoding")), "gzip") {
		gzipReader, err := gzip.NewReader(limitedBody)
		if err != nil {
			return nil, fmt.Errorf("KabusController APIのgzip応答を展開できません: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("KabusController APIの応答本文を読み取れません: %w", err)
	}
	if int64(len(body)) > maximum {
		return nil, fmt.Errorf("KabusController APIの応答本文が上限%dバイトを超えています", maximum)
	}
	if limitedBody.N == 0 {
		return nil, fmt.Errorf("KabusController APIの圧縮前応答本文が上限%dバイトを超えています", maximum)
	}
	return body, nil
}

// ----------------------------------------

/*
decodeJSON は、JSON数値を精度保持可能なjson.Numberで復号します。

機能:
  - 正しいUTF-8と1つだけのJSON値を要求する
  - 上流オブジェクトや配列の形を変更せずanyへ復号する

引数:
  - body []byte: 検証するJSON本文

返り値:
  - any: json.Numberを含む標準JSON互換値
  - error: UTF-8、JSON構文、余分なJSON値のエラー
*/
func decodeJSON(body []byte) (any, error) {
	if !utf8.Valid(body) {
		return nil, errors.New("KabusController APIのJSONが正しいUTF-8ではありません")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("KabusController APIのJSONを復号できません: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("KabusController APIのJSONの後ろに余分な値があります")
	}
	return result, nil
}

// ----------------------------------------

/*
responseSourceURL は、最終応答URLから公開用取得元を生成します。

機能:
  - リダイレクト後のscheme、host、pathを反映する
  - userinfo、query、fragmentを公開metadataから除去する

引数:
  - response *http.Response: HTTP clientが返した最終応答
  - fallback string: 最終要求URLがない場合の固定endpoint URL

返り値:
  - string: 公開metadataへ格納できる取得元URL
*/
func responseSourceURL(response *http.Response, fallback string) string {
	var source *url.URL
	if response != nil && response.Request != nil && response.Request.URL != nil {
		value := *response.Request.URL
		source = &value
	} else {
		parsed, err := url.Parse(fallback)
		if err != nil {
			return fallback
		}
		source = parsed
	}
	source.User = nil
	source.RawQuery = ""
	source.ForceQuery = false
	source.Fragment = ""
	return source.String()
}
