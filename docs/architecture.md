# アーキテクチャ

## 目的

MarketDataCollectorは、市場情報を要求時に取得し、REST APIと標準MCP Streamable HTTPから同じ意味で返す。データベースや定期収集を初期要件に含めず、取得元の追加と接続方式の追加を独立して行える構成にする。

## レイヤー

```text
REST /api/datalist ─┐
REST /api/collect  ─┼─> internal/service ─> provider registry ─┬─> 225225.jp HTTP
MCP datalist       ─┤                                          ├─> J-Quants API HTTP
MCP collect        ─┘                                          ├─> Polymarket Gamma/CLOB/Data HTTP
                                                               └─> Python adapter
                                                                    ├─> yfinance
                                                                    └─> investpy
```

- `internal/domain`: 接続方式に依存しない `DataList`、`CollectRequest`、`CollectResponse`、エラー分類
- `internal/service`: 有効なproviderだけを一覧化し、providerとdatasetの存在、共通入力を検証して収集処理を呼ぶ
- `internal/provider`: provider共通契約
- `internal/provider/nikkei225jp`: 225225.jpの同一ホストHTTP、本文上限、厳格パーサー
- `internal/provider/nikkei225`: 225225.jpの13データセットを共通契約へ変換し、ローカル絞り込みを適用
- `internal/provider/jquants`: J-Quants API v2の固定endpoint、プラン・アドオン別catalog、APIキー送信、Gzip展開、本文上限を管理するGoネイティブprovider
- `internal/provider/polymarket`: Polymarketの公開Gamma/CLOB/Data APIの固定GET、入力検証、JSON正規化、本文上限、単一FIFOとquotaを管理するGoネイティブprovider
- `internal/provider/python`: 子プロセスの期限、標準出力上限、厳密JSONを管理
- `python/collector.py`: yfinanceとinvestpyの許可済み関数だけを呼び、Python固有値をJSONへ正規化
- `internal/restapi`: HTTPメソッド、Content-Type、JSON、状態コードだけを扱う薄いadapter
- `internal/mcpserver`: `datalist` と `collect` を公式Go SDKへ登録する薄いadapter
- `internal/httpserver`: RESTとMCPに共通のCORS応答、要求期限、HTTPルーティング

## URIと操作名

MCPはRESTのようにtoolごとのURIを持たず、1つのStreamable HTTP endpointにJSON-RPCを送る。独自仕様を作らず、次の対応を安定契約とする。

| 共通操作名 | REST                | MCP transport | MCP tool   |
| ---------- | ------------------- | ------------- | ---------- |
| `datalist` | `GET /api/datalist` | `POST /mcp`   | `datalist` |
| `collect`  | `POST /api/collect` | `POST /mcp`   | `collect`  |

接続方式を表す `/api` と `/mcp` 以外では、操作名を同じにする。REST/MCP adapter内でprovider処理を再実装しない。

## 設定責務

- `[SYSTEM]` はPortとREST/MCP共通の要求期限・本文上限だけを持つ。待受ホストは持たず、全インターフェースで待ち受ける。
- `[providers.nikkei225jp]` は有効状態に加え、225225.jpへのHTTP接続、User-Agent、本文上限を持つ。
- `[providers.jquants]` は有効状態、APIオリジン、秘密APIキー、契約プラン、アドオン、HTTP期限、User-Agent、未圧縮・Gzip圧縮・展開後本文の上限を持つ。
- `[providers.polymarket]` は有効状態、Gamma/CLOB/Dataの3 APIオリジン、HTTP期限、User-Agent、本文上限を持つ。認証情報は持たない。
- `[providers.yfinance]` と `[providers.investingpy]` は、それぞれの有効状態を独立して持つ。
- トップレベル `[python]` は2つのPython providerが共有する実行ファイル、script、期限、出力上限、プロセス枠を持つ。

各providerは `enabled=true` の場合だけregistryと `datalist` に公開する。`false` のproviderは未登録として扱い、`collect` では未定義providerと同じ `NOT_FOUND` にする。

