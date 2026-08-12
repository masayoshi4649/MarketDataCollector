package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
TestDefault は、Defaultが新しい責務分離後の既定値を返すことを検証します。

機能:
  - SYSTEMにHTTPサーバー共通制限だけが設定されることを確認する
  - Python共通実行設定がトップレベルに存在することを確認する
  - 225225.jp、J-Quants、kabus-controller、Polymarket固有HTTP設定と各providerの有効状態を確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.System.Port != 8080 {
		t.Errorf("Port = %d, 期待値は8080", cfg.System.Port)
	}
	if cfg.System.RequestTimeout.Duration != 60*time.Second {
		t.Errorf("RequestTimeout = %s, 期待値は%s", cfg.System.RequestTimeout, 60*time.Second)
	}
	if cfg.System.MaxRequestBytes != 1024*1024 {
		t.Errorf("MaxRequestBytes = %d, 期待値は%d", cfg.System.MaxRequestBytes, 1024*1024)
	}
	// ----------------------------------------

	if cfg.Python.Executable != "python" || cfg.Python.Script != "python/collector.py" {
		t.Errorf(
			"Python実行設定 = (%q, %q), 期待値は(%q, %q)",
			cfg.Python.Executable,
			cfg.Python.Script,
			"python",
			"python/collector.py",
		)
	}
	if cfg.Python.Timeout.Duration != 60*time.Second {
		t.Errorf("Python.Timeout = %s, 期待値は%s", cfg.Python.Timeout, 60*time.Second)
	}
	if cfg.Python.MaxResponseBytes != 16*1024*1024 {
		t.Errorf("Python.MaxResponseBytes = %d, 期待値は%d", cfg.Python.MaxResponseBytes, 16*1024*1024)
	}
	if cfg.Python.MaxConcurrentProcesses != 2 {
		t.Errorf("Python.MaxConcurrentProcesses = %d, 期待値は2", cfg.Python.MaxConcurrentProcesses)
	}

	// ----------------------------------------

	nikkei225JP := cfg.Providers.Nikkei225JP
	if !nikkei225JP.Enabled || nikkei225JP.BaseURL != "https://225225.jp" {
		t.Errorf(
			"225225.jp基本設定 = (%t, %q), 期待値は(true, %q)",
			nikkei225JP.Enabled,
			nikkei225JP.BaseURL,
			"https://225225.jp",
		)
	}
	if nikkei225JP.Timeout.Duration != 10*time.Second || nikkei225JP.UserAgent != "MarketDataCollector/0.1" {
		t.Errorf(
			"225225.jp HTTP設定 = (%s, %q), 期待値は(%s, %q)",
			nikkei225JP.Timeout,
			nikkei225JP.UserAgent,
			10*time.Second,
			"MarketDataCollector/0.1",
		)
	}
	if nikkei225JP.MaxResponseBytes != 4*1024*1024 || nikkei225JP.MaxChartResponseBytes != 32*1024*1024 {
		t.Errorf(
			"225225.jp応答上限 = (%d, %d), 期待値は(%d, %d)",
			nikkei225JP.MaxResponseBytes,
			nikkei225JP.MaxChartResponseBytes,
			4*1024*1024,
			32*1024*1024,
		)
	}

	// ----------------------------------------

	jQuants := cfg.Providers.JQuants
	if jQuants.Enabled || jQuants.BaseURL != "https://api.jquants.com" || jQuants.APIKey != "" {
		t.Errorf(
			"J-Quants基本設定 = (%t, %q, APIキー空=%t), 期待値は(false, %q, true)",
			jQuants.Enabled,
			jQuants.BaseURL,
			jQuants.APIKey == "",
			"https://api.jquants.com",
		)
	}
	if jQuants.Plan != "standard" || len(jQuants.Addons) != 0 {
		t.Errorf(
			"J-Quants契約設定 = (%q, %v), 期待値は(%q, 空配列)",
			jQuants.Plan,
			jQuants.Addons,
			"standard",
		)
	}
	if jQuants.Timeout.Duration != 30*time.Second || jQuants.UserAgent != "MarketDataCollector/0.1" {
		t.Errorf(
			"J-Quants HTTP設定 = (%s, %q), 期待値は(%s, %q)",
			jQuants.Timeout,
			jQuants.UserAgent,
			30*time.Second,
			"MarketDataCollector/0.1",
		)
	}
	if jQuants.MaxResponseBytes != 16*1024*1024 {
		t.Errorf(
			"J-Quants応答上限 = %d, 期待値は%d",
			jQuants.MaxResponseBytes,
			16*1024*1024,
		)
	}

	// ----------------------------------------

	kabusController := cfg.Providers.KabusController
	if !kabusController.Enabled || kabusController.BaseURL != "http://10.10.100.1:8080" {
		t.Errorf(
			"kabus-controller基本設定 = (%t, %q), 期待値は(true, %q)",
			kabusController.Enabled,
			kabusController.BaseURL,
			"http://10.10.100.1:8080",
		)
	}
	if kabusController.Timeout.Duration != 15*time.Second ||
		kabusController.UserAgent != "MarketDataCollector/0.1" {
		t.Errorf(
			"kabus-controller HTTP設定 = (%s, %q), 期待値は(%s, %q)",
			kabusController.Timeout,
			kabusController.UserAgent,
			15*time.Second,
			"MarketDataCollector/0.1",
		)
	}
	if kabusController.MaxResponseBytes != 16*1024*1024 {
		t.Errorf(
			"kabus-controller応答上限 = %d, 期待値は%d",
			kabusController.MaxResponseBytes,
			16*1024*1024,
		)
	}

	// ----------------------------------------

	polymarket := cfg.Providers.Polymarket
	if !polymarket.Enabled ||
		polymarket.GammaBaseURL != "https://gamma-api.polymarket.com" ||
		polymarket.CLOBBaseURL != "https://clob.polymarket.com" ||
		polymarket.DataBaseURL != "https://data-api.polymarket.com" {
		t.Errorf("Polymarket基本設定 = %+v, 公式3 APIを使う有効な既定値を期待", polymarket)
	}
	if polymarket.Timeout.Duration != 15*time.Second ||
		polymarket.UserAgent != "MarketDataCollector/0.1" {
		t.Errorf(
			"Polymarket HTTP設定 = (%s, %q), 期待値は(%s, %q)",
			polymarket.Timeout,
			polymarket.UserAgent,
			15*time.Second,
			"MarketDataCollector/0.1",
		)
	}
	if polymarket.MaxResponseBytes != 16*1024*1024 {
		t.Errorf(
			"Polymarket応答上限 = %d, 期待値は%d",
			polymarket.MaxResponseBytes,
			16*1024*1024,
		)
	}

	// ----------------------------------------

	if cfg.Providers.YFinance.Enabled || cfg.Providers.InvestingPy.Enabled {
		t.Error("利用条件の確認が必要なPython providerが既定値で有効です")
	}
}

