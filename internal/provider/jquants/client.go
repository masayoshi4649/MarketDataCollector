package jquants

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
	"strings"
	"time"
)

const statusNoData = 210

// APIClient は、固定したJ-Quants API endpointからJSONを取得する契約です。
type APIClient interface {
	Fetch(context.Context, string, map[string]string) (APIResponse, error)
}

// ClientConfig は、J-Quants API clientの接続条件を保持します。
type ClientConfig struct {
	BaseURL          string
	APIKey           string
	HTTPClient       *http.Client
	UserAgent        string
	MaxResponseBytes int64
}

// APIResponse は、正常なJ-Quants API応答と取得時の付帯情報を保持します。
type APIResponse struct {
	Body          any
	SourceURL     string
	StatusCode    int
	FetchedAt     time.Time
	ResponseBytes int64
}

// APIError は、J-Quants APIが返した非2xx HTTP応答を表します。
type APIError struct {
	StatusCode int
	RetryAfter string
	Message    string
	Endpoint   string
}

/*
Error は、秘密値と上流応答本文を含まないJ-Quants APIエラー文字列を返します。

機能:
  - HTTP状態、固定endpoint、公開可能な定型メッセージだけを整形する
  - APIキー、query、上流応答本文をエラーへ含めない

引数:
  - なし

返り値:
  - string: 利用者へ公開可能なJ-Quants APIエラー文字列
*/
func (e *APIError) Error() string {
	if e == nil {
		return "J-Quants APIエラー"
	}
	message := e.Message
	if message == "" {
		message = "上流APIが予期しないHTTP状態を返しました"
	}
	return fmt.Sprintf("J-Quants API %s がHTTP %dを返しました: %s", e.Endpoint, e.StatusCode, message)
}

// Client は、固定許可リストに従ってJ-Quants APIへHTTP GETを送信します。
type Client struct {
	baseURL          url.URL
	apiKey           string
	httpClient       *http.Client
	userAgent        string
	maxResponseBytes int64
	endpoints        map[string]clientEndpoint
}

type clientEndpoint struct {
	path         string
	allowedQuery map[string]struct{}
	forcedQuery  map[string]string
}

type redactedTransportError struct {
	cause   error
	message string
}

// ----------------------------------------

/*
Error は、APIキーだけを伏せた通信エラーメッセージを返します。

機能:
  - 元の通信診断情報を保ちながらAPIキーの完全一致部分だけを置換する

引数:
  - なし

返り値:
  - string: APIキーを含まない通信エラーメッセージ
*/
func (e *redactedTransportError) Error() string {
	if e == nil {
		return "J-Quants API通信エラー"
	}
	return e.message
}

// ----------------------------------------

/*
Is は、APIキーを含む元エラーを公開せずerrors.Isの原因判定を保持します。

機能:
  - context終了等の既知エラーを元の通信エラーへ照合する
  - errors.Unwrapやerrors.AsからAPIキーを含む元エラーへ到達させない

引数:
  - target error: errors.Isが照合する対象エラー

返り値:
  - bool: 元の通信エラーがtargetに一致する場合はtrue
*/
func (e *redactedTransportError) Is(target error) bool {
	return e != nil && errors.Is(e.cause, target)
}

// ----------------------------------------

/*
NewClient は、J-Quants API clientを生成します。

機能:
  - 接続オリジン、APIキー、User-Agent、本文上限を検証する
  - endpointSpecsからdatasetと固定pathの対応を再検証する
  - 渡されたHTTP clientを複製し、通常のリダイレクトを追跡する
  - 異なるoriginへのリダイレクト時だけx-api-keyを転送対象から除く

引数:
  - config ClientConfig: 接続先、認証、HTTP client、識別値、展開後本文上限

返り値:
  - *Client: 並行利用可能なJ-Quants API client
  - error: 設定または固定endpoint仕様が不正な場合のエラー
*/
func NewClient(config ClientConfig) (*Client, error) {
	baseURL, err := normalizeBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("J-Quants APIのbase URLが不正です: %w", err)
	}
	if err := validateAPIKey(config.APIKey); err != nil {
		return nil, err
	}

	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	if strings.TrimSpace(userAgent) == "" || !validHTTPHeaderValue(userAgent, true) {
		return nil, errors.New("J-Quants APIのUser-Agentが不正です")
	}

	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = DefaultMaxResponseBytes
	}
	if maxResponseBytes < 1 {
		return nil, errors.New("J-Quants APIの応答本文上限は1バイト以上である必要があります")
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
	httpClient.CheckRedirect = redirectPolicy(httpClient.CheckRedirect)

	return &Client{
		baseURL:          baseURL,
		apiKey:           config.APIKey,
		httpClient:       httpClient,
		userAgent:        userAgent,
		maxResponseBytes: maxResponseBytes,
		endpoints:        endpoints,
	}, nil
}

