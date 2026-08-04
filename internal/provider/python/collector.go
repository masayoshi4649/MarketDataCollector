// Package python は、許可済みPythonライブラリを安全な子プロセス経由で利用します。
package python

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
	"github.com/masayoshi4649/MarketDataCollector/internal/strictjson"
)

const (
	maximumStderrBytes            = int64(8 * 1024)
	pythonExitInvalidArgument     = 2
	pythonExitProviderUnavailable = 3
	pythonExitUpstreamError       = 4
	pythonProcessWaitDelay        = 2 * time.Second
)

var pythonEnvironmentAllowlist = []string{
	"SystemRoot", "WINDIR", "COMSPEC", "PATH", "PATHEXT",
	"TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "TZ",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE",
}

// Runner は、Pythonアダプターへ1件の収集要求を渡す機能を表します。
//
// 主な特徴:
//   - 実運用ではSubprocessRunnerを利用する
//   - テストでは外部プロセスを起動しない差し替え実装を利用できる
type Runner interface {
	Collect(context.Context, string, string, map[string]any) (domain.ProviderResult, error)
}

// Collector は、1つのPython providerを共通サービスへ登録します。
type Collector struct {
	providerName string
	runner       Runner
}

// NewCollector は、yfinanceまたはinvestingpyのコレクターを生成します。
//
// 引数:
//   - providerName string: yfinanceまたはinvestingpyの外部識別子。
//   - runner Runner: Pythonアダプターの実行機能。
//
// 返り値:
//   - *Collector: 共通サービスへ登録できるproviderコレクター。
//   - error: provider名が未対応、またはrunnerがnilの場合のエラー。
func NewCollector(providerName string, runner Runner) (*Collector, error) {
	if providerName != "yfinance" && providerName != "investingpy" {
		return nil, fmt.Errorf("未対応のPython providerです: %q", providerName)
	}
	if isNilRunner(runner) {
		return nil, errors.New("Pythonアダプター実行機能がありません")
	}
	return &Collector{providerName: providerName, runner: runner}, nil
}

// Descriptor は、Python providerの固定データセット仕様を返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - domain.ProviderDescriptor: 通信せずに返せる入力仕様。
func (c *Collector) Descriptor() domain.ProviderDescriptor {
	if c.providerName == "yfinance" {
		return yfinanceDescriptor()
	}
	return investingpyDescriptor()
}

// Collect は、固定データセットだけをPythonアダプターへ渡します。
//
// 引数:
//   - ctx context.Context: 子プロセスの期限とキャンセルを伝えるコンテキスト。
//   - dataset string: datalistに掲載されたデータセット識別子。
//   - parameters map[string]any: データセット固有の入力項目。
//
// 返り値:
//   - domain.ProviderResult: Python側で標準JSONへ正規化した収集結果。
//   - error: 入力、起動、ライブラリ、上流取得のエラー。
func (c *Collector) Collect(
	ctx context.Context,
	dataset string,
	parameters map[string]any,
) (domain.ProviderResult, error) {
	if !hasDataset(c.Descriptor().Datasets, dataset) {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorNotFound,
			fmt.Sprintf("provider %qにdataset %qはありません", c.providerName, dataset),
			nil,
		)
	}
	return c.runner.Collect(ctx, c.providerName, dataset, parameters)
}

// ----------------------------------------

