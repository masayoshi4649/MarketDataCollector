# Provider仕様

## 共通方針

- 収集は `collect` 要求時だけ実行する。
- providerとdatasetは固定許可リストから選ぶ。
- provider固有入力の未知項目を拒否する。
- 取得時刻と市場データ内の日時を区別する。
- 欠測値は推測で埋めず `null` または配信元の空値として保持する。
- provider固有の変化へ対応できるよう、共通外枠とdataset固有 `data` を分離する。
- Python providerのmetadataには、ライブラリ情報に加えて `source_name`、`source_url`、`unofficial_client`、`terms_url` を付与する。
- providerの有効状態は `[providers.nikkei225jp]`、`[providers.jquants]`、`[providers.kabus-controller]`、`[providers.polymarket]`、`[providers.yfinance]`、`[providers.investingpy]` の各 `enabled` で独立して設定する。`true` のproviderだけを `datalist` に掲載し、`false` のproviderへの収集要求は `NOT_FOUND` とする。

## `225225jp`

225225.jpが画面表示へ使用する内部JavaScript/JSONを、ブラウザ、WebSocket、ページHTMLなしで取得する。ニュースは対象外である。

上流HTTPの `timeout` と `user_agent` は `[providers.nikkei225jp]` に設定する。`user_agent` は225225.jpへ送信し、利用元を識別可能にする文字列である。

上流レスポンスはローカルに一切保持しない。`catalog` を除く各収集要求で225225.jpへ接続する。標準設定では通常レスポンス本文を4 MiB、チャート本文を32 MiBまで受け付ける。

| dataset              | 取得内容                                   | 上流通信                            |
| -------------------- | ------------------------------------------ | ----------------------------------- |
| `catalog`            | 対応市場、コード、チャート範囲             | なし                                |
| `current`            | 現在値、変化、騰落率、高値、安値、配信時刻 | 1 GET                               |
| `chart`              | `intraday` または `history` 点列           | 短期は原則1 GET、履歴は1コード1 GET |
| `japan_components`   | 日経225構成銘柄、価格、ウェイト、寄与度    | 1 GET                               |
| `japan_contributors` | 日経225寄与度上位・下位                    | 1 GET                               |
| `japan_industries`   | 東証33業種                                 | 1 GET                               |
| `japan_ranking`      | 日本株の値上がり・値下がり・出来高         | 1 GET                               |
| `us_equities`        | FANG+、DOW30、NASDAQ100等                  | 1 GET                               |
| `us_industries`      | 米国業種指数                               | 1 GET                               |
| `us_ranking`         | 米国株の値上がり・値下がり・出来高         | 1 GET                               |
| `adr`                | 日本株ADR、PTS、東証価格と比較率           | 1 GET                               |
| `fx_rates`           | 為替レート表                               | 1 GET                               |
| `crypto_assets`      | 円価格、時価総額、期間別騰落率             | 1 GET                               |

### 市場分類

`top`、`nikkei_futures`、`japan`、`us`、`adr`、`europe`、`asia`、`commodities`、`fx`、`bitcoin`。

詳細な許可コードは `catalog` または全体の `datalist` で確認する。`adr` は追加数値表だけで、現在値とチャートはない。`nikkei_futures` は日足履歴に対応しない。

### チャート

- `range=intraday`: ページ別の短期複合配信または確認済み単一コード配信
- `range=history`: `HS_DATA_DAY/S{code}.json` の配信全日足履歴
- `from_millis` と `to_millis`: 取得後にローカルで時刻範囲を絞る。REST/MCP間で整数精度を一致させるため最大9007199254740991
- `max_points_per_series`: 最大250000。先頭と末尾を保つ均等間引き

名称が長期画面の「6か月」に由来していても、履歴応答は配信される全日足を返す。Unixミリ秒へJSTの9時間を加算しない。

### 注意

これらは公開APIとして互換性保証されたURLではない。形式変更時は厳格パーサーがエラーにし、誤った数値を黙って返さない。休場時は正常応答でも値が古い場合があるため、`fetched_at` と `market_time` を区別する。ローカルに上流レスポンスを保持しないため、同じ要求の繰り返しも225225.jpへの通信と負荷を発生させる。

## `jquants`

J-Quants API v2を公式オリジン `https://api.jquants.com` の固定パスから取得するGoネイティブproviderである。Pythonと外部クライアントライブラリは使用しない。`[providers.jquants]` の `plan` と `addons` により、契約上取得できるdatasetだけを `datalist` に掲載する。

Standardプラン、アドオンなしでは、17データAPIと `bulk_list`、`bulk_get` の合計19 datasetを掲載する。Premium限定データとアドオン限定データを含む詳細な30件の表は [J-Quants API v2 対応状況](jquants.md) に集約し、ここでは重複させない。

