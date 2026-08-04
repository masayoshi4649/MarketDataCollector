// Package nikkei225jp は、225225.jpの公開ページを低負荷で取得して構造化します。
package nikkei225jp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL               = "https://225225.jp"
	DefaultCurrentPath           = "/_data/_nfsDATA/ajaxindex/ajax_TOP.js"
	DefaultReferer               = "https://225225.jp/"
	DefaultUserAgent             = "225225jp-lowload-go/2.0"
	DefaultMaxResponseBytes      = int64(4 * 1024 * 1024)
	DefaultMaxChartResponseBytes = int64(32 * 1024 * 1024)
)

const defaultTimeout = 10 * time.Second

var currentLinePattern = regexp.MustCompile(`(?m)^A\[(\d+)\]="([^"\r\n]*)";\r?$`)

// Config は、ClientのHTTP取得設定を表します。
//
// 主な特徴:
//   - ゼロ値の項目には低負荷取得向けの既定値を適用する
//   - BaseURLとCurrentPathはテスト用サーバーへ差し替えられる
//   - HTTPClientを省略した場合は10秒タイムアウトを設定する
type Config struct {
	BaseURL               string
	CurrentPath           string
	Referer               string
	HTTPClient            *http.Client
	UserAgent             string
	MaxResponseBytes      int64
	MaxChartResponseBytes int64
}

// Client は、数値データ配信を直列に都度取得するHTTPクライアントです。
//
// 主な特徴:
//   - 同一Client内の取得を直列化する
//   - 取得要求ごとに上流へ無条件GETを送信する
//   - JavaScript本文を実行せず、定義済み形式だけを解析する
type Client struct {
	baseURL               *url.URL
	currentPath           string
	referer               string
	httpClient            *http.Client
	userAgent             string
	maxResponseBytes      int64
	maxChartResponseBytes int64
	requestSlot           chan struct{}
}

// ----------------------------------------

// NewClient は、設定を検証して低負荷取得用Clientを生成します。
//
// 引数:
//   - config Config: 接続先、HTTPクライアント、UA、本文上限の設定。
//
// 返り値:
//   - *Client: 上流データを都度取得するクライアント。
//   - error: URLまたは設定値が不正な場合のエラー。
func NewClient(config Config) (*Client, error) {
	if config.BaseURL == "" {
		config.BaseURL = DefaultBaseURL
	}
	if config.CurrentPath == "" {
		config.CurrentPath = DefaultCurrentPath
	}
	if config.Referer == "" {
		config.Referer = DefaultReferer
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: defaultTimeout}
	}
	httpClient := *config.HTTPClient
	httpClient.CheckRedirect = rejectRedirect
	if config.UserAgent == "" {
		config.UserAgent = DefaultUserAgent
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("本文サイズ上限は1以上で指定してください")
	}
	if config.MaxChartResponseBytes == 0 {
		config.MaxChartResponseBytes = DefaultMaxChartResponseBytes
	}
	if config.MaxChartResponseBytes < 1 {
		return nil, errors.New("チャート本文サイズ上限は1以上で指定してください")
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("ベースURLを解析できません: %w", err)
	}
	if baseURL.Scheme != "https" && baseURL.Scheme != "http" {
		return nil, errors.New("ベースURLのスキームはhttpまたはhttpsにしてください")
	}
	if baseURL.Host == "" {
		return nil, errors.New("ベースURLにホストがありません")
	}
	if baseURL.User != nil {
		return nil, errors.New("ベースURLにuserinfoを含めることはできません")
	}
	if baseURL.EscapedPath() != "" && baseURL.EscapedPath() != "/" {
		return nil, errors.New("ベースURLにパスを含めることはできません")
	}
	if baseURL.RawQuery != "" || baseURL.ForceQuery {
		return nil, errors.New("ベースURLにクエリを含めることはできません")
	}
	if baseURL.Fragment != "" {
		return nil, errors.New("ベースURLにフラグメントを含めることはできません")
	}
	baseURL.Path = ""
	baseURL.RawPath = ""
	if err := validateResourcePath(config.CurrentPath); err != nil {
		return nil, fmt.Errorf("現在値パスが不正です: %w", err)
	}

	return &Client{
		baseURL:               baseURL,
		currentPath:           config.CurrentPath,
		referer:               config.Referer,
		httpClient:            &httpClient,
		userAgent:             config.UserAgent,
		maxResponseBytes:      config.MaxResponseBytes,
		maxChartResponseBytes: config.MaxChartResponseBytes,
		requestSlot:           make(chan struct{}, 1),
	}, nil
}

