# 設定仕様

## 読み込み規則

- 既定の設定ディレクトリは `conf`。
- `-conf` または `MARKET_DATA_COLLECTOR_CONF_DIR` で変更できる。
- ディレクトリ直下で拡張子が正確に `.toml` のファイルだけを読む。
- ファイル名昇順で既定値へマージする。
- 後のファイルで指定した項目だけが前の値を上書きする。
- TOMLファイルが0件、解析失敗、未知項目、統合後の不正値は起動エラーにする。

リポジトリには既定の `conf/default.toml` がある。現在はPython providerが有効なため、Python環境を構築するか、両Python providerを `false` へ上書きした上で、ルートから `go run .` で起動する。端末固有設定は、`default.toml` より後へ並ぶ `conf/zz-local.local.toml` などへ保存する。この拡張名も `.toml` なので読み込まれ、Gitでは無視される。J-Quantsの実APIキーは必ず `conf/*.local.toml` だけに保存し、追跡対象の `default.toml`、サンプル、文書には保存しない。

## `[SYSTEM]`

| 項目              | 既定値    | 制約・内容              |
| ----------------- | --------- | ----------------------- |
| `Port`            | `8080`    | 1～65535                |
| `RequestTimeout`  | `60s`     | 1秒～10分。REST/MCP共通 |
| `MaxRequestBytes` | `1048576` | 1～16 MiB               |

待受ホストを選ぶ設定はない。サーバーは常に指定Portの全ネットワークインターフェースで待ち受ける。たとえば既定値は `:8080` であり、`127.0.0.1:8080` だけに限定されない。

## `[providers.nikkei225jp]`

| 項目                       | 既定値                    | 内容                                                                                                   |
| -------------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------ |
| `enabled`                  | `true`                    | 収集を許可する                                                                                         |
| `base_url`                 | `https://225225.jp`       | HTTP/HTTPSの同一ホスト固定パス用オリジン。パス、クエリ、フラグメントは指定不可                         |
| `timeout`                  | `10s`                     | 1秒～5分。225225.jpへの1 HTTP要求期限                                                                  |
| `user_agent`               | `MarketDataCollector/0.1` | 225225.jpへ送る、利用元を識別可能にするUser-Agent文字列。空文字とHTTP headerで禁止された制御文字は不可 |
| `max_response_bytes`       | `4194304`                 | 1～16 MiB。通常レスポンス本文上限。既定は4 MiB                                                         |
| `max_chart_response_bytes` | `33554432`                | 1～64 MiB。チャート本文上限。既定は32 MiB                                                              |

225225.jpの上流レスポンスはローカルに保持しない。`catalog` を除く収集要求ごとに上流へ接続し、自動再試行は行わない。

## `[providers.jquants]`

J-Quants API v2へ直接接続するGoネイティブproviderの設定である。

| 項目                 | 既定値                    | 制約・内容                                                                                                        |
| -------------------- | ------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `enabled`            | `false`                   | J-Quants収集を許可する。実APIキーを追跡対象へ保存しないよう既定は無効                                             |
| `base_url`           | `https://api.jquants.com` | HTTP/HTTPSのAPIオリジン。userinfo、パス、クエリ、フラグメントは指定不可。実APIキー利用時は公式HTTPSから変更しない |
| `api_key`            | `""`                      | V2の `x-api-key` ヘッダーへ設定する秘密値。有効化時は必須。前後空白とHTTP制御文字は不可                           |
| `plan`               | `standard`                | `free`、`light`、`standard`、`premium` のいずれか。契約中のプランと合わせる                                       |
| `addons`             | `[]`                      | 契約済みアドオンの配列。`minute` と `tdnet` だけを許可し、未知値と重複は不可。Freeプランでは指定不可              |
| `timeout`            | `30s`                     | 1秒～5分。J-Quantsへの1 HTTP要求の期限                                                                            |
| `user_agent`         | `MarketDataCollector/0.1` | J-Quantsへ送るUser-Agent。空文字とHTTP headerで禁止された制御文字は不可                                           |
| `max_response_bytes` | `16777216`                | 1～64 MiB。未圧縮本文、Gzipヘッダーを含む圧縮本文、展開後本文の上限。既定は16 MiB                                 |

次の例は `conf/zz-jquants.local.toml` など、`default.toml` より後へ並ぶGit管理外の `conf/*.local.toml` だけに保存する。設定はファイル名順で読み込まれるため、既定値を上書きするローカルファイルを `default.toml` より前へ並べない。`YOUR_JQUANTS_API_KEY` は説明用のプレースホルダーであり、実際のキーを文書やGit差分へ貼り付けない。

