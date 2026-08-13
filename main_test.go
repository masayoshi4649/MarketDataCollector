package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/masayoshi4649/MarketDataCollector/internal/config"
)

/*
TestBuildCollectorsRegistersDefaultEnabledProviders は、初期provider構成を検証します。

機能:
  - 既定で有効な225225.jp、kabus-controller、Polymarketを設定順に登録する
  - 既定で無効なJ-QuantsおよびPython providerを一覧から除外する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsRegistersDefaultEnabledProviders(t *testing.T) {
	collectors, err := buildCollectors(config.Default())
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 3 {
		t.Fatalf("provider件数 = %d, 3を期待", len(collectors))
	}
	providerNames := []string{
		collectors[0].Descriptor().Name,
		collectors[1].Descriptor().Name,
		collectors[2].Descriptor().Name,
	}
	if providerNames[0] != "225225jp" ||
		providerNames[1] != "kabus-controller" ||
		providerNames[2] != "polymarket" {
		t.Errorf("provider順 = %v, [225225jp kabus-controller polymarket]を期待", providerNames)
	}
}

// ----------------------------------------

/*
TestBuildCollectorsRegistersJQuantsProviderForStandardPlan は、Standard契約のJ-Quants provider登録を検証します。

機能:
  - J-Quantsだけを有効にして専用HTTPクライアントとcollectorを生成する
  - provider名とStandard契約で利用可能なデータセット件数を確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsRegistersJQuantsProviderForStandardPlan(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.Nikkei225JP.Enabled = false
	cfg.Providers.JQuants.Enabled = true
	cfg.Providers.JQuants.APIKey = "test-api-key"
	cfg.Providers.JQuants.Plan = "standard"
	cfg.Providers.JQuants.Addons = []string{}
	cfg.Providers.KabusController.Enabled = false
	cfg.Providers.Polymarket.Enabled = false
	cfg.Providers.YFinance.Enabled = false
	cfg.Providers.InvestingPy.Enabled = false

	collectors, err := buildCollectors(cfg)
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 1 {
		t.Fatalf("provider件数 = %d, 1を期待", len(collectors))
	}
	descriptor := collectors[0].Descriptor()
	if descriptor.Name != "jquants" {
		t.Errorf("provider = %+v, jquantsを期待", descriptor)
	}
	if len(descriptor.Datasets) != 19 {
		t.Errorf("Standardのデータセット件数 = %d, 19を期待", len(descriptor.Datasets))
	}
}

// ----------------------------------------

/*
TestBuildCollectorsKeepsGoProviderRegistrationOrder は、Go providerの固定登録順を検証します。

機能:
  - 225225.jp、J-Quants、kabus-controller、Polymarketを同時に有効化する
  - datalistへ掲載されるprovider順が設定構造や有効状態に左右されないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsKeepsGoProviderRegistrationOrder(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.JQuants.Enabled = true
	cfg.Providers.JQuants.APIKey = "test-api-key"
	cfg.Providers.YFinance.Enabled = false
	cfg.Providers.InvestingPy.Enabled = false

	collectors, err := buildCollectors(cfg)
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	providerNames := make([]string, len(collectors))
	for index, collector := range collectors {
		providerNames[index] = collector.Descriptor().Name
	}
	want := []string{"225225jp", "jquants", "kabus-controller", "polymarket"}
	if strings.Join(providerNames, ",") != strings.Join(want, ",") {
		t.Errorf("provider順 = %v, %vを期待", providerNames, want)
	}
}

// ----------------------------------------

/*
TestBuildCollectorsRegistersKabusControllerProvider は、kabus-controller providerの登録を検証します。

機能:
  - kabus-controllerだけを有効にして専用HTTPクライアントとcollectorを生成する
  - provider名と公開される18データセットを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsRegistersKabusControllerProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.Nikkei225JP.Enabled = false
	cfg.Providers.JQuants.Enabled = false
	cfg.Providers.KabusController.Enabled = true
	cfg.Providers.Polymarket.Enabled = false
	cfg.Providers.YFinance.Enabled = false
	cfg.Providers.InvestingPy.Enabled = false

	collectors, err := buildCollectors(cfg)
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 1 {
		t.Fatalf("provider件数 = %d, 1を期待", len(collectors))
	}
	descriptor := collectors[0].Descriptor()
	if descriptor.Name != "kabus-controller" {
		t.Errorf("provider = %+v, kabus-controllerを期待", descriptor)
	}
	if len(descriptor.Datasets) != 18 {
		t.Errorf("kabus-controllerのデータセット件数 = %d, 18を期待", len(descriptor.Datasets))
	}
}

// ----------------------------------------

/*
TestBuildCollectorsRegistersPolymarketProvider は、Polymarket providerの登録を検証します。

機能:
  - Polymarketだけを有効にして公開3 API用クライアントとcollectorを生成する
  - provider名と公開されるデータセット件数を確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsRegistersPolymarketProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.Nikkei225JP.Enabled = false
	cfg.Providers.JQuants.Enabled = false
	cfg.Providers.KabusController.Enabled = false
	cfg.Providers.Polymarket.Enabled = true
	cfg.Providers.YFinance.Enabled = false
	cfg.Providers.InvestingPy.Enabled = false

	collectors, err := buildCollectors(cfg)
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 1 {
		t.Fatalf("provider件数 = %d, 1を期待", len(collectors))
	}
	descriptor := collectors[0].Descriptor()
	if descriptor.Name != "polymarket" {
		t.Errorf("provider = %+v, polymarketを期待", descriptor)
	}
	if len(descriptor.Datasets) != 37 {
		t.Errorf("Polymarketのデータセット件数 = %d, 37を期待", len(descriptor.Datasets))
	}
}

// ----------------------------------------

/*
TestBuildCollectorsRegistersPythonProvidersIndependently は、Python providerの個別登録を検証します。

機能:
  - yfinanceだけを有効にして共有runnerを生成する
  - investingpyは別設定がfalseなら一覧へ登録しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsRegistersPythonProvidersIndependently(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	cfg := config.Default()
	cfg.Providers.KabusController.Enabled = false
	cfg.Providers.Polymarket.Enabled = false
	cfg.Providers.YFinance.Enabled = true
	cfg.Python.Executable = executable
	cfg.Python.Script = "main.go"
	collectors, err := buildCollectors(cfg)
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 2 {
		t.Fatalf("provider件数 = %d, 2を期待", len(collectors))
	}
	if descriptor := collectors[1].Descriptor(); descriptor.Name != "yfinance" {
		t.Errorf("2件目のprovider = %+v, yfinanceを期待", descriptor)
	}
}

// ----------------------------------------

/*
TestBuildCollectorsAllowsAllProvidersDisabled は、全providerを一覧から外せることを検証します。

機能:
  - 6つのenabledがすべてfalseの場合に空のprovider一覧を返す
  - 使用しないHTTPクライアントやPython runnerを生成しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsAllowsAllProvidersDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.Nikkei225JP.Enabled = false
	cfg.Providers.JQuants.Enabled = false
	cfg.Providers.KabusController.Enabled = false
	cfg.Providers.Polymarket.Enabled = false
	cfg.Providers.YFinance.Enabled = false
	cfg.Providers.InvestingPy.Enabled = false

	collectors, err := buildCollectors(cfg)
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 0 {
		t.Errorf("provider件数 = %d, 0を期待", len(collectors))
	}
}

// ----------------------------------------

/*
TestListenAddressUsesAllNetworkInterfaces は、固定の待受ホスト仕様を検証します。

機能:
  - 設定されたポートだけを使ってホスト部が空の待受アドレスを生成する
  - 配置先ごとのホスト名やIPアドレスを設定不要にする

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestListenAddressUsesAllNetworkInterfaces(t *testing.T) {
	if actual := listenAddress(8080); actual != ":8080" {
		t.Errorf("listenAddress(8080) = %q, :8080を期待", actual)
	}
}

// ----------------------------------------

/*
TestRunReturnsHelpWithoutStartingServer は、ヘルプ要求時の終了条件を検証します。

機能:
  - -hで設定読込やHTTP待受へ進まずflag.ErrHelpを返す
  - 利用方法を標準エラー出力先へ表示する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestRunReturnsHelpWithoutStartingServer(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), []string{"-h"}, &stdout, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("run(-h) error = %v, flag.ErrHelpを期待", err)
	}
	if !strings.Contains(stderr.String(), "-conf") {
		t.Errorf("ヘルプ出力 = %q, -confの説明を期待", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("標準出力 = %q, 空を期待", stdout.String())
	}
}
