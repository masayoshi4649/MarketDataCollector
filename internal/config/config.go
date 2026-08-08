// Package config は、MarketDataCollector の設定ファイルを読み込み、検証する機能を提供します。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultSystemPort            = 8080
	defaultRequestTimeout        = 60 * time.Second
	defaultMaxRequestBytes int64 = 1 * 1024 * 1024

	defaultNikkei225JPBaseURL                     = "https://225225.jp"
	defaultNikkei225JPTimeout                     = 10 * time.Second
	defaultNikkei225JPUserAgent                   = "MarketDataCollector/0.1"
	defaultNikkei225JPMaxResponseBytes      int64 = 4 * 1024 * 1024
	defaultNikkei225JPMaxChartResponseBytes int64 = 32 * 1024 * 1024
	defaultJQuantsBaseURL                         = "https://api.jquants.com"
	defaultJQuantsPlan                            = "standard"
	defaultJQuantsTimeout                         = 30 * time.Second
	defaultJQuantsUserAgent                       = "MarketDataCollector/0.1"
	defaultJQuantsMaxResponseBytes          int64 = 16 * 1024 * 1024
	defaultPythonExecutable                       = "python"
	defaultPythonScript                           = "python/collector.py"
	defaultPythonTimeout                          = 60 * time.Second
	defaultPythonMaxResponseBytes           int64 = 16 * 1024 * 1024
	defaultPythonMaxConcurrentProcesses           = 2

	minRequestTimeout       = time.Second
	maxRequestTimeout       = 10 * time.Minute
	maxRequestBytes   int64 = 16 * 1024 * 1024

	minNikkei225JPTimeout                  = time.Second
	maxNikkei225JPTimeout                  = 5 * time.Minute
	maxNikkei225JPResponseBytes      int64 = 16 * 1024 * 1024
	maxNikkei225JPChartResponseBytes int64 = 64 * 1024 * 1024
	minJQuantsTimeout                      = time.Second
	maxJQuantsTimeout                      = 5 * time.Minute
	minJQuantsResponseBytes          int64 = 1 * 1024 * 1024
	maxJQuantsResponseBytes          int64 = 64 * 1024 * 1024
	minPythonTimeout                       = time.Second
	maxPythonTimeout                       = 10 * time.Minute
	maxPythonResponseBytes           int64 = 64 * 1024 * 1024
	maxPythonConcurrentProcesses           = 8
)

// Duration は、TOML の文字列で指定された期間を保持します。
type Duration struct {
	time.Duration
}

// Config は、MarketDataCollector 全体の設定を保持します。
type Config struct {
	System    SystemConfig    `toml:"SYSTEM"`
	Python    PythonConfig    `toml:"python"`
	Providers ProvidersConfig `toml:"providers"`
}

// SystemConfig は、HTTPサーバーの待受ポートと要求単位の制限を保持します。
type SystemConfig struct {
	Port            int      `toml:"Port"`
	RequestTimeout  Duration `toml:"RequestTimeout"`
	MaxRequestBytes int64    `toml:"MaxRequestBytes"`
}

// ProvidersConfig は、各情報取得元の設定を保持します。
type ProvidersConfig struct {
	Nikkei225JP Nikkei225JPConfig `toml:"nikkei225jp"`
	JQuants     JQuantsConfig     `toml:"jquants"`
	YFinance    ProviderConfig    `toml:"yfinance"`
	InvestingPy ProviderConfig    `toml:"investingpy"`
}

// ProviderConfig は、個別providerの有効状態を保持します。
type ProviderConfig struct {
	Enabled bool `toml:"enabled"`
}

// Nikkei225JPConfig は、225225.jpへのHTTP接続と応答本文の制限を保持します。
type Nikkei225JPConfig struct {
	Enabled               bool     `toml:"enabled"`
	BaseURL               string   `toml:"base_url"`
	Timeout               Duration `toml:"timeout"`
	UserAgent             string   `toml:"user_agent"`
	MaxResponseBytes      int64    `toml:"max_response_bytes"`
	MaxChartResponseBytes int64    `toml:"max_chart_response_bytes"`
}