// ----------------------------------------

/*
TestLoadDir は、分割したTOMLテーブルをファイル名順に統合することを検証します。

機能:
  - SYSTEM、python、6つのproviderテーブルを新構造へ復号する
  - 後順位ファイルが指定項目だけを上書きする
  - J-Quants、kabus-controller、Polymarket、yfinance、investingpyを独立して有効化できることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "20-override.toml", `
[SYSTEM]
Port = 9090

[python]
timeout = "2m"

[providers.nikkei225jp]
base_url = "https://mirror.example.test"
timeout = "3s"

[providers.jquants]
enabled = true
plan = "premium"
timeout = "45s"

[providers.kabus-controller]
enabled = true
timeout = "25s"

[providers.polymarket]
enabled = true
timeout = "20s"

[providers.yfinance]
enabled = true
`)
	writeTestFile(t, dir, "10-base.toml", `
[SYSTEM]
RequestTimeout = "45s"

[python]
executable = "python3"
script = "scripts/collector.py"
max_response_bytes = 33554432
max_concurrent_processes = 3

[providers.nikkei225jp]
user_agent = "統合テスト/1.0"
max_response_bytes = 131072

[providers.jquants]
base_url = "https://jquants.example.test"
api_key = "integration-test-key"
plan = "light"
addons = ["minute", "tdnet"]
user_agent = "J-Quants統合テスト/1.0"
max_response_bytes = 33554432

[providers.kabus-controller]
enabled = false
base_url = "http://kabus-controller.example.test:8080"
user_agent = "kabus-controller統合テスト/1.0"
max_response_bytes = 33554432

[providers.polymarket]
enabled = false
gamma_base_url = "https://gamma.example.test"
clob_base_url = "https://clob.example.test"
data_base_url = "https://data.example.test"
user_agent = "Polymarket統合テスト/1.0"
max_response_bytes = 33554432

[providers.investingpy]
enabled = true
`)
	writeTestFile(t, dir, "README.txt", "TOML以外は読み込みません")

	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if cfg.System.Port != 9090 || cfg.System.RequestTimeout.Duration != 45*time.Second {
		t.Errorf(
			"SYSTEM設定 = (%d, %s), 期待値は(9090, %s)",
			cfg.System.Port,
			cfg.System.RequestTimeout,
			45*time.Second,
		)
	}
	if cfg.Python.Executable != "python3" || cfg.Python.Script != "scripts/collector.py" ||
		cfg.Python.Timeout.Duration != 2*time.Minute || cfg.Python.MaxConcurrentProcesses != 3 {
		t.Errorf("Python設定 = %+v, 分割ファイルの統合結果と一致しません", cfg.Python)
	}
	if cfg.Providers.Nikkei225JP.BaseURL != "https://mirror.example.test" ||
		cfg.Providers.Nikkei225JP.Timeout.Duration != 3*time.Second ||
		cfg.Providers.Nikkei225JP.UserAgent != "統合テスト/1.0" {
		t.Errorf("225225.jp HTTP設定 = %+v, 分割ファイルの統合結果と一致しません", cfg.Providers.Nikkei225JP)
	}
	jQuants := cfg.Providers.JQuants
	if !jQuants.Enabled || jQuants.BaseURL != "https://jquants.example.test" ||
		jQuants.APIKey != "integration-test-key" || jQuants.Plan != "premium" ||
		jQuants.Timeout.Duration != 45*time.Second || jQuants.UserAgent != "J-Quants統合テスト/1.0" ||
		jQuants.MaxResponseBytes != 32*1024*1024 || len(jQuants.Addons) != 2 ||
		jQuants.Addons[0] != "minute" || jQuants.Addons[1] != "tdnet" {
		t.Error("J-Quants設定が分割ファイルの統合結果と一致しません")
	}
	kabusController := cfg.Providers.KabusController
	if !kabusController.Enabled ||
		kabusController.BaseURL != "http://kabus-controller.example.test:8080" ||
		kabusController.Timeout.Duration != 25*time.Second ||
		kabusController.UserAgent != "kabus-controller統合テスト/1.0" ||
		kabusController.MaxResponseBytes != 32*1024*1024 {
		t.Error("kabus-controller設定が分割ファイルの統合結果と一致しません")
	}
	polymarket := cfg.Providers.Polymarket
	if !polymarket.Enabled || polymarket.GammaBaseURL != "https://gamma.example.test" ||
		polymarket.CLOBBaseURL != "https://clob.example.test" ||
		polymarket.DataBaseURL != "https://data.example.test" ||
		polymarket.Timeout.Duration != 20*time.Second ||
		polymarket.UserAgent != "Polymarket統合テスト/1.0" ||
		polymarket.MaxResponseBytes != 32*1024*1024 {
		t.Error("Polymarket設定が分割ファイルの統合結果と一致しません")
	}
	if !cfg.Providers.YFinance.Enabled || !cfg.Providers.InvestingPy.Enabled {
		t.Errorf(
			"provider有効状態 = (yfinance=%t, investingpy=%t), 両方trueを期待",
			cfg.Providers.YFinance.Enabled,
			cfg.Providers.InvestingPy.Enabled,
		)
	}
}

// ----------------------------------------

/*
TestLoadDirAppliesLaterJQuantsLocalOverride は、後順位のローカル設定がJ-Quants既定値を上書きすることを検証します。

機能:
  - 追跡対象のconf/default.tomlだけではJ-Quantsが無効であることを確認する
  - 偽APIキーを持つzz-jquants.local.tomlを追加するとenabledとAPIキーが後勝ちになることを確認する
  - 実運用のローカル秘密設定を読み取らず、一時ディレクトリだけで並び順を再現する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestLoadDirAppliesLaterJQuantsLocalOverride(t *testing.T) {
	defaultPath := filepath.Join("..", "..", "conf", "default.toml")
	defaultContent, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatalf("追跡対象のdefault.tomlを読み込めません: %v", err)
	}
	dir := t.TempDir()
	writeTestFile(t, dir, "default.toml", string(defaultContent))

	defaultConfig, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("default.tomlだけのLoadDir() error = %v", err)
	}
	if defaultConfig.Providers.JQuants.Enabled {
		t.Fatal("追跡対象のdefault.tomlでJ-Quantsが有効です")
	}

	writeTestFile(t, dir, "zz-jquants.local.toml", `
[providers.jquants]
enabled = true
api_key = "ordering-test-key"
plan = "standard"
addons = []
`)
	overriddenConfig, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("ローカル上書き後のLoadDir() error = %v", err)
	}
	if !overriddenConfig.Providers.JQuants.Enabled {
		t.Error("後順位のzz-jquants.local.tomlでJ-Quantsが有効になりません")
	}
	if overriddenConfig.Providers.JQuants.APIKey != "ordering-test-key" {
		t.Error("J-Quants APIキーが後順位のローカル設定値で上書きされていません")
	}
}

// ----------------------------------------

/*
TestLoadDirRejectsRemovedAndUnknownKeys は、旧構造および未知のTOML項目を拒否することを検証します。

機能:
  - 削除したHost、ShutdownTimeout、MaxConcurrentRequests、キャッシュ設定、http、providers.pythonを拒否する
  - エラーへ未知の完全修飾項目名を含めることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestLoadDirRejectsRemovedAndUnknownKeys(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		wantPath string
	}{
		{name: "Host", content: "[SYSTEM]\nHost = \"127.0.0.1\"\n", wantPath: "SYSTEM.Host"},
		{name: "ShutdownTimeout", content: "[SYSTEM]\nShutdownTimeout = \"10s\"\n", wantPath: "SYSTEM.ShutdownTimeout"},
		{name: "MaxConcurrentRequests", content: "[SYSTEM]\nMaxConcurrentRequests = 8\n", wantPath: "SYSTEM.MaxConcurrentRequests"},
		{name: "min_request_interval", content: "[providers.nikkei225jp]\nmin_request_interval = \"1m\"\n", wantPath: "providers.nikkei225jp.min_request_interval"},
		{name: "history_cache_ttl", content: "[providers.nikkei225jp]\nhistory_cache_ttl = \"1h\"\n", wantPath: "providers.nikkei225jp.history_cache_ttl"},
		{name: "http", content: "[http]\ntimeout = \"3s\"\n", wantPath: "http"},
		{
			name:     "providers.python",
			content:  "[providers.python]\nyfinance_enabled = true\n",
			wantPath: "providers.python",
		},
		{
			name:     "J-Quants provider未知項目",
			content:  "[providers.jquants]\nunknown_value = true\n",
			wantPath: "providers.jquants.unknown_value",
		},
		{
			name:     "kabus-controller provider未知項目",
			content:  "[providers.kabus-controller]\nunknown_value = true\n",
			wantPath: "providers.kabus-controller.unknown_value",
		},
		{
			name:     "Polymarket provider未知項目",
			content:  "[providers.polymarket]\nunknown_value = true\n",
			wantPath: "providers.polymarket.unknown_value",
		},
		{
			name:     "provider未知項目",
			content:  "[providers.yfinance]\nunknown_value = true\n",
			wantPath: "providers.yfinance.unknown_value",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, dir, "10-config.toml", testCase.content)
			_, err := LoadDir(dir)
			if err == nil {
				t.Fatal("LoadDir() error = nil, 未知項目のエラーを期待")
			}
			if !strings.Contains(err.Error(), testCase.wantPath) {
				t.Errorf("LoadDir() error = %q, %qを含むことを期待", err, testCase.wantPath)
			}
		})
	}
}

// ----------------------------------------

/*
TestLoadDirRejectsNoTOML は、対象TOMLファイルが0件の場合にエラーとなることを検証します。

機能:
  - TOML以外のファイルしかない設定ディレクトリを拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestLoadDirRejectsNoTOML(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config.toml.sample", "サンプル")
	writeTestFile(t, dir, "CONFIG.TOML", "大文字拡張子")

	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "TOML ファイルがありません") {
		t.Fatalf("LoadDir() error = %v, TOMLファイル0件のエラーを期待", err)
	}
}

// ----------------------------------------

/*
TestLoadDirRejectsInvalidDuration は、不正な期間文字列を拒否することを検証します。

機能:
  - 225225.jp固有timeoutをGoの期間表記として解析できない場合にエラーにする

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestLoadDirRejectsInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "10-config.toml", `
[providers.nikkei225jp]
timeout = "十秒"
`)

	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "期間") {
		t.Fatalf("LoadDir() error = %v, 期間文字列の解析エラーを期待", err)
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidSystemValues は、HTTPサーバー要求設定の範囲外値を拒否することを検証します。

機能:
  - ポート、要求期限、要求サイズを一括検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidSystemValues(t *testing.T) {
	cfg := Default()
	cfg.System.Port = 70000
	cfg.System.RequestTimeout = Duration{Duration: maxRequestTimeout + time.Second}
	cfg.System.MaxRequestBytes = maxRequestBytes + 1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, SYSTEM検証エラーを期待")
	}
	for _, expected := range []string{
		"SYSTEM.Port",
		"SYSTEM.RequestTimeout",
		"SYSTEM.MaxRequestBytes",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Validate() error = %q, %qを含むことを期待", err, expected)
		}
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidNikkei225JPValues は、225225.jp固有設定の不正値を拒否することを検証します。

機能:
  - URL、HTTP期限、User-Agent、応答サイズを一括検証する
  - providerが無効でも将来の有効化に備えて全設定を検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidNikkei225JPValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.Nikkei225JP.Enabled = false
	cfg.Providers.Nikkei225JP.BaseURL = "file:///tmp/data"
	cfg.Providers.Nikkei225JP.Timeout = Duration{}
	cfg.Providers.Nikkei225JP.UserAgent = "client\x7f"
	cfg.Providers.Nikkei225JP.MaxResponseBytes = 0
	cfg.Providers.Nikkei225JP.MaxChartResponseBytes = maxNikkei225JPChartResponseBytes + 1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, 225225.jp検証エラーを期待")
	}
	for _, expected := range []string{
		"providers.nikkei225jp.base_url",
		"providers.nikkei225jp.timeout",
		"providers.nikkei225jp.user_agent",
		"providers.nikkei225jp.max_response_bytes",
		"providers.nikkei225jp.max_chart_response_bytes",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Validate() error = %q, %qを含むことを期待", err, expected)
		}
	}
}

// ----------------------------------------

/*
TestValidateAcceptsNikkei225JPUserAgent は、有効なHTTP識別値を許可することを検証します。

機能:
  - 日本語を含むHTTP field-valueを225225.jpのUser-Agentとして許可する
  - 空文字および禁止制御文字だけが拒否対象であることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateAcceptsNikkei225JPUserAgent(t *testing.T) {
	cfg := Default()
	cfg.Providers.Nikkei225JP.UserAgent = "市場収集/1.0"
	if err := cfg.Validate(); err != nil {
		t.Errorf("有効なUser-AgentのValidate() error = %v", err)
	}
}

// ----------------------------------------

/*
TestValidateRejectsProviderBaseURLPath は、固定パスを安全に解決できないオリジン設定を拒否することを検証します。

機能:
  - base_urlにサブパスを指定した設定を拒否する
  - 固定配信パスが意図せずオリジン直下へ解決される設定差を防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsProviderBaseURLPath(t *testing.T) {
	cfg := Default()
	cfg.Providers.Nikkei225JP.BaseURL = "https://example.com/mirror"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "providers.nikkei225jp.base_url") {
		t.Fatalf("Validate() error = %v, base_urlパスの拒否を期待", err)
	}
}

// ----------------------------------------

/*
TestValidateAcceptsJQuantsValues は、有効なJ-Quants設定を受け付けることを検証します。

機能:
  - provider無効時は空のAPIキーを許可する
  - provider有効時は有効なAPIキー、契約プラン、アドオンおよび境界値を許可する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateAcceptsJQuantsValues(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("provider無効時の空APIキーを含むDefault設定のValidate() error = %v", err)
	}

	cfg.Providers.JQuants.Enabled = true
	cfg.Providers.JQuants.APIKey = "integration-test-key"
	cfg.Providers.JQuants.Plan = "premium"
	cfg.Providers.JQuants.Addons = []string{"minute", "tdnet"}
	cfg.Providers.JQuants.Timeout = Duration{Duration: minJQuantsTimeout}
	cfg.Providers.JQuants.UserAgent = "市場収集/1.0"
	cfg.Providers.JQuants.MaxResponseBytes = minJQuantsResponseBytes
	if err := cfg.Validate(); err != nil {
		t.Errorf("有効なJ-Quants設定のValidate() error = %v", err)
	}
}

// ----------------------------------------

/*
TestValidateAcceptsKabusControllerValues は、有効なkabus-controller設定を受け付けることを検証します。

機能:
  - LAN内HTTPオリジン、HTTP期限、User-Agent、応答本文上限の境界値を許可する
  - providerの有効状態にかかわらず同じ接続設定を検証できることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateAcceptsKabusControllerValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.KabusController.Enabled = false
	cfg.Providers.KabusController.BaseURL = "http://127.0.0.1:8080"
	cfg.Providers.KabusController.Timeout = Duration{Duration: minKabusControllerTimeout}
	cfg.Providers.KabusController.UserAgent = "市場収集/1.0"
	cfg.Providers.KabusController.MaxResponseBytes = minKabusControllerResponseBytes

	if err := cfg.Validate(); err != nil {
		t.Errorf("有効なkabus-controller設定のValidate() error = %v", err)
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidKabusControllerValues は、kabus-controller固有設定の不正値を拒否することを検証します。

機能:
  - URL、HTTP期限、User-Agent、応答本文上限を一括検証する
  - providerが無効でも接続設定全体を検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidKabusControllerValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.KabusController.Enabled = false
	cfg.Providers.KabusController.BaseURL = "http://10.10.100.1:8080/api"
	cfg.Providers.KabusController.Timeout = Duration{Duration: maxKabusControllerTimeout + time.Second}
	cfg.Providers.KabusController.UserAgent = "client\x7f"
	cfg.Providers.KabusController.MaxResponseBytes = maxKabusControllerResponseBytes + 1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, kabus-controller検証エラーを期待")
	}
	for _, expected := range []string{
		"providers.kabus-controller.base_url",
		"providers.kabus-controller.timeout",
		"providers.kabus-controller.user_agent",
		"providers.kabus-controller.max_response_bytes",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Validate() error = %q, %qを含むことを期待", err, expected)
		}
	}
}

// ----------------------------------------

/*
TestValidateRejectsKabusControllerResponseSizeOutOfRange は、kabus-controller応答本文上限の範囲外値を拒否することを検証します。

機能:
  - 1MiB未満と64MiB超の応答本文上限を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsKabusControllerResponseSizeOutOfRange(t *testing.T) {
	testCases := []struct {
		name  string
		value int64
	}{
		{name: "下限未満", value: minKabusControllerResponseBytes - 1},
		{name: "上限超過", value: maxKabusControllerResponseBytes + 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := Default()
			cfg.Providers.KabusController.MaxResponseBytes = testCase.value

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "providers.kabus-controller.max_response_bytes") {
				t.Fatalf("Validate() error = %v, 応答本文上限の検証エラーを期待", err)
			}
		})
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidJQuantsValues は、J-Quants固有設定の不正値を拒否することを検証します。

機能:
  - URL、契約プラン、アドオン、HTTP期限、User-Agent、応答サイズを一括検証する
  - providerが無効でもAPIキーの必須性以外の設定を検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidJQuantsValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.JQuants.BaseURL = "https://api.jquants.com/v2"
	cfg.Providers.JQuants.Plan = "enterprise"
	cfg.Providers.JQuants.Addons = []string{"minute", "minute", "unknown"}
	cfg.Providers.JQuants.Timeout = Duration{Duration: maxJQuantsTimeout + time.Second}
	cfg.Providers.JQuants.UserAgent = "client\x7f"
	cfg.Providers.JQuants.MaxResponseBytes = maxJQuantsResponseBytes + 1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, J-Quants検証エラーを期待")
	}
	for _, expected := range []string{
		"providers.jquants.base_url",
		"providers.jquants.plan",
		"providers.jquants.addons に未知の値",
		"providers.jquants.addons に重複した値",
		"providers.jquants.timeout",
		"providers.jquants.user_agent",
		"providers.jquants.max_response_bytes",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Validate() error = %q, %qを含むことを期待", err, expected)
		}
	}
}

// ----------------------------------------

/*
TestValidateRejectsJQuantsAddonOnFreePlan は、FreeプランとAdd-onの不正な組み合わせを拒否します。

機能:
  - collector生成前の設定検証でFreeプランへのAdd-on指定を検出する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsJQuantsAddonOnFreePlan(t *testing.T) {
	cfg := Default()
	cfg.Providers.JQuants.Plan = "free"
	cfg.Providers.JQuants.Addons = []string{"minute"}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "freeプランでは利用できません") {
		t.Fatalf("Validate() error = %v, FreeプランのAdd-on拒否を期待", err)
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidJQuantsAPIKeyWithoutDisclosure は、不正なAPIキーを値の漏えいなしで拒否することを検証します。

機能:
  - provider有効時の空APIキーを拒否する
  - 非空APIキーの前後空白とHTTP制御文字をproviderの有効状態にかかわらず拒否する
  - 検証エラーへAPIキーの実値を含めないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidJQuantsAPIKeyWithoutDisclosure(t *testing.T) {
	testCases := []struct {
		name    string
		enabled bool
		apiKey  string
	}{
		{name: "有効時に空", enabled: true, apiKey: ""},
		{name: "前後空白", enabled: false, apiKey: " disclosure-test-key "},
		{name: "水平タブ", enabled: false, apiKey: "disclosure-test-key\tvalue"},
		{name: "改行", enabled: false, apiKey: "disclosure-test-key\nvalue"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := Default()
			cfg.Providers.JQuants.Enabled = testCase.enabled
			cfg.Providers.JQuants.APIKey = testCase.apiKey

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "providers.jquants.api_key") {
				t.Fatalf("Validate() error = %v, APIキー検証エラーを期待", err)
			}
			if strings.Contains(err.Error(), "disclosure-test-key") {
				t.Errorf("Validate() errorにAPIキーの実値が含まれています: %v", err)
			}
		})
	}
}

// ----------------------------------------

/*
TestValidateRejectsJQuantsResponseSizeOutOfRange は、J-Quants応答本文上限の範囲外値を拒否することを検証します。

機能:
  - 1MiB未満と64MiB超の応答本文上限を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsJQuantsResponseSizeOutOfRange(t *testing.T) {
	testCases := []struct {
		name  string
		value int64
	}{
		{name: "下限未満", value: minJQuantsResponseBytes - 1},
		{name: "上限超過", value: maxJQuantsResponseBytes + 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := Default()
			cfg.Providers.JQuants.MaxResponseBytes = testCase.value

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "providers.jquants.max_response_bytes") {
				t.Fatalf("Validate() error = %v, 応答本文上限の検証エラーを期待", err)
			}
		})
	}
}

// ----------------------------------------

/*
TestValidateAcceptsPolymarketValues は、有効なPolymarket接続設定を受け付けることを検証します。

機能:
  - 公開3 APIのHTTPオリジン、期限、User-Agent、応答本文上限の境界値を許可する
  - providerの有効状態にかかわらず同じ接続設定を検証できることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateAcceptsPolymarketValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.Polymarket.Enabled = false
	cfg.Providers.Polymarket.GammaBaseURL = "https://gamma.example.test"
	cfg.Providers.Polymarket.CLOBBaseURL = "https://clob.example.test"
	cfg.Providers.Polymarket.DataBaseURL = "https://data.example.test"
	cfg.Providers.Polymarket.Timeout = Duration{Duration: minPolymarketTimeout}
	cfg.Providers.Polymarket.UserAgent = "市場収集/1.0"
	cfg.Providers.Polymarket.MaxResponseBytes = minPolymarketResponseBytes

	if err := cfg.Validate(); err != nil {
		t.Errorf("有効なPolymarket設定のValidate() error = %v", err)
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidPolymarketValues は、Polymarket固有設定の不正値を拒否することを検証します。

機能:
  - 公開3 APIのURL、HTTP期限、User-Agent、応答本文上限を一括検証する
  - providerが無効でも接続設定全体を検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidPolymarketValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.Polymarket.Enabled = false
	cfg.Providers.Polymarket.GammaBaseURL = "https://gamma-api.polymarket.com/events"
	cfg.Providers.Polymarket.CLOBBaseURL = "https://user@clob.polymarket.com"
	cfg.Providers.Polymarket.DataBaseURL = "https://data-api.polymarket.com?query=true"
	cfg.Providers.Polymarket.Timeout = Duration{Duration: maxPolymarketTimeout + time.Second}
	cfg.Providers.Polymarket.UserAgent = "client\x7f"
	cfg.Providers.Polymarket.MaxResponseBytes = maxPolymarketResponseBytes + 1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, Polymarket検証エラーを期待")
	}
	for _, expected := range []string{
		"providers.polymarket.gamma_base_url",
		"providers.polymarket.clob_base_url",
		"providers.polymarket.data_base_url",
		"providers.polymarket.timeout",
		"providers.polymarket.user_agent",
		"providers.polymarket.max_response_bytes",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Validate() error = %q, %qを含むことを期待", err, expected)
		}
	}
}

// ----------------------------------------

/*
TestValidateRejectsPolymarketResponseSizeOutOfRange は、Polymarket応答本文上限の範囲外値を拒否することを検証します。

機能:
  - 1MiB未満と64MiB超の応答本文上限を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsPolymarketResponseSizeOutOfRange(t *testing.T) {
	testCases := []struct {
		name  string
		value int64
	}{
		{name: "下限未満", value: minPolymarketResponseBytes - 1},
		{name: "上限超過", value: maxPolymarketResponseBytes + 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := Default()
			cfg.Providers.Polymarket.MaxResponseBytes = testCase.value

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "providers.polymarket.max_response_bytes") {
				t.Fatalf("Validate() error = %v, 応答本文上限の検証エラーを期待", err)
			}
		})
	}
}

// ----------------------------------------

/*
TestValidateRejectsInvalidPythonValues は、Python共通実行設定の不正値を拒否することを検証します。

機能:
  - 実行ファイル、スクリプト、期限、応答サイズ、同時プロセス数を一括検証する
  - 両Python providerが無効でも共通実行設定を検証する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestValidateRejectsInvalidPythonValues(t *testing.T) {
	cfg := Default()
	cfg.Providers.YFinance.Enabled = false
	cfg.Providers.InvestingPy.Enabled = false
	cfg.Python.Executable = ""
	cfg.Python.Script = " "
	cfg.Python.Timeout = Duration{}
	cfg.Python.MaxResponseBytes = maxPythonResponseBytes + 1
	cfg.Python.MaxConcurrentProcesses = maxPythonConcurrentProcesses + 1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, Python検証エラーを期待")
	}
	for _, expected := range []string{
		"python.executable",
		"python.script",
		"python.timeout",
		"python.max_response_bytes",
		"python.max_concurrent_processes",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Validate() error = %q, %qを含むことを期待", err, expected)
		}
	}
}

// ----------------------------------------

/*
TestDurationString は、Durationが標準の期間文字列を返すことを検証します。

機能:
  - 保持したtime.DurationをGo標準の期間表記へ変換する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestDurationString(t *testing.T) {
	duration := Duration{Duration: 90 * time.Second}
	if duration.String() != "1m30s" {
		t.Errorf("Duration.String() = %q, 期待値は%q", duration.String(), "1m30s")
	}
}

// ----------------------------------------

/*
writeTestFile は、テスト用ディレクトリへ指定内容のファイルを作成します。

機能:
  - 指定した名前と内容のテストファイルを所有者限定の権限で作成する

引数:
  - t *testing.T: テスト状態を管理する値
  - dir string: テストファイルを格納するディレクトリ
  - name string: 作成するファイル名
  - content string: ファイルへ書き込む内容

返り値:
  - なし
*/
func writeTestFile(t *testing.T, dir string, name string, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("テストファイル %q を作成できません: %v", path, err)
	}
}
