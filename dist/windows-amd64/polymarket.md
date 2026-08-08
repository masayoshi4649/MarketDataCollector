# Polymarket公開API対応状況

## 対応範囲

`polymarket` はPolymarket公式の公開APIから予測市場、価格、注文板、公開ウォレット情報を要求時に取得する、読取専用のGoネイティブproviderである。APIキー、ウォレット署名、Python、Node.js、Polymarket SDKは使用しない。

| API   | 既定オリジン                       | 主な用途                                                                   | 認証                              |
| ----- | ---------------------------------- | -------------------------------------------------------------------------- | --------------------------------- |
| Gamma | `https://gamma-api.polymarket.com` | 検索、イベント、市場、タグ、シリーズ、スポーツ、コメント、公開プロフィール | 不要                              |
| CLOB  | `https://clob.polymarket.com`      | 注文板、価格、履歴、スプレッド、取引条件、市場メタデータ                   | このproviderが使う公開GETでは不要 |
| Data  | `https://data-api.polymarket.com`  | 公開ウォレットのポジション・取引・活動、保有者、出来高、ランキング         | 不要                              |

`enabled=true` でも起動時と `datalist` ではPolymarketへ接続しない。上流通信は `collect` 時だけ行い、データを保存しない。事前検証PJで確認した基礎10機能を移植し、公開読取専用APIを27データセット追加した、合計37データセットを実装している。

仕様確認日は2026年8月8日である。この日より後の公式変更は自動反映されない。

## 実装済みデータセット

すべてHTTP `GET` であり、1回の `collect` は上流要求を1回だけ実行する。表のパスは3つの既定オリジンからの相対パスである。入力の完全な名前、型、必須条件、許可値は、実行中サーバーの `datalist` を正とする。

### 事前検証PJから移植した基礎10件

| dataset          | API   | GETパス                                    | 内容・分岐                                        |
| ---------------- | ----- | ------------------------------------------ | ------------------------------------------------- |
| `search`         | Gamma | `/public-search`                           | 市場・イベント・タグ等の横断検索                  |
| `events`         | Gamma | `/events/keyset`                           | イベント一覧                                      |
| `event`          | Gamma | `/events/{id}`、`/events/slug/{slug}`      | IDまたはslug指定のイベント詳細                    |
| `markets`        | Gamma | `/markets/keyset`                          | 市場一覧                                          |
| `market`         | Gamma | `/markets/{id}`、`/markets/slug/{slug}`    | IDまたはslug指定の市場詳細                        |
| `order_book`     | CLOB  | `/book`                                    | token ID指定の注文板                              |
| `token_price`    | CLOB  | `/price`、`/midpoint`、`/last-trade-price` | `price_type` に応じた最良気配、中間値、直近約定値 |
| `price_history`  | CLOB  | `/prices-history`                          | token ID指定の価格履歴                            |
| `user_positions` | Data  | `/positions`                               | 公開walletの現在ポジション                        |
| `user_activity`  | Data  | `/activity`                                | 公開walletの取引・償還・報酬等の活動              |

### Data APIへ追加した9件

| dataset                | GETパス                | 内容                           |
| ---------------------- | ---------------------- | ------------------------------ |
| `trades`               | `/trades`              | 公開取引履歴                   |
| `closed_positions`     | `/closed-positions`    | 公開walletの決済済みポジション |
| `holders`              | `/holders`             | 市場の公開保有者               |
| `market_positions`     | `/v1/market-positions` | 市場別の公開ポジション         |
| `position_value`       | `/value`               | 公開walletのポジション総額     |
| `traded_markets_count` | `/traded`              | 公開walletの取引済み市場数     |
| `open_interest`        | `/oi`                  | 建玉情報                       |
| `live_volume`          | `/live-volume`         | ライブ出来高                   |
| `leaderboard`          | `/v1/leaderboard`      | 公開ランキング                 |

### Gamma APIへ追加した10件