// JQuantsConfig は、J-Quants APIへの接続、契約プラン、アドオンおよび応答本文の制限を保持します。
type JQuantsConfig struct {
	Enabled          bool     `toml:"enabled"`
	BaseURL          string   `toml:"base_url"`
	APIKey           string   `toml:"api_key"`
	Plan             string   `toml:"plan"`
	Addons           []string `toml:"addons"`
	Timeout          Duration `toml:"timeout"`
	UserAgent        string   `toml:"user_agent"`
	MaxResponseBytes int64    `toml:"max_response_bytes"`
}

// PythonConfig は、Python provider間で共有する子プロセス実行設定を保持します。
type PythonConfig struct {
	Executable             string   `toml:"executable"`
	Script                 string   `toml:"script"`
	Timeout                Duration `toml:"timeout"`
	MaxResponseBytes       int64    `toml:"max_response_bytes"`
	MaxConcurrentProcesses int      `toml:"max_concurrent_processes"`
}

/*
Default は、アプリケーション設定の既定値を生成します。

機能:
  - HTTPサーバー、225225.jp通信、J-Quants API通信、Python実行環境の標準の既定値を組み立てる
  - APIキーが必要なJ-Quants API連携を既定では無効にする
  - 利用条件の確認が必要なPython連携を既定では無効にする

引数:
  - なし

返り値:
  - Config: 既定値を格納した設定
*/
func Default() Config {
	return Config{
		System: SystemConfig{
			Port:            defaultSystemPort,
			RequestTimeout:  Duration{Duration: defaultRequestTimeout},
			MaxRequestBytes: defaultMaxRequestBytes,
		},
		Python: PythonConfig{
			Executable:             defaultPythonExecutable,
			Script:                 defaultPythonScript,
			Timeout:                Duration{Duration: defaultPythonTimeout},
			MaxResponseBytes:       defaultPythonMaxResponseBytes,
			MaxConcurrentProcesses: defaultPythonMaxConcurrentProcesses,
		},
		Providers: ProvidersConfig{
			Nikkei225JP: Nikkei225JPConfig{
				Enabled:               true,
				BaseURL:               defaultNikkei225JPBaseURL,
				Timeout:               Duration{Duration: defaultNikkei225JPTimeout},
				UserAgent:             defaultNikkei225JPUserAgent,
				MaxResponseBytes:      defaultNikkei225JPMaxResponseBytes,
				MaxChartResponseBytes: defaultNikkei225JPMaxChartResponseBytes,
			},
			JQuants: JQuantsConfig{
				Enabled:          false,
				BaseURL:          defaultJQuantsBaseURL,
				APIKey:           "",
				Plan:             defaultJQuantsPlan,
				Addons:           []string{},
				Timeout:          Duration{Duration: defaultJQuantsTimeout},
				UserAgent:        defaultJQuantsUserAgent,
				MaxResponseBytes: defaultJQuantsMaxResponseBytes,
			},
			YFinance:    ProviderConfig{Enabled: false},
			InvestingPy: ProviderConfig{Enabled: false},
		},
	}
}

// ----------------------------------------

/*
LoadDir は、指定ディレクトリ直下の TOML ファイルをファイル名昇順で読み込みます。

各ファイルは同じ設定へ順次デコードされるため、後のファイルで指定された項目だけが
前の設定を上書きします。すべてのファイルを統合した後、最終的な設定値を検証します。

機能:
  - 既定値へ複数のTOMLファイルを名前順で上書きする
  - 未知項目、TOMLファイル0件、統合後の不正値をエラーにする

引数:
  - dir string: TOML設定ファイルを格納したディレクトリのパス

返り値:
  - Config: 既定値と全TOMLファイルを統合した設定
  - error: ディレクトリの読み込み、TOMLの解析、未知項目、または検証に失敗した場合のエラー
*/
func LoadDir(dir string) (Config, error) {
	if strings.TrimSpace(dir) == "" {
		return Config{}, errors.New("設定ディレクトリが指定されていません")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return Config{}, fmt.Errorf("設定ディレクトリ %q を読み込めません: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return Config{}, fmt.Errorf("設定ディレクトリ %q に TOML ファイルがありません", dir)
	}

	cfg := Default()
	for _, path := range paths {
		metadata, decodeErr := toml.DecodeFile(path, &cfg)
		if decodeErr != nil {
			return Config{}, fmt.Errorf("設定ファイル %q を解析できません: %w", path, decodeErr)
		}
		if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
			keys := make([]string, 0, len(undecoded))
			for _, key := range undecoded {
				keys = append(keys, key.String())
			}
			sort.Strings(keys)
			return Config{}, fmt.Errorf(
				"設定ファイル %q に未知の項目があります: %s",
				path,
				strings.Join(keys, ", "),
			)
		}
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("設定値が不正です: %w", err)
	}
	return cfg, nil
}

