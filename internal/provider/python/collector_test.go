package python

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type fakeRunner struct {
	provider   string
	dataset    string
	parameters map[string]any
	result     domain.ProviderResult
	err        error
}

/*
Collect は、Python providerから渡された要求を記録して固定結果を返します。

機能:
  - 子プロセスを起動せずCollectorの振り分けを検証できるようにする

引数:
  - ctx context.Context: 要求コンテキスト
  - provider string: Python provider識別子
  - dataset string: データセット識別子
  - parameters map[string]any: provider固有入力

返り値:
  - domain.ProviderResult: 設定済みの結果
  - error: 設定済みのエラー
*/
func (f *fakeRunner) Collect(
	ctx context.Context,
	provider string,
	dataset string,
	parameters map[string]any,
) (domain.ProviderResult, error) {
	_ = ctx
	f.provider = provider
	f.dataset = dataset
	f.parameters = parameters
	return f.result, f.err
}

// ----------------------------------------

/*
TestCollectorPublishesAndRunsAllowlistedDataset は、Python provider仕様と振り分けを検証します。

機能:
  - yfinanceの固定データセット一覧を公開することを確認する
  - 許可済みdatasetだけをrunnerへ渡すことを確認する
  - 未知datasetをrunner実行前に拒否することを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorPublishesAndRunsAllowlistedDataset(t *testing.T) {
	runner := &fakeRunner{result: domain.ProviderResult{Data: map[string]any{"price": 123}}}
	collector, err := NewCollector("yfinance", runner)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	descriptor := collector.Descriptor()
	if descriptor.Name != "yfinance" || len(descriptor.Datasets) != 10 {
		t.Fatalf("Descriptor() = %+v, yfinanceの10データセットを期待", descriptor)
	}

	parameters := map[string]any{"ticker": "AAPL"}
	if _, err := collector.Collect(context.Background(), "quote", parameters); err != nil {
		t.Fatalf("Collect(quote) error = %v", err)
	}
	if runner.provider != "yfinance" || runner.dataset != "quote" || runner.parameters["ticker"] != "AAPL" {
		t.Errorf("runner要求 = (%q, %q, %+v)", runner.provider, runner.dataset, runner.parameters)
	}
	_, err = collector.Collect(context.Background(), "arbitrary_function", parameters)
	var serviceErr *domain.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Kind != domain.ErrorNotFound {
		t.Errorf("未知dataset error = %v, NOT_FOUNDを期待", err)
	}
}

// ----------------------------------------

/*
TestNewCollectorRejectsTypedNilRunner は、interface内の型付きnilを検証します。

機能:
  - providerへ型付きnilのSubprocessRunnerを渡した場合に拒否する
  - Collect時のnilポインター参照を起動時に防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewCollectorRejectsTypedNilRunner(t *testing.T) {
	var runner *SubprocessRunner
	if _, err := NewCollector("yfinance", runner); err == nil {
		t.Fatal("NewCollector() error = nil, 型付きnil runnerの拒否を期待")
	}
}

// ----------------------------------------

/*
TestLimitedBufferBoundsMemory は、Python出力bufferの上限処理を検証します。

機能:
  - 上限までのバイトだけを保持する
  - 呼び出し元には全バイト受理を通知して超過状態を記録する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestLimitedBufferBoundsMemory(t *testing.T) {
	buffer := newLimitedBuffer(4)
	written, err := buffer.Write([]byte("abcdef"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != 6 || string(buffer.Bytes()) != "abcd" || !buffer.Exceeded() {
		t.Errorf("buffer = (%d, %q, %t), (6, abcd, true)を期待", written, buffer.String(), buffer.Exceeded())
	}
}

// ----------------------------------------

/*
TestSubprocessRunnerClassifiesErrorsAndPreservesUTF8 は、Python境界の実プロセス契約を検証します。

機能:
  - 非ASCII文字をUTF-8 JSONとして往復する
  - 専用終了コードと構造化JSONを共通エラー分類へ変換する
  - 親プロセスの任意秘密値を子プロセスへ継承しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestSubprocessRunnerClassifiesErrorsAndPreservesUTF8(t *testing.T) {
	executable, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python実行ファイルがないため実プロセステストを省略します")
	}
	t.Setenv("MARKET_DATA_COLLECTOR_TEST_SECRET", "子プロセスへ渡さない秘密")
	script := writeBridgeTestScript(t)
	runner, err := NewSubprocessRunner(executable, script, 5*time.Second, 64*1024, 2)
	if err != nil {
		t.Fatalf("NewSubprocessRunner() error = %v", err)
	}

	result, err := runner.Collect(
		context.Background(),
		"yfinance",
		"echo",
		map[string]any{"value": "日本株"},
	)
	if err != nil {
		t.Fatalf("Collect(echo) error = %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["value"] != "日本株" {
		t.Fatalf("Collect(echo) data = %#v, UTF-8文字列を期待", result.Data)
	}
	if inherited, exists := data["inherited_secret"]; exists && inherited != nil {
		t.Fatalf("Python子プロセスが秘密値を継承しました: %#v", inherited)
	}

	testCases := []struct {
		dataset string
		kind    domain.ErrorKind
	}{
		{dataset: "invalid", kind: domain.ErrorInvalidArgument},
		{dataset: "unavailable", kind: domain.ErrorProviderUnavailable},
		{dataset: "upstream", kind: domain.ErrorUpstream},
	}
	for _, testCase := range testCases {
		t.Run(testCase.dataset, func(t *testing.T) {
			_, collectErr := runner.Collect(
				context.Background(), "yfinance", testCase.dataset, map[string]any{},
			)
			assertPythonServiceErrorKind(t, collectErr, testCase.kind)
		})
	}
	for _, dataset := range []string{"missing_data", "extra_field"} {
		t.Run(dataset, func(t *testing.T) {
			_, collectErr := runner.Collect(
				context.Background(), "yfinance", dataset, map[string]any{},
			)
			assertPythonServiceErrorKind(t, collectErr, domain.ErrorProviderUnavailable)
		})
	}
}

// ----------------------------------------

/*
TestSubprocessRunnerHonorsTimeout は、子プロセスの期限と待機上限を検証します。

機能:
  - 長時間処理を設定期限で中止する
  - TIMEOUTへ分類して呼び出しを有限時間内に返す

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestSubprocessRunnerHonorsTimeout(t *testing.T) {
	executable, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python実行ファイルがないため実プロセステストを省略します")
	}
	runner, err := NewSubprocessRunner(
		executable, writeBridgeTestScript(t), 100*time.Millisecond, 64*1024, 1,
	)
	if err != nil {
		t.Fatalf("NewSubprocessRunner() error = %v", err)
	}
	startedAt := time.Now()
	_, collectErr := runner.Collect(
		context.Background(), "yfinance", "timeout", map[string]any{},
	)
	assertPythonServiceErrorKind(t, collectErr, domain.ErrorTimeout)
	if elapsed := time.Since(startedAt); elapsed > pythonProcessWaitDelay+2*time.Second {
		t.Fatalf("timeout処理時間 = %s, 有限時間内の返却を期待", elapsed)
	}
}

// ----------------------------------------

/*
TestProductionBridgeUsesUTF8OnWindowsPipes は、本番Pythonアダプターの文字コード契約を検証します。

機能:
  - 本番collector.pyへ日本語を含む不正provider要求をUTF-8で渡す
  - 入力値を含む日本語エラーJSONがGoへ文字化けせず戻ることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestProductionBridgeUsesUTF8OnWindowsPipes(t *testing.T) {
	executable, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python実行ファイルがないため実プロセステストを省略します")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("テストソースのパスを取得できません")
	}
	script := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "python", "collector.py"))
	runner, err := NewSubprocessRunner(executable, script, 5*time.Second, 64*1024, 2)
	if err != nil {
		t.Fatalf("NewSubprocessRunner() error = %v", err)
	}
	_, collectErr := runner.Collect(
		context.Background(), "日本市場", "quote", map[string]any{"ticker": "トヨタ"},
	)
	assertPythonServiceErrorKind(t, collectErr, domain.ErrorInvalidArgument)
	if !strings.Contains(collectErr.Error(), "日本市場") {
		t.Fatalf("UTF-8入力値が失敗応答に保持されていません: %v", collectErr)
	}
}

// writeBridgeTestScript は、実プロセス境界用の固定Pythonスクリプトを生成します。
//
// 引数:
//   - t *testing.T: 一時ディレクトリと失敗報告を管理する値。
//
// 返り値:
//   - string: UTF-8で保存したテストスクリプトの絶対パス。
func writeBridgeTestScript(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "bridge_test.py")
	script := `import json
import os
import sys
import time

for stream in (sys.stdin, sys.stdout, sys.stderr):
    stream.reconfigure(encoding="utf-8", errors="strict")

request = json.load(sys.stdin)
dataset = request["dataset"]
if dataset == "echo":
    print(json.dumps({"data": {"value": request["parameters"]["value"], "inherited_secret": os.environ.get("MARKET_DATA_COLLECTOR_TEST_SECRET")}, "metadata": {"library": "fake"}}, ensure_ascii=False))
    raise SystemExit(0)
if dataset == "invalid":
    print(json.dumps({"error": {"kind": "INVALID_ARGUMENT", "message": "入力値が不正です"}}, ensure_ascii=False))
    raise SystemExit(2)
if dataset == "unavailable":
    print(json.dumps({"error": {"kind": "PROVIDER_UNAVAILABLE", "message": "実行環境を利用できません"}}, ensure_ascii=False))
    raise SystemExit(3)
if dataset == "upstream":
    print(json.dumps({"error": {"kind": "UPSTREAM_ERROR", "message": "上流取得に失敗しました"}}, ensure_ascii=False))
    raise SystemExit(4)
if dataset == "timeout":
    time.sleep(30)
if dataset == "missing_data":
    print(json.dumps({"metadata": {"library": "fake"}}))
    raise SystemExit(0)
if dataset == "extra_field":
    print(json.dumps({"data": None, "metadata": {"library": "fake"}, "extra": True}))
    raise SystemExit(0)
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("テストPythonスクリプトを書き込めません: %v", err)
	}
	return scriptPath
}

// assertPythonServiceErrorKind は、Python境界エラーの共通分類を確認します。
//
// 引数:
//   - t *testing.T: 失敗報告を管理する値。
//   - err error: Python runnerから返されたエラー。
//   - expected domain.ErrorKind: 期待する共通エラー分類。
//
// 返り値:
//   - なし。
func assertPythonServiceErrorKind(t *testing.T, err error, expected domain.ErrorKind) {
	t.Helper()
	var serviceErr *domain.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Kind != expected {
		t.Fatalf("error = %v, kind=%sを期待", err, expected)
	}
}