### 取得契約

- 1回の `collect` は上流APIへ1回だけ要求し、1ページを返す。全ページの自動取得・結合は行わない。
- 上流応答全体をproviderの `data` に格納するため、`pagination_key`、`cursor`、署名付きURLを失わない。
- [公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)は総ページ数、総件数、現在ページを返さない。後続ページは検索条件を変えず、前回応答の最新 `pagination_key` を指定する。キーが返らなくなった応答で全件取得完了と判断するため、事前の全ページ数や進捗率は提供できない。
- `cursor` は日本時間の当日差分取得に使う公式の不透明値で、対象は `fins_summary`、`fins_details`、`td_list` の3 datasetである。値を解釈・加工せず受け渡し、ページング時は最終ページにだけ返る。次回は同じ日本時間当日の `date` とcursorを指定するが、providerは自動追跡、永続化、自動差分取得を行わない。詳細は [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md) を参照する。
- Standardプラン・アドオンなしではcursor入力を公開しない。`cursor` と `pagination_key` は同時に指定できない。
- `Accept-Encoding: gzip` で取得し、`max_response_bytes` を未圧縮本文、Gzipヘッダーを含む圧縮本文、展開後本文へ適用する。既定は16 MiB、設定範囲は1～64 MiBである。
- HTTP 210は2xxの成功応答として取得結果を返す。400は `INVALID_ARGUMENT`、401・403と429は `PROVIDER_UNAVAILABLE`、5xxとその他の非2xxは `UPSTREAM_ERROR` に分類する。
- providerは全J-Quants要求で共有するプロセス内の単一FIFOキューを使い、受付順を厳密に維持する。基本・財務・株価分足／ティック・TDnetの独立quotaにより、[公式レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)の50%で要求を開始する。基本枠はFree 2.5、Light 30、Standard 60、Premium 250要求/分、追加枠は財務30、株価分足・ティック30、TDnet 50要求/分である。財務APIは基本枠と財務枠の両方、アドオンAPIと対象Bulk要求は対応する独立quotaを使う。
- 429の自動再試行は行わない。FIFOキューとquotaはプロセス内でのみ共有されるため、同じAPIキーを複数プロセスから使う場合は利用枠を呼び出し側で合算する。

BulkとTDnetのダウンロード系datasetは、J-Quantsが発行した署名付きURLをそのまま返す。MarketDataCollector自体はそのURLへ接続せず、CSV、PDF、XBRL、gzipファイルの取得、展開、保存を行わない。BulkのURLは5分、TDnetのURLは15分の有効期限があるため、呼び出し側が必要な場合だけ使用する。

### APIキーとデータ利用

実APIキーはGit管理外の `conf/*.local.toml` にだけ保存する。providerは `x-api-key` ヘッダーだけへキーを設定し、応答とmetadataには含めない。HTTPリダイレクトは通常どおり追跡し、同一originではキーを維持し、異なるoriginでは `x-api-key` だけを除去する。

通信エラーではAPIキーの完全一致部分だけを伏せ、URL、query、接続先などの診断情報と `errors.Is` による原因判定を保持する。APIキー以外のquery値は秘密値として自動マスクしないため、固定入力へ独自の秘密値を入れない。非2xxの上流本文は公開応答やmetadataへ保持しない。

実APIキーを使う `base_url` は公式HTTPSから変更しない。設定上のHTTP許可は隔離したテスト先用であり、HTTPで実キーを送ると暗号化されない。

本サーバーは全ネットワークインターフェースで待ち受け、CORSは `*` である。到達可能な利用者はAPIキーを知らなくてもJ-Quantsの契約利用枠を消費し、取得データを閲覧できる。J-Quantsデータの第三者配信に該当するおそれがあるため、契約上許可された利用者だけが到達できるよう、OSファイアウォール、プライベートネットワーク、認証付きリバースプロキシなどで制限する。