// ----------------------------------------

/*
Fetch は、datasetに対応する固定endpointからJSONを1ページ取得します。

機能:
  - datasetを固定許可リストへ再照合し、queryをurl.Valuesで符号化する
  - GET要求へx-api-key、User-Agent、Acceptを設定する
  - gzipを自動展開後または手動展開後の本文へサイズ上限を適用する
  - JSON数値をjson.Numberとして保持し、余分なJSON値を拒否する
  - 非2xx応答を本文と秘密値を含まないAPIErrorへ変換する

引数:
  - ctx context.Context: HTTP要求の期限とキャンセルを伝えるコンテキスト
  - dataset string: endpointSpecsに存在するdataset識別子
  - query map[string]string: collectorが上流名へ変換済みのquery項目

返り値:
  - APIResponse: JSON本文、HTTP状態、取得元URL、取得時刻、展開後本文サイズ
  - error: 未知dataset、要求作成、通信、HTTP状態、圧縮、本文、MIME、JSONのエラー
*/
func (c *Client) Fetch(
	ctx context.Context,
	dataset string,
	query map[string]string,
) (APIResponse, error) {
	endpoint, exists := c.endpoints[dataset]
	if !exists {
		return APIResponse{}, fmt.Errorf("未対応のJ-Quants datasetです: %q", dataset)
	}
	for name := range query {
		if _, allowed := endpoint.allowedQuery[name]; !allowed {
			return APIResponse{}, fmt.Errorf(
				"J-Quants dataset %qに未知のquery項目があります: %q",
				dataset,
				name,
			)
		}
	}
	for name, expected := range endpoint.forcedQuery {
		if actual, exists := query[name]; !exists || actual != expected {
			return APIResponse{}, fmt.Errorf(
				"J-Quants dataset %qの固定query項目 %qが不正です",
				dataset,
				name,
			)
		}
	}

	requestURL := c.baseURL
	requestURL.Path = endpoint.path
	requestURL.RawPath = ""
	sourceURL := requestURL.String()
	values := make(url.Values, len(query))
	for name, value := range query {
		values.Set(name, value)
	}
	requestURL.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return APIResponse{}, fmt.Errorf("J-Quants API要求を作成できません: %w", err)
	}
	request.Header.Set("x-api-key", c.apiKey)
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "gzip")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return APIResponse{}, fmt.Errorf(
			"J-Quants APIへ接続できません: %w",
			redactTransportError(err, c.apiKey),
		)
	}
	defer response.Body.Close()
	sourceURL = responseSourceURL(response, sourceURL)

	fetchedAt := time.Now().UTC()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return APIResponse{}, newAPIError(endpoint.path, response)
	}

	body, err := readResponseBody(response, c.maxResponseBytes)
	if err != nil {
		return APIResponse{}, err
	}
	if response.StatusCode == statusNoData && len(bytes.TrimSpace(body)) == 0 {
		return APIResponse{
			Body: nil, SourceURL: sourceURL, StatusCode: response.StatusCode,
			FetchedAt: fetchedAt, ResponseBytes: int64(len(body)),
		}, nil
	}
	if err := validateJSONContentType(response.Header.Get("Content-Type")); err != nil {
		return APIResponse{}, err
	}
	decoded, err := decodeJSON(body)
	if err != nil {
		return APIResponse{}, err
	}

	return APIResponse{
		Body: decoded, SourceURL: sourceURL, StatusCode: response.StatusCode,
		FetchedAt: fetchedAt, ResponseBytes: int64(len(body)),
	}, nil
}

// ----------------------------------------

/*
redirectPolicy は、通常のHTTPリダイレクトを追跡する方針を生成します。

機能:
  - 同一originへのリダイレクトではx-api-keyを維持する
  - 異なるoriginへのリダイレクトではx-api-keyだけを削除して追跡する
  - 呼び出し元の方針がある場合は委譲後にx-api-keyの転送範囲を確定する
  - 呼び出し元の方針がない場合は標準と同じ10回上限を適用する

引数:
  - previous func(*http.Request, []*http.Request) error: 呼び出し元HTTP clientの既存方針

返り値:
  - func(*http.Request, []*http.Request) error: Client複製へ設定するredirect方針
*/
func redirectPolicy(
	previous func(*http.Request, []*http.Request) error,
) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if previous != nil {
			if err := previous(request, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return errors.New("J-Quants APIのリダイレクト回数が10回を超えました")
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, request.URL) {
			request.Header.Del("x-api-key")
		}
		return nil
	}
}