// ----------------------------------------

/*
Validate は、アプリケーション設定が実行可能な範囲の値であることを検証します。

機能:
  - HTTPサーバー、225225.jp通信、J-Quants API通信、Python実行環境の範囲と形式をまとめて検証する
  - J-Quants APIキーの必須性を除き、providerの有効状態にかかわらず設定ファイル全体を起動時に検証する
  - 検出した複数の不正値を結合して返す

引数:
  - なし

返り値:
  - error: 設定値が不正な場合のエラー。すべて有効な場合はnil
*/
func (c Config) Validate() error {
	var validationErrors []error

	if c.System.Port < 1 || c.System.Port > 65535 {
		validationErrors = append(
			validationErrors,
			fmt.Errorf("SYSTEM.Port は 1 以上 65535 以下である必要があります: %d", c.System.Port),
		)
	}
	if c.System.RequestTimeout.Duration < minRequestTimeout ||
		c.System.RequestTimeout.Duration > maxRequestTimeout {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"SYSTEM.RequestTimeout は %s 以上 %s 以下である必要があります",
				minRequestTimeout,
				maxRequestTimeout,
			),
		)
	}
	if c.System.MaxRequestBytes < 1 || c.System.MaxRequestBytes > maxRequestBytes {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"SYSTEM.MaxRequestBytes は 1 以上 %d 以下である必要があります: %d",
				maxRequestBytes,
				c.System.MaxRequestBytes,
			),
		)
	}
	if err := validateBaseURL(c.Providers.Nikkei225JP.BaseURL); err != nil {
		validationErrors = append(
			validationErrors,
			fmt.Errorf("providers.nikkei225jp.base_url が不正です: %w", err),
		)
	}
	if c.Providers.Nikkei225JP.Timeout.Duration < minNikkei225JPTimeout ||
		c.Providers.Nikkei225JP.Timeout.Duration > maxNikkei225JPTimeout {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"providers.nikkei225jp.timeout は %s 以上 %s 以下である必要があります",
				minNikkei225JPTimeout,
				maxNikkei225JPTimeout,
			),
		)
	}
	if strings.TrimSpace(c.Providers.Nikkei225JP.UserAgent) == "" {
		validationErrors = append(
			validationErrors,
			errors.New("providers.nikkei225jp.user_agent は空にできません"),
		)
	} else if !validHTTPHeaderFieldValue(c.Providers.Nikkei225JP.UserAgent) {
		validationErrors = append(
			validationErrors,
			errors.New("providers.nikkei225jp.user_agent にHTTPヘッダーで利用できない制御文字があります"),
		)
	}
	if c.Providers.Nikkei225JP.MaxResponseBytes < 1 ||
		c.Providers.Nikkei225JP.MaxResponseBytes > maxNikkei225JPResponseBytes {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"providers.nikkei225jp.max_response_bytes は 1 以上 %d 以下である必要があります: %d",
				maxNikkei225JPResponseBytes,
				c.Providers.Nikkei225JP.MaxResponseBytes,
			),
		)
	}
	if c.Providers.Nikkei225JP.MaxChartResponseBytes < 1 ||
		c.Providers.Nikkei225JP.MaxChartResponseBytes > maxNikkei225JPChartResponseBytes {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"providers.nikkei225jp.max_chart_response_bytes は 1 以上 %d 以下である必要があります: %d",
				maxNikkei225JPChartResponseBytes,
				c.Providers.Nikkei225JP.MaxChartResponseBytes,
			),
		)
	}

	// ----------------------------------------

	if err := validateBaseURL(c.Providers.JQuants.BaseURL); err != nil {
		validationErrors = append(
			validationErrors,
			fmt.Errorf("providers.jquants.base_url が不正です: %w", err),
		)
	}
	jQuantsAPIKey := c.Providers.JQuants.APIKey
	if c.Providers.JQuants.Enabled && jQuantsAPIKey == "" {
		validationErrors = append(
			validationErrors,
			errors.New("providers.jquants.api_key はJ-Quants providerが有効な場合に必須です"),
		)
	}
	if jQuantsAPIKey != "" {
		if strings.TrimSpace(jQuantsAPIKey) != jQuantsAPIKey {
			validationErrors = append(
				validationErrors,
				errors.New("providers.jquants.api_key の前後に空白を含めることはできません"),
			)
		}
		if strings.ContainsRune(jQuantsAPIKey, '\t') || !validHTTPHeaderFieldValue(jQuantsAPIKey) {
			validationErrors = append(
				validationErrors,
				errors.New("providers.jquants.api_key にHTTPヘッダーで利用できない制御文字があります"),
			)
		}
	}
	switch c.Providers.JQuants.Plan {
	case "free", "light", "standard", "premium":
	default:
		validationErrors = append(
			validationErrors,
			errors.New("providers.jquants.plan はfree、light、standard、premiumのいずれかである必要があります"),
		)
	}
	seenJQuantsAddons := make(map[string]struct{}, len(c.Providers.JQuants.Addons))
	for _, addon := range c.Providers.JQuants.Addons {
		switch addon {
		case "minute", "tdnet":
		default:
			validationErrors = append(
				validationErrors,
				fmt.Errorf("providers.jquants.addons に未知の値があります: %q", addon),
			)
		}
		if _, exists := seenJQuantsAddons[addon]; exists {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("providers.jquants.addons に重複した値があります: %q", addon),
			)
		}
		seenJQuantsAddons[addon] = struct{}{}
	}
	if c.Providers.JQuants.Plan == "free" && len(c.Providers.JQuants.Addons) > 0 {
		validationErrors = append(
			validationErrors,
			errors.New("providers.jquants.addons はfreeプランでは利用できません"),
		)
	}
	if c.Providers.JQuants.Timeout.Duration < minJQuantsTimeout ||
		c.Providers.JQuants.Timeout.Duration > maxJQuantsTimeout {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"providers.jquants.timeout は %s 以上 %s 以下である必要があります",
				minJQuantsTimeout,
				maxJQuantsTimeout,
			),
		)
	}
	if strings.TrimSpace(c.Providers.JQuants.UserAgent) == "" {
		validationErrors = append(
			validationErrors,
			errors.New("providers.jquants.user_agent は空にできません"),
		)
	} else if !validHTTPHeaderFieldValue(c.Providers.JQuants.UserAgent) {
		validationErrors = append(
			validationErrors,
			errors.New("providers.jquants.user_agent にHTTPヘッダーで利用できない制御文字があります"),
		)
	}
	if c.Providers.JQuants.MaxResponseBytes < minJQuantsResponseBytes ||
		c.Providers.JQuants.MaxResponseBytes > maxJQuantsResponseBytes {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"providers.jquants.max_response_bytes は %d 以上 %d 以下である必要があります: %d",
				minJQuantsResponseBytes,
				maxJQuantsResponseBytes,
				c.Providers.JQuants.MaxResponseBytes,
			),
		)
	}

	// ----------------------------------------

	if strings.TrimSpace(c.Python.Executable) == "" {
		validationErrors = append(
			validationErrors,
			errors.New("python.executable は空にできません"),
		)
	} else if strings.ContainsRune(c.Python.Executable, '\x00') {
		validationErrors = append(
			validationErrors,
			errors.New("python.executable にNUL文字を含めることはできません"),
		)
	}
	if strings.TrimSpace(c.Python.Script) == "" {
		validationErrors = append(validationErrors, errors.New("python.script は空にできません"))
	} else if strings.ContainsRune(c.Python.Script, '\x00') {
		validationErrors = append(
			validationErrors,
			errors.New("python.script にNUL文字を含めることはできません"),
		)
	}
	if c.Python.Timeout.Duration < minPythonTimeout ||
		c.Python.Timeout.Duration > maxPythonTimeout {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"python.timeout は %s 以上 %s 以下である必要があります",
				minPythonTimeout,
				maxPythonTimeout,
			),
		)
	}
	if c.Python.MaxResponseBytes < 1 ||
		c.Python.MaxResponseBytes > maxPythonResponseBytes {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"python.max_response_bytes は 1 以上 %d 以下である必要があります: %d",
				maxPythonResponseBytes,
				c.Python.MaxResponseBytes,
			),
		)
	}
	if c.Python.MaxConcurrentProcesses < 1 ||
		c.Python.MaxConcurrentProcesses > maxPythonConcurrentProcesses {
		validationErrors = append(
			validationErrors,
			fmt.Errorf(
				"python.max_concurrent_processes は 1 以上 %d 以下である必要があります: %d",
				maxPythonConcurrentProcesses,
				c.Python.MaxConcurrentProcesses,
			),
		)
	}

	return errors.Join(validationErrors...)
}

