# J-Quants API v2 対応状況

## 仕様の確認基準

- 対象APIは J-Quants API v2 である。
- 仕様確認日は2026年8月8日である。
- [公式リリース履歴](https://jpx-jquants.com/ja/spec/release.md)の2026年8月3日リリースまでを確認している。
- 認証は `x-api-key` ヘッダーを使用する。
- APIの基点は `https://api.jquants.com`、固定APIパスは `/v2` 配下とする。

J-Quants APIは更新頻度が高いため、この文書の確認日より後に公開されたAPI、入力項目、返却項目、プラン条件は自動では反映されない。更新時の確認手順は「将来更新時の確認項目」に記載する。

## 実装方針

- 公式APIパスを固定許可リストへ対応付け、利用者が任意URLを指定する機能は提供しない。
- `plan` と `addons` の設定から、契約上利用できるdatasetだけを `datalist` に掲載する。Freeプランへのアドオン指定は起動時に拒否する。
- 現在のローカル設定は `plan = "standard"`、`addons = []` である。
- 返却JSONは項目名を変換せず、J-Quants APIのレスポンス全体を `data` に格納する。これにより `pagination_key`、`cursor`、署名付きURLも失わず返す。
- JSON数値は `json.Number` として復号し、大きな整数を `float64` へ変換しない。
- 1回の `collect` は上流APIを1回だけ呼び、指定された1ページを返す。ページングとcursorの詳細は後述する。
- `cursor` と `pagination_key` の同時指定を拒否する。Standardプラン・アドオンなしではcursor入力を掲載しない。
- `max_response_bytes` を未圧縮本文、Gzipヘッダーを含む圧縮本文、Gzip展開後本文へ適用する。
- HTTPリダイレクトを通常どおり追跡する。同一originではAPIキーを維持し、異なるoriginでは `x-api-key` だけを除去する。
- 全J-Quants要求で共有する単一FIFOキューとAPI区分別の独立quotaにより、受付順を維持しながら公式レート上限の50%で要求開始を均等化する。
- `429 Too Many Requests` を自動再試行しない。呼び出し側が時間を空けて再実行する。
- APIキーは応答、metadata、文書、Git管理対象ファイルへ含めない。通信エラーはAPIキーの完全一致だけを伏せ、URLやqueryなどの診断情報を保持する。

## エンドポイント対応表

`実装済み` はコードが対応していることを表す。`現在掲載` は現在のStandardプラン・アドオンなし設定で `datalist` に掲載されることを表す。

| dataset | J-Quants API | 実装済み | 現在掲載 | 利用条件・補足 |
| --- | --- | :---: | :---: | --- |
| `equities_master` | `/v2/equities/master` | はい | はい | Standardで過去10年 |
| `equities_bars_daily` | `/v2/equities/bars/daily` | はい | はい | Standardで過去10年。前場・後場別項目はPremium限定 |
| `equities_bars_daily_am` | `/v2/equities/bars/daily/am` | はい | いいえ | Premium限定 |
| `equities_investor_types` | `/v2/equities/investor-types` | はい | はい | Standardで過去10年 |
| `markets_margin_interest` | `/v2/markets/margin-interest` | はい | はい | Standardで過去10年 |
| `markets_short_ratio` | `/v2/markets/short-ratio` | はい | はい | Standardで過去10年 |
| `markets_short_sale_report` | `/v2/markets/short-sale-report` | はい | はい | Standardで過去10年 |
| `markets_margin_alert` | `/v2/markets/margin-alert` | はい | はい | Standardで過去10年 |
| `markets_breakdown` | `/v2/markets/breakdown` | はい | いいえ | Premium限定 |
| `markets_calendar` | `/v2/markets/calendar` | はい | はい | Standardで翌年末から過去10年 |
| `indices_bars_daily` | `/v2/indices/bars/daily` | はい | はい | Standardで過去10年 |
| `indices_bars_daily_topix` | `/v2/indices/bars/daily/topix` | はい | はい | Standardで過去10年 |
| `fins_summary` | `/v2/fins/summary` | はい | はい | Standardで過去10年。cursorはPremium限定 |
| `fins_details` | `/v2/fins/details` | はい | いいえ | Premium限定 |
| `fins_dividend` | `/v2/fins/dividend` | はい | いいえ | Premium限定 |
| `fins_earnings_date` | `/v2/fins/earnings-date` | はい | はい | Standardで過去10年 |
| `equities_earnings_calendar` | `/v2/equities/earnings-calendar` | はい | はい | 全プラン、直近データのみ |
| `derivatives_bars_daily_options_225` | `/v2/derivatives/bars/daily/options/225` | はい | はい | Standardで過去10年 |
| `derivatives_bars_daily_futures` | `/v2/derivatives/bars/daily/futures` | はい | いいえ | Premium限定 |
| `derivatives_bars_daily_options` | `/v2/derivatives/bars/daily/options` | はい | いいえ | Premium限定 |
| `edinet_major_shareholders` | `/v2/edinet/major-shareholders` | はい | はい | Standardで過去10年、APIのみ |
| `edinet_cross_shareholdings` | `/v2/edinet/cross-shareholdings` | はい | はい | Standardで過去10年、APIのみ |
| `edinet_large_volume_shareholders` | `/v2/edinet/large-volume-shareholders` | はい | はい | Standardで過去10年、APIのみ |
| `bulk_list` | `/v2/bulk/list` | はい | はい | 利用可能なCSVファイル一覧を返す |
| `bulk_get` | `/v2/bulk/get` | はい | はい | 有効期間5分の署名付きURLを返す |
| `equities_bars_minute` | `/v2/equities/bars/minute` | はい | いいえ | 株価分足・ティックアドオンが必要 |
| `equities_trades` | `/v2/bulk/list` | はい | いいえ | RESTデータAPIはなく、`endpoint=/equities/trades` のBulk CSV一覧として実装。公開`date`は同日の`from`・`to`へ変換する。株価分足・ティックアドオンが必要 |
| `td_list` | `/v2/td/list` | はい | いいえ | TDnetアドオンが必要 |
| `td_files` | `/v2/td/files` | はい | いいえ | TDnetアドオンが必要。署名付きURLは15分有効 |
| `td_bulk` | `/v2/td/bulk` | はい | いいえ | TDnetアドオンが必要。署名付きURLは15分有効 |

現在のStandard設定では、Standard対象17データAPIとBulk API 2件の合計19 datasetを掲載する。Premiumへ変更するとPremium限定6件が加わり、アドオンを設定すると対応する5件が加わる。

## ページングとcursor

[公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)では、応答に `pagination_key` がある間は未取得データが残り、キーがなくなった応答で全件が返却済みとなる。キーはページごとに変わるため、検索条件を変えず、直前の最新キーを次の `collect` へ指定する。

公式応答には総ページ数、総件数、現在ページがない。このため、本providerは全ページ数、残りページ数、進捗率を提供できず、キーの有無だけで継続・完了を判断する。全ページの自動取得や結合は行わない。

`cursor` は [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md) に基づく、日本時間当日の開示情報を差分取得するための不透明値である。対応datasetは次の3件に限定する。

| dataset | 利用条件 | 使い方 |
| --- | --- | --- |
| `fins_summary` | Premium限定 | 日本時間当日の `date` と前回cursorを指定し、`code` は指定しない |
| `fins_details` | Premium限定 | 日本時間当日の `date` と前回cursorを指定し、`code` は指定しない |
| `td_list` | TDnetアドオン限定 | 日本時間当日の `date` と前回cursorを指定 |

1回の応答がページングされる場合、cursorは最終ページにだけ返る。途中ページでは `pagination_key` で残りを取得し、最終ページのcursorを次回差分取得へ使う。本providerはcursorを不透明値のまま解釈・加工せず入出力へ受け渡すだけで、自動追跡、保存、期限管理、定期実行、自動差分取得は行わない。現在のStandardプラン・アドオンなし設定では、3件ともcursor入力を `datalist` に掲載しない。

## レート制御

[公式レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)に対し、本providerは余裕を持たせた50%をプロセス内の実効上限とする。全J-Quants要求で共有する単一FIFOキューへ受付順に登録し、基本・財務・株価分足／ティック・TDnetの独立quotaで要求開始を制御する。

| 対象 | 公式上限 | 本providerの実効上限 |
| --- | ---: | ---: |
| Free基本枠 | 5要求/分 | 2.5要求/分 |
| Light基本枠 | 60要求/分 | 30要求/分 |
| Standard基本枠 | 120要求/分 | 60要求/分 |
| Premium基本枠 | 500要求/分 | 250要求/分 |
| 財務API | 60要求/分 | 30要求/分 |
| 株価分足・ティックアドオン | 60要求/分 | 30要求/分 |
| TDnetアドオン | 100要求/分 | 50要求/分 |

`fins_summary` と `fins_details` は基本quotaと財務quotaの両方を待つ。分足・ティックのAPIと、それらを対象にするBulk要求は株価アドオンquotaだけを使う。TDnet APIはTDnet quotaだけを使う。単一workerは受付順に通信を開始し、前の通信が完了してから次を開始するため、表の値は最大開始レートであり、上流応答が遅い場合の実効レートはさらに低くなる。待機は要求contextの期限対象で、キャンセルされた要求を単一FIFOキューから除く。キューとquotaはプロセス間で共有されないため、同じAPIキーを複数プロセスで使う場合は利用量を外側で合算する。429や通信失敗は自動再試行しない。

## HTTP応答と秘密値

`max_response_bytes` は設定可能で、既定16 MiB、設定範囲1～64 MiBである。未圧縮本文だけでなく、Gzipヘッダーを含む圧縮本文の読み取り量と展開後本文の両方に適用し、いずれかが上限を超えた場合は取得を失敗させる。

通常のHTTPリダイレクトを追跡する。同一scheme・host・portのoriginでは `x-api-key` を維持し、異なるoriginでは同ヘッダーだけを削除した上で追跡する。APIキー以外のヘッダーやURL処理はGo HTTPクライアントの通常動作に従う。

秘密値として自動的に伏せるのはAPIキーだけである。APIキーは応答やmetadataへ含めず、通信エラーに完全一致する場合も置換する。一方、障害診断に必要なURL、query、接続先と `errors.Is` による原因判定は保持するため、queryへ独自の秘密値を指定しない。非2xxの上流本文は公開応答やmetadataへ保持しない。

## ガイド・注意事項・参照表の対応

公式仕様のGuides、Notices、コード表、計算説明は独立した市場データAPIではないため、datasetとして登録しない。ただし、対象範囲を曖昧にしないため、各ページを次のように実装または文書へ反映している。

### Guides

| 公式仕様 | 本プロジェクトでの扱い |
| --- | --- |
| リリース | 2026年8月3日リリースまでを手動確認済み。実行時の自動差分検出は未対応 |
| J-Quants APIについて | V2の公式APIをGoネイティブproviderとして実装 |
| V1 APIからV2 APIへの変更点 | V2の `x-api-key` 認証、API基点、パス、queryへ対応。V1互換機能は提供しない |
| 契約ごとに利用可能なAPIとデータ格納期間 | `plan` と `addons` により `datalist` を絞り込み、対応表へ現在の利用条件を記載 |
| 提供データの更新タイミング | 値を保存・補正せず、`collect` ごとに上流へ接続。更新時刻のschedulerは持たない |
| クイックスタート | APIキー認証、GET要求、query、JSON応答の基本契約へ反映 |
| MCPサーバー | 公式MCPサーバーは内包せず、本プロジェクトの既存MCP `datalist` / `collect` からproviderを利用 |
| J-Quants CLI | 公式CLIの代理実行や内包は行わない |
| ファイルダウンロード | Bulk APIで一覧と署名付きURLを取得。ファイル本体の取得、展開、保存は行わない |

### Notices

| 公式仕様 | 本プロジェクトでの扱い |
| --- | --- |
| データ修正履歴・制約事項 | 値を推測で修正せず上流レスポンスをそのまま返す。利用時は公式の最新情報を確認 |
| レスポンスのページングについて | `pagination_key` を入力・出力に保持し、1要求1ページ。キー消失を完了条件とし、公式が返さない全ページ数は提供しない |
| レートリミットについて | 全要求共通の単一FIFOと区分別独立quotaで公式上限の50%に抑える。429を分類して自動再試行しない |
| APIレスポンスのGzip化 | 圧縮本文と展開後本文の双方に設定可能な本文上限を適用 |
| レスポンスステータス | 210を成功として扱う。400、401、403、429、500系とその他の非2xxを共通エラー分類へ変換 |
| cursorを使った差分取得 | 対象3 APIで当日の不透明値を入力・出力に保持。最終ページのcursorを利用し、永続化と自動継続取得は未対応 |

### コード表・計算説明

| 公式仕様 | 本プロジェクトでの扱い |
| --- | --- |
| 17業種コード及び業種名 | 上流のコードと名称を変換せず保持 |
| 33業種コード及び業種名 | 上流のコードと名称を変換せず保持 |
| 市場区分コード及び市場区分名 | 上流のコードと名称を変換せず保持 |
| 商品区分コード及び商品区分名 | 上流のコードと名称を変換せず保持 |
| 調整済み株価の計算方法 | 再計算せずAPI返却値を保持 |
| 市場名 | 入力値と返却値を変換せず保持 |
| 公表の理由 | 上流のコードと名称を変換せず保持 |
| 東証信用貸借規制区分 | 上流のコードと名称を変換せず保持 |
| 休日区分 | 入力値と返却値を変換せず保持 |
| 配信対象指数コード | 入力値と返却値を変換せず保持 |
| 開示書類種別 | 上流のコードと名称を変換せず保持 |
| リファレンスナンバー | 上流の値を変換せず保持 |
| 先物商品区分コード | 入力値と返却値を変換せず保持 |
| オプション商品区分コード | 入力値と返却値を変換せず保持 |
| 指定可能なエンドポイント一覧 | `bulk_list` と `bulk_get` の入力を固定許可リストで検証 |

入力候補のコードや名称は仕様変更で増減し得るため、利用時は公式の最新コード表も確認する。

## 現在未対応の範囲

- 公式リリース履歴や仕様ページを実行時に巡回する自動差分検出
- 未知の新規APIを自動登録する機能
- 全ページの自動取得と結合
- cursorの永続化と定期差分収集
- BulkまたはTDnetの署名付きURLからファイル本体を取得、展開、保存する処理
- CSVの調整済み株価計算
- J-Quants MCPサーバーまたはJ-Quants CLIの代理実行
- APIレスポンス項目ごとの型付きDTOへの変換

## 将来更新時の確認項目

1. [リリース履歴](https://jpx-jquants.com/ja/spec/release.md)を前回確認日以降について確認する。
2. [仕様目次](https://jpx-jquants.com/ja/spec.md)と本書の30 datasetを比較する。
3. [契約別APIと期間](https://jpx-jquants.com/ja/spec/data-spec.md)を確認し、プラン・アドオン条件を更新する。
4. endpointのHTTP method、パス、query、必須・排他条件、ページング・cursorを更新する。
5. `internal/provider/jquants` のendpoint仕様と単体テストを更新する。
6. 本書の対応表、確認日、確認済みリリース日を更新する。
7. Standardの実APIで、掲載datasetの最小疎通を明示的に実行する。

## 一次資料

- [J-Quants API仕様](https://jpx-jquants.com/ja/spec.md)
- [リリース履歴](https://jpx-jquants.com/ja/spec/release.md)
- [契約別APIとデータ格納期間](https://jpx-jquants.com/ja/spec/data-spec.md)
- [ページング](https://jpx-jquants.com/ja/spec/pagination.md)
- [レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)
- [レスポンスステータス](https://jpx-jquants.com/ja/spec/response-status.md)
- [cursor差分取得](https://jpx-jquants.com/ja/spec/cursor.md)
- [Bulk対象endpoint](https://jpx-jquants.com/ja/spec/bulk-list/endpoints.md)
