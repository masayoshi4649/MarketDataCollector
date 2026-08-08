package polymarket

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// APIClient は、固定許可リスト内のPolymarket公開GETを実行する契約です。
type APIClient interface {
	Fetch(context.Context, string, url.Values) (APIResponse, error)
}

// ClientConfig は、3種類の公開API client接続条件を保持します。
type ClientConfig struct {
	GammaBaseURL     string
	CLOBBaseURL      string
	DataBaseURL      string
	HTTPClient       *http.Client
	UserAgent        string
	MaxResponseBytes int64
}

// APIResponse は、正常応答と取得時の付帯情報を保持します。
type APIResponse struct {
	Body          any
	SourceURL     string
	Endpoint      string
	StatusCode    int
	FetchedAt     time.Time
	ResponseBytes int64
}

// APIError は、上流の非2xx応答を本文なしで表します。
type APIError struct {
	StatusCode int
	RetryAfter string
	Message    string
	Endpoint   string
}

// Error は、上流本文を含まない公開可能なエラー文字列を返します。
//
// 機能:
//   - HTTP状態、固定endpoint、定型メッセージだけを整形する
//
// 引数:
//   - なし
//
// 返り値:
//   - string: 公開可能なPolymarket APIエラー文字列
func (e *APIError) Error() string {
	if e == nil {
		return "Polymarket APIエラー"
	}
	message := e.Message
	if message == "" {
		message = "上流APIが予期しないHTTP状態を返しました"
	}
	return fmt.Sprintf("Polymarket API %s がHTTP %dを返しました: %s", e.Endpoint, e.StatusCode, message)
}

// Client は、3サービスの固定許可リストへHTTP GETを送信します。
type Client struct {
	baseURLs         map[apiService]url.URL
	httpClient       *http.Client
	userAgent        string
	maxResponseBytes int64
	endpoints        map[string]clientEndpoint
}

type clientEndpoint struct {
	spec         endpointSpec
	allowedQuery map[string]struct{}
}

var (
	positiveIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	slugPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
	walletPattern     = regexp.MustCompile(`(?i)^0x[0-9a-f]{40}$`)
	conditionPattern  = regexp.MustCompile(`(?i)^0x[0-9a-f]{64}$`)
	tokenPattern      = regexp.MustCompile(`(?i)^(?:[0-9]{1,100}|0x[0-9a-f]{1,98})$`)
)

// ----------------------------------------

