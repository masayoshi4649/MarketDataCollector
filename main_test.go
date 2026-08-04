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
TestBuildCollectorsRegistersOnlyDefaultEnabledProvider は、初期provider構成を検証します。

機能:
  - 既定で有効な225225.jpだけを登録する
  - 既定で無効なPython providerを一覧から除外する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsRegistersOnlyDefaultEnabledProvider(t *testing.T) {
	collectors, err := buildCollectors(config.Default())
	if err != nil {
		t.Fatalf("buildCollectors() error = %v", err)
	}
	if len(collectors) != 1 {
		t.Fatalf("provider件数 = %d, 1を期待", len(collectors))
	}
	descriptor := collectors[0].Descriptor()
	if descriptor.Name != "225225jp" {
		t.Errorf("provider = %+v, 225225jpを期待", descriptor)
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
  - 3つのenabledがすべてfalseの場合に空のprovider一覧を返す
  - 使用しないHTTPクライアントやPython runnerを生成しない

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestBuildCollectorsAllowsAllProvidersDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.Nikkei225JP.Enabled = false

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