// ----------------------------------------

/*
responseSourceURL は、成功応答を実際に返したURLから公開用取得元を生成します。

機能:
  - リダイレクト後のscheme、host、pathを取得元へ反映する
  - userinfo、query、fragmentを除去し、入力値や継続キーをmetadataへ重複掲載しない
  - 応答に最終要求URLがない場合は検証済みの固定endpoint URLへフォールバックする

引数:
  - response *http.Response: HTTP clientが返した最終応答
  - fallback string: redirect前の固定endpoint URL

返り値:
  - string: 公開metadataへ格納できる取得元URL
*/
func responseSourceURL(response *http.Response, fallback string) string {
	if response == nil || response.Request == nil || response.Request.URL == nil {
		return fallback
	}
	value := *response.Request.URL
	value.User = nil
	value.RawQuery = ""
	value.ForceQuery = false
	value.Fragment = ""
	return value.String()
}

// ----------------------------------------

/*
sameOrigin は、2つのURLが同じHTTP originを表すか確認します。

機能:
  - scheme、hostname、既定値を補正したportを比較する
  - nil URLを異なるoriginとして扱う

引数:
  - first *url.URL: 最初のHTTP要求URL
  - second *url.URL: リダイレクト先URL

返り値:
  - bool: 同じoriginの場合はtrue
*/
func sameOrigin(first *url.URL, second *url.URL) bool {
	if first == nil || second == nil {
		return false
	}
	return strings.EqualFold(first.Scheme, second.Scheme) &&
		strings.EqualFold(first.Hostname(), second.Hostname()) &&
		effectiveURLPort(first) == effectiveURLPort(second)
}

// ----------------------------------------

/*
effectiveURLPort は、URLの明示portまたはschemeの既定portを返します。

機能:
  - httpを80、httpsを443へ補正してorigin比較へ利用する

引数:
  - value *url.URL: portを取得するURL

返り値:
  - string: 明示port、既定port、または未対応schemeの空文字
*/
func effectiveURLPort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

// ----------------------------------------

/*
redactTransportError は、通信エラーからAPIキーの完全一致だけを伏せます。

機能:
  - APIキーを含まない通常エラーはそのまま返して診断情報を保持する
  - APIキーを含む場合だけ表示文字列を置換し、Unwrap可能なwrapperを返す

引数:
  - err error: HTTP clientが返した通信エラー
  - apiKey string: x-api-keyへ設定した秘密値

返り値:
  - error: 診断情報を必要以上に隠さずAPIキーだけを除いたエラー
*/
func redactTransportError(err error, apiKey string) error {
	if err == nil || apiKey == "" || !strings.Contains(err.Error(), apiKey) {
		return err
	}
	return &redactedTransportError{
		cause:   err,
		message: strings.ReplaceAll(err.Error(), apiKey, "[APIキー]"),
	}
}

// ----------------------------------------

/*
normalizeBaseURL は、J-Quants APIのHTTPオリジンを検証して正規化します。

機能:
  - 空値へ公式オリジンを適用する
  - httpまたはhttpsの絶対URLだけを許可する
  - userinfo、path、query、fragmentを拒否して固定pathの安全な結合を保証する

引数:
  - value string: ClientConfigで指定されたbase URL

返り値:
  - url.URL: 固定endpoint pathを設定できる検証済みオリジン
  - error: URLの形式または構成が不正な場合のエラー
*/
func normalizeBaseURL(value string) (url.URL, error) {
	if value == "" {
		value = DefaultBaseURL
	}
	if strings.TrimSpace(value) != value {
		return url.URL{}, errors.New("base URLの前後に空白を含めることはできません")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return url.URL{}, errors.New("base URLを解析できません")
	}
	if parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.Hostname() == "" {
		return url.URL{}, errors.New("httpまたはhttpsのホスト付き絶対URLが必要です")
	}
	if parsed.User != nil {
		return url.URL{}, errors.New("base URLにuserinfoを含めることはできません")
	}
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return url.URL{}, errors.New("base URLにpathを含めることはできません")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return url.URL{}, errors.New("base URLにqueryまたはfragmentを含めることはできません")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return *parsed, nil
}