// ----------------------------------------

/*
validHTTPHeaderFieldValue は、net/httpで送信可能なfield-valueか確認します。

機能:
  - HTABを除く0x20未満の制御文字とDELを拒否する
  - UTF-8を構成する0x80以上のバイトはobs-textとして許可する

引数:
  - value string: User-Agent等のHTTPヘッダー値

返り値:
  - bool: net/httpで有効なfield-valueの場合はtrue
*/
func validHTTPHeaderFieldValue(value string) bool {
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
validateBaseURL は、情報取得元の基点URLが安全なHTTP URLか検証します。

機能:
  - httpまたはhttpsの絶対URLだけを許可する
  - userinfo、クエリ、フラグメントを含むURLを拒否する

引数:
  - value string: 検証する基点URL

返り値:
  - error: URLが不正な場合のエラー。有効な場合はnil
*/
func validateBaseURL(value string) error {
	parsed, err := parseHTTPURL(value)
	if err != nil {
		return err
	}
	if parsed.User != nil {
		return errors.New("userinfoを含めることはできません")
	}
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return errors.New("パスを含めることはできません")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return errors.New("クエリを含めることはできません")
	}
	if parsed.Fragment != "" {
		return errors.New("フラグメントを含めることはできません")
	}
	return nil
}

// ----------------------------------------