| dataset               | GETパス                                                                                                                            | 内容・分岐                                           |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| `tags`                | `/tags`                                                                                                                            | タグ一覧                                             |
| `tag`                 | `/tags/{id}`、`/tags/slug/{slug}`                                                                                                  | IDまたはslug指定のタグ詳細                           |
| `related_tags`        | `/tags/{id}/related-tags`、`/tags/slug/{slug}/related-tags`、`/tags/{id}/related-tags/tags`、`/tags/slug/{slug}/related-tags/tags` | IDまたはslug指定の関連tag relationshipまたはtag本体  |
| `series`              | `/series`                                                                                                                          | シリーズ一覧                                         |
| `series_item`         | `/series/{id}`                                                                                                                     | ID指定のシリーズ詳細                                 |
| `sports`              | `/sports`                                                                                                                          | スポーツ一覧                                         |
| `sports_market_types` | `/sports/market-types`                                                                                                             | スポーツ市場種別                                     |
| `teams`               | `/teams`                                                                                                                           | チーム一覧                                           |
| `comments`            | `/comments`、`/comments/{id}`、`/comments/user_address/{user_address}`                                                             | 一覧、comment ID、公開walletのいずれかでコメント取得 |
| `public_profile`      | `/public-profile`                                                                                                                  | 公開プロフィール                                     |

### CLOB APIへ追加した8件

| dataset           | GETパス                                                                    | 内容・分岐                      |
| ----------------- | -------------------------------------------------------------------------- | ------------------------------- |
| `server_time`     | `/time`                                                                    | CLOBサーバー時刻                |
| `spread`          | `/spread`                                                                  | token ID指定のスプレッド        |
| `tick_size`       | `/tick-size`                                                               | token ID指定の最小価格刻み      |
| `fee_rate`        | `/fee-rate`                                                                | token ID指定の手数料率          |
| `negative_risk`   | `/neg-risk`                                                                | token ID指定のnegative risk状態 |
| `clob_markets`    | `/simplified-markets`、`/sampling-markets`、`/sampling-simplified-markets` | `kind` に応じたCLOB市場一覧     |
| `clob_market`     | `/clob-markets/{condition_id}`                                             | condition ID指定のCLOB市場詳細  |
| `market_by_token` | `/markets-by-token/{token_id}`                                             | token ID指定のCLOB市場詳細      |

## ページング契約

providerはページを自動追跡・結合・保存しない。1回の `collect` で1ページだけを取得し、呼び出し側が同じ検索条件と継続値を次の `collect` へ渡す。

| 対象                  | 方式                | 継続方法                                                                          |
| --------------------- | ------------------- | --------------------------------------------------------------------------------- |
| `search`              | ページ番号          | `page` を呼び出し側で進める                                                       |
| `events`、`markets`   | Gamma keyset cursor | 応答の `next_cursor` を次回の `after_cursor` へそのまま渡す                       |
| `clob_markets`        | CLOB cursor         | 応答の `next_cursor` を次回の `next_cursor` 入力へそのまま渡す                    |
| Data APIの一覧dataset | offset              | `offset` を明示し、endpointごとの公式上限と必要な期間分割に従って次回値を管理する |

その他の一覧datasetも、個別仕様が `limit`、`offset` 等を公開する場合は、その値を1要求分だけ受け渡す。cursorは上流の不透明値として解釈・加工しない。ページング対象の結果は `metadata.pagination.mode` と `metadata.pagination.total_pages_known` を常に返す。上流が総ページ数を提供しない場合は `total_pages_known=false` とし、`total_pages` を作らない。総件数、現在ページ、`has_more` も上流応答が実際に提供する場合だけ保持し、返却件数等から推測値を追加しない。Data APIのoffset型応答は `has_more_known=false` とし、総件数や次のoffsetを作らないため、providerは完了を断定しない。呼び出し側が各endpointの公式offset上限と期間・市場等の絞り込み条件に従い、必要なら期間を分割して取得する。