// ----------------------------------------

/*
validateAPIKey は、APIキーを値の漏えいなしで検証します。

機能:
  - 空値と前後空白を拒否する
  - HTTPヘッダーへ設定できない制御文字を拒否する

引数:
  - apiKey string: x-api-keyへ設定する秘密値

返り値:
  - error: APIキーが不正な場合の定型エラー。有効な場合はnil
*/
func validateAPIKey(apiKey string) error {
	if apiKey == "" {
		return errors.New("J-Quants APIキーが指定されていません")
	}
	if strings.TrimSpace(apiKey) != apiKey || !validHTTPHeaderValue(apiKey, false) {
		return errors.New("J-Quants APIキーの形式が不正です")
	}
	return nil
}

// ----------------------------------------

/*
validHTTPHeaderValue は、HTTP field-valueに利用できる文字列か確認します。

機能:
  - 0x20未満の制御文字とDELを拒否する
  - User-Agentの場合だけHTTPで許容される水平タブを受け付ける

引数:
  - value string: 検証するHTTPヘッダー値
  - allowHorizontalTab bool: 水平タブを許可する場合はtrue

返り値:
  - bool: HTTPヘッダー値として利用できる場合はtrue
*/
func validHTTPHeaderValue(value string, allowHorizontalTab bool) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == '\t' && allowHorizontalTab {
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
buildClientEndpoints は、endpointSpecsをHTTP client用の固定許可リストへ変換します。

機能:
  - dataset重複と空値を拒否する
  - 各pathがAPI版配下の安全な絶対pathであることを再検証する
  - 上流query名とForcedQueryをdatasetごとの固定許可リストへ変換する

引数:
  - なし

返り値:
  - map[string]clientEndpoint: datasetをキー、固定pathとquery仕様を値とする許可リスト
  - error: endpointSpecsに重複または不正な固定値がある場合のエラー
*/
func buildClientEndpoints() (map[string]clientEndpoint, error) {
	result := make(map[string]clientEndpoint, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if spec.Dataset == "" || strings.TrimSpace(spec.Dataset) != spec.Dataset {
			return nil, errors.New("J-Quants endpoint仕様に不正なdatasetがあります")
		}
		if _, exists := result[spec.Dataset]; exists {
			return nil, fmt.Errorf("J-Quants endpoint仕様のdataset %qが重複しています", spec.Dataset)
		}
		if err := validateEndpointPath(spec.Path); err != nil {
			return nil, fmt.Errorf("J-Quants dataset %qの固定pathが不正です: %w", spec.Dataset, err)
		}
		allowedQuery := make(map[string]struct{}, len(spec.Parameters)+len(spec.ForcedQuery))
		for _, parameter := range spec.Parameters {
			upstreamName := parameter.UpstreamName
			if upstreamName == "" {
				upstreamName = parameter.Name
			}
			if upstreamName == "" {
				return nil, fmt.Errorf("J-Quants dataset %qに空のquery項目があります", spec.Dataset)
			}
			if _, exists := allowedQuery[upstreamName]; exists {
				return nil, fmt.Errorf(
					"J-Quants dataset %qのquery項目 %qが重複しています",
					spec.Dataset,
					upstreamName,
				)
			}
			allowedQuery[upstreamName] = struct{}{}
		}
		forcedQuery := make(map[string]string, len(spec.ForcedQuery))
		for name, value := range spec.ForcedQuery {
			if name == "" {
				return nil, fmt.Errorf("J-Quants dataset %qに空の固定query項目があります", spec.Dataset)
			}
			allowedQuery[name] = struct{}{}
			forcedQuery[name] = value
		}
		result[spec.Dataset] = clientEndpoint{
			path: spec.Path, allowedQuery: allowedQuery, forcedQuery: forcedQuery,
		}
	}
	return result, nil
}

// ----------------------------------------

/*
validateEndpointPath は、固定endpoint pathを安全にオリジンへ結合できるか確認します。

機能:
  - 現在のAPI版を先頭に持つ正規化済み絶対pathだけを許可する
  - host、query、fragment、userinfo、escape表現、dot segmentを拒否する

引数:
  - value string: endpointSpecsに定義された固定path

返り値:
  - error: 固定pathが不正な場合のエラー。有効な場合はnil
*/
func validateEndpointPath(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return errors.New("path以外のURL要素を含めることはできません")
	}
	if !strings.HasPrefix(value, "/"+APIVersion+"/") || parsed.EscapedPath() != value {
		return fmt.Errorf("/%s/配下のescapeなし絶対pathである必要があります", APIVersion)
	}
	if strings.Contains(value, "\\") || path.Clean(value) != value {
		return errors.New("dot segmentまたは逆向き区切りを含めることはできません")
	}
	return nil
}

