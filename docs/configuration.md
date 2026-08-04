# 設定仕様

## 読み込み規則

- 既定の設定ディレクトリは `conf`。
- `-conf` または `MARKET_DATA_COLLECTOR_CONF_DIR` で変更できる。
- ディレクトリ直下で拡張子が正確に `.toml` のファイルだけを読む。
- ファイル名昇順で既定値へマージする。
- 後のファイルで指定した項目だけが前の値を上書きする。
- TOMLファイルが0件、解析失敗、未知項目、統合後の不正値は起動エラーにする。

リポジトリには既定の `conf/default.toml` がある。現在はPython providerが有効なため、Python環境を構築するか、両Python providerを `false` へ上書きした上で、ルートから `go run .` で起動する。端末固有設定は `conf/90-local.local.toml` などへ保存する。この拡張名も `.toml` なので読み込まれ、Gitでは無視される。

## `[SYSTEM]`

| 項目 | 既定値 | 制約・内容 |
| --- | --- | --- |
| `Port` | `8080` | 1～65535 |
| `RequestTimeout` | `60s` | 1秒～10分。REST/MCP共通 |
| `MaxRequestBytes` | `1048576` | 1～16 MiB |

待受ホストを選ぶ設定はない。サーバーは常に指定Portの全ネットワークインターフェースで待ち受ける。たとえば既定値は `:8080` であり、`127.0.0.1:8080` だけに限定されない。

## `[providers.nikkei225jp]`

| 項目 | 既定値 | 内容 |
| --- | --- | --- |
| `enabled` | `true` | 収集を許可する |
| `base_url` | `https://225225.jp` | HTTP/HTTPSの同一ホスト固定パス用オリジン。パス、クエリ、フラグメントは指定不可 |
| `timeout` | `10s` | 1秒～5分。225225.jpへの1 HTTP要求期限 |
| `user_agent` | `MarketDataCollector/0.1` | 225225.jpへ送る、利用元を識別可能にするUser-Agent文字列。空文字とHTTP headerで禁止された制御文字は不可 |
| `max_response_bytes` | `4194304` | 1～16 MiB。通常レスポンス本文上限。既定は4 MiB |
| `max_chart_response_bytes` | `33554432` | 1～64 MiB。チャート本文上限。既定は32 MiB |

225225.jpの上流レスポンスはローカルに保持しない。`catalog` を除く収集要求ごとに上流へ接続し、自動再試行は行わない。

## `[providers.yfinance]`

| 項目 | 既定値 | 内容 |
| --- | --- | --- |
| `enabled` | `true` | 同梱 `conf/default.toml` の実効値。yfinance providerの収集を許可する |

## `[providers.investingpy]`

| 項目 | 既定値 | 内容 |
| --- | --- | --- |
| `enabled` | `true` | 同梱 `conf/default.toml` の実効値。investingpy識別子のproviderの収集を許可する |

Go内部の基底値は両providerとも `false` だが、同梱の `conf/default.toml` は現在両方を `true` で上書きする。各providerは独立して設定でき、`enabled=true` のproviderだけが `datalist` に掲載される。`false` のproviderを `collect` に指定しても、存在しないproviderと同じ `NOT_FOUND` になる。

## `[python]`

| 項目 | 既定値 | 内容 |
| --- | --- | --- |
| `executable` | `python` | Python実行ファイル名またはパス。空文字とNUL文字は不可 |
| `script` | `python/collector.py` | 標準入出力adapter。空文字とNUL文字は不可 |
| `timeout` | `60s` | 1秒～10分。子プロセス1件の期限 |
| `max_response_bytes` | `16777216` | 1～64 MiB。stdoutの最大保持サイズ |
| `max_concurrent_processes` | `2` | 1～8。2つのPython providerで共有する子プロセス専用枠 |

`[python]` はyfinanceとinvestingpyの共有実行設定である。いずれかのproviderが有効な場合は起動時に実行ファイルとscriptの存在を確認する。各Pythonライブラリのimportは要求時に行い、不足時はGoサーバーを落とさず `PROVIDER_UNAVAILABLE` を返す。

Python要求は `max_concurrent_processes` の専用枠を取得して子プロセスを起動する。専用枠待ちも `timeout` に含むため、混雑時は子プロセスを起動する前に `TIMEOUT` となる場合がある。

通常の依存インストールには、CPython 3.14 / Windowsで検証した `python/requirements.lock.txt` を使う。直接依存だけの `python/requirements.txt` はlock更新用の入力である。hashは固定していないため、公開配布物では対象環境ごとのhash付きlockとSBOMを別途生成することを推奨する。

## 旧設定からの移行

未知項目を拒否するため、旧構成のままでは起動できない。次のように移行する。

- `[SYSTEM]` から現在の定義にない旧項目を削除する。
- 旧 `[http]` の `timeout` と `user_agent` を `[providers.nikkei225jp]` へ移す。
- 225225.jpの旧ローカル保持期間項目を削除する。
- 旧 `[providers.python]` の有効フラグを `[providers.yfinance].enabled` と `[providers.investingpy].enabled` へ分ける。
- 旧 `[providers.python]` のPython実行項目をトップレベル `[python]` へ移す。

## ネットワーク公開時の注意

アプリケーションは常に全インターフェースで待ち受け、Origin制限を行わない。CORSは `Access-Control-Allow-Origin: *` であり、ブラウザを含む任意Originから利用できる。CORSはアクセス制御ではない。

したがって、サーバーへ到達可能な利用者は全員、RESTとMCPから収集処理を実行できる。操作が読取専用でも、外部providerへの通信、上流負荷、利用規約・データ利用条件のリスクは残る。

意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御する。あわせてOS側のCPU・メモリ・プロセス制限を適用する。
