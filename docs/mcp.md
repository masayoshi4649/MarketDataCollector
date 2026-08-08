# HTTP MCP仕様

## Transport

- エンドポイントは正確な `POST /mcp` とする。
- 公式Go SDKのStreamable HTTPを使用する。
- ステートレスかつJSON応答で動作する。
- セッションID、SSE、resources、prompts、loggingは公開しない。
- MCPクライアントとのprotocol version交渉はSDKへ委ねる。
- `Content-Type: application/json` を必須とする。
- CORS preflightの `OPTIONS` を除き、POST以外、圧縮要求、本文上限超過を拒否する。

クライアント設定:

```text
http://127.0.0.1:8080/mcp
```

これは同一端末から接続する例である。サーバーは指定Portの全ネットワークインターフェースで待ち受ける。

## RESTとの対応

標準MCPはtoolごとのURIを定義しない。そのため `/mcp/datalist` や `/mcp/collect` という独自HTTP APIは公開しない。

| MCP tool   | REST                | 共通処理           |
| ---------- | ------------------- | ------------------ |
| `datalist` | `GET /api/datalist` | `service.DataList` |
| `collect`  | `POST /api/collect` | `service.Collect`  |

tool名、共通入力、共通出力をRESTの末尾操作名と一致させる。

## `datalist`

入力項目はない。外部通信を行わず、`enabled=true` のproviderだけを含むRESTと同じ `domain.DataList` をstructured contentとJSON text contentとして返す。providerの掲載自体が利用可能であることを表す。

用途:

1. 掲載されたproviderを選ぶ。
2. provider内のdataset識別子を選ぶ。
3. datasetのparameters、型、必須、許可値、既定値を確認する。
4. 同じ内容を `collect` の引数へ渡す。

`enabled=false` のproviderは掲載されず、`collect` に直接指定しても未定義providerと同じ `NOT_FOUND` になる。

output schemaには `version` からparameterの `allowed`、`default` まで、`domain.DataList` の固定階層を公開する。Schemaは共通DTOの `json`・`jsonschema` タグから生成し、項目追加時にGo型と二重管理しない。これによりクライアントとモデルはtoolを呼び出す前に結果の形を判断できる。

tool annotationは読み取り専用、非破壊、閉じた世界として公開する。

## `collect`

入力:

```json
{
  "provider": "225225jp",
  "dataset": "current",
  "parameters": {
    "section": "japan",
    "codes": ["111", "511"]
  }
}
```

入力と出力は [REST API仕様](rest-api.md) の `/api/collect` と同じである。成功時は `domain.CollectResponse` をstructured contentとJSON text contentとして返す。

output schemaには `version`、`provider`、`dataset`、`collected_at`、`metadata`、`data` の共通外枠を公開する。Schemaは `domain.CollectResponse` のGo型から生成する。provider固有 `data` はdatasetごとに形が異なるため、型を限定しないJSON値として説明し、具体的な入力と結果の意味は `datalist` とprovider仕様で公開する。

成功値は一度だけJSON化し、structured contentとtext contentへ同じ生JSONを設定する。Go SDKにはSchemaを広告する一方、動的JSONをSchema適用で `float64` へ再変換する経路を通さない。これにより2^53を超えるJSON整数もREST/MCPの送信時点では丸めず保持する。

J-QuantsのページングとcursorもRESTと同じである。1回の `collect` は上流の1ページだけを取得し、返却された `pagination_key` または `cursor` はprovider固有 `data` 内に保持する。後続ページまたは差分は、同じ検索条件と返却キーを次の `collect` へ渡して取得する。

PolymarketのページングもRESTと同じである。1回の `collect` は1回の公開GETと1ページだけを取得する。検索は `page`、Gammaは応答の `next_cursor` を次回の `after_cursor` へ、CLOBは応答の `next_cursor` を同名の次回入力へ、Dataは `offset` を進めて継続し、tool側では自動追跡・結合しない。ページング対象のmetadataは `total_pages_known` を常に返し、総ページ数の実値は上流が提供する場合だけ返す。

tool annotationは読み取り専用、非破壊、外部接続ありとして公開する。要求時の市場データは時間により変わるため、idempotent hintはfalseとする。

## エラー

共通サービスの `ServiceError` をMCP tool errorへ変換する。エラー文字列の先頭に安定分類を含む。

```text
INVALID_ARGUMENT: parametersが不正です
```

provider内部の通信詳細、Python stderr、ファイルパスはtool resultへ含めず、サーバーログへ記録する。

provider、dataset、parametersを正しく復号した後のserviceエラー分類はRESTと共通である。一方、JSON-RPC自体の不正、必須tool引数の欠落、型違い、未知の最上位引数は、handler到達前にMCP SDKが標準JSON-RPCの `InvalidParams` 等として拒否する。RESTのHTTP 400 JSONとはtransport表現が異なる。

## HTTP境界

MCPにもRESTと同じ要求期限と本文上限を適用する。Origin制限はなく、Origin付きHTTP応答には `Access-Control-Allow-Origin: *` を設定する。CORS preflightの `OPTIONS` には204を返す。

サーバーへ到達可能な利用者は全員、`datalist` と `collect` を呼び出せる。tool annotationの読取専用・非破壊という表示はアクセス制御ではない。`collect` は外部providerへ通信するため、上流負荷と利用規約・データ利用条件のリスクが残る。

意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御する。OS側のCPU・メモリ・プロセス制限も併用する。