## 要求処理

1. 共通HTTP境界がCORS応答ヘッダーを付け、要求期限を設定する。
2. RESTまたはMCP adapterが要求を `domain.CollectRequest` へ変換する。
3. serviceがproviderとdatasetをdatalistの固定仕様と照合する。
4. providerがdataset固有入力を未知項目も含めて検証する。
5. providerが外部情報を収集し、標準JSONで表現できる値へ正規化する。Polymarketは `UseNumber` で数値表現を保持し、Python providerは取得元metadataも付ける。
6. serviceがversion、provider、dataset、完了UTC時刻を付ける。
7. RESTとMCPが同じ `domain.CollectResponse` を返す。

## 225225.jpの取得境界

- 上流レスポンスをローカルに保持しない。
- 同一Clientの上流取得は1件ずつ処理するが、`catalog` を除く各収集要求で無条件GETを送る。
- 通常レスポンス本文の既定上限は4 MiB、チャート本文は32 MiBとする。
- 自動再試行しない。
- リダイレクトを追跡しない。
- 固定の同一ホストパスだけを解決する。
- MIME、HTTP状態、本文サイズ、JavaScript代入形式、数値有限性を検証する。

## J-Quantsの取得境界

J-QuantsはPython adapterを経由せず、GoのHTTPクライアントからJ-Quants API v2へ直接接続する。契約プランとアドオンで利用可能な固定datasetだけをregistryへ登録する。Standardプラン、アドオンなしの設定では、17データAPIとBulk API 2件の合計19 datasetを掲載する。詳細は [J-Quants API v2 対応状況](jquants.md) に集約する。