// validateResourcePath は、同一ホスト内で解決できる絶対パスか検証します。
//
// 引数:
//   - resourcePath string: 検証するHTTP取得先のパス。
//
// 返り値:
//   - error: パスがスラッシュで始まらない場合、またはホスト指定を含む場合のエラー。
func validateResourcePath(resourcePath string) error {
	if !strings.HasPrefix(resourcePath, "/") {
		return errors.New("パスはスラッシュから開始してください")
	}
	reference, err := url.Parse(resourcePath)
	if err != nil {
		return fmt.Errorf("パスを解析できません: %w", err)
	}
	if reference.IsAbs() || reference.Host != "" {
		return errors.New("パスに取得先ホストを指定できません")
	}
	return nil
}

// rejectRedirect は、取得先が別URLへ変わるHTTPリダイレクトを停止します。
//
// 引数:
//   - request *http.Request: リダイレクト後に送信される予定だった要求。
//   - via []*http.Request: それ以前に送信された要求の一覧。
//
// 返り値:
//   - error: リダイレクト追跡を停止するためのhttp.ErrUseLastResponse。
func rejectRedirect(request *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

// ----------------------------------------

// FetchCurrent は、全銘柄を1回のHTTP GETで取得して安全に解析します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - CurrentData: 銘柄コード順の現在値とHTTP付帯情報。
//   - error: 通信、HTTP状態、本文上限、形式検証のいずれかに失敗した場合のエラー。
func (c *Client) FetchCurrent(ctx context.Context) (CurrentData, error) {
	release, err := c.acquireRequestSlot(ctx)
	if err != nil {
		return CurrentData{}, err
	}
	defer release()

	requestURL, err := c.resolveResourceURL(c.currentPath)
	if err != nil {
		return CurrentData{}, err
	}
	body, metadata, err := c.fetchLocked(
		ctx,
		requestURL,
		"現在値",
		c.maxResponseBytes,
	)
	if err != nil {
		return CurrentData{}, err
	}

	quotes, err := parseCurrent(body)
	if err != nil {
		return CurrentData{}, fmt.Errorf("現在値本文を解析できません: %w", err)
	}

	return CurrentData{Metadata: metadata, Quotes: quotes}, nil
}

// acquireRequestSlot は、Client内で共有するHTTP取得枠を確保します。
//
// 引数:
//   - ctx context.Context: 待機のキャンセルと期限を制御するコンテキスト。
//
// 返り値:
//   - func(): 確保した取得枠を解放する関数。
//   - error: コンテキストにより待機を中止した場合のエラー。
func (c *Client) acquireRequestSlot(ctx context.Context) (func(), error) {
	select {
	case c.requestSlot <- struct{}{}:
		return func() {
			<-c.requestSlot
		}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("取得順番の待機を中止しました: %w", ctx.Err())
	}
}

// resolveResourceURL は、同一ホスト内のパスを絶対URLへ変換します。
//
// 引数:
//   - resourcePath string: ベースURLと同じホストから取得する絶対パス。
//
// 返り値:
//   - string: 検証済みの取得先絶対URL。
//   - error: パスまたは解決後のホストが不正な場合のエラー。
func (c *Client) resolveResourceURL(resourcePath string) (string, error) {
	if err := validateResourcePath(resourcePath); err != nil {
		return "", err
	}
	resolved := c.baseURL.ResolveReference(&url.URL{Path: resourcePath})
	if resolved.Host != c.baseURL.Host || resolved.Scheme != c.baseURL.Scheme {
		return "", errors.New("取得先はベースURLと同一ホストにしてください")
	}
	return resolved.String(), nil
}

// fetchLocked は、HTTP GETを1回実行します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - requestURL string: 取得対象の絶対URL。
//   - resourceName string: エラー表示に利用する取得対象の名称。
//   - maxResponseBytes int64: 許容する本文サイズの上限。
//
// 返り値:
//   - []byte: 200応答の本文。
//   - ResponseMetadata: HTTP応答の付帯情報。
//   - error: 要求作成、通信、HTTP状態、MIME、本文上限のエラー。
func (c *Client) fetchLocked(
	ctx context.Context,
	requestURL string,
	resourceName string,
	maxResponseBytes int64,
) ([]byte, ResponseMetadata, error) {
	return c.fetchWithRefererLocked(
		ctx,
		requestURL,
		resourceName,
		maxResponseBytes,
		c.referer,
	)
}

// fetchWithRefererLocked は、指定Refererを付けてHTTP GETを1回実行します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - requestURL string: 取得対象の絶対URL。
//   - resourceName string: エラー表示に利用する取得対象の名称。
//   - maxResponseBytes int64: 許容する本文サイズの上限。
//   - referer string: 対象配信が要求する同一サイト内の参照元URL。
//
// 返り値:
//   - []byte: 200応答の本文。
//   - ResponseMetadata: HTTP応答の付帯情報。
//   - error: 要求作成、通信、HTTP状態、MIME、本文上限のエラー。
func (c *Client) fetchWithRefererLocked(
	ctx context.Context,
	requestURL string,
	resourceName string,
	maxResponseBytes int64,
	referer string,
) ([]byte, ResponseMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, ResponseMetadata{}, fmt.Errorf("HTTP要求を作成できません: %w", err)
	}
	request.Header.Set("Accept", "application/json,application/javascript,text/javascript;q=0.9,*/*;q=0.1")
	request.Header.Set("User-Agent", c.userAgent)
	if referer != "" {
		request.Header.Set("Referer", referer)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, ResponseMetadata{}, fmt.Errorf("%sを取得できません: %w", resourceName, err)
	}
	defer response.Body.Close()

	metadata := responseMetadata(requestURL, response)
	if response.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return nil, ResponseMetadata{}, fmt.Errorf("%s配信がHTTP %dを返しました", resourceName, response.StatusCode)
	}
	if err := validateDataContentType(response.Header.Get("Content-Type")); err != nil {
		return nil, ResponseMetadata{}, err
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, ResponseMetadata{}, fmt.Errorf("%s本文を読み取れません: %w", resourceName, err)
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, ResponseMetadata{}, fmt.Errorf("%s本文が上限%dバイトを超えました", resourceName, maxResponseBytes)
	}
	metadata.ResponseBytes = int64(len(body))

	return body, metadata, nil
}