仕様確認日は2026年8月8日であり、[公式リリース履歴](https://jpx-jquants.com/ja/spec/release.md)の2026年8月3日リリースまでを確認している。新しいendpointやプラン変更は自動反映されないため、更新時は [確認基準と将来更新手順](jquants.md) も参照する。

一次資料:

- [J-Quants API v2仕様](https://jpx-jquants.com/ja/spec.md)
- [契約別APIとデータ格納期間](https://jpx-jquants.com/ja/spec/data-spec.md)
- [レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)
- [レスポンスステータス](https://jpx-jquants.com/ja/spec/response-status.md)

## `kabus-controller`

KabusControllerの既定オリジン `http://10.10.100.1:8080` へ直接接続し、登録中の先物・オプション一覧と板情報を取得するGoネイティブproviderである。APIキー、Python、外部SDKは使わず、固定許可した6つの `GET` に限定する。登録変更、発注、取消などの更新操作は実装しない。

| dataset | 取得内容 | 上流通信 |
| ------- | -------- | -------- |
| `future_registrations` | 先物登録一覧 | `GET /api/trade/registrations/future` |
| `option_registrations` | オプション登録一覧 | `GET /api/trade/registrations/option` |
| `market_data` | 登録中の先物・オプションすべての板情報 | `GET /api/trade/market-data` |
| `future_market_data` | 登録中の先物だけの板情報 | `GET /api/trade/market-data/future` |
| `option_market_data` | 登録中のオプションだけの板情報 | `GET /api/trade/market-data/option` |
| `symbol_market_data` | 指定銘柄1件の板情報 | `GET /api/trade/market-data/:symbol` |

`symbol_market_data` だけ `symbol` が必須で、先物とオプションのどちらにも使用できる。入力から任意URLや任意pathを選ぶ機能は提供しない。1回の `collect` は1 GETであり、HTTPリダイレクト、複数応答の結合、自動再試行、保存は行わない。`enabled=true` でも起動時と `datalist` では接続せず、収集時だけ上流へ接続する。

既定接続先はLAN内の平文HTTPである。KabusControllerとMarketDataCollectorの両方についてネットワーク到達範囲を制限し、取得データの利用条件を確認する。設定、metadata、応答境界を含む詳細は [kabus-controller対応状況](kabus-controller.md) を参照する。

## `polymarket`

Polymarket公式の公開Gamma、CLOB、Data APIへ直接接続するGoネイティブproviderである。APIキー、wallet署名、Python、Node.js、外部SDKは使わず、すべて固定許可した公開 `GET` に限定する。注文、キャンセル、入出金、認証付きアカウント情報は実装しない。

事前検証PJで確認した検索、イベント、市場、注文板、価格、価格履歴、公開walletの基礎10機能を移植し、Data 9件、Gamma 10件、CLOB 8件を追加した合計37 datasetを実装する。dataset、固定パス、実装済み・未実装・認証必須・状態変更の区分は [Polymarket公開API対応状況](polymarket.md) に集約する。

### 取得契約

- `enabled=true` でも起動時と `datalist` では上流通信せず、`collect` 時だけ接続する。上流応答は保存しない。
- 1回の `collect` は1回のGETだけを実行する。複数endpointの結果合成、ページの自動追跡・結合、定期収集、永続化は行わない。
- 検索は `page`、Gammaのイベント・市場一覧は応答の `next_cursor` を次回の `after_cursor` へ、CLOB市場一覧は応答の `next_cursor` を同名の次回入力へ、Data一覧は `offset` を進めて継続する。cursorを解釈・加工しない。ページング対象では `total_pages_known` を常に返し、総ページ数等の実値は上流が提供する場合だけ返す。Dataのoffset型応答は `has_more_known=false` とし、返却件数から完了や次のoffsetを推測せず、呼び出し側が公式offset上限と期間分割を管理する。
- 全Polymarket要求をプロセス内で共有する単一FIFOキューへ入れ、1件ずつ、[公式レートリミット](https://docs.polymarket.com/api-reference/rate-limits)の50%以下で開始する。429を自動再試行しない。
- JSONは `json.Decoder.UseNumber` で復号して再帰的に標準JSONへ正規化し、巨大な整数を途中で `float64` へ変換して丸めない。
- `Accept-Encoding: gzip` を明示し、HTTP本文上限 `max_response_bytes` を未圧縮・Gzip圧縮・展開後本文へ適用する。既定16 MiB、設定範囲1～64 MiBである。上限超過、不正JSON、余分なJSON値、HTTP状態を共通エラーへ分類する。

### `/price` の注意

2026年8月8日時点の現行OpenAPIを表示する [CLOB `/price` API Reference](https://docs.polymarket.com/api-reference/market-data/get-market-price) と同日の実測では `BUY` がbest bid、`SELL` がbest askだった。一方、[高レベルのPrices and Order Books](https://docs.polymarket.com/market-data/prices-order-books) は `BUY` をlowest ask、`SELL` をhighest bidと逆に説明している。本実装は前者と実測に従い、`best_bid` を `BUY`、`best_ask` を `SELL` へ変換する。仕様更新時は両sideを再確認する。

### 公開wallet情報と利用条件

## `yfinance`

Pythonの `yfinance==1.5.2` を使う。`[providers.yfinance].enabled` で個別に有効化し、子プロセスの実行条件はトップレベル `[python]` を使う。

| dataset      | 内容                           | 主な必須入力 |
| ------------ | ------------------------------ | ------------ |
| `quote`      | 銘柄基本情報                   | `ticker`     |
| `history`    | 単一銘柄価格履歴               | `ticker`     |
| `actions`    | 配当・分割                     | `ticker`     |
| `financials` | 損益、貸借、キャッシュフロー   | `ticker`     |
| `analysis`   | 目標価格、業績予想、推奨等     | `ticker`     |
| `holders`    | 主要、機関、投信、インサイダー | `ticker`     |
| `options`    | 満期一覧またはチェーン         | `ticker`     |
| `news`       | 銘柄関連ニュース               | `ticker`     |
| `search`     | 銘柄、ニュース等の横断検索     | `query`      |
| `download`   | 複数銘柄の価格履歴             | `tickers`    |

`history` の `start` は含み、`end` は含まない。分足は直近60日以内という上流制限がある。`auto_adjust` と `repair` は結果の意味を変えるため、要求で明示することを推奨する。

pandas DataFrame/Series、MultiIndex、NumPy scalar、Timestamp、NaN、NaT、InfはPython adapterでJSON object/array、ISO 8601、`null`へ正規化する。tuple/MultiIndex相当のキーは要素ごとの安定形式からJSON文字列キーを作る。未対応のobjectやキー型、循環参照、非有限数のキー、および正規化後に同じ文字列となるキーの衝突は拒否する。

一次資料:

- [yfinanceプロジェクトのドキュメント](https://ranaroussi.github.io/yfinance/)
- [yfinanceプロジェクトのAPI一覧](https://ranaroussi.github.io/yfinance/reference/index.html)
- [Ticker API](https://ranaroussi.github.io/yfinance/reference/api/yfinance.Ticker.html)
- [価格履歴仕様](https://ranaroussi.github.io/yfinance/reference/yfinance.price_history.html)
- [Yahoo利用規約](https://legal.yahoo.com/us/en/yahoo/terms/otos/index.html)


## `investingpy`

外部provider識別子は要件に合わせて `investingpy` とする。ただしPyPIの同名パッケージは使用せず、非公式OSS `investpy==1.0.8` をimportする。investpyはInvesting.comが提供する公式クライアントではない。`[providers.investingpy].enabled` で個別に有効化し、子プロセスの実行条件はトップレベル `[python]` を使う。

| dataset                | 内容                 | 主な必須入力                                                   |
| ---------------------- | -------------------- | -------------------------------------------------------------- |
| `search`               | 商品種別別の銘柄検索 | `product`, `query`                                             |
| `recent`               | 直近価格             | `product`, `name`、商品により`country`                         |
| `historical`           | 期間価格             | `product`, `name`, `from_date`, `to_date`、商品により`country` |
| `information`          | 商品基本情報         | `product`, `name`、商品により`country`                         |
| `overview`             | 市場概要             | `product` と商品別の `country` / `currency` / `group`          |
| `economic_calendar`    | 経済指標カレンダー   | なし                                                           |
| `technical_indicators` | テクニカル指標       | `product`, `name`、商品により`country`                         |
| `moving_averages`      | 移動平均             | 同上                                                           |
| `pivot_points`         | ピボットポイント     | 同上                                                           |

商品種別は `stock`、`etf`、`fund`、`index`、`currency_cross`、`commodity`、`bond`、`certificate`、`crypto`。テクニカル分析は `crypto` に対応しない。

`recent`、`historical`、`information` の `country` 条件:

- stock、etf、fund、index、certificate: 必須
- commodity: 任意
- currency_cross、bond、crypto: 指定禁止

`search` の `country` は商品種別によらず任意の絞り込みであり、文字列または文字列配列を指定できる。テクニカル分析系ではcurrency_crossとcommodityだけが任意で、それ以外の対応商品では必須となる。

`overview` の商品別入力:

- stock、etf、fund、index、certificate: `country` 必須、`n_results` 任意
- currency_cross: `currency` 必須、`n_results` 任意
- commodity: `group` 必須、`n_results` 任意
- bond: `country` 必須
- crypto: `n_results` 任意

一次資料:

- [investpyプロジェクトリポジトリ](https://github.com/alvarobartt/investpy)
- [investpyプロジェクトのAPI資料](https://investpy.readthedocs.io/api.html)
- [Investing.comのAPI非提供案内](https://pro.investing-support.com/hc/en-us/articles/4408847632017-Do-You-Offer-API-Access-at-Investing-com)
- [Investing.com利用規約](https://cdn.investing.com/about-us/terms_and_conditions.pdf)