`search` は上流の `pagination.hasMore` と `pagination.totalResults` をそのまま保持するが、上流は総ページ数を返さない。`limit_per_type` と総件数から総ページ数を推測して追加せず、`hasMore` と指定した `page` で継続を判断する。Gamma keysetは `next_cursor` の省略を公式どおり既知の終端として `has_more_known=true`、`has_more=false` とする。CLOB keysetはcursorがあり `LTE=` なら既知の終端、それ以外のcursorなら既知の続行とするが、field自体が欠落した場合は終端を断定せず `has_more_known=false` とする。`comments` のcomment ID詳細routeだけは一覧ではないため、ページングmetadataを付けない。

## HTTP、JSON、レート制御

- Gamma/CLOB/Dataの全要求を、プロセス内で共有する単一FIFOキューへ受付順に入れ、1件ずつ開始する。
- datasetごとのquotaは [公式レートリミット](https://docs.polymarket.com/api-reference/rate-limits) の2026年8月8日時点の値を基準にし、その50%以下で要求開始を均等化する。公式quotaが変わっても実装値は自動更新されない。
- キュー待機と上流通信は要求contextの期限対象である。HTTP 429を含む失敗を自動再試行しない。
- 1回の `collect` は1回のGETだけを行う。価格種別やlookup方式によるパス分岐があっても、複数endpointを合成しない。
- `json.Decoder.UseNumber` で復号し、object、array、string、bool、`null`、`json.Number` から成る標準JSONへ再帰的に正規化する。巨大な整数を途中で `float64` に変換して丸めない。
- `Accept-Encoding: gzip` を明示し、`max_response_bytes` を未圧縮本文、Gzip圧縮本文、Gzip展開後本文へ適用する。既定16 MiB、設定範囲1～64 MiBである。
- HTTPリダイレクトはGo HTTP clientの通常方針または呼び出し側から渡された方針で処理する。Polymarket用clientはcookie jarを共有せず、APIキーやwallet署名を送らない。
- 400・422は `INVALID_ARGUMENT`、404は `NOT_FOUND`、401・403・425・429は `PROVIDER_UNAVAILABLE`、408・504は `TIMEOUT`、その他の非2xx・通信失敗・JSON異常・本文上限超過は `UPSTREAM_ERROR` に分類する。非成功時の上流本文を公開エラーへ転記せず、429を含め自動再試行しない。

## `/price` の `side` に関する公式資料の不整合

2026年8月8日時点で、現行OpenAPIを表示する [CLOB `/price` のAPI Reference](https://docs.polymarket.com/api-reference/market-data/get-market-price) は `BUY` をbest bid、`SELL` をbest askと説明しており、同日の実測結果もこの対応だった。一方、[高レベルのPrices and Order Books](https://docs.polymarket.com/market-data/prices-order-books) は `BUY` をlowest ask、`SELL` をhighest bidと説明しており、両資料は逆である。

本実装の `token_price` は、確認済みの現行OpenAPI/API Referenceと実測を優先し、`price_type=best_bid` を `/price?side=BUY`、`price_type=best_ask` を `/price?side=SELL` へ変換する。これは一般的な注文sideの意味を再定義するものではなく、読取endpointの現行挙動への対応である。公式仕様または実測が変わった場合は、両sideを再確認して実装とこの記載を同時に更新する。

## 未実装範囲

### 実装済みendpoint内の未対応query

37 datasetは各表の取得経路に対応するが、公式endpointが公開するqueryをすべて公開しているわけではない。2026年8月8日時点で、主に次のqueryは未対応である。

- `search`: cache、events tag、任意sort、tag検索、recurrence、除外tag、optimized等
- `events`: ID・slug配列、featured・cyom、流動性・出来高・日時範囲、series・game・creator・親子event、各include条件等
- `markets`: ID・slug・token・condition・question ID、上限側フィルター、日時範囲、sports・RFQ・UMA・include_tag等
- `event`: include_chat、include_template
- `market`: include_tag

これらは任意queryを透過する形では追加せず、型、上流名、配列encoding、排他条件、ページング、rate classを固定仕様とテストへ追加してから公開する。

また、基礎検証PJとの互換性と1要求あたりの負荷を抑えるため、`events`・`markets` の `limit=10` と `ascending=false`、価格履歴の `interval=1w`・`fidelity=60`・整数Unix秒、各一覧の1件以上という下限などをローカル既定値・安全制約として使う。これらは公式API自体の既定値または下限を示すものではない。

### 公開だが未実装

- CLOBの複数注文板・複数価格・複数midpoint・複数spread・複数直近約定・batch価格履歴等、query `GET` またはbody `POST` を使うbatch読取endpoint
- CLOBのfee rate・tick sizeの別path形式、Dataの会計snapshot、builder analytics、公開reward・rebate情報等、表にない公開分析endpoint
- Gammaの従来offset型 `/events`・`/markets`。本実装はkeyset版を採用する
- Market WebSocket、Sports WebSocket、RTDS、Chainlink TWAP等のストリーミング・リアルタイム経路
- Bridge、Relayer、geoblock確認、health check等、表にない公開endpoint
- 2026年8月8日より後に追加されたendpoint、および表にない公開GET

これらを取得できないことは不具合ではない。実装済み範囲は上の37 datasetを固定許可リストとし、任意パスを代理取得する機能は持たない。

### 認証が必要、または状態を変更するため未実装

- CLOBのAPI credential作成・取得・削除、balance/allowance、注文・約定・通知等のアカウント情報
- 注文作成、注文キャンセル、allowance更新、署名、認証付きWebSocket user channel
- Bridgeのアドレス作成、quote、入出金、Relayer送信などの資産移動または更新操作
- その他、APIキー、署名、wallet接続を必要とするendpoint

MarketDataCollectorは認証情報を受け取らず、取引・キャンセル・入出金を行わない。公開GETの追加時も、状態変更や認証付き機能と混在させず別途安全性を確認する。

## 公開wallet情報、規約、地域制限

Data APIのwallet関連datasetとGammaの `public_profile` は、呼び出し側が指定した公開walletアドレスに紐づくポジション、取引、活動、プロフィール等を返し得る。認証不要で取得できることは、個人・組織との関連付け、保存、外部公開、再配布、商用利用が無条件に許可されることを意味しない。

[公式Geographic Restrictions](https://docs.polymarket.com/api-reference/geoblock) では、2026年8月8日時点の日本はfrontendでclose-only、API自体は制限対象外と記載されている。この状態は変更され得る。本providerは公開データの読取だけを行い、取引機能やgeoblock回避機能を提供しない。読取専用であることを、利用地域での取引可否やデータ利用許可の根拠にしない。

## 仕様更新時の確認手順

Polymarket APIは変更されるため、少なくとも次を同時に確認する。

1. [Predictions Changelog](https://docs.polymarket.com/changelog/predictions) でendpoint追加、廃止、pagination、query、limitの変更を確認する。
2. [Predictions API Overview](https://docs.polymarket.com/api-reference/predictions/overview)、各API Reference、公式OpenAPIと実サイト応答を照合する。
3. 37 datasetのDescriptor、固定パス、入力仕様、1 collect=1 GET、本文上限、エラー分類、FIFO順序、50% quotaをテストする。
4. `/price` は `BUY` と `SELL` を両方実測し、API Referenceと高レベル資料の不整合が解消・変更されていないか確認する。
5. Terms of Use、Geographic Restrictions、公式Rate Limitsの確認日と実装内の固定値を更新する。
6. 実装済み、公開だが未実装、認証必須、状態変更の区分、およびこの文書の表を同じ変更で更新する。

一次資料:

- [Predictions API Overview](https://docs.polymarket.com/api-reference/predictions/overview)
- [Market Data Overview](https://docs.polymarket.com/market-data/overview)
- [API Rate Limits](https://docs.polymarket.com/api-reference/rate-limits)
- [Predictions Changelog](https://docs.polymarket.com/changelog/predictions)
- [Terms of Use](https://polymarket.com/tos)
- [Geographic Restrictions](https://docs.polymarket.com/api-reference/geoblock)
