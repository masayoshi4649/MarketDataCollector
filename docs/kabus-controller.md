# kabus-controller対応状況

`kabus-controller` は、LAN内で稼働するKabusControllerから先物・オプションの登録一覧と板情報を要求時に取得する、認証不要・読取専用のGoネイティブproviderである。外部APIで指定する正規provider名は `kabus-controller`、表示名は `KabusController` とする。

既定オリジンは `http://10.10.100.1:8080` である。起動時と `datalist` では上流へ接続せず、`collect` 時だけ接続する。取得結果は保存せず、登録変更、発注、取消などの更新操作は行わない。

## 実装済みdataset

| dataset | 上流GET | 取得内容 | 必須入力 |
| ------- | ------- | -------- | -------- |
| `future_registrations` | `/api/trade/registrations/future` | 登録中の先物一覧 | なし |
| `option_registrations` | `/api/trade/registrations/option` | 登録中のオプション一覧 | なし |
| `market_data` | `/api/trade/market-data` | 登録中の先物・オプションすべての板情報 | なし |
| `future_market_data` | `/api/trade/market-data/future` | 登録中の先物だけの板情報 | なし |
| `option_market_data` | `/api/trade/market-data/option` | 登録中のオプションだけの板情報 | なし |
| `symbol_market_data` | `/api/trade/market-data/:symbol` | 先物・オプションを問わない指定銘柄1件の板情報 | `symbol` |

入力項目の型、必須条件、説明は実行中サーバーの `datalist` を正とする。`symbol_market_data` は `parameters.symbol` に100文字以内の英数字、ピリオド、アンダースコア、ハイフンからなる銘柄コードを1件指定し、その他のdatasetは空の `parameters` を受け付ける。固定パスと衝突する `.`, `..`, `future`, `option` はsymbolとして受け付けない。任意URL、任意HTTP method、任意queryを指定する機能は提供しない。

## 取得契約

- 1回の `collect` は、対応表にある固定GETのうち1件だけを実行する。複数endpointの結果合成、自動再試行、定期収集、永続化は行わない。
- HTTPリダイレクトは追跡せず、固定6 GET以外のURLへ接続しない。3xx応答は `UPSTREAM_ERROR` として扱う。
- `enabled=true` の場合だけ `datalist` へ掲載する。`enabled=false` のproviderを `collect` に指定すると、未定義providerと同じ `NOT_FOUND` になる。
- `Accept: application/json` と `Accept-Encoding: gzip` を送る。成功応答は `application/json` または `+json` のContent-Typeを必須とする。
- 上流JSON全体をキー変換せず `data` へ格納する。`json.Decoder.UseNumber` で数値表現を保持し、大きな整数を途中で `float64` へ変換して丸めない。
- 成功metadataには `source_name`、queryを含まない実取得先の `source_url`、固定パスまたは個別銘柄用の `/api/trade/market-data/:symbol` テンプレートを示す `endpoint`、`upstream_status`、`upstream_fetched`、`response_bytes`、`read_only=true`、`on_demand=true` を含める。
- `timeout` はKabusControllerへのHTTP要求期限、`max_response_bytes` は未圧縮本文、Gzip圧縮本文、展開後本文の読み取り上限である。既定値は15秒、16 MiBとする。

HTTP 400・422は `INVALID_ARGUMENT`、`symbol_market_data` の404は `NOT_FOUND`、401・403・425・429・503は `PROVIDER_UNAVAILABLE`、408・504は `TIMEOUT`、その他の非2xxは `UPSTREAM_ERROR` に分類する。要求contextの期限超過は `TIMEOUT`、キャンセルは `PROVIDER_UNAVAILABLE`、その他の通信、MIME、JSON、本文上限の異常は `UPSTREAM_ERROR` とする。非2xxの上流本文は公開応答やmetadataへ含めない。

## 設定

```toml
[providers.kabus-controller]
enabled = true
base_url = "http://10.10.100.1:8080"
timeout = "15s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

`base_url` はhttp/httpsの絶対オリジンだけを受け付け、userinfo、パス、クエリ、フラグメントは指定できない。KabusControllerのホストやポートが異なる場合は、`conf/default.toml` を直接編集せず、`conf/zz-kabus-controller.local.toml` など `default.toml` より後へ並ぶローカルTOMLで上書きする。

## ネットワークと運用上の注意

既定接続先はプライベートアドレス上の平文HTTPである。通信経路を信頼できるLANまたはVPN内に限定し、KabusControllerの8080番ポートをインターネットへ公開しない。必要に応じてホスト側ファイアウォールで、MarketDataCollectorを実行する端末からだけ到達できるようにする。

MarketDataCollector自身も全ネットワークインターフェースで待ち受け、CORSは `*` である。到達可能な利用者はKabusControllerへの収集を実行して板情報を閲覧できるため、OSファイアウォール、プライベートネットワーク、認証付きリバースプロキシ等で利用者を制限する。

このproviderが読取専用であることは、取得データの保存、共有、再配布、商用利用が無条件に許可されることを意味しない。KabusControllerおよび元データの利用条件を運用者が確認する。

## 仕様更新時の確認手順

KabusControllerのAPI仕様は自動同期しない。ホスト、パス、HTTP method、入力、応答形式、本文サイズ、認証要否が変わった場合は、固定endpoint、Descriptor、入力検証、クライアントテスト、設定例、本書を同時に更新する。更新操作や認証情報が必要なendpointは、読取専用providerへ自動追加しない。
