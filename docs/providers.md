# Provider仕様

## 共通方針

- 収集は `collect` 要求時だけ実行する。
- providerとdatasetは固定許可リストから選ぶ。
- provider固有入力の未知項目を拒否する。
- 取得時刻と市場データ内の日時を区別する。
- 欠測値は推測で埋めず `null` または配信元の空値として保持する。
- provider固有の変化へ対応できるよう、共通外枠とdataset固有 `data` を分離する。
- Python providerのmetadataには、ライブラリ情報に加えて `source_name`、`source_url`、`unofficial_client`、`terms_url` を付与する。
- providerの有効状態は `[providers.nikkei225jp]`、`[providers.yfinance]`、`[providers.investingpy]` の各 `enabled` で独立して設定する。`true` のproviderだけを `datalist` に掲載し、`false` のproviderへの収集要求は `NOT_FOUND` とする。

## `225225jp`

225225.jpが画面表示へ使用する内部JavaScript/JSONを、ブラウザ、WebSocket、ページHTMLなしで取得する。ニュースは対象外である。

上流HTTPの `timeout` と `user_agent` は `[providers.nikkei225jp]` に設定する。`user_agent` は225225.jpへ送信し、利用元を識別可能にする文字列である。

上流レスポンスはローカルに一切保持しない。`catalog` を除く各収集要求で225225.jpへ接続する。標準設定では通常レスポンス本文を4 MiB、チャート本文を32 MiBまで受け付ける。

| dataset | 取得内容 | 上流通信 |
| --- | --- | --- |
| `catalog` | 対応市場、コード、チャート範囲 | なし |
| `current` | 現在値、変化、騰落率、高値、安値、配信時刻 | 1 GET |
| `chart` | `intraday` または `history` 点列 | 短期は原則1 GET、履歴は1コード1 GET |
| `japan_components` | 日経225構成銘柄、価格、ウェイト、寄与度 | 1 GET |
| `japan_contributors` | 日経225寄与度上位・下位 | 1 GET |
| `japan_industries` | 東証33業種 | 1 GET |
| `japan_ranking` | 日本株の値上がり・値下がり・出来高 | 1 GET |
| `us_equities` | FANG+、DOW30、NASDAQ100等 | 1 GET |
| `us_industries` | 米国業種指数 | 1 GET |
| `us_ranking` | 米国株の値上がり・値下がり・出来高 | 1 GET |
| `adr` | 日本株ADR、PTS、東証価格と比較率 | 1 GET |
| `fx_rates` | 為替レート表 | 1 GET |
| `crypto_assets` | 円価格、時価総額、期間別騰落率 | 1 GET |

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

## `yfinance`

Pythonの `yfinance==1.5.2` を使う。`[providers.yfinance].enabled` で個別に有効化し、子プロセスの実行条件はトップレベル `[python]` を使う。

| dataset | 内容 | 主な必須入力 |
| --- | --- | --- |
| `quote` | 銘柄基本情報 | `ticker` |
| `history` | 単一銘柄価格履歴 | `ticker` |
| `actions` | 配当・分割 | `ticker` |
| `financials` | 損益、貸借、キャッシュフロー | `ticker` |
| `analysis` | 目標価格、業績予想、推奨等 | `ticker` |
| `holders` | 主要、機関、投信、インサイダー | `ticker` |
| `options` | 満期一覧またはチェーン | `ticker` |
| `news` | 銘柄関連ニュース | `ticker` |
| `search` | 銘柄、ニュース等の横断検索 | `query` |
| `download` | 複数銘柄の価格履歴 | `tickers` |

`history` の `start` は含み、`end` は含まない。分足は直近60日以内という上流制限がある。`auto_adjust` と `repair` は結果の意味を変えるため、要求で明示することを推奨する。

pandas DataFrame/Series、MultiIndex、NumPy scalar、Timestamp、NaN、NaT、InfはPython adapterでJSON object/array、ISO 8601、`null`へ正規化する。tuple/MultiIndex相当のキーは要素ごとの安定形式からJSON文字列キーを作る。未対応のobjectやキー型、循環参照、非有限数のキー、および正規化後に同じ文字列となるキーの衝突は拒否する。

一次資料:

- [yfinanceプロジェクトのドキュメント](https://ranaroussi.github.io/yfinance/)
- [yfinanceプロジェクトのAPI一覧](https://ranaroussi.github.io/yfinance/reference/index.html)
- [Ticker API](https://ranaroussi.github.io/yfinance/reference/api/yfinance.Ticker.html)
- [価格履歴仕様](https://ranaroussi.github.io/yfinance/reference/yfinance.price_history.html)
- [Yahoo利用規約](https://legal.yahoo.com/us/en/yahoo/terms/otos/index.html)

yfinanceはYahoo公式SDKではなく、yfinanceプロジェクトのドキュメントもYahoo Finance APIを個人利用向けとして案内している。OSSライセンスは取得データの再配布権を付与しない。一般公開、組織共有、商用利用では、Yahooとデータ権利者の許諾を別途確認する。

## `investingpy`

外部provider識別子は要件に合わせて `investingpy` とする。ただしPyPIの同名パッケージは使用せず、非公式OSS `investpy==1.0.8` をimportする。investpyはInvesting.comが提供する公式クライアントではない。`[providers.investingpy].enabled` で個別に有効化し、子プロセスの実行条件はトップレベル `[python]` を使う。

| dataset | 内容 | 主な必須入力 |
| --- | --- | --- |
| `search` | 商品種別別の銘柄検索 | `product`, `query` |
| `recent` | 直近価格 | `product`, `name`、商品により`country` |
| `historical` | 期間価格 | `product`, `name`, `from_date`, `to_date`、商品により`country` |
| `information` | 商品基本情報 | `product`, `name`、商品により`country` |
| `overview` | 市場概要 | `product` と商品別の `country` / `currency` / `group` |
| `economic_calendar` | 経済指標カレンダー | なし |
| `technical_indicators` | テクニカル指標 | `product`, `name`、商品により`country` |
| `moving_averages` | 移動平均 | 同上 |
| `pivot_points` | ピボットポイント | 同上 |

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

investpyプロジェクトは、Investing.com側のAPIと保護方式の変更により現在正常動作しない旨を警告している。さらにInvesting.com自身も公開APIを提供していないと案内している。Webページの自動抽出にはInvesting.comとデータ権利者の規約が適用されるため、書面許諾と動作確認の両方がない環境では有効化しない。