// isNilRunner は、interfaceへ格納された型付きnilを含めて検出します。
//
// 引数:
//   - runner Runner: Pythonアダプターの実行機能。
//
// 返り値:
//   - bool: runnerがnilまたは型付きnilの場合はtrue。
func isNilRunner(runner Runner) bool {
	if runner == nil {
		return true
	}
	value := reflect.ValueOf(runner)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ----------------------------------------

// SubprocessRunner は、要求ごとに分離したPython子プロセスを実行します。
type SubprocessRunner struct {
	executable       string
	script           string
	timeout          time.Duration
	maxResponseBytes int64
	processSlots     chan struct{}
}

type bridgeRequest struct {
	Provider   string         `json:"provider"`
	Dataset    string         `json:"dataset"`
	Parameters map[string]any `json:"parameters"`
}

type bridgeResponse struct {
	Data     json.RawMessage `json:"data"`
	Metadata map[string]any  `json:"metadata"`
}

type bridgeErrorResponse struct {
	Error json.RawMessage `json:"error"`
}

type bridgeError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// NewSubprocessRunner は、実行パスと出力制限を検証してrunnerを生成します。
//
// 引数:
//   - executable string: Python実行ファイル名または絶対パス。
//   - script string: stdin/stdoutアダプタースクリプトのパス。
//   - timeout time.Duration: 1件のPython処理を中止する期限。
//   - maxResponseBytes int64: 標準出力として受け付ける最大バイト数。
//   - maxConcurrentProcesses int: 同時に起動できるPythonプロセス数。
//
// 返り値:
//   - *SubprocessRunner: provider間で共有できる子プロセスrunner。
//   - error: 必須値または制限値が不正な場合のエラー。
func NewSubprocessRunner(
	executable string,
	script string,
	timeout time.Duration,
	maxResponseBytes int64,
	maxConcurrentProcesses int,
) (*SubprocessRunner, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("Python実行ファイルが指定されていません")
	}
	if strings.TrimSpace(script) == "" {
		return nil, errors.New("Pythonアダプタースクリプトが指定されていません")
	}
	if timeout <= 0 {
		return nil, errors.New("Python処理期限は0秒より長くしてください")
	}
	if maxResponseBytes < 1 {
		return nil, errors.New("Python応答上限は1以上にしてください")
	}
	if maxConcurrentProcesses < 1 {
		return nil, errors.New("Python同時プロセス数は1以上にしてください")
	}
	return &SubprocessRunner{
		executable: executable, script: script, timeout: timeout, maxResponseBytes: maxResponseBytes,
		processSlots: make(chan struct{}, maxConcurrentProcesses),
	}, nil
}

