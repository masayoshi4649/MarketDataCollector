package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/config"
	"github.com/masayoshi4649/MarketDataCollector/internal/httpserver"
	"github.com/masayoshi4649/MarketDataCollector/internal/mcpserver"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider/jquants"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider/kabuscontroller"
	nikkei225provider "github.com/masayoshi4649/MarketDataCollector/internal/provider/nikkei225"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider/nikkei225jp"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider/polymarket"
	pythonprovider "github.com/masayoshi4649/MarketDataCollector/internal/provider/python"
	"github.com/masayoshi4649/MarketDataCollector/internal/restapi"
	"github.com/masayoshi4649/MarketDataCollector/internal/service"
)

const (
	configurationDirectoryEnvironmentName = "MARKET_DATA_COLLECTOR_CONF_DIR"
	gracefulShutdownTimeout               = 10 * time.Second
)

// main は、OSシグナル対応のコンテキストでHTTPサーバーを起動します。
//
// 引数:
//   - なし。os.Argsをコマンドライン入力として使用する。
//
// 返り値:
//   - なし。起動または実行失敗時は標準エラーへ記録して終了コード1にする。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go restoreSignalHandlingAfterCancellation(ctx, stop)
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		_, _ = fmt.Fprintf(os.Stderr, "MarketDataCollectorを実行できません: %v\n", err)
		os.Exit(1)
	}
}

// restoreSignalHandlingAfterCancellation は、最初の停止シグナル後に既定動作へ戻します。
//
// 引数:
//   - ctx context.Context: 最初のシグナルでキャンセルされるコンテキスト。
//   - stop context.CancelFunc: signal.NotifyContextの通知登録を解除する関数。
//
// 返り値:
//   - なし。安全停止中の2回目以降のシグナルをOS既定動作へ渡す。
func restoreSignalHandlingAfterCancellation(ctx context.Context, stop context.CancelFunc) {
	<-ctx.Done()
	stop()
}

// run は、設定読込、依存組み立て、HTTPサーバー実行を順に行います。
//
// 引数:
//   - ctx context.Context: OSシグナルでキャンセルされる実行コンテキスト。
//   - arguments []string: -confを含むコマンドライン引数。
//   - stdout io.Writer: 通常ログの出力先。
//   - stderr io.Writer: 引数解析エラーの出力先。
//
// 返り値:
//   - error: ヘルプ表示時のflag.ErrHelp、または引数、設定、依存生成、待受、停止の失敗。
func run(ctx context.Context, arguments []string, stdout io.Writer, stderr io.Writer) error {
	configurationDirectory := strings.TrimSpace(os.Getenv(configurationDirectoryEnvironmentName))
	if configurationDirectory == "" {
		configurationDirectory = "conf"
	}
	flags := flag.NewFlagSet("MarketDataCollector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configurationDirectoryFlag := flags.String("conf", configurationDirectory, "TOML設定ディレクトリ")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未対応の引数があります: %s", strings.Join(flags.Args(), " "))
	}

	cfg, err := config.LoadDir(*configurationDirectoryFlag)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	collectors, err := buildCollectors(cfg)
	if err != nil {
		return err
	}
	collectionService, err := service.New(collectors)
	if err != nil {
		return fmt.Errorf("収集サービスを生成できません: %w", err)
	}
	restServer, err := restapi.New(collectionService, cfg.System.MaxRequestBytes, logger)
	if err != nil {
		return fmt.Errorf("REST APIを生成できません: %w", err)
	}
	mcpServer, err := mcpserver.New(collectionService, cfg.System.MaxRequestBytes, logger)
	if err != nil {
		return fmt.Errorf("MCPを生成できません: %w", err)
	}
	applicationHandler, err := httpserver.NewHandler(
		restServer.Handler(),
		mcpServer.Handler(),
		httpserver.Options{
			RequestTimeout: cfg.System.RequestTimeout.Duration,
		},
	)
	if err != nil {
		return fmt.Errorf("HTTPルートを生成できません: %w", err)
	}

	address := listenAddress(cfg.System.Port)
	httpServer := &http.Server{
		Addr:              address,
		Handler:           applicationHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      cfg.System.RequestTimeout.Duration + 15*time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 * 1024 * 1024,
	}
	logger.Info(
		"MarketDataCollectorを起動します",
		"address", address,
		"rest_datalist", "/api/datalist",
		"rest_collect", "/api/collect",
		"mcp", "/mcp",
	)
	return serve(ctx, httpServer, gracefulShutdownTimeout, logger)
}