```toml
[providers.jquants]
enabled = true
base_url = "https://api.jquants.com"
api_key = "YOUR_JQUANTS_API_KEY"
plan = "standard"
addons = []
timeout = "30s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

`plan` と `addons` は `datalist` に掲載するdatasetを制限する。Standardプラン、アドオンなしでは17データAPIとBulk API 2件の合計19 datasetを掲載し、cursor入力は公開しない。cursorはPremiumの `fins_summary`・`fins_details` とTDnetアドオンの `td_list` でだけ利用できる。Freeプランにアドオンを設定すると起動時に拒否する。詳細な30件の対応表は [J-Quants API v2 対応状況](jquants.md) に集約する。

同梱の `default.toml` では `jquants` が無効なため、現在の `datalist` は `225225jp`、`kabus-controller`、`polymarket`、`yfinance`、`investingpy` の5 providerである。Git管理外のローカル設定で `jquants` を有効化すると、6 providerになる。

`enabled=true` の場合だけ `api_key` を必須とする。providerはAPIキーを応答とmetadataに出力しない。不正なAPIキー設定の検証エラーも、値自体を含めず設定パスだけを示す。通信エラーはAPIキーの完全一致部分だけを伏せ、URL、query、接続先などの診断情報と `errors.Is` による原因判定を保持するため、queryへ独自の秘密値を入れない。

`http` は隔離したローカルテスト先のために許可している。実APIキーをHTTP接続で使うと `x-api-key` が暗号化されず送信されるため、実運用では `base_url = "https://api.jquants.com"` を保持する。

HTTPリダイレクトは通常どおり追跡する。同一originでは `x-api-key` を維持し、異なるoriginでは同ヘッダーだけを除去する。`max_response_bytes` は未圧縮本文だけでなく、Gzip圧縮本文と展開後本文の双方を制限する。

全J-Quants要求はプロセス内で共有する単一FIFOキューへ受付順に入り、基本・財務・株価分足／ティック・TDnetの独立quotaにより [公式レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)の50%で開始する。実効上限は基本枠がFree 2.5、Light 30、Standard 60、Premium 250要求/分、追加枠が財務30、株価分足・ティック30、TDnet 50要求/分である。キューとquotaはプロセス内だけで共有し、429を自動再試行しない。

[公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)は総ページ数、総件数、現在ページを返さない。本providerは1要求1ページとし、同じ検索条件へ最新 `pagination_key` を指定して継続し、キーが返らなくなった応答で完了と判断する。cursorは [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md) に従う日本時間当日の差分用の不透明値で、対象3 APIの最終ページにだけ返る。値を解釈・加工せず受け渡すが、pagination keyとcursorの自動追跡や永続化は行わない。

## `[providers.kabus-controller]`

kabus-controllerの先物・オプション登録一覧と板情報へ直接接続する、認証不要・読取専用のGoネイティブprovider設定である。Python設定は使用しない。

| 項目                 | 既定値                    | 制約・内容                                                                                                     |
| -------------------- | ------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `enabled`            | `true`                    | kabus-controller収集を許可する。`true` でも通信は `collect` 時だけであり、起動時と `datalist` では通信しない |
| `base_url`           | `http://10.10.100.1:8080` | HTTP/HTTPSのAPIオリジン。userinfo、パス、クエリ、フラグメントは指定不可                                        |
| `timeout`            | `15s`                     | 1秒～5分。kabus-controllerへの1 HTTP要求期限                                                                   |
| `user_agent`         | `MarketDataCollector/0.1` | kabus-controllerへ送るUser-Agent。空文字とHTTP headerで禁止された制御文字は不可                                |
| `max_response_bytes` | `16777216`                | 1～64 MiB。未圧縮・Gzip圧縮・展開後のHTTP応答本文上限。既定は16 MiB                                            |

