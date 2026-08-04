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

| MCP tool | REST | 共通処理 |
| --- | --- | --- |
| `datalist` | `GET /api/datalist` | `service.DataList` |
| `collect` | `POST /api/collect` | `service.Collect` |

tool名、共通入力、共通出力をRESTの末尾操作名と一致させる。

## `datalist`

入力項目はない。外部通信を行わず、`enabled=true` のproviderだけを含むRESTと同じ `domain.DataList` をstructured contentとJSON text contentとして返す。providerの掲載自体が利用可能であることを表す。

用途:

1. 掲載されたproviderを選ぶ。
2. provider内のdataset識別子を選ぶ。
3. datasetのparameters、型、必須、許可値、既定値を確認する。
4. 同じ内容を `collect` の引数へ渡す。

`enabled=false` のproviderは掲載されず、`collect` に直接指定しても未定義providerと同じ `NOT_FOUND` になる。

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

provider固有 `data` は形が異なるため、toolの固定output schemaは公開しない。Go SDKのoutput schema適用が動的JSONをfloat64へ再変換しない経路を使い、structured contentとtext contentへ同じ生JSONを設定する。これにより2^53を超えるJSON整数もREST/MCPの送信時点では丸めず保持する。

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