// NewClient は、Polymarket公開API clientを生成します。
//
// 機能:
//   - 3つのorigin、User-Agent、本文上限を検証する
//   - endpointSpecsからpath/query許可リストを構築する
//   - 渡されたHTTP clientを複製し、redirect方針を維持しつつcookie jarを共有しない
//
// 引数:
//   - config ClientConfig: 接続origin、HTTP client、User-Agent、本文上限
//
// 返り値:
//   - *Client: 並行利用可能な公開API client
//   - error: 接続設定または固定仕様が不正な場合のエラー
func NewClient(config ClientConfig) (*Client, error) {
	gamma, err := normalizeBaseURL(config.GammaBaseURL, DefaultGammaBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Gamma APIのbase URLが不正です: %w", err)
	}
	clob, err := normalizeBaseURL(config.CLOBBaseURL, DefaultCLOBBaseURL)
	if err != nil {
		return nil, fmt.Errorf("CLOB APIのbase URLが不正です: %w", err)
	}
	data, err := normalizeBaseURL(config.DataBaseURL, DefaultDataBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Data APIのbase URLが不正です: %w", err)
	}
	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	if strings.TrimSpace(userAgent) == "" || !validHeaderValue(userAgent) {
		return nil, errors.New("Polymarket APIのUser-Agentが不正です")
	}
	maxBytes := config.MaxResponseBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	if maxBytes < 1 {
		return nil, errors.New("Polymarket APIの応答本文上限は1バイト以上である必要があります")
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
	return &Client{
		baseURLs:   map[apiService]url.URL{serviceGamma: gamma, serviceCLOB: clob, serviceData: data},
		httpClient: httpClient, userAgent: userAgent, maxResponseBytes: maxBytes, endpoints: endpoints,
	}, nil
}

// ----------------------------------------

// Fetch は、datasetに対応する公開endpointからJSONを1回だけ取得します。
//
// 機能:
//   - 固定path/query許可リストと動的path selectorを再検証する
//   - JSON配列queryの同名反復を保持してURLへ符号化する
//   - gzip圧縮前後の本文へ同じ上限を適用し、JSON数値をjson.Numberで保持する
//   - 非2xxをRetry-After付きAPIErrorへ変換し、自動再試行しない
//
// 引数:
//   - ctx context.Context: HTTP要求の期限とキャンセル
//   - dataset string: endpointSpecsに存在するdataset識別子
//   - query url.Values: collectorが上流名へ変換済みのquery値
//
// 返り値:
//   - APIResponse: JSON本文、取得元、endpoint、状態、取得時刻、展開後bytes
//   - error: 未知dataset、許可外query、通信、HTTP、圧縮、MIME、JSONのエラー
func (c *Client) Fetch(ctx context.Context, dataset string, query url.Values) (APIResponse, error) {
	endpoint, exists := c.endpoints[dataset]
	if !exists {
		return APIResponse{}, fmt.Errorf("未対応のPolymarket datasetです: %q", dataset)
	}
	values := cloneValues(query)
	for name := range values {
		if _, allowed := endpoint.allowedQuery[name]; !allowed {
			return APIResponse{}, fmt.Errorf("Polymarket dataset %qに未知のquery項目があります: %q", dataset, name)
		}
	}
	if err := validateSelectorCardinality(endpoint.spec, values); err != nil {
		return APIResponse{}, err
	}
	endpointPath, err := resolveEndpointPath(endpoint.spec, values)
	if err != nil {
		return APIResponse{}, err
	}
	baseURL, exists := c.baseURLs[endpoint.spec.Service]
	if !exists {
		return APIResponse{}, fmt.Errorf("Polymarket dataset %qのAPI serviceが不正です", dataset)
	}
	requestURL := baseURL
	requestURL.Path = endpointPath
	requestURL.RawPath = ""
	requestURL.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return APIResponse{}, fmt.Errorf("Polymarket API要求を作成できません: %w", err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return APIResponse{}, fmt.Errorf("Polymarket APIへ接続できません: %w", err)
	}
	defer response.Body.Close()
	fetchedAt := time.Now().UTC()
	actualURL := responseSourceURL(response, requestURL.String())
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return APIResponse{}, newAPIError(endpointPath, response)
	}
	if err := validateResponseContentType(dataset, response.Header.Get("Content-Type")); err != nil {
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
	return APIResponse{Body: decoded, SourceURL: actualURL, Endpoint: endpointPath, StatusCode: response.StatusCode, FetchedAt: fetchedAt, ResponseBytes: int64(len(body))}, nil
}

// ----------------------------------------

// buildClientEndpoints は、endpointSpecsから固定query許可リストを生成します。
//
// 機能:
//   - dataset重複、service、固定path、query名を検証する
//   - 動的path selectorを含む公開項目名も許可リストへ登録する
//
// 引数:
//   - なし
//
// 返り値:
//   - map[string]clientEndpoint: datasetをキーにした固定許可リスト
//   - error: endpoint仕様に重複または不正値がある場合のエラー
func buildClientEndpoints() (map[string]clientEndpoint, error) {
	result := make(map[string]clientEndpoint, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if spec.Dataset == "" || strings.TrimSpace(spec.Dataset) != spec.Dataset {
			return nil, errors.New("Polymarket endpoint仕様に不正なdatasetがあります")
		}
		if _, exists := result[spec.Dataset]; exists {
			return nil, fmt.Errorf("Polymarket dataset %qが重複しています", spec.Dataset)
		}
		if spec.Service != serviceGamma && spec.Service != serviceCLOB && spec.Service != serviceData {
			return nil, fmt.Errorf("Polymarket dataset %qのserviceが不正です", spec.Dataset)
		}
		if err := validateEndpointPath(spec.Path); err != nil {
			return nil, fmt.Errorf("Polymarket dataset %qのpathが不正です: %w", spec.Dataset, err)
		}
		allowed := make(map[string]struct{}, len(spec.QueryNames)+4)
		for _, name := range spec.QueryNames {
			if name == "" {
				return nil, fmt.Errorf("Polymarket dataset %qに空のquery名があります", spec.Dataset)
			}
			allowed[name] = struct{}{}
		}
		for _, name := range routeSelectorNames(spec.Route) {
			allowed[name] = struct{}{}
		}
		result[spec.Dataset] = clientEndpoint{spec: spec, allowedQuery: allowed}
	}
	return result, nil
}

// ----------------------------------------

// routeSelectorNames は、routeごとのclient内部selector名を返します。
//
// 機能:
//   - QueryNamesには含めないpath/endpoint選択値だけを固定列挙する
//
// 引数:
//   - route routeKind: datasetのroute種別
//
// 返り値:
//   - []string: clientだけが消費するselector名
func routeSelectorNames(route routeKind) []string {
	switch route {
	case routeEntity, routeTag:
		return []string{"id", "slug"}
	case routeRelatedTags:
		return []string{"id", "slug", "resolved_tags"}
	case routeSeriesItem:
		return []string{"id"}
	case routeComments:
		return []string{"comment_id", "user_address"}
	case routeTokenPrice:
		return []string{"price_type"}
	case routeCLOBMarkets:
		return []string{"kind"}
	case routeCondition:
		return []string{"condition_id"}
	case routeTokenPath:
		return []string{"token_id"}
	default:
		return nil
	}
}

// ----------------------------------------

// validateSelectorCardinality は、path selectorが単一の非空値か確認します。
//
// 機能:
//   - 欠落可能なselectorは許可し、存在時の空値と同名多重指定を拒否する
//   - selector異常による意図しない別endpointへのfallbackを防ぐ
//
// 引数:
//   - spec endpointSpec: datasetのroute仕様
//   - values url.Values: 検証するquery
//
// 返り値:
//   - error: selectorの個数または値が不正な場合のエラー。有効な場合はnil
func validateSelectorCardinality(spec endpointSpec, values url.Values) error {
	for _, name := range routeSelectorNames(spec.Route) {
		items, exists := values[name]
		if !exists {
			continue
		}
		if len(items) != 1 || items[0] == "" {
			return fmt.Errorf("Polymarket dataset %qのselector %qは単一の非空値が必要です", spec.Dataset, name)
		}
	}
	return nil
}

// ----------------------------------------

// resolveEndpointPath は、検証済みselectorから実際の固定系列pathを選択します。
//
// 機能:
//   - entity、tag、comments、price、市場一覧の公式GET分岐を選ぶ
//   - path専用selectorをqueryから削除する
//   - selectorを固定形式へ再検証してpath注入を防ぐ
//
// 引数:
//   - spec endpointSpec: datasetのroute仕様
//   - values url.Values: selectorを含む上流query
//
// 返り値:
//   - string: 実際に要求する安全な絶対path
//   - error: selectorが欠落または不正な場合のエラー
func resolveEndpointPath(spec endpointSpec, values url.Values) (string, error) {
	pathValue := spec.Path
	switch spec.Route {
	case routeFixed:
	case routeEntity:
		id, slug := takeSingle(values, "id"), takeSingle(values, "slug")
		if (id == "") == (slug == "") {
			return "", fmt.Errorf("Polymarket dataset %qはidまたはslugを1つ指定する必要があります", spec.Dataset)
		}
		if id != "" {
			if !positiveIDPattern.MatchString(id) {
				return "", fmt.Errorf("Polymarket dataset %qのidが不正です", spec.Dataset)
			}
			pathValue += "/" + id
		} else {
			if !slugPattern.MatchString(slug) {
				return "", fmt.Errorf("Polymarket dataset %qのslugが不正です", spec.Dataset)
			}
			pathValue += "/slug/" + slug
		}
	case routeTag:
		id, slug := takeSingle(values, "id"), takeSingle(values, "slug")
		if (id == "") == (slug == "") {
			return "", errors.New("tagはidまたはslugを1つ指定する必要があります")
		}
		if id != "" {
			if !positiveIDPattern.MatchString(id) {
				return "", errors.New("tagのidが不正です")
			}
			pathValue += "/" + id
		} else {
			if !slugPattern.MatchString(slug) {
				return "", errors.New("tagのslugが不正です")
			}
			pathValue += "/slug/" + slug
		}
	case routeRelatedTags:
		id, slug := takeSingle(values, "id"), takeSingle(values, "slug")
		resolvedValue := takeSingle(values, "resolved_tags")
		if resolvedValue != "" && resolvedValue != "true" && resolvedValue != "false" {
			return "", errors.New("related_tagsのresolved_tagsが不正です")
		}
		resolved := resolvedValue == "true"
		if (id == "") == (slug == "") {
			return "", errors.New("related_tagsはidまたはslugを1つ指定する必要があります")
		}
		if id != "" {
			if !positiveIDPattern.MatchString(id) {
				return "", errors.New("related_tagsのidが不正です")
			}
			pathValue += "/" + id + "/related-tags"
		} else {
			if !slugPattern.MatchString(slug) {
				return "", errors.New("related_tagsのslugが不正です")
			}
			pathValue += "/slug/" + slug + "/related-tags"
		}
		if resolved {
			pathValue += "/tags"
		}
	case routeSeriesItem:
		id := takeSingle(values, "id")
		if id == "" {
			return "", errors.New("series_itemはidが必要です")
		}
		if !positiveIDPattern.MatchString(id) {
			return "", errors.New("series_itemのidが不正です")
		}
		pathValue += "/" + id
	case routeComments:
		commentID, address := takeSingle(values, "comment_id"), takeSingle(values, "user_address")
		if commentID != "" && address != "" {
			return "", errors.New("commentsのcomment_idとuser_addressは同時指定できません")
		}
		if commentID != "" {
			if !positiveIDPattern.MatchString(commentID) {
				return "", errors.New("commentsのcomment_idが不正です")
			}
			for name := range values {
				if name != "get_positions" {
					return "", fmt.Errorf("commentsのcomment_id指定時にquery %qは利用できません", name)
				}
			}
			pathValue += "/" + commentID
		} else if address != "" {
			if !walletPattern.MatchString(address) {
				return "", errors.New("commentsのuser_addressが不正です")
			}
			for name := range values {
				switch name {
				case "limit", "offset", "order", "ascending":
				default:
					return "", fmt.Errorf("commentsのuser_address指定時にquery %qは利用できません", name)
				}
			}
			pathValue += "/user_address/" + address
		}
	case routeTokenPrice:
		priceType := takeSingle(values, "price_type")
		switch priceType {
		case "best_bid":
			values.Set("side", "BUY")
		case "best_ask":
			values.Set("side", "SELL")
		case "midpoint":
			pathValue = "/midpoint"
			values.Del("side")
		case "last_trade":
			pathValue = "/last-trade-price"
			values.Del("side")
		default:
			return "", errors.New("token_priceのprice_typeが不正です")
		}
	case routeCLOBMarkets:
		kind := takeSingle(values, "kind")
		switch kind {
		case "simplified":
			pathValue = "/simplified-markets"
		case "sampling":
			pathValue = "/sampling-markets"
		case "sampling_simplified":
			pathValue = "/sampling-simplified-markets"
		default:
			return "", errors.New("clob_marketsのkindが不正です")
		}
	case routeCondition:
		conditionID := takeSingle(values, "condition_id")
		if conditionID == "" {
			return "", errors.New("clob_marketはcondition_idが必要です")
		}
		if !conditionPattern.MatchString(conditionID) {
			return "", errors.New("clob_marketのcondition_idが不正です")
		}
		pathValue += "/" + conditionID
	case routeTokenPath:
		tokenID := takeSingle(values, "token_id")
		if tokenID == "" {
			return "", errors.New("market_by_tokenはtoken_idが必要です")
		}
		if !tokenPattern.MatchString(tokenID) {
			return "", errors.New("market_by_tokenのtoken_idが不正です")
		}
		pathValue += "/" + tokenID
	default:
		return "", fmt.Errorf("Polymarket dataset %qのrouteが不正です", spec.Dataset)
	}
	if err := validateEndpointPath(pathValue); err != nil {
		return "", fmt.Errorf("Polymarket dataset %qの解決済みpathが不正です: %w", spec.Dataset, err)
	}
	return pathValue, nil
}

// ----------------------------------------

// takeSingle は、単一値selectorをqueryから取り出して削除します。
//
// 機能:
//   - selectorの多重指定を空値として拒否側へ渡す
//
// 引数:
//   - values url.Values: selectorを含むquery
//   - name string: 取り出すquery名
//
// 返り値:
//   - string: 単一値。欠落または多重指定時は空文字
func takeSingle(values url.Values, name string) string {
	items, exists := values[name]
	values.Del(name)
	if !exists || len(items) != 1 {
		return ""
	}
	return items[0]
}

// ----------------------------------------

// cloneValues は、queryと各値sliceを複製します。
//
// 機能:
//   - client内のselector削除が呼び出し元へ影響しないようにする
//
// 引数:
//   - source url.Values: 複製元query
//
// 返り値:
//   - url.Values: 深い複製
func cloneValues(source url.Values) url.Values {
	result := make(url.Values, len(source))
	for name, values := range source {
		result[name] = append([]string(nil), values...)
	}
	return result
}

// ----------------------------------------

// normalizeBaseURL は、HTTP originを検証して正規化します。
//
// 機能:
//   - 空値へ公式originを適用し、path、query、userinfo、fragmentを拒否する
//
// 引数:
//   - value string: 設定されたbase URL
//   - defaultValue string: 空値時の公式base URL
//
// 返り値:
//   - url.URL: 検証済みorigin
//   - error: URL形式または構成が不正な場合のエラー
func normalizeBaseURL(value, defaultValue string) (url.URL, error) {
	if value == "" {
		value = defaultValue
	}
	if strings.TrimSpace(value) != value {
		return url.URL{}, errors.New("base URLの前後に空白を含めることはできません")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return url.URL{}, errors.New("httpまたはhttpsのホスト付き絶対URLが必要です")
	}
	if parsed.User != nil || (parsed.EscapedPath() != "" && parsed.EscapedPath() != "/") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return url.URL{}, errors.New("base URLはoriginだけを指定してください")
	}
	parsed.Path, parsed.RawPath = "", ""
	return *parsed, nil
}

