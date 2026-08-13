# kabus-controller対応状況

`kabus-controller` は、LAN内で稼働するKabusControllerとkabuステーション互換APIから、先物・オプション登録一覧、板、ランキング、規制、派生商品コード、銘柄情報、為替、信用プレミアム料、注文ソフトリミットを要求時に取得するGoネイティブproviderである。外部APIで指定する正規provider名は `kabus-controller`、表示名は `KabusController` とする。

既定オリジンは `http://10.10.100.1:8080` である。起動時と `datalist` では上流へ接続せず、`collect` 時だけ接続する。上流へ `X-API-KEY`、`Authorization`、Cookieを送信せず、発注、取消、登録解除などの更新APIは呼び出さない。

ただし、kabuステーションAPIの `info` 系GETには、要求した銘柄をAPI登録銘柄リストへ自動登録する仕様がある。該当datasetはHTTP GETでも外部状態を変更し得るため、純粋な読み取り専用ではない。登録はREST/PUSH合計最大50銘柄である。既存登録の所有者を判別できないため、本providerは自動登録解除を行わない。

## 実装済みdataset

| dataset | 上流GET・処理 | 取得内容 | 主な入力 |
| ------- | ------------ | -------- | -------- |
| `future_registrations` | `/api/trade/registrations/future` | 登録中の先物一覧 | なし |
| `option_registrations` | `/api/trade/registrations/option` | 登録中のオプション一覧 | なし |
| `market_data` | `/api/trade/market-data` | 登録中の先物・オプション全板 | なし |
| `future_market_data` | `/api/trade/market-data/future` | 登録中の先物板 | なし |
| `option_market_data` | `/api/trade/market-data/option` | 登録中のオプション板 | なし |
| `symbol_market_data` | `/api/trade/market-data/:symbol` | controller登録済みの指定銘柄板 | `symbol` |
| `kabus_ranking` | `/kabusapi/ranking` | 詳細ランキング1～15 | `ranking_type`、`exchange_division` |
| `kabus_regulations` | `/kabusapi/regulations/:symbol@:exchange` | 規制・空売り規制 | `symbol`、`exchange` |
| `derivative_symbol_resolver` | `/kabusapi/symbolname/*` | 先物・OP・限週OPコード解決 | `kind`、`product_code`、`deriv_month`等 |
| `nt_pair_symbol_resolver` | 先物コード解決を2回 | 同じ明示限月のTOPIX miniと日経mini/micro | `deriv_month`、`nikkei_product_code` |
| `arbitrary_board_snapshot` | `/kabusapi/board/:symbol@:exchange` | 任意の株式・先物・OP板 | `symbol`、`exchange` |
| `option_chain_snapshot` | 登録OP一覧と登録OP板を結合 | 登録済みOPの中心行使価格前後のCall/Put板 | `option_code`、`deriv_month`、`center_strike`等 |
| `kabus_symbol_info` | `/kabusapi/symbol/:symbol@:exchange` | 銘柄基本・追加情報 | `symbol`、`exchange`、`add_info` |
| `kabus_primary_exchange` | `/kabusapi/primaryexchange/:symbol` | 株式の優先市場 | `symbol` |
| `kabus_fx_snapshot` | `/kabusapi/exchange/:pair` | 11通貨ペアの為替スナップショット | `pair` |
| `kabus_margin_premium` | `/kabusapi/margin/marginpremium/:symbol` | 一般信用・デイトレ信用プレミアム料 | `symbol` |
| `kabus_api_soft_limits` | `/kabusapi/apisoftlimit` | 1注文の金額・枚数上限とkabuステーション版 | なし |
| `kabus_api_capacity` | ソフトリミット・先物登録・OP登録を結合 | controllerが把握する登録数と残枠の上限 | なし |

入力型、必須条件、列挙値、既定値は、実行中サーバーの `datalist` を正とする。公開入力から固定許可したpathとqueryだけを組み立て、任意URL、任意HTTP method、未知query、複合path文字列は受け付けない。

`kabus_symbol_info` と `kabus_primary_exchange` は意図的に別datasetである。J-Quantsの5桁コード候補や銘柄マスターとの存在確認もこのprovider単独では行わない。必要な分析では利用側が両datasetとJ-Quants銘柄マスターを時点付きで結合する。`kabus_margin_premium` は上流生値を返し、ショート可否やコスト警告への解釈はSKILL側で行う。

## 派生商品コードとNTペア

`derivative_symbol_resolver` の `kind` は `future`、`option`、`mini_option_weekly` のいずれかである。種類に応じて必要な `product_code`、`put_or_call`、`strike_price`、`deriv_weekly` を通信前に検証する。`deriv_month` は `0` または有効な `YYYYMM` とし、`0` は上流が選ぶ直近限月を意味する。

resolverの標準情報GETは解決銘柄をAPI登録し得る。NTペアは原子的な操作ではなく、2脚目が失敗して結果全体をエラーにしても、1脚目の登録が上流に残る場合がある。既存登録の所有権を判別できないため、失敗時にも自動解除しない。