- 1回の `collect` につき上流HTTP要求を1回だけ実行し、1ページの応答全体を返す。
- [公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)は総ページ数、総件数、現在ページを返さないため、本providerも事前の全ページ数や進捗率を提供しない。応答に `pagination_key` がある間は同じ検索条件と最新キーで1ページずつ継続し、キーがなくなった応答を完了条件とする。
- [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md)の不透明値を解釈・加工せず、日本時間の当日差分取得に対応する `fins_summary`、`fins_details`、`td_list` だけで受け渡す。ページング時は最終ページにだけcursorが返るため、途中ページではページングを優先する。次回差分要求では同じ日本時間当日の `date` とcursorを指定するが、自動追跡、永続化、自動継続は行わない。
- Standardプラン・アドオンなしでは、Premium限定の2財務cursorとTDnetアドオンのcursorを入力仕様に掲載しない。すべての対応APIで `cursor` と `pagination_key` の同時指定を拒否する。
- `Accept-Encoding: gzip` を明示し、`max_response_bytes` を未圧縮本文、Gzipヘッダーを含む圧縮本文、展開後本文のそれぞれへ適用する。既定は16 MiB、設定範囲は1～64 MiBである。
- 固定APIパスからの通常のHTTPリダイレクトを追跡する。同一originでは `x-api-key` を維持し、異なるoriginでは同ヘッダーだけを除去して追跡する。
- 成功応答はJSON Content-Typeを必須とし、`json.Number` で数値精度を保ったまま、上流JSON全体をproviderの `data` へ格納する。
- HTTP 210は2xx成功として応答を返す。400は `INVALID_ARGUMENT`、401・403と429は `PROVIDER_UNAVAILABLE`、5xxとその他の非2xxは `UPSTREAM_ERROR` に分類する。
- 全J-Quants要求で共有するプロセス内の単一FIFOキューにより、受付順を厳密に維持する。基本・財務・株価分足／ティック・TDnetの独立quotaを持ち、[公式上限](https://jpx-jquants.com/ja/spec/rate-limits.md)の50%で要求開始を均等化する。基本枠はFree 2.5、Light 30、Standard 60、Premium 250要求/分、追加枠は財務30、株価分足・ティック30、TDnet 50要求/分である。財務APIは基本枠と財務枠の両方、アドオンAPIと対象Bulk要求は対応する独立quotaを使う。待機中も要求contextの期限対象とする。
- 429や上流障害を自動再試行しない。FIFOキューとquotaはプロセス単位のため、同じAPIキーを複数プロセスで使う場合は呼び出し側で合算し、応答分類に応じて再実行を判断する。
- BulkとTDnetのダウンロード系応答は署名付きURLだけを返し、ファイル本体の取得、展開、保存は行わない。
- APIキーは `x-api-key` ヘッダーのみに設定し、応答とmetadataへ含めない。通信エラーはAPIキーの完全一致部分だけを伏せ、URL、query、接続先などの診断情報と `errors.Is` による原因判定を保持する。非2xxの上流本文は応答やmetadataへ保持せず、固定のメッセージへ置き換える。

## Polymarketの取得境界

PolymarketはPython adapterを経由せず、GoのHTTPクライアントから公開Gamma、CLOB、Data APIへ直接接続する。認証情報やwallet署名を扱わず、固定許可した37 datasetのGETだけをregistryへ登録する。事前検証PJの基礎10機能を移植し、公開読取専用APIを27 dataset追加している。詳細なパスと未実装範囲は [Polymarket公開API対応状況](polymarket.md) に集約する。

- `enabled=true` でも起動時と `datalist` では接続せず、`collect` 時だけ上流へ接続する。上流応答を保存しない。
- 1回の `collect` につき上流GETを1回だけ実行する。分岐datasetも入力から1パスだけを選び、複数応答を合成しない。
- 検索は `page`、Gammaのイベント・市場一覧は応答の `next_cursor` を次回の `after_cursor` へ、CLOB市場一覧は応答の `next_cursor` を同名の次回入力へ、Data一覧は `offset` を進めて継続する。自動追跡・結合・永続化を行わない。ページング対象では `total_pages_known` を常に保持し、総ページ数の実値は上流が提供する場合だけ保持する。Dataのoffset型応答は `has_more_known=false` とし、返却件数から完了や次のoffsetを推測しない。
- 全要求で共有するプロセス内の単一FIFOキューへ受付順に入れ、1件ずつ、[公式上限](https://docs.polymarket.com/api-reference/rate-limits)の50%以下で開始する。キュー待機を要求contextの期限対象とし、429を自動再試行しない。
- 成功JSONを `json.Decoder.UseNumber` で復号し、標準JSON値へ再帰的に正規化する。巨大整数を途中で `float64` へ変換しない。
- `Accept-Encoding: gzip` を明示し、`max_response_bytes` を未圧縮本文、Gzip圧縮本文、Gzip展開後本文へ適用する。既定は16 MiB、設定範囲は1～64 MiBである。過大本文、不正JSON、余分なJSON値、非成功HTTP状態をエラー分類へ変換し、上流本文を公開エラーに含めない。
- 2026年8月8日時点のCLOB `/price` は、API Referenceと実測に合わせてbest bidを `BUY`、best askを `SELL` へ変換する。高レベル公式ページは逆に記載するため、仕様更新時は両sideを実測して再確認する。
- 注文、キャンセル、入出金、API credential、認証付きアカウント情報、WebSocketは呼び出さない。

## Python境界

初期版は要求ごとにPython子プロセスを1つ起動する。GoがstdinへJSONを1件渡し、stdoutからJSONを1件だけ受ける。次を境界で強制する。

- 実行ファイルとscriptを別々の引数として渡し、シェルを起動しない。
- Pythonを `-I` で起動し、`PYTHON*` 環境変数とuser siteの影響を避ける。
- 子プロセスへ渡す環境変数を、Windows実行、PATH、一時ディレクトリ、locale・timezone、TLS証明書に必要な固定許可リストへ制限する。
- stdin、stdout、stderrをUTF-8のstrict modeへ固定する。
- provider、dataset、product、ライブラリ関数を固定許可リストで選ぶ。
- providerごとの処理期限を設定する。
- yfinanceとinvestingpyで共有する専用枠により、同時Python子プロセス数を既定2、設定範囲1～8へ制限する。
- stdoutとstderrの保持上限を設ける。
- 失敗時もstdoutへ構造化エラーJSONを1件返し、終了コード2を入力不正、3を実行環境・正規化不備、4を上流失敗としてGoの共通分類へ対応させる。
- 非0終了、期限、過大出力、不正JSON、余分なJSONを分類する。
- Python providerがすべて `enabled=false` なら、Python依存がなくてもGoサーバー単体で起動できる。

外部返却値は、pandas/NumPy、日時、欠測値を明示ルールで変換する。tuple/MultiIndex相当のキーは安定したJSON文字列キーへ変換し、未対応object・キー型、循環参照、正規化後のキー衝突は拒否する。成功metadataには `source_name`、`source_url`、`unofficial_client`、`terms_url` を含める。

`-I` と環境変数許可リストはPython実行環境の偶発的な影響を減らす境界であり、OS sandboxではない。信頼できないPython実行ファイルやscriptを指定しない。公開運用ではOSまたはコンテナ側でもCPU、メモリ、プロセス数、ファイル・ネットワーク権限を制限する。

子プロセス起動時間が問題になる場合は、同じJSON契約を維持した常駐worker poolへ置き換えられる。transportとserviceの変更は不要である。

Python専用枠の待機を含めてPython処理期限を適用するため、待機要求が無制限に子プロセス化されることはない。この専用枠はOS resource制限の代替ではない。

MCPの `datalist` は固定階層、`collect` は共通外枠のoutput schemaを公開する。`collect.data` はprovider固有の動的JSON値として型を限定しない。成功値を一度だけJSON化してstructured contentとtext contentの両方へ同じ生JSONを設定し、SDKのJSON Schema適用時に発生するfloat64再変換を避ける。これにより大整数を丸めず、RESTのJSONと送信値を一致させる。

## エラー境界

共通分類は `INVALID_ARGUMENT`、`NOT_FOUND`、`PROVIDER_UNAVAILABLE`、`UPSTREAM_ERROR`、`TIMEOUT`、`INTERNAL` とする。

内部原因、上流本文、実行パスは公開応答へ含めず、サーバーログだけへ記録する。RESTは分類をHTTP状態へ変換し、MCPはtool errorとして返す。

J-Quantsの場合は上流HTTP 400、401、403、429、5xxを前述の固定分類へ変換する。署名付きURLの取得とファイル本体の取得は別操作であり、本サーバーは後者の失敗を分類しない。Polymarketの場合も入力不正、期限、429を含む利用不能、その他の上流HTTP・JSON失敗を共通分類へ変換し、429を自動再試行しない。

## セキュリティ

- 待受ホストの設定はなく、常に指定Portの全ネットワークインターフェースで待ち受ける。
- Originを制限せず、Origin付きHTTP応答へ `Access-Control-Allow-Origin: *` を設定する。CORS preflightには204を返す。
- `/healthz`、REST、MCPの全経路を、サーバーへ到達可能な利用者が呼び出せる。
- 要求本文は既定1 MiB、要求期限は既定60秒とする。
- MCPはPOSTと `application/json` だけを受け付け、圧縮要求を拒否する。
- REST collectもPOSTと `application/json` だけを受け付け、未知の最上位項目を拒否する。

CORSはブラウザの読取可否を示す仕組みであり、アクセス制御ではない。

収集操作は読取専用だが、外部providerへの通信を発生させるため、上流負荷と利用規約・データ利用条件のリスクは残る。

J-Quants providerのAPIキーはサーバー内だけで使用するが、全インターフェース待受とCORS `*` により、到達可能な利用者はAPIキー自体を知らなくてもJ-Quants収集を実行できる。これは契約のレートリミット・利用枠の消費と、取得データの第三者配信につながる。J-Quantsを有効化する環境では、外部公開せず、契約上許可された利用者だけへネットワーク到達範囲を制限する。

意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御する。OS側でもCPU、メモリ、プロセス数、ファイル・ネットワーク権限を制限する。