// ----------------------------------------

// validateEndpointPath は、安全な絶対pathか確認します。
//
// 機能:
//   - 正規化済み絶対pathだけを許可し、dot segmentやURL構成要素を拒否する
//
// 引数:
//   - value string: 検証するendpoint path
//
// 返り値:
//   - error: pathが不正な場合のエラー。有効な場合はnil
func validateEndpointPath(value string) error {
	if value == "" || !strings.HasPrefix(value, "/") || strings.Contains(value, "//") || strings.Contains(value, "..") || strings.ContainsAny(value, "?#\\") {
		return errors.New("正規化済み絶対pathではありません")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.EscapedPath() != value {
		return errors.New("path以外のURL構成要素またはescapeを含めることはできません")
	}
	return nil
}

// ----------------------------------------

// validHeaderValue は、HTTP field-valueとして安全か確認します。
//
// 機能:
//   - 改行、制御文字、DELを拒否し、水平tabだけを許可する
//
// 引数:
//   - value string: 検証するHTTPヘッダー値
//
// 返り値:
//   - bool: 利用可能な場合はtrue
func validHeaderValue(value string) bool {
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

// readResponseBody は、圧縮前後の本文へ設定上限を適用して読み取ります。
//
// 機能:
//   - gzip時は圧縮bytesを先に制限し、その後展開bytesも同じ上限で制限する
//   - identity応答は本文を直接制限し、未知のContent-Encodingを拒否する
//
// 引数:
//   - response *http.Response: 成功したHTTP応答
//   - maximum int64: 圧縮前後それぞれの最大bytes
//
// 返り値:
//   - []byte: 展開済み本文
//   - error: encoding、gzip、読み取り、上限超過のエラー
func readResponseBody(response *http.Response, maximum int64) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding")))
	if response.Uncompressed {
		encoding = ""
	}
	raw, err := readLimited(response.Body, maximum, "圧縮前")
	if err != nil {
		return nil, err
	}
	if encoding == "" || encoding == "identity" {
		return raw, nil
	}
	if encoding != "gzip" {
		return nil, fmt.Errorf("未対応のContent-Encodingです: %q", encoding)
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("gzip本文を開始できません")
	}
	defer reader.Close()
	return readLimited(reader, maximum, "gzip展開後")
}

// ----------------------------------------

// readLimited は、readerを上限より1byte多く読み上限超過を検出します。
//
// 機能:
//   - 全量を保持せずLimitReaderで読み取り量を制限する
//
// 引数:
//   - reader io.Reader: 読み取り元
//   - maximum int64: 最大bytes
//   - stage string: エラーへ掲載する段階名
//
// 返り値:
//   - []byte: 上限内の本文
//   - error: 読み取り失敗または上限超過のエラー
func readLimited(reader io.Reader, maximum int64, stage string) ([]byte, error) {
	limit := maximum + 1
	if maximum == math.MaxInt64 {
		limit = maximum
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, fmt.Errorf("Polymarket API本文を読み取れません: %w", err)
	}
	if int64(len(body)) > maximum {
		return nil, fmt.Errorf("Polymarket APIの%s本文が設定上限%d bytesを超えました", stage, maximum)
	}
	return body, nil
}

// ----------------------------------------

// validateResponseContentType は、datasetごとの公開応答MIMEを確認します。
//
// 機能:
//   - 通常datasetにはJSON互換MIMEだけを許可する
//   - server_timeだけは実APIが返すtext/plainのJSON整数も許可する
//
// 引数:
//   - dataset string: 応答元dataset
//   - value string: Content-Typeヘッダー値
//
// 返り値:
//   - error: datasetで許可されないMIMEの場合のエラー。有効な場合はnil
func validateResponseContentType(dataset, value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if dataset == "server_time" && err == nil && mediaType == "text/plain" {
		return nil
	}
	return validateJSONContentType(value)
}

// ----------------------------------------

// validateJSONContentType は、JSON互換MIMEか確認します。
//
// 機能:
//   - application/jsonと+json suffixを許可し、空値や不正値を拒否する
//
// 引数:
//   - value string: Content-Typeヘッダー値
//
// 返り値:
//   - error: JSON互換でない場合のエラー。有効な場合はnil
func validateJSONContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
		return fmt.Errorf("Polymarket APIのContent-TypeがJSONではありません: %q", value)
	}
	return nil
}