// responseMetadata は、HTTP応答ヘッダーから公開用の付帯情報を生成します。
//
// 引数:
//   - requestURL string: 実際に要求した絶対URL。
//   - response *http.Response: 取得済みHTTP応答。
//
// 返り値:
//   - ResponseMetadata: URL、取得時刻、検証子、キャッシュ指示を格納した値。
func responseMetadata(requestURL string, response *http.Response) ResponseMetadata {
	return ResponseMetadata{
		SourceURL:    requestURL,
		FetchedAt:    time.Now().UTC(),
		LastModified: response.Header.Get("Last-Modified"),
		ETag:         response.Header.Get("ETag"),
		CacheControl: response.Header.Get("Cache-Control"),
	}
}

// validateDataContentType は、数値データ配信として許容するMIME型か検証します。
//
// 引数:
//   - contentType string: Content-Typeヘッダーの値。
//
// 返り値:
//   - error: JSONまたはJavaScript系MIMEでない場合、またはMIMEを解析できない場合のエラー。
func validateDataContentType(contentType string) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("Content-Typeを解析できません: %w", err)
	}
	switch mediaType {
	case "application/json", "application/javascript", "text/javascript", "application/x-javascript":
		return nil
	default:
		return fmt.Errorf("想定外のContent-Typeです: %s", mediaType)
	}
}