// Collect は、JSONをstdinへ渡し、stdoutの厳密JSONを共通結果へ変換します。
//
// 引数:
//   - ctx context.Context: 呼び出し元の期限とキャンセルを伝えるコンテキスト。
//   - providerName string: yfinanceまたはinvestingpy。
//   - dataset string: Python側の固定許可リストにあるデータセット。
//   - parameters map[string]any: データセット固有の入力項目。
//
// 返り値:
//   - domain.ProviderResult: Python側で正規化済みのデータとライブラリ情報。
//   - error: JSON変換、起動、期限、出力上限、応答形式のエラー。
func (r *SubprocessRunner) Collect(
	ctx context.Context,
	providerName string,
	dataset string,
	parameters map[string]any,
) (domain.ProviderResult, error) {
	requestBody, err := json.Marshal(bridgeRequest{
		Provider: providerName, Dataset: dataset, Parameters: parameters,
	})
	if err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorInvalidArgument,
			"parametersをJSONへ変換できません",
			err,
		)
	}

	processContext, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	select {
	case r.processSlots <- struct{}{}:
		defer func() { <-r.processSlots }()
	case <-processContext.Done():
		if errors.Is(processContext.Err(), context.DeadlineExceeded) {
			return domain.ProviderResult{}, domain.NewServiceError(
				domain.ErrorTimeout,
				fmt.Sprintf("provider %qのPython実行待機が制限時間を超えました", providerName),
				processContext.Err(),
			)
		}
		return domain.ProviderResult{}, processContext.Err()
	}
	command := exec.CommandContext(processContext, r.executable, "-I", r.script)
	command.Env = safePythonEnvironment()
	command.WaitDelay = pythonProcessWaitDelay
	command.Stdin = bytes.NewReader(requestBody)
	stdout := newLimitedBuffer(r.maxResponseBytes)
	stderr := newLimitedBuffer(maximumStderrBytes)
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	if errors.Is(processContext.Err(), context.DeadlineExceeded) {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorTimeout,
			fmt.Sprintf("provider %qの収集処理が制限時間を超えました", providerName),
			runErr,
		)
	}
	if errors.Is(processContext.Err(), context.Canceled) {
		return domain.ProviderResult{}, processContext.Err()
	}
	if stdout.Exceeded() {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			fmt.Sprintf("provider %qの応答がサイズ上限を超えました", providerName),
			runErr,
		)
	}
	if runErr != nil {
		kind := domain.ErrorProviderUnavailable
		message := fmt.Sprintf("provider %qのPython実行環境を利用できません", providerName)
		expectedBridgeKind := string(domain.ErrorProviderUnavailable)
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			switch exitErr.ExitCode() {
			case pythonExitInvalidArgument:
				kind = domain.ErrorInvalidArgument
				message = fmt.Sprintf("provider %qの入力が不正です", providerName)
				expectedBridgeKind = string(domain.ErrorInvalidArgument)
			case pythonExitProviderUnavailable:
				kind = domain.ErrorProviderUnavailable
			case pythonExitUpstreamError:
				kind = domain.ErrorUpstream
				message = fmt.Sprintf("provider %qから情報を収集できません", providerName)
				expectedBridgeKind = string(domain.ErrorUpstream)
			}
		}
		if bridgeErrorValue, decodeErr := decodeBridgeError(stdout.Bytes()); decodeErr == nil &&
			bridgeErrorValue.Kind == expectedBridgeKind {
			message = bridgeErrorValue.Message
		}
		cause := fmt.Errorf("%w: Python標準エラー=%dバイト", runErr, len(stderr.Bytes()))
		return domain.ProviderResult{}, domain.NewServiceError(
			kind,
			message,
			cause,
		)
	}

	var response bridgeResponse
	if err := strictjson.DecodeObject(stdout.Bytes(), &response, "data", "metadata"); err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			fmt.Sprintf("provider %qのPython応答がJSONではありません", providerName),
			err,
		)
	}
	if response.Metadata == nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			fmt.Sprintf("provider %qのPython応答にmetadataがありません", providerName),
			nil,
		)
	}
	if len(response.Data) == 0 {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			fmt.Sprintf("provider %qのPython応答にdataがありません", providerName),
			nil,
		)
	}
	dataDecoder := json.NewDecoder(bytes.NewReader(response.Data))
	dataDecoder.UseNumber()
	var data any
	if err := dataDecoder.Decode(&data); err != nil {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			fmt.Sprintf("provider %qのPython応答dataが不正です", providerName),
			err,
		)
	}
	if err := dataDecoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.ProviderResult{}, domain.NewServiceError(
			domain.ErrorProviderUnavailable,
			fmt.Sprintf("provider %qのPython応答dataに余分なJSON値があります", providerName),
			err,
		)
	}
	return domain.ProviderResult{Data: data, Metadata: response.Metadata}, nil
}

// ----------------------------------------

// decodeBridgeError は、Python失敗時の厳密JSONを検証して共通分類を返します。
//
// 引数:
//   - data []byte: Python標準出力にある失敗JSON。
//
// 返り値:
//   - bridgeError: 検証済みの分類と公開メッセージ。
//   - error: JSON形式、未知項目、必須値、余分なJSON値が不正な場合のエラー。
func decodeBridgeError(data []byte) (bridgeError, error) {
	var response bridgeErrorResponse
	if err := strictjson.DecodeObject(data, &response, "error"); err != nil {
		return bridgeError{}, err
	}
	var result bridgeError
	if err := strictjson.DecodeObject(response.Error, &result, "kind", "message"); err != nil {
		return bridgeError{}, err
	}
	result.Kind = strings.TrimSpace(result.Kind)
	result.Message = strings.TrimSpace(result.Message)
	if result.Kind == "" || result.Message == "" {
		return bridgeError{}, errors.New("Python失敗応答のkindまたはmessageが空です")
	}
	return result, nil
}

// safePythonEnvironment は、Python子プロセスへ渡してよい環境変数だけを複製します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - []string: 許可リスト外の値やPYTHONPATH等を除外した環境変数一覧。
func safePythonEnvironment() []string {
	result := make([]string, 0, len(pythonEnvironmentAllowlist))
	for _, name := range pythonEnvironmentAllowlist {
		if value, exists := os.LookupEnv(name); exists {
			result = append(result, name+"="+value)
		}
	}
	return result
}

// ----------------------------------------

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

