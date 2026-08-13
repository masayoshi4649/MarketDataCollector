# REST API仕様

## 共通仕様

- ベースURIは `/api` とする。
- 要求と応答は正しいUTF-8のJSONとし、不正なUTF-8を置換せず拒否する。
- 全応答へ `Cache-Control: no-store` と `X-Content-Type-Options: nosniff` を付ける。
- Origin付き応答へ `Access-Control-Allow-Origin: *`、許可header、許可methodを付け、CORS preflightの `OPTIONS` には204を返す。
- `collect` は `Content-Type: application/json` を必須とする。
- 圧縮された要求本文は受け付けない。
- JSONのキーは大文字小文字を区別し、最上位の未知項目と重複項目を受け付けない。
- provider固有 `parameters` でもdatasetごとのキー名を完全一致で照合し、未知項目を受け付けない。

Origin制限はない。CORSの `*` はブラウザによる任意Originからの読取を許可するが、アクセス制御機能ではない。サーバーへ到達可能な利用者は全員、`collect` を実行できる。

## `GET /api/datalist`

外部通信を行わず、`enabled=true` のprovider、dataset、入力項目、許可値、既定値を返す。

応答例:

```json
{
  "version": "v1",
  "providers": [
    {
      "name": "225225jp",
      "display_name": "225225.jp",
      "description": "...",
      "datasets": [
        {
          "name": "current",
          "description": "...",
          "parameters": [
            {
              "name": "section",
              "type": "string",
              "required": false,
              "description": "市場分類。",
              "allowed": ["top", "nikkei_futures", "japan"],
              "default": "top"
            }
          ]
        }
      ]
    }
  ]
}
```

`providers` 配列への掲載自体が利用可能であることを表す。`enabled=false` のproviderは一覧へ含めない。掲載されていないproviderを `collect` に指定すると、未定義providerと同じ `NOT_FOUND` になる。

## `POST /api/collect`

指定providerから要求時に情報を収集する。

要求:

| 項目 | 型 | 必須 | 内容 |
| --- | --- | --- | --- |
| `provider` | string | はい | datalistに掲載されたprovider識別子 |
| `dataset` | string | はい | provider内のdataset識別子 |
| `parameters` | object | いいえ | dataset固有入力。省略時は空object |

`parameters` は省略またはJSON objectだけを許可し、明示的な `null` はMCP tool schemaと同様に拒否する。

```json
{
  "provider": "225225jp",
  "dataset": "fx_rates",
  "parameters": {
    "codes": ["511", "514"],
    "limit": 10
  }
}
```

成功応答:

```json
{
  "version": "v1",
  "provider": "225225jp",
  "dataset": "fx_rates",
  "collected_at": "2026-08-02T15:00:00Z",
  "metadata": {
    "source": "https://225225.jp/",
    "read_only": true,
    "on_demand": true
  },
  "data": {}
}
```

`data` はdataset固有の値である。provider共通の識別子、完了時刻、実行ライブラリ等は外側へ保持する。225225.jpの取得URLと取得時刻はdataset固有データ内の `metadata` または `sources` に含む。J-Quantsは上流レスポンス全体を `data` に保持するため、上流の `data` 配列、`pagination_key`、`cursor`、署名付きURLはこの内側に現れる。kabus-controllerは単一endpointでは上流JSONを保持し、NTペア、登録済みOPチェーン、controller既知登録容量では複数応答を正規化して `data` に保持する。取得先、状態、本文サイズ、要求時取得、鮮度、銘柄登録副作用は外側のmetadataに含める。Polymarketも上流の `next_cursor` 等をdataset固有 `data` 内に保持し、検索の `page`、Gamma/CLOBのcursor、Dataのoffsetを自動追跡・結合しない。ページング対象では外側の `metadata.pagination.total_pages_known` を常に返し、総ページ数の実値は上流が提供する場合だけ現れる。

## エラー

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "providerを指定してください"
  }
}
```

| HTTP | code | 内容 |
| ---: | --- | --- |
| 400 | `INVALID_ARGUMENT` | JSON形式、必須値、範囲、未知入力が不正 |
| 404 | `NOT_FOUND` | URI、dataset、または有効なproviderが存在しない |
| 405 | `METHOD_NOT_ALLOWED` | 既知REST URIへ非対応メソッドを送信 |
| 413 | `REQUEST_TOO_LARGE` | 要求本文が上限を超過 |
| 415 | `INVALID_CONTENT_TYPE` | Content-TypeがJSONではない |
| 415 | `UNSUPPORTED_CONTENT_ENCODING` | 圧縮要求を送信 |
| 502 | `UPSTREAM_ERROR` | 外部データ取得または形式検証に失敗 |
| 503 | `PROVIDER_UNAVAILABLE` | Python実行環境、J-QuantsのAPIキー・契約・レート制限、KabusControllerの401・403・425・429・503、Polymarketの429等により利用不能 |
| 504 | `TIMEOUT` | 収集期限を超過 |
| 500 | `INTERNAL` | 未分類の内部失敗 |

内部原因はサーバーログだけへ記録し、公開JSONには含めない。

## `GET /healthz`

外部providerへ接続せず、GoプロセスがHTTP要求へ応答できることだけを返す。

```json
{"status":"ok"}
```