`nt_pair_symbol_resolver` では、脚ごとに異なる直近限月を選ぶ事故を防ぐため `deriv_month=0` を拒否し、明示的な `YYYYMM` を必須とする。同じ指定限月で `TOPIXmini` と `NK225mini` または `NK225micro` を順に解決し、返却銘柄名から限月を確認できたか、両脚が指定限月と一致したかを合成結果へ含める。一致を確認できない場合は `usable_for_nt=false`、`execution_blocked=true` とし、返却symbolをNT発注案へ使用しない。

## 板とオプションチェーン

`arbitrary_board_snapshot` は株式市場 `1`、`3`、`5`、`6` と、デリバティブ市場 `2`、`23`、`24` を許可する。`symbol` と `exchange` を別入力にし、provider内で `symbol@exchange` を構築する。このGETは指定銘柄をAPI登録銘柄リストへ追加し得る。応答が不完全でも、所有権不明の既存登録を守るため自動解除しない。

本providerにはAPI登録枠を原子的に予約・解放する上流機能がない。`arbitrary_board_snapshot` を異なる銘柄へ反復すると最大50銘柄の枠を消費し得るため、利用者が明示的に許可した銘柄だけへ限定する。`kabus_api_capacity` は非確定の参考上限であり、事前確認しても登録成功を保証しない。所有権付きの登録・解除をKabusController側へ実装するまでは、無制限な市場横断走査へ使用しない。

kabuステーション仕様では、Board応答の英語キー `Bid*` と `Ask*` が通常の意味と逆である。売り最良気配は `Sell1`、買い最良気配は `Buy1` を正とし、`BidPrice`を買い、`AskPrice`を売りとして解釈してはならない。この注意を該当datasetのmetadataにも付与する。

`option_chain_snapshot` は新しい銘柄を生成・登録せず、既存の `option_registrations` と `option_market_data` を `symbol` で結合する。明示した中心行使価格に最も近い登録ストライクから前後を選び、Call/Put、板の欠損、登録範囲を返す。登録外の権利行使価格は取得できないため完全な市場チェーンではない。kabu板には建玉がないので `open_interest_available=false` とし、建玉が必要な場合はJ-Quants等の日次OPデータを別に取得する。

各Call/Putには `has_current_price`、`has_buy_quote`、`has_sell_quote`、`has_two_sided_quote` とraw boardを返す。買い気配は `Buy1.Price`、売り気配は `Sell1.Price` が正数の場合だけ利用可能と判定し、両方が利用可能な場合だけ `has_two_sided_quote=true` とする。

チェーン全体の `volume_availability` は、`TradingVolume` が正数の契約だけを利用可能とし、正数を `available_contract_count`、ゼロを `zero_value_contract_count`、不正値を `invalid_contract_count`、欠損を `missing_contract_count` へ分ける。`quote_time_availability` は、`CurrentPriceTime` をRFC3339日時として解析できた契約だけを `available_contract_count` に数え、不正値と欠損を同じ2項目で区別する。どちらも `registered_contract_count`、`board_contract_count`、`present_contract_count` と `available_definition` を併記する。

`coverage.complete` は登録・板object・要求範囲の構造的な完全性だけを表し、現在値、両側気配、出来高、時刻が売買判断に利用可能であることを保証しない。登録一覧の監査情報は `registration_snapshot.status`、`registration_snapshot.state`、`registration_snapshot.updated_at` へ返し、欠損した値を取得時刻から推測しない。

## 鮮度とmetadata

共通サービスの外側に `collected_at` をUTCで付与する。provider metadataには取得元、HTTP状態、取得時刻、要求パラメーター、副作用、限月、および次の鮮度項目を含める。

- `source_at`: 上流が日付付き時刻を返した場合だけ設定する。複数板では解析できた最古時刻を保守的な代表値とする。
- `source_at_latest`: 複数板で解析できた最新時刻。`source_at` と分離し、最新の1件だけで全体を新鮮と扱わない。
- `age_seconds`: `source_at` と取得時刻の差を安全に計算できる場合だけ設定する。
- `source_time_parsed_count`: RFC3339の `CurrentPriceTime` を解析できた件数。
- `source_time_missing_or_unparseable_count`: `CurrentPriceTime` が欠損または解析不能だった件数。
- `market_state`: 上流から確定できない場合は `unknown` とする。
- `contract_month`: 明示限月を持つ要求で設定する。
- `is_stale`: 市場状態と失効閾値を確定できない場合は `null` とする。
- `stale_reason`: 日付欠損、閾値未定など、機械判定できない理由を示す。

`option_chain_snapshot` の鮮度項目は、上流が返した全登録オプションではなく、結果の `strikes` に選択されたCall/Putだけから再計算する。別商品、別限月、選択範囲外の新しい価格時刻を対象チェーンの鮮度として扱わない。