// ----------------------------------------

// decodeJSON は、JSON数値を精度保持可能なjson.Numberでdecodeします。
//
// 機能:
//   - 空本文と複数JSON値を拒否する
//
// 引数:
//   - body []byte: 展開済みJSON本文
//
// 返り値:
//   - any: decode済みJSON値
//   - error: JSONが不正または余分な値を含む場合のエラー
func decodeJSON(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("Polymarket APIのJSONを解析できません: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("Polymarket APIのJSONに余分な値があります")
	}
	return result, nil
}

// ----------------------------------------

// newAPIError は、非2xx応答を本文なしのAPIErrorへ変換します。
//
// 機能:
//   - Retry-Afterを保持し、状態別の定型メッセージを設定する
//
// 引数:
//   - endpoint string: 実際に要求した固定系列path
//   - response *http.Response: 非2xx応答
//
// 返り値:
//   - *APIError: 公開可能な上流エラー
func newAPIError(endpoint string, response *http.Response) *APIError {
	message := "上流APIが要求を処理できませんでした"
	switch response.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		message = "上流APIが要求内容を受理しませんでした"
	case http.StatusUnauthorized, http.StatusForbidden:
		message = "公開APIの利用境界が変更されたか、現在利用できません"
	case http.StatusNotFound:
		message = "指定した公開データが見つかりません"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		message = "上流API要求が時間切れになりました"
	case http.StatusTooManyRequests:
		message = "上流APIのレート上限へ到達しました"
	}
	return &APIError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After"), Message: message, Endpoint: endpoint}
}

// ----------------------------------------

// responseSourceURL は、最終応答URLを公開metadata用に整えます。
//
// 機能:
//   - 通常redirect後のURLと公開queryを保持し、userinfoとfragmentだけを除く
//
// 引数:
//   - response *http.Response: HTTP clientが返した最終応答
//   - fallback string: 最終要求URLがない場合の取得元
//
// 返り値:
//   - string: 公開metadataへ格納できる取得元URL
func responseSourceURL(response *http.Response, fallback string) string {
	if response == nil || response.Request == nil || response.Request.URL == nil {
		return fallback
	}
	value := *response.Request.URL
	value.User = nil
	value.Fragment = ""
	return value.String()
}
