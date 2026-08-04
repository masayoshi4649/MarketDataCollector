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
  - 225225.jp固有HTTP設定と各providerの有効状態を確認する

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
	if cfg.Providers.YFinance.Enabled || cfg.Providers.InvestingPy.Enabled {
		t.Error("利用条件の確認が必要なPython providerが既定値で有効です")
	}
}

// ----------------------------------------

/*
TestLoadDir は、分割したTOMLテーブルをファイル名順に統合することを検証します。

機能:
  - SYSTEM、python、3つのproviderテーブルを新構造へ復号する
  - 後順位ファイルが指定項目だけを上書きする
  - yfinanceとinvestingpyを独立して有効化できることを確認する

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