/*
parseHTTPURL は、文字列をホスト付きのHTTP絶対URLとして解析します。

機能:
  - 前後空白、相対URL、HTTP以外のスキーム、無効なポートを拒否する
  - 後続の用途別検証で利用する解析済みURLを返す

引数:
  - value string: 解析するURL文字列

返り値:
  - *url.URL: 解析済みのURL
  - error: URLが不正な場合のエラー。解析できた場合はnil
*/
func parseHTTPURL(value string) (*url.URL, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return nil, errors.New("URLは空にできず、前後に空白を含めることもできません")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("URLを解析できません: %w", err)
	}
	if parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("httpまたはhttpsの絶対URLである必要があります")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("ホストが必要です")
	}

	port := parsed.Port()
	if port != "" {
		portNumber, parseErr := strconv.Atoi(port)
		if parseErr != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("ポートは1以上65535以下である必要があります: %q", port)
		}
	}
	return parsed, nil
}

// ----------------------------------------

/*
UnmarshalText は、TOML の期間文字列を time.Duration として解析します。

機能:
  - Goの期間表記を解析してDurationへ設定する

引数:
  - text []byte: 「30s」や「5m」のような期間文字列

返り値:
  - error: 期間文字列を解析できない場合のエラー。解析に成功した場合はnil
*/
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("期間 %q を解析できません: %w", string(text), err)
	}
	d.Duration = parsed
	return nil
}

// ----------------------------------------

/*
String は、保持している期間を文字列へ変換します。

機能:
  - 内部のtime.Durationを標準の期間表記へ変換する

引数:
  - なし

返り値:
  - string: time.Duration形式の期間文字列
*/
func (d Duration) String() string {
	return d.Duration.String()
}