ランキングの `CurrentPriceTime` と為替の `Time` は日付を含まない。深夜、休場、朝のデータクリア時間帯に取得日を単純合成すると誤るため、完全な `source_at` や `age_seconds` を推測しない。ランキングは平日7:53頃から9:00過ぎ頃に空配列となり得て、信用ランキングは毎週第3営業日7:55頃更新という上流仕様も考慮する。

`kabus_api_soft_limits` が返すのは現物・信用の金額上限と先物・OPの1注文枚数上限であり、API登録銘柄数や残枠ではない。登録残枠として解釈しない。

`kabus_api_capacity` はソフトリミット、controllerの先物登録一覧、OP登録一覧を3 GETで取得し、重複を除いた既知symbol数と `50 - controller_known_unique_symbol_count` を `remaining_upper_bound` として返す。symbol欠損要素はunique数へ含めないため、この値は確定残枠ではなく上限である。標準API側の株式登録、PUSH、他clientによる登録を網羅できず、custom登録一覧がREST/PUSH共通上限に属することもこのAPIだけでは検証できない。`remaining_is_exact=false`、`shared_limit_membership_verified=false` と計算前提を返し、発注可能性や新規登録の保証値には使わない。

登録一覧の `status` と `data.state` が存在する場合、それぞれ `ok` と `succeeded` であることを合成前に検証する。先物・OP別件数と生合計に加えて、`controller_known_unique_symbol_count`、`controller_registration_missing_symbol_count`、`controller_registration_duplicate_count` を返し、重複や欠損を残枠計算から隠さない。両一覧の監査情報は `registration_snapshots.future` と `registration_snapshots.option` に `status`、`state`、`updated_at` として保持し、HTTP取得時刻と登録一覧の基準時刻を混同しない。

## 取得契約

- HTTPリダイレクトは追跡しない。3xxは `UPSTREAM_ERROR` として扱う。
- `Accept: application/json` と `Accept-Encoding: gzip` を送る。成功応答は `application/json` または `+json` のContent-Typeを必須とする。
- 単一endpoint datasetは上流JSON全体をキー変換せず `data` へ格納する。複合datasetだけ、時点の異なる部分成功を返さず、全取得成功後に合成する。
- `json.Decoder.UseNumber` で数値表現を保持し、大きな整数を途中で `float64` へ変換して丸めない。
- 標準info APIの流量制御はKabusController側で実装されているため、本providerでは追加の待機や直列化を行わない。429は自動再試行しない。
- 成功metadataには `read_only` と `may_register_symbol` をdataset別に設定する。登録副作用のないdatasetは `read_only=true`、銘柄登録を伴い得るdatasetは `read_only=false` とする。
- `timeout` は上流要求の期限、`max_response_bytes` は未圧縮本文、Gzip圧縮本文、展開後本文の上限である。既定値は15秒、16 MiBとする。

HTTP 400・422は `INVALID_ARGUMENT`、銘柄・コード解決系の404は `NOT_FOUND`、401・403・425・429・503は `PROVIDER_UNAVAILABLE`、408・504は `TIMEOUT`、その他の非2xxは `UPSTREAM_ERROR` に分類する。要求contextの期限超過は `TIMEOUT`、キャンセルは `PROVIDER_UNAVAILABLE`、その他の通信、MIME、JSON、本文上限、合成形式の異常は `UPSTREAM_ERROR` とする。非2xxの上流本文は公開応答やmetadataへ含めない。

## 設定

```toml
[providers.kabus-controller]
enabled = true
base_url = "http://10.10.100.1:8080"
timeout = "15s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

`base_url` はhttp/httpsの絶対オリジンだけを受け付け、userinfo、パス、query、fragmentは指定できない。KabusControllerのホストやポートが異なる場合は、`conf/default.toml` を直接編集せず、`conf/zz-kabus-controller.local.toml` など後順位のローカルTOMLで上書きする。

## ネットワークと運用上の注意

既定接続先はプライベートアドレス上の平文HTTPである。通信経路を信頼できるLANまたはVPN内に限定し、KabusControllerの8080番ポートをインターネットへ公開しない。MarketDataCollector自身への到達可能者はKabusControllerの取得とAPI銘柄登録を実行できるため、OSファイアウォールや認証付きリバースプロキシ等で利用者を制限する。

本providerは認証ヘッダーを送らないが、取得データの保存、共有、再配布、商用利用が無条件に許可されることを意味しない。KabusController、kabuステーションおよび元データの利用条件を運用者が確認する。

## 仕様更新時の確認手順

上流仕様は自動同期しない。ホスト、パス、HTTP method、入力、応答形式、レート、登録副作用、本文サイズ、認証要否が変わった場合は、固定endpoint、Descriptor、入力検証、クライアントテスト、MCP annotation、設定例、本書を同時に更新する。発注・取消・登録解除など明示的な更新endpointは追加しない。