// newLimitedBuffer は、保持する最大バイト数を指定してbufferを生成します。
//
// 引数:
//   - limit int64: メモリへ保持する最大バイト数。
//
// 返り値:
//   - *limitedBuffer: 超過分を破棄しながら検出できるwriter。
func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

// Write は、上限まで保持しつつ超過分を破棄して子プロセスを継続させます。
//
// 引数:
//   - data []byte: 子プロセスが書き込んだバイト列。
//
// 返り値:
//   - int: 子プロセスへ通知する受理済みバイト数。
//   - error: 常にnil。超過はExceededで後から確認する。
func (b *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.exceeded = true
		return originalLength, nil
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
		b.exceeded = true
	}
	_, _ = b.buffer.Write(data)
	return originalLength, nil
}

// Bytes は、上限内で保持したバイト列を返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - []byte: bufferが保持するバイト列。
func (b *limitedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

// String は、上限内で保持した内容を文字列として返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - string: bufferが保持するUTF-8想定の文字列。
func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

// Exceeded は、書き込みが上限を超えたか返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - bool: 1バイト以上破棄した場合はtrue。
func (b *limitedBuffer) Exceeded() bool {
	return b.exceeded
}

// ----------------------------------------

// hasDataset は、固定仕様にdatasetが存在するか確認します。
//
// 引数:
//   - datasets []domain.DatasetDescriptor: providerの固定データセット一覧。
//   - name string: 検索するdataset識別子。
//
// 返り値:
//   - bool: 完全一致するdatasetが存在する場合はtrue。
func hasDataset(datasets []domain.DatasetDescriptor, name string) bool {
	for _, dataset := range datasets {
		if dataset.Name == name {
			return true
		}
	}
	return false
}

// yfinanceDescriptor は、yfinanceの固定データセット仕様を生成します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - domain.ProviderDescriptor: yfinanceの10データセットと入力仕様。
func yfinanceDescriptor() domain.ProviderDescriptor {
	ticker := pythonParameter("ticker", "string", true, "Yahoo Financeの銘柄ティッカー。最大128文字。", nil, nil)
	historyOptions := []domain.ParameterDescriptor{
		ticker,
		pythonParameter("period", "string", false, "start/end未指定時の取得期間。", []string{"1d", "5d", "1mo", "3mo", "6mo", "1y", "2y", "5y", "10y", "ytd", "max"}, "1mo"),
		pythonParameter("start", "string", false, "取得開始日。開始日は含みます。", nil, nil),
		pythonParameter("end", "string", false, "取得終了日。終了日は含みません。", nil, nil),
		pythonParameter("interval", "string", false, "価格間隔。分足は直近60日以内です。", []string{"1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "5d", "1wk", "1mo", "3mo"}, "1d"),
		pythonParameter("prepost", "boolean", false, "時間外取引を含めます。", nil, false),
		pythonParameter("actions", "boolean", false, "配当と分割列を含めます。", nil, true),
		pythonParameter("auto_adjust", "boolean", false, "OHLCを自動調整します。", nil, true),
		pythonParameter("back_adjust", "boolean", false, "過去方向へ調整します。", nil, false),
		pythonParameter("repair", "boolean", false, "既知の価格異常を補正します。", nil, false),
		pythonParameter("keepna", "boolean", false, "欠損行を保持します。", nil, false),
		pythonParameter("rounding", "boolean", false, "価格値を丸めます。", nil, false),
		pythonParameter("timeout", "number", false, "上流通信のタイムアウト秒数。0より大きく300以下。", nil, 10),
		pythonParameter("raise_errors", "boolean", false, "yfinance内部の取得エラーを送出します。", nil, false),
	}
	downloadOptions := []domain.ParameterDescriptor{
		pythonParameter("tickers", "string|array<string>", true, "銘柄ティッカーまたはその配列。最大100件、各128文字。", nil, nil),
		pythonParameter("start", "string", false, "取得開始日。開始日は含みます。", nil, nil),
		pythonParameter("end", "string", false, "取得終了日。終了日は含みません。", nil, nil),
		pythonParameter("actions", "boolean", false, "配当と分割列を含めます。", nil, false),
		pythonParameter("threads", "boolean|integer", false, "並列ダウンロードを有効化するか、1から32のスレッド数を指定します。", nil, true),
		pythonParameter("ignore_tz", "boolean", false, "銘柄間のタイムゾーン差を無視します。", nil, nil),
		pythonParameter("group_by", "string", false, "複数銘柄列のグループ方法。", []string{"column", "ticker"}, "column"),
		pythonParameter("auto_adjust", "boolean", false, "OHLCを自動調整します。", nil, true),
		pythonParameter("back_adjust", "boolean", false, "過去方向へ調整します。", nil, false),
		pythonParameter("repair", "boolean", false, "既知の価格異常を補正します。", nil, false),
		pythonParameter("keepna", "boolean", false, "欠損行を保持します。", nil, false),
		pythonParameter("progress", "boolean", false, "進捗表示を有効にします。", nil, true),
		pythonParameter("period", "string", false, "start/end未指定時の取得期間。", []string{"1d", "5d", "1mo", "3mo", "6mo", "1y", "2y", "5y", "10y", "ytd", "max"}, "1mo"),
		pythonParameter("interval", "string", false, "価格間隔。分足は直近60日以内です。", []string{"1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "5d", "1wk", "1mo", "3mo"}, "1d"),
		pythonParameter("prepost", "boolean", false, "時間外取引を含めます。", nil, false),
		pythonParameter("rounding", "boolean", false, "価格値を丸めます。", nil, false),
		pythonParameter("timeout", "number", false, "上流通信のタイムアウト秒数。0より大きく300以下。", nil, 10),
		pythonParameter("multi_level_index", "boolean", false, "複数階層の列索引で返します。", nil, true),
	}
	return domain.ProviderDescriptor{
		Name:        "yfinance",
		DisplayName: "yfinance / Yahoo Finance",
		Description: "yfinanceを利用してYahoo Financeの情報を要求時に取得します。",
		Datasets: []domain.DatasetDescriptor{
			{Name: "quote", Description: "銘柄の基本情報を返します。", Parameters: []domain.ParameterDescriptor{ticker}},
			{Name: "history", Description: "単一銘柄の価格履歴を返します。", Parameters: historyOptions},
			{Name: "actions", Description: "配当・分割などの企業行動を返します。", Parameters: []domain.ParameterDescriptor{ticker, pythonParameter("period", "string", false, "取得期間。", []string{"1d", "5d", "1mo", "3mo", "6mo", "1y", "2y", "5y", "10y", "ytd", "max"}, "max")}},
			{Name: "financials", Description: "年次または四半期の財務諸表を返します。", Parameters: []domain.ParameterDescriptor{
				ticker,
				pythonParameter("statement", "string", false, "財務諸表の種類。", []string{"all", "income", "balance_sheet", "cash_flow"}, "all"),
				pythonParameter("frequency", "string", false, "財務諸表の頻度。", []string{"annual", "quarterly"}, "annual"),
			}},
			{Name: "analysis", Description: "アナリスト予想・推奨情報を返します。", Parameters: []domain.ParameterDescriptor{
				ticker, pythonParameter("section", "string", false, "分析情報の種類。", []string{"all", "analyst_price_targets", "earnings_estimate", "revenue_estimate", "earnings_history", "eps_trend", "eps_revisions", "growth_estimates", "recommendations"}, "all"),
			}},
			{Name: "holders", Description: "主要・機関・投信・インサイダー保有情報を返します。", Parameters: []domain.ParameterDescriptor{
				ticker, pythonParameter("section", "string", false, "保有者情報の種類。", []string{"all", "major", "institutional", "mutualfund", "insider_transactions", "insider_purchases", "insider_roster"}, "all"),
			}},
			{Name: "options", Description: "オプション満期一覧または指定日のチェーンを返します。", Parameters: []domain.ParameterDescriptor{
				ticker, pythonParameter("date", "string", false, "オプション満期日。省略時は満期一覧。", nil, nil),
			}},
			{Name: "news", Description: "銘柄関連ニュースを返します。", Parameters: []domain.ParameterDescriptor{
				ticker, pythonParameter("count", "integer", false, "最大件数。1以上100以下。", nil, 10),
				pythonParameter("tab", "string", false, "ニュース分類。", []string{"news", "all", "press releases"}, "news"),
			}},
			{Name: "search", Description: "銘柄・ニュース等を横断検索します。", Parameters: []domain.ParameterDescriptor{
				pythonParameter("query", "string", true, "検索語。最大512文字。", nil, nil),
				pythonParameter("max_results", "integer", false, "銘柄検索の最大件数。0以上100以下。", nil, 8),
				pythonParameter("news_count", "integer", false, "ニュース最大件数。0以上100以下。", nil, 8),
				pythonParameter("lists_count", "integer", false, "リスト最大件数。0以上100以下。", nil, 8),
				pythonParameter("include_cb", "boolean", false, "カンファレンスボード情報を含めます。", nil, true),
				pythonParameter("include_nav_links", "boolean", false, "ナビゲーション候補を含めます。", nil, false),
				pythonParameter("include_research", "boolean", false, "調査情報を含めます。", nil, false),
				pythonParameter("include_cultural_assets", "boolean", false, "文化関連候補を含めます。", nil, false),
				pythonParameter("enable_fuzzy_query", "boolean", false, "あいまい検索を有効にします。", nil, false),
				pythonParameter("recommended", "integer", false, "推奨候補の最大件数。0以上100以下。", nil, 8),
				pythonParameter("timeout", "number", false, "上流通信のタイムアウト秒数。0より大きく300以下。", nil, 30),
				pythonParameter("raise_errors", "boolean", false, "yfinance内部の取得エラーを送出します。", nil, true),
			}},
			{Name: "download", Description: "複数銘柄の価格履歴を一括取得します。", Parameters: downloadOptions},
		},
	}
}

// investingpyDescriptor は、investpy互換providerの固定仕様を生成します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - domain.ProviderDescriptor: investingpy識別子で公開する9データセット。
func investingpyDescriptor() domain.ProviderDescriptor {
	products := []string{"stock", "etf", "fund", "index", "currency_cross", "commodity", "bond", "certificate", "crypto"}
	product := pythonParameter("product", "string", true, "金融商品の種類。", products, nil)
	technicalProduct := product
	technicalProduct.Allowed = []string{
		"stock", "etf", "fund", "index", "currency_cross", "commodity", "bond", "certificate",
	}
	name := pythonParameter("name", "string", true, "金融商品の名称またはシンボル。", nil, nil)
	namedCountry := pythonParameter("country", "string", false, "stock、etf、fund、index、certificateでは必須、commodityでは任意です。currency_cross、bond、cryptoには指定できません。", nil, nil)
	technicalCountry := pythonParameter("country", "string", false, "currency_cross、commodityでは任意、その他の対応商品では必須です。", nil, nil)
	return domain.ProviderDescriptor{
		Name:        "investingpy",
		DisplayName: "investpy / Investing.com",
		Description: "Investing.comから情報を取得します。",
		Datasets: []domain.DatasetDescriptor{
			{Name: "search", Description: "商品種別を指定して銘柄を横断検索します。", Parameters: []domain.ParameterDescriptor{
				product, pythonParameter("query", "string", true, "検索語。", nil, nil),
				pythonParameter("country", "string|array<string>", false, "対象国または対象国一覧。", nil, nil),
				pythonParameter("n_results", "integer", false, "最大件数。1以上1000以下。", nil, nil),
			}},
			{Name: "recent", Description: "指定商品の直近価格を返します。", Parameters: []domain.ParameterDescriptor{
				product, name, namedCountry, pythonParameter("order", "string", false, "並び順。", []string{"ascending", "descending"}, "ascending"),
				pythonParameter("interval", "string", false, "価格間隔。", []string{"Daily", "Weekly", "Monthly"}, "Daily"),
			}},
			{Name: "historical", Description: "指定商品の期間価格を返します。", Parameters: []domain.ParameterDescriptor{
				product, name, namedCountry,
				pythonParameter("from_date", "string", true, "開始日。dd/mm/yyyy形式。", nil, nil),
				pythonParameter("to_date", "string", true, "終了日。dd/mm/yyyy形式。", nil, nil),
				pythonParameter("order", "string", false, "並び順。", []string{"ascending", "descending"}, "ascending"),
				pythonParameter("interval", "string", false, "価格間隔。", []string{"Daily", "Weekly", "Monthly"}, "Daily"),
			}},
			{Name: "information", Description: "指定商品の基本情報を返します。", Parameters: []domain.ParameterDescriptor{product, name, namedCountry}},
			{Name: "overview", Description: "商品種別ごとの市場概要を返します。", Parameters: []domain.ParameterDescriptor{
				product,
				pythonParameter("country", "string", false, "stock、etf、fund、index、bond、certificateでは必須です。", nil, nil),
				pythonParameter("currency", "string", false, "currency_crossでは必須の基準通貨です。", nil, nil),
				pythonParameter("group", "string", false, "commodityでは必須の商品グループです。", nil, nil),
				pythonParameter("n_results", "integer", false, "bond以外で利用できる最大件数。1以上1000以下。", nil, 100),
			}},
			{Name: "economic_calendar", Description: "経済指標カレンダーを返します。", Parameters: []domain.ParameterDescriptor{
				pythonParameter("time_zone", "string", false, "Investing.comで利用できるタイムゾーン。", nil, nil),
				pythonParameter("time_filter", "string", false, "時刻の表示方法。", []string{"time_only", "time_remain"}, "time_only"),
				pythonParameter("from_date", "string", false, "開始日。dd/mm/yyyy形式。", nil, nil),
				pythonParameter("to_date", "string", false, "終了日。dd/mm/yyyy形式。", nil, nil),
				pythonParameter("countries", "array<string>", false, "対象国一覧。", nil, nil),
				pythonParameter("importances", "array<string>", false, "重要度一覧。", []string{"low", "medium", "high"}, nil),
				pythonParameter("categories", "array<string>", false, "経済指標の分類一覧。", nil, nil),
			}},
			{Name: "technical_indicators", Description: "指定商品のテクニカル指標を返します。", Parameters: technicalParameters(technicalProduct, name, technicalCountry)},
			{Name: "moving_averages", Description: "指定商品の移動平均を返します。", Parameters: technicalParameters(technicalProduct, name, technicalCountry)},
			{Name: "pivot_points", Description: "指定商品のピボットポイントを返します。", Parameters: technicalParameters(technicalProduct, name, technicalCountry)},
		},
	}
}

// technicalParameters は、investpyテクニカル系の共通入力仕様を生成します。
//
// 引数:
//   - product domain.ParameterDescriptor: 商品種別項目。
//   - name domain.ParameterDescriptor: 商品名項目。
//   - country domain.ParameterDescriptor: 国項目。
//
// 返り値:
//   - []domain.ParameterDescriptor: intervalを含む共通入力仕様。
func technicalParameters(
	product domain.ParameterDescriptor,
	name domain.ParameterDescriptor,
	country domain.ParameterDescriptor,
) []domain.ParameterDescriptor {
	return []domain.ParameterDescriptor{
		product, name, country,
		pythonParameter("interval", "string", false, "テクニカル分析の間隔。", []string{"5mins", "15mins", "30mins", "1hour", "5hours", "daily", "weekly", "monthly"}, "daily"),
	}
}

// pythonParameter は、Python providerの入力項目説明を生成します。
//
// 引数:
//   - name string: JSON項目名。
//   - valueType string: JSON上の値型。
//   - required bool: 必須入力かどうか。
//   - description string: 利用者向け説明。
//   - allowed []string: 省略可能な許可値一覧。
//   - defaultValue any: 省略時の既定値。
//
// 返り値:
//   - domain.ParameterDescriptor: datalistへ掲載する入力仕様。
func pythonParameter(
	name string,
	valueType string,
	required bool,
	description string,
	allowed []string,
	defaultValue any,
) domain.ParameterDescriptor {
	return domain.ParameterDescriptor{
		Name: name, Type: valueType, Required: required, Description: description,
		Allowed: allowed, Default: defaultValue,
	}
}
