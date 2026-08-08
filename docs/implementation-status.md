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
- [x] J-Quants API v2の30 datasetを固定許可リストとして実装する
- [x] J-Quantsの契約プランとアドオンに応じ、実際に利用可能なdatasetだけを一覧へ掲載する
- [x] Standardプランで利用可能な17データAPIとBulk API 2件を要求時に取得できる
- [x] J-Quantsの1要求1ページ継続、当日差分cursor、Gzip圧縮前後の本文上限、HTTP状態、JSON整数精度を安全に扱う
- [x] 全J-Quants要求共通の単一FIFOとAPI区分別独立quotaにより、受付順を保って公式上限の50%に抑える
- [x] J-Quantsの同一originリダイレクトではAPIキーを維持し、異なるoriginではAPIキーだけを除去する
- [x] Python dataset・product・関数を固定許可リストにする
- [x] Python固有値を厳密JSONへ正規化し、未知objectとキー衝突を拒否する
- [x] Python providerを各providerセクションの `enabled` で個別設定し、`true` のproviderだけを一覧へ掲載する
- [x] PythonをUTF-8・`-I`・環境変数許可リストで起動し、構造化エラーを終了コード2/3/4で分類する
- [x] Python子プロセス数を2つのPython providerで共有する専用枠により制限する
- [x] Python応答metadataへ取得元名、URL、非公式client表示、規約URLを付ける
- [x] CPython 3.14 / Windowsで解決した `requirements.lock.txt` を提供する
- [x] providerを `[providers.nikkei225jp]`、`[providers.jquants]`、`[providers.yfinance]`、`[providers.investingpy]` で個別設定する
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
11. J-Quantsは公式V2 APIをGoから直接呼び、APIパスとqueryをdatasetごとの固定許可リストにした。
12. `datalist` への掲載が利用可能性を表す既存契約を維持するため、J-Quantsは `plan` と `addons` でdatasetを絞り込む。
13. J-Quantsのページングは自動結合せず、1要求につき1ページを返す。公式応答に全ページ数、総件数、現在ページはなく、`pagination_key` が返らなくなった応答を完了条件とする。
14. 株価ティックはRESTデータAPIがないため、対応アドオン設定時にBulk CSV一覧として公開する。
15. cursorは日本時間当日の差分取得に使う公式の不透明値として、`fins_summary`、`fins_details`、`td_list` の3件だけで解釈・加工せず受け渡す。最終ページのcursorを呼び出し側へ返すが、自動追跡や永続化は行わない。
16. 全J-Quants要求で共有する単一FIFOキューと、基本・財務・株価分足／ティック・TDnetの独立quotaを設け、受付順を保って公式レート上限の50%で要求開始を均等化する。
17. HTTPリダイレクトは通常どおり追跡し、同一originでは `x-api-key` を維持し、異なるoriginでは同ヘッダーだけを除去する。
18. 通信診断はAPIキーの完全一致だけを伏せ、URLやquery等を保持する。`max_response_bytes` はGzip圧縮本文と展開後本文の双方に適用する。

## J-Quants API v2の実装基準

- 仕様確認日: 2026年8月8日
- 確認済み公式リリース: 2026年8月3日
- 実装済みdataset: 30件
- 現在のStandard・アドオンなし設定で掲載するdataset: 19件
- Premium設定で追加掲載するdataset: 6件
- アドオン設定で追加掲載するdataset: 5件
- 現在のStandard・アドオンなし設定で公開するcursor入力: 0件
- cursor対応dataset: 3件（`fins_summary`、`fins_details`、`td_list`）
- J-Quants本文上限: 既定16 MiB、設定範囲1～64 MiB。未圧縮・Gzip圧縮・展開後本文へ適用
- レート制御: 全要求共通の単一FIFOと区分別独立quotaで公式上限の50%。基本枠Free 2.5、Light 30、Standard 60、Premium 250要求/分、財務30、株価分足・ティック30、TDnet 50要求/分

30 datasetごとのAPIパス、現在の掲載状態、利用条件、未対応範囲、将来更新時の確認手順は [J-Quants API v2 対応状況](jquants.md) に記載する。公式仕様は自動同期しないため、確認日より後の変更はこの文書の完了表示に含まれない。

ページングの完了条件は [公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)、当日差分のcursor契約は [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md)、実装上限の基準は [公式レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md) に従う。

## 通常検証

`test.ps1` は次を実行する。

1. Goソースのgofmt差分確認
2. `go test ./...`
3. `go vet ./...`
4. `go mod tidy -diff`
5. Python adapterの外部通信なしunittest
6. ルート実行パッケージのbuild

Goテストには、設定、共通service、REST、公式MCPクライアント結合、共通HTTP境界、Python Go境界、225225.jpパーサーとHTTPクライアント、J-Quantsの固定endpoint・入力・認証・ページング・cursor・FIFO順序・50%レート・リダイレクト別キー転送・Gzip圧縮前後の本文上限・状態分類のテストを含む。

Pythonテストは偽yfinance/investpyモジュールを注入し、許可リスト、入力検証、pandas/NumPy相当値、日時、NaN、循環参照、標準入出力を検証する。

## 任意live確認

225225.jpは `LIVE_225225=1` の場合に実サイトテストを実行できる。通常CIでは実行しない。

yfinanceとinvestpyのlive testは用意しない。利用許諾済み環境で、API経由の最小要求を手動確認する。

J-Quantsは通常テストで外部接続せず、明示的な実API疎通だけをローカルで実行する。通常CIでは実行しない。

## 今後の候補

- Python子プロセスの常駐worker pool化
- provider別の短期singleflightとcircuit breaker
- OpenAPI文書の自動生成
- 許諾済みproviderの追加
- 保存要件が追加された場合のRepository層とscheduler層
- J-Quants公式リリース履歴と実装済みendpointの自動差分検出
- J-Quantsのページング・cursorを使う上限付き継続収集