```toml
[providers.kabus-controller]
enabled = true
base_url = "http://10.10.100.1:8080"
timeout = "15s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

`base_url` は現在のLAN内エンドポイントを既定値とする。上流の6つの固定GET以外へは接続せず、認証情報や取引操作を扱わない。ホスト、ネットワーク構成、ポートが変わった場合は、`default.toml` を直接編集せず、`conf/zz-kabus-controller.local.toml` など後順位のローカル設定でオリジンだけを上書きする。

既定値は平文HTTPである。通信内容を信頼できないネットワークへ流さず、到達範囲をLAN、VPN、ファイアウォール等で制限する。`enabled=true` でも起動確認は行わないため、`10.10.100.1:8080` へ到達できない環境でも起動と `datalist` は成功し、実際の `collect` 時にエラーとなる。

## `[providers.polymarket]`

Polymarketの公開Gamma、CLOB、Data APIへ直接接続する、認証不要・読取専用のGoネイティブprovider設定である。Python設定は使用しない。

| 項目                 | 既定値                             | 制約・内容                                                                                             |
| -------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `enabled`            | `true`                             | Polymarket収集を許可する。`true` でも通信は `collect` 時だけであり、起動時と `datalist` では通信しない |
| `gamma_base_url`     | `https://gamma-api.polymarket.com` | HTTP/HTTPSのGamma APIオリジン。userinfo、パス、クエリ、フラグメントは指定不可                          |
| `clob_base_url`      | `https://clob.polymarket.com`      | HTTP/HTTPSのCLOB APIオリジン。同じURL制約を適用                                                        |
| `data_base_url`      | `https://data-api.polymarket.com`  | HTTP/HTTPSのData APIオリジン。同じURL制約を適用                                                        |
| `timeout`            | `15s`                              | 1秒～5分。Polymarketへの1 HTTP要求期限。FIFOキュー待機は上位の要求期限対象                             |
| `user_agent`         | `MarketDataCollector/0.1`          | 3 APIへ送るUser-Agent。空文字とHTTP headerで禁止された制御文字は不可                                   |
| `max_response_bytes` | `16777216`                         | 1～64 MiB。未圧縮・Gzip圧縮・展開後のHTTP応答本文上限。既定は16 MiB                                    |

```toml
[providers.polymarket]
enabled = true
gamma_base_url = "https://gamma-api.polymarket.com"
clob_base_url = "https://clob.polymarket.com"
data_base_url = "https://data-api.polymarket.com"
timeout = "15s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

実オリジンは公式HTTPSから変更しない。`http` は隔離したローカルテスト先のために許可している。3 APIへAPIキーやwallet署名を送らず、固定許可した公開GETだけを実行する。

全Polymarket要求はプロセス内で共有する単一FIFOキューへ入り、1件ずつ、[公式レートリミット](https://docs.polymarket.com/api-reference/rate-limits)の50%以下で開始する。キュー待機は要求contextの期限対象で、429を自動再試行しない。ページングは検索の `page`、Gamma応答の `next_cursor` から次回の `after_cursor`、CLOBの `next_cursor`、Dataの `offset` を呼び出し側が管理し、自動追跡・結合しない。ページング対象では `total_pages_known` を常に返し、総ページ数の実値は上流が提供する場合だけ返す。Dataのoffset型応答では `has_more_known=false` とし、完了や次のoffsetを推測しない。

JSONは `UseNumber` で復号して標準JSONへ再帰的に正規化する。`max_response_bytes` を超える本文、不正JSON、余分なJSON値、非成功HTTP状態はエラーにする。詳細な37 dataset、`/price` の公式資料間の `side` 不整合、未実装範囲、規約と地域制限は [Polymarket公開API対応状況](polymarket.md) を参照する。

## `[providers.yfinance]`

| 項目      | 既定値 | 内容                                                                 |
| --------- | ------ | -------------------------------------------------------------------- |
| `enabled` | `true` | 同梱 `conf/default.toml` の実効値。yfinance providerの収集を許可する |

## `[providers.investingpy]`

| 項目      | 既定値 | 内容                                                                           |
| --------- | ------ | ------------------------------------------------------------------------------ |
| `enabled` | `true` | 同梱 `conf/default.toml` の実効値。investingpy識別子のproviderの収集を許可する |

Go内部の基底値は両providerとも `false` だが、同梱の `conf/default.toml` は現在両方を `true` で上書きする。各providerは独立して設定でき、`enabled=true` のproviderだけが `datalist` に掲載される。`false` のproviderを `collect` に指定しても、存在しないproviderと同じ `NOT_FOUND` になる。

## `[python]`

| 項目                       | 既定値                | 内容                                                  |
| -------------------------- | --------------------- | ----------------------------------------------------- |
| `executable`               | `python`              | Python実行ファイル名またはパス。空文字とNUL文字は不可 |
| `script`                   | `python/collector.py` | 標準入出力adapter。空文字とNUL文字は不可              |
| `timeout`                  | `60s`                 | 1秒～10分。子プロセス1件の期限                        |
| `max_response_bytes`       | `16777216`            | 1～64 MiB。stdoutの最大保持サイズ                     |
| `max_concurrent_processes` | `2`                   | 1～8。2つのPython providerで共有する子プロセス専用枠  |

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
意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御する。あわせてOS側のCPU・メモリ・プロセス制限を適用する。