// ----------------------------------------

// parseCurrent は、A[code]形式のJavaScript代入だけを現在値へ変換します。
//
// 引数:
//   - body []byte: JavaScriptとして配信された現在値本文。
//
// 返り値:
//   - []CurrentQuote: 銘柄コードの数値順に並べた現在値。
//   - error: 行形式、列数、数値、重複コードに異常がある場合のエラー。
func parseCurrent(body []byte) ([]CurrentQuote, error) {
	matches := currentLinePattern.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, errors.New("現在値の代入行がありません")
	}
	if remainder := strings.TrimSpace(string(currentLinePattern.ReplaceAll(body, nil))); remainder != "" {
		return nil, errors.New("未対応のJavaScript記述が含まれています")
	}

	quotes := make([]CurrentQuote, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		code := string(match[1])
		if _, exists := seen[code]; exists {
			return nil, fmt.Errorf("銘柄コード%sが重複しています", code)
		}
		seen[code] = struct{}{}

		fields := strings.Split(string(match[2]), "_")
		if len(fields) != 7 {
			return nil, fmt.Errorf("銘柄コード%sの列数が%dです", code, len(fields))
		}

		value, err := parseOptionalFloat(fields[0])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの現在値が不正です: %w", code, err)
		}
		change, err := parseOptionalFloat(fields[1])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの前日比が不正です: %w", code, err)
		}
		changePercent, err := parseOptionalFloat(fields[2])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの騰落率が不正です: %w", code, err)
		}
		displayStatus, err := parseOptionalInt(fields[4])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの表示状態が不正です: %w", code, err)
		}
		high, err := parseOptionalFloat(fields[5])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの高値が不正です: %w", code, err)
		}
		low, err := parseOptionalFloat(fields[6])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの安値が不正です: %w", code, err)
		}

		quotes = append(quotes, CurrentQuote{
			Code:          code,
			Name:          InstrumentName(code),
			Value:         value,
			Change:        change,
			ChangePercent: changePercent,
			MarketTime:    fields[3],
			DisplayStatus: displayStatus,
			High:          high,
			Low:           low,
		})
	}

	sort.Slice(quotes, func(i, j int) bool {
		left, leftErr := strconv.Atoi(quotes[i].Code)
		right, rightErr := strconv.Atoi(quotes[j].Code)
		if leftErr != nil || rightErr != nil {
			return quotes[i].Code < quotes[j].Code
		}
		return left < right
	})
	return quotes, nil
}

// parseOptionalFloat は、空欄をnilとして10進小数を解析します。
//
// 引数:
//   - raw string: 空文字、符号付き整数、または符号付き小数の文字列。
//
// 返り値:
//   - *float64: 空欄ならnil、それ以外は解析した数値へのポインター。
//   - error: float64として解析できない場合のエラー。
func parseOptionalFloat(raw string) (*float64, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, errors.New("有限値ではありません")
	}
	return &value, nil
}

// parseOptionalInt は、空欄をnilとして10進整数を解析します。
//
// 引数:
//   - raw string: 空文字または符号付き10進整数の文字列。
//
// 返り値:
//   - *int: 空欄ならnil、それ以外は解析した整数へのポインター。
//   - error: intとして解析できない場合のエラー。
func parseOptionalInt(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