// ----------------------------------------

// listenAddress は、全ネットワークインターフェースで待ち受けるアドレスを生成します。
//
// 引数:
//   - port int: 設定で検証済みのTCPポート番号。
//
// 返り値:
//   - string: ホスト部を空にした「:ポート」形式の待受アドレス。
func listenAddress(port int) string {
	return fmt.Sprintf(":%d", port)
}

// ----------------------------------------

// buildCollectors は、設定で有効なproviderと共有runnerを組み立てます。
//
// 引数:
//   - cfg config.Config: 検証済みの全アプリケーション設定。
//
// 返り値:
//   - []provider.Collector: 有効な225225.jp、J-Quants、kabus-controller、Polymarket、yfinance、investingpyの設定順一覧。
//   - error: HTTPクライアント、各Go provider、Python実行環境の生成エラー。
func buildCollectors(cfg config.Config) ([]provider.Collector, error) {
	collectors := make([]provider.Collector, 0, 6)
	if cfg.Providers.Nikkei225JP.Enabled {
		httpClient := &http.Client{Timeout: cfg.Providers.Nikkei225JP.Timeout.Duration}
		nikkeiClient, err := nikkei225jp.NewClient(nikkei225jp.Config{
			BaseURL:               cfg.Providers.Nikkei225JP.BaseURL,
			HTTPClient:            httpClient,
			UserAgent:             cfg.Providers.Nikkei225JP.UserAgent,
			MaxResponseBytes:      cfg.Providers.Nikkei225JP.MaxResponseBytes,
			MaxChartResponseBytes: cfg.Providers.Nikkei225JP.MaxChartResponseBytes,
		})
		if err != nil {
			return nil, fmt.Errorf("225225.jpクライアントを生成できません: %w", err)
		}
		nikkeiCollector, err := nikkei225provider.New(nikkeiClient)
		if err != nil {
			return nil, fmt.Errorf("225225.jp providerを生成できません: %w", err)
		}
		collectors = append(collectors, nikkeiCollector)
	}

	// ----------------------------------------

	if cfg.Providers.JQuants.Enabled {
		httpClient := &http.Client{Timeout: cfg.Providers.JQuants.Timeout.Duration}
		jQuantsClient, err := jquants.NewClient(jquants.ClientConfig{
			BaseURL:          cfg.Providers.JQuants.BaseURL,
			APIKey:           cfg.Providers.JQuants.APIKey,
			HTTPClient:       httpClient,
			UserAgent:        cfg.Providers.JQuants.UserAgent,
			MaxResponseBytes: cfg.Providers.JQuants.MaxResponseBytes,
		})
		if err != nil {
			return nil, fmt.Errorf("J-Quants APIクライアントを生成できません: %w", err)
		}
		jQuantsCollector, err := jquants.NewCollector(jQuantsClient, jquants.Options{
			Plan:   cfg.Providers.JQuants.Plan,
			Addons: cfg.Providers.JQuants.Addons,
		})
		if err != nil {
			return nil, fmt.Errorf("J-Quants providerを生成できません: %w", err)
		}
		collectors = append(collectors, jQuantsCollector)
	}

	// ----------------------------------------

	if cfg.Providers.KabusController.Enabled {
		httpClient := &http.Client{Timeout: cfg.Providers.KabusController.Timeout.Duration}
		kabusControllerClient, err := kabuscontroller.NewClient(kabuscontroller.ClientConfig{
			BaseURL:          cfg.Providers.KabusController.BaseURL,
			HTTPClient:       httpClient,
			UserAgent:        cfg.Providers.KabusController.UserAgent,
			MaxResponseBytes: cfg.Providers.KabusController.MaxResponseBytes,
		})
		if err != nil {
			return nil, fmt.Errorf("kabus-controller APIクライアントを生成できません: %w", err)
		}
		kabusControllerCollector, err := kabuscontroller.NewCollector(kabusControllerClient)
		if err != nil {
			return nil, fmt.Errorf("kabus-controller providerを生成できません: %w", err)
		}
		collectors = append(collectors, kabusControllerCollector)
	}

	// ----------------------------------------

	if cfg.Providers.Polymarket.Enabled {
		httpClient := &http.Client{Timeout: cfg.Providers.Polymarket.Timeout.Duration}
		polymarketClient, err := polymarket.NewClient(polymarket.ClientConfig{
			GammaBaseURL:     cfg.Providers.Polymarket.GammaBaseURL,
			CLOBBaseURL:      cfg.Providers.Polymarket.CLOBBaseURL,
			DataBaseURL:      cfg.Providers.Polymarket.DataBaseURL,
			HTTPClient:       httpClient,
			UserAgent:        cfg.Providers.Polymarket.UserAgent,
			MaxResponseBytes: cfg.Providers.Polymarket.MaxResponseBytes,
		})
		if err != nil {
			return nil, fmt.Errorf("Polymarket APIクライアントを生成できません: %w", err)
		}
		polymarketCollector, err := polymarket.NewCollector(polymarketClient)
		if err != nil {
			return nil, fmt.Errorf("Polymarket providerを生成できません: %w", err)
		}
		collectors = append(collectors, polymarketCollector)
	}

	// ----------------------------------------

	var pythonRunner pythonprovider.Runner
	pythonEnabled := cfg.Providers.YFinance.Enabled || cfg.Providers.InvestingPy.Enabled
	if pythonEnabled {
		if _, err := exec.LookPath(cfg.Python.Executable); err != nil {
			return nil, fmt.Errorf("python実行ファイルが見つかりません: %w", err)
		}
		fileInfo, err := os.Stat(cfg.Python.Script)
		if err != nil {
			return nil, fmt.Errorf("pythonアダプタースクリプトを参照できません: %w", err)
		}
		if fileInfo.IsDir() {
			return nil, errors.New("pythonアダプタースクリプトにディレクトリは指定できません")
		}
		pythonRunner, err = pythonprovider.NewSubprocessRunner(
			cfg.Python.Executable,
			cfg.Python.Script,
			cfg.Python.Timeout.Duration,
			cfg.Python.MaxResponseBytes,
			cfg.Python.MaxConcurrentProcesses,
		)
		if err != nil {
			return nil, fmt.Errorf("python runnerを生成できません: %w", err)
		}
	}
	if cfg.Providers.YFinance.Enabled {
		yfinanceCollector, err := pythonprovider.NewCollector("yfinance", pythonRunner)
		if err != nil {
			return nil, fmt.Errorf("yfinance providerを生成できません: %w", err)
		}
		collectors = append(collectors, yfinanceCollector)
	}
	if cfg.Providers.InvestingPy.Enabled {
		investingpyCollector, err := pythonprovider.NewCollector("investingpy", pythonRunner)
		if err != nil {
			return nil, fmt.Errorf("investingpy providerを生成できません: %w", err)
		}
		collectors = append(collectors, investingpyCollector)
	}
	return collectors, nil
}

// serve は、HTTPサーバーを起動しキャンセル時に安全停止します。
//
// 引数:
//   - ctx context.Context: 停止要求を通知する実行コンテキスト。
//   - server *http.Server: 起動する設定済みHTTPサーバー。
//   - shutdownTimeout time.Duration: 安全停止を待つ最大時間。
//   - logger *slog.Logger: 起動終了を記録するロガー。
//
// 返り値:
//   - error: 待受または安全停止に失敗した場合のエラー。
func serve(
	ctx context.Context,
	server *http.Server,
	shutdownTimeout time.Duration,
	logger *slog.Logger,
) error {
	serverError := make(chan error, 1)
	go func() {
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTPサーバーが異常終了しました: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("HTTPサーバーを安全停止できません: %w", err)
		}
		logger.Info("MarketDataCollectorを停止しました")
		return nil
	}
}
