# アーキテクチャ

## 目的

MarketDataCollectorは、市場情報を要求時に取得し、REST APIと標準MCP Streamable HTTPから同じ意味で返す。データベースや定期収集を初期要件に含めず、取得元の追加と接続方式の追加を独立して行える構成にする。

## レイヤー

```text
REST /api/datalist ─┐
REST /api/collect  ─┼─> internal/service ─> provider registry ─┬─> 225225.jp HTTP
MCP datalist       ─┤                                          └─> Python adapter
MCP collect        ─┘                                              ├─> yfinance
                                                                  └─> investpy
```

- `internal/domain`: 接続方式に依存しない `DataList`、`CollectRequest`、`CollectResponse`、エラー分類
- `internal/service`: 有効なproviderだけを一覧化し、providerとdatasetの存在、共通入力を検証して収集処理を呼ぶ
- `internal/provider`: provider共通契約
- `internal/provider/nikkei225jp`: 225225.jpの同一ホストHTTP、本文上限、厳格パーサー
- `internal/provider/nikkei225`: 225225.jpの13データセットを共通契約へ変換し、ローカル絞り込みを適用
- `internal/provider/python`: 子プロセスの期限、標準出力上限、厳密JSONを管理
- `python/collector.py`: yfinanceとinvestpyの許可済み関数だけを呼び、Python固有値をJSONへ正規化
- `internal/restapi`: HTTPメソッド、Content-Type、JSON、状態コードだけを扱う薄いadapter
- `internal/mcpserver`: `datalist` と `collect` を公式Go SDKへ登録する薄いadapter
- `internal/httpserver`: RESTとMCPに共通のCORS応答、要求期限、HTTPルーティング

## URIと操作名

MCPはRESTのようにtoolごとのURIを持たず、1つのStreamable HTTP endpointにJSON-RPCを送る。独自仕様を作らず、次の対応を安定契約とする。

| 共通操作名 | REST | MCP transport | MCP tool |
| --- | --- | --- | --- |
| `datalist` | `GET /api/datalist` | `POST /mcp` | `datalist` |
| `collect` | `POST /api/collect` | `POST /mcp` | `collect` |

接続方式を表す `/api` と `/mcp` 以外では、操作名を同じにする。REST/MCP adapter内でprovider処理を再実装しない。

## 設定責務

- `[SYSTEM]` はPortとREST/MCP共通の要求期限・本文上限だけを持つ。待受ホストは持たず、全インターフェースで待ち受ける。
- `[providers.nikkei225jp]` は有効状態に加え、225225.jpへのHTTP接続、User-Agent、本文上限を持つ。
- `[providers.yfinance]` と `[providers.investingpy]` は、それぞれの有効状態を独立して持つ。
- トップレベル `[python]` は2つのPython providerが共有する実行ファイル、script、期限、出力上限、プロセス枠を持つ。

各providerは `enabled=true` の場合だけregistryと `datalist` に公開する。`false` のproviderは未登録として扱い、`collect` では未定義providerと同じ `NOT_FOUND` にする。

## 要求処理

1. 共通HTTP境界がCORS応答ヘッダーを付け、要求期限を設定する。
2. RESTまたはMCP adapterが要求を `domain.CollectRequest` へ変換する。
3. serviceがproviderとdatasetをdatalistの固定仕様と照合する。
4. providerがdataset固有入力を未知項目も含めて検証する。
5. providerが外部情報を収集し、標準JSONで表現できる値へ正規化する。Python providerは取得元metadataも付ける。
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

MCP成功出力はprovider固有の動的 `data` を含むため固定output schemaを設定せず、SDKへ動的出力として渡す。SDKが生成した生JSONをstructured contentとtext contentの両方へ使うことで、JSON Schema適用時のfloat64再変換による大整数の丸めを避け、RESTのJSONと送信値を一致させる。

## エラー境界

共通分類は `INVALID_ARGUMENT`、`NOT_FOUND`、`PROVIDER_UNAVAILABLE`、`UPSTREAM_ERROR`、`TIMEOUT`、`INTERNAL` とする。

内部原因、上流本文、実行パスは公開応答へ含めず、サーバーログだけへ記録する。RESTは分類をHTTP状態へ変換し、MCPはtool errorとして返す。

## セキュリティ

- 待受ホストの設定はなく、常に指定Portの全ネットワークインターフェースで待ち受ける。
- Originを制限せず、Origin付きHTTP応答へ `Access-Control-Allow-Origin: *` を設定する。CORS preflightには204を返す。
- `/healthz`、REST、MCPの全経路を、サーバーへ到達可能な利用者が呼び出せる。
- 要求本文は既定1 MiB、要求期限は既定60秒とする。
- MCPはPOSTと `application/json` だけを受け付け、圧縮要求を拒否する。
- REST collectもPOSTと `application/json` だけを受け付け、未知の最上位項目を拒否する。

収集操作は読取専用だが、外部providerへの通信を発生させるため、上流負荷と利用規約・データ利用条件のリスクは残る。CORSはブラウザの読取可否を示す仕組みであり、アクセス制御ではない。

意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御する。OS側でもCPU、メモリ、プロセス数、ファイル・ネットワーク権限を制限する。