// ----------------------------------------

/*
readResponseBody は、必要に応じてgzipを展開して上限以内の本文を読み取ります。

機能:
  - Go HTTP transportが自動展開した本文をそのまま読む
  - Content-Encodingが残るgzip本文を手動展開する
  - gzipヘッダーを含む圧縮本文と展開後本文の両方へmaxResponseBytesを適用する

引数:
  - response *http.Response: bodyを閉じる責務を呼び出し側が持つHTTP応答
  - maxResponseBytes int64: 展開後本文の最大バイト数

返り値:
  - []byte: 展開済みのHTTP応答本文
  - error: 未対応encoding、gzip、読み取り、本文上限のエラー
*/
func readResponseBody(response *http.Response, maxResponseBytes int64) ([]byte, error) {
	reader := io.Reader(response.Body)
	contentEncoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	if !response.Uncompressed && contentEncoding != "" && !strings.EqualFold(contentEncoding, "identity") {
		if !strings.EqualFold(contentEncoding, "gzip") {
			return nil, errors.New("J-Quants API応答のContent-Encodingが不正です")
		}
		compressedReader := io.LimitReader(response.Body, maxResponseBytes)
		gzipReader, err := gzip.NewReader(compressedReader)
		if err != nil {
			return nil, errors.New("J-Quants APIのgzip応答を展開できません")
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("J-Quants API応答本文を読み取れません: %w", err)
	}
	if int64(len(body)) == maxResponseBytes {
		var extra [1]byte
		readBytes, readErr := io.ReadFull(reader, extra[:])
		if readBytes > 0 {
			return nil, fmt.Errorf("J-Quants API応答本文が上限%dバイトを超えました", maxResponseBytes)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("J-Quants API応答本文を読み取れません: %w", readErr)
		}
	}
	return body, nil
}

// ----------------------------------------

/*
validateJSONContentType は、J-Quants API応答がJSON MIME型か確認します。

機能:
  - parameter付きapplication/jsonを許可する
  - 欠落、不正形式、JSON以外のMIME型を拒否する

引数:
  - contentType string: HTTP Content-Typeヘッダー値

返り値:
  - error: application/jsonではない場合のエラー。有効な場合はnil
*/
func validateJSONContentType(contentType string) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return errors.New("J-Quants API応答のContent-Typeはapplication/jsonである必要があります")
	}
	return nil
}

// ----------------------------------------

/*
decodeJSON は、応答本文を数値精度を保った動的JSON値へ復号します。

機能:
  - json.Decoder.UseNumberで整数をfloat64へ変換しない
  - 先頭JSON値の後ろにある余分なJSON値を拒否する

引数:
  - body []byte: 1つのJSON値を含む展開済み応答本文

返り値:
  - any: json.Numberを保持するJSON互換値
  - error: JSON形式または余分なJSON値が不正な場合のエラー
*/
func decodeJSON(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("J-Quants API応答JSONを復号できません: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("J-Quants API応答JSONの後ろに余分な値があります")
	}
	return value, nil
}

// ----------------------------------------

/*
newAPIError は、非2xx HTTP応答を公開可能な定型エラーへ変換します。

機能:
  - HTTP状態ごとの定型メッセージを設定する
  - Retry-Afterを保持し、上流応答本文、query、APIキーを含めない

引数:
  - endpoint string: endpointSpecsから取得した固定path
  - response *http.Response: 非2xxのJ-Quants API応答

返り値:
  - *APIError: collectorがerrors.Asで状態分類できるエラー
*/
func newAPIError(endpoint string, response *http.Response) *APIError {
	message := "上流APIが予期しないHTTP状態を返しました"
	switch response.StatusCode {
	case http.StatusBadRequest:
		message = "上流APIが要求を受け付けませんでした"
	case http.StatusUnauthorized:
		message = "上流APIの認証に失敗しました"
	case http.StatusForbidden:
		message = "契約または権限により上流APIを利用できません"
	case http.StatusTooManyRequests:
		message = "上流APIのレート制限に達しました"
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			message = "上流APIが一時的に利用できません"
		}
	}
	return &APIError{
		StatusCode: response.StatusCode,
		RetryAfter: response.Header.Get("Retry-After"),
		Message:    message,
		Endpoint:   endpoint,
	}
}
