# 実装状況

## 受入条件

- [x] Go製HTTPサーバーとして `go run .` で起動できる
- [x] `conf` 直下の複数TOMLをファイル名順に読み込める
- [x] 未知設定と不正値を起動時に拒否する
- [x] REST `datalist` とMCP `datalist` が同じserviceを使う
- [x] REST `collect` とMCP `collect` が同じserviceを使う
- [x] 標準MCP Streamable HTTPを `/mcp` で提供する
- [x] 225225.jpの13データセットを要求時に取得できる
- [x] 225225.jpを毎回上流取得し、通常4 MiB・チャート32 MiBの既定本文上限と厳格パースを適用する
- [x] yfinanceの10データセットをPython adapter経由で取得できる
- [x] investingpy識別子から非公式OSS `investpy==1.0.8` の9データセットを呼べる
- [x] Python dataset・product・関数を固定許可リストにする
- [x] Python固有値を厳密JSONへ正規化し、未知objectとキー衝突を拒否する
- [x] Python providerを各providerセクションの `enabled` で個別設定し、`true` のproviderだけを一覧へ掲載する
- [x] PythonをUTF-8・`-I`・環境変数許可リストで起動し、構造化エラーを終了コード2/3/4で分類する
- [x] Python子プロセス数を2つのPython providerで共有する専用枠により制限する
- [x] Python応答metadataへ取得元名、URL、非公式client表示、規約URLを付ける
- [x] CPython 3.14 / Windowsで解決した `requirements.lock.txt` を提供する
- [x] providerを `[providers.nikkei225jp]`、`[providers.yfinance]`、`[providers.investingpy]` で個別設定する
- [x] Python共有実行設定をトップレベル `[python]` に分離する
- [x] 指定Portの全インターフェースで待ち受け、REST/MCPへ共通の要求期限と本文上限を適用する
- [x] Origin制限なしでCORS `*` を返す公開境界を文書化する
- [x] MCP出力のSchema再変換を避け、2^53超のJSON整数をRESTと同じ送信値で保持する
- [x] docsへ構成、REST、MCP、設定、provider仕様を残す
- [x] 通常テストを外部通信なしで実行できる

## 設計判断

1. MCP標準にtool別URIがないため、`/mcp/datalist` は作らず、`/mcp` 内のtool名をREST末尾と同じ `datalist` / `collect` にした。
2. transport adapterはproviderを直接呼ばず、共通 `internal/service` だけを呼ぶ。
3. 初期要件は要求時収集のため、データベースとschedulerを導入しない。
4. 225225.jpの既存調査実装をprovider層へ移し、固定取得先、本文上限、厳格パーサーを維持した。
5. yfinance/investpyはGoで非公開上流APIを再実装せず、固定JSON契約のPython子プロセスへ分離した。
6. PyPI `investingpy` は採用せず、外部識別子だけを維持して非公式OSS `investpy==1.0.8` を使う。
7. Python providerのGo内部基底値は無効とし、同梱設定で利用するproviderを明示的に有効化する。
8. 225225.jpの市場時刻、データ内日時、HTTP取得時刻を統合せず保持した。
9. 待受はPortだけで常に全インターフェースとし、到達制御が必要な環境ではアプリケーション外のネットワーク境界へ委ねる。
10. `enabled=false` のproviderは一覧へ出さず、収集時も未定義providerと同じ `NOT_FOUND` にした。

## 通常検証

`test.ps1` は次を実行する。

1. Goソースのgofmt差分確認
2. `go test ./...`
3. `go vet ./...`
4. `go mod tidy -diff`
5. Python adapterの外部通信なしunittest
6. ルート実行パッケージのbuild

Goテストには、設定、共通service、REST、公式MCPクライアント結合、共通HTTP境界、Python Go境界、225225.jpパーサーとHTTPクライアントのテストを含む。

Pythonテストは偽yfinance/investpyモジュールを注入し、許可リスト、入力検証、pandas/NumPy相当値、日時、NaN、循環参照、標準入出力を検証する。

## 任意live確認

225225.jpだけ、`LIVE_225225=1` の場合に実サイトテストを実行できる。通常CIでは実行しない。

yfinanceとinvestpyのlive testは用意しない。利用許諾済み環境で、API経由の最小要求を手動確認する。

## 今後の候補

- Python子プロセスの常駐worker pool化
- provider別の短期singleflightとcircuit breaker
- OpenAPI文書の自動生成
- 許諾済みproviderの追加
- 保存要件が追加された場合のRepository層とscheduler層
