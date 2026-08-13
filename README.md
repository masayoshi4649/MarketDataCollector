# MarketDataCollector

市場情報を要求時に収集し、同じ入出力仕様で REST API と HTTP MCP から返す Go サーバーです。

対応 provider は次の6種類です。

- `225225jp`: 225225.jp の現在値、チャート、日経225構成銘柄、ランキングなど13データセット
- `jquants`: J-Quants API v2を直接利用するGoネイティブprovider。契約プランとアドオンに応じて利用可能なdatasetを公開
- `kabus-controller`: LAN内のKabusControllerとkabuステーション互換APIから、板、ランキング、規制、銘柄情報等を取得する認証不要のGoネイティブprovider。18データセット
- `polymarket`: Polymarketの公開Gamma/CLOB/Data APIを認証なしで直接利用する、読取専用のGoネイティブprovider。37データセット
- `yfinance`: 価格、企業行動、財務、分析、保有者、オプション、ニュース、検索など10データセット
- `investingpy`: 外部識別子は要件に合わせてこの名前を使い、Pythonでは非公式OSS `investpy==1.0.8` の情報取得機能を利用

データは保存せず、`collect` 要求を受けた時点で取得します。225225.jp、J-Quants API、KabusController、Polymarket APIの上流レスポンスもローカルに保持せず、取得を伴う要求ごとに上流へ接続します。

## RESTとMCPの対応

標準 MCP の Streamable HTTP は1つの transport URI 内で tool 名により操作を識別します。そのため独自の `/mcp/collect` は作らず、次のように操作名と共通サービスを対応させています。

| 機能             | REST                | MCP                              |
| ---------------- | ------------------- | -------------------------------- |
| データセット一覧 | `GET /api/datalist` | `POST /mcp` 内の `datalist` tool |
| 要求時収集       | `POST /api/collect` | `POST /mcp` 内の `collect` tool  |
| 死活監視         | `GET /healthz`      | 対応なし                         |

`datalist` と `collect` は、RESTとMCPの両方から同じ `internal/service` を呼びます。provider名、dataset名、parameters、返却値、service以降のエラー分類は接続方式で変わりません。HTTP形式不正とJSON-RPC Schema違反のようなtransport境界エラーだけは、それぞれの標準形式で返します。

`datalist` に掲載するのは設定で `enabled=true` のproviderだけです。`enabled=false` のproviderを `collect` に指定した場合は、存在しないproviderと同じ `NOT_FOUND` を返します。

## 必要環境

- Go 1.24.2 以上
- Python providerを使う場合だけPython 3.12以上と `python/requirements.lock.txt` の依存ライブラリ
- J-Quants providerを使う場合は、J-QuantsのサブスクリプションとAPIキー。Pythonは不要
- kabus-controller providerはAPIキーとPythonが不要。既定では `http://10.10.100.1:8080` へのネットワーク到達性が必要
- Polymarket providerは公開APIだけを使うためAPIキーとPythonは不要

Python依存の固定版はCPython 3.14 / Windowsで検証しています。現在のlockはPython 3.12未満には導入できません。別のPython・OSで利用する場合は、その環境でもインストールと単体テストを確認してください。

## 起動

ビルド済み配布物の配置、Pythonの要否、仮想環境、依存パッケージ、Windows/Linux別の起動方法は [構築・配置手順書](docs/setup-guide.md) を参照してください。

リポジトリルートで次を実行します。

現在の `conf/default.toml` はPython providerを有効にしているため、先にPython環境を構築するか、使用しないPython providerを `enabled=false` にしてください。

J-Quants providerは既定で無効です。利用する場合は、実際のAPIキーをGit管理外の `conf/*.local.toml` だけに保存してから有効化します。

kabus-controller providerも既定で有効です。固定許可したGETだけを呼び、発注、取消、登録解除などの更新APIは呼びません。ただしkabuステーションの銘柄指定情報GETは、指定銘柄をAPI登録銘柄リストへ自動登録し得ます。起動時と `datalist` では接続しないため、疎通は実際の `collect` で確認します。

Polymarket providerは既定で有効です。Gamma/CLOB/Dataの公開GETだけを呼び、注文、キャンセル、入出金などの更新操作は行いません。

```powershell
go run .
```

既定の接続先です。

- RESTデータ一覧: `http://127.0.0.1:8080/api/datalist`
- REST収集: `http://127.0.0.1:8080/api/collect`
- MCP: `http://127.0.0.1:8080/mcp`
- 死活監視: `http://127.0.0.1:8080/healthz`

`127.0.0.1` は同一端末から接続する例です。サーバーはHost設定を持たず、常に指定Portの全ネットワークインターフェースで待ち受けます。既定の待受は `:8080` です。

別の設定ディレクトリは `-conf` または環境変数で指定できます。

```powershell
go run . -conf C:\path\to\conf

$env:MARKET_DATA_COLLECTOR_CONF_DIR = 'C:\path\to\conf'
go run .
```

## REST利用例

データセットと入力仕様を確認します。

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/datalist
```

225225.jp の日本市場現在値を収集します。

```powershell
$body = @{
  provider = '225225jp'
  dataset = 'current'
  parameters = @{
    section = 'japan'
    codes = @('111', '112', '511')
  }
} | ConvertTo-Json -Depth 5

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/collect `
  -ContentType 'application/json' `
  -Body $body
```

チャートは取得後に返却点を絞れます。

```json
{
  "provider": "225225jp",
  "dataset": "chart",
  "parameters": {
    "section": "commodities",
    "range": "intraday",
    "codes": ["921_m1"],
    "max_points_per_series": 300
  }
}
```

J-Quantsの株価四本値を1ページ取得する例です。事前に `jquants` providerを有効化してください。

```powershell
$body = @{
  provider = 'jquants'
  dataset = 'equities_bars_daily'
  parameters = @{
    code = '86970'
    date = '20230324'
  }
} | ConvertTo-Json -Depth 5

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/collect `
  -ContentType 'application/json' `
  -Body $body
```

J-Quants providerは1回の `collect` で上流APIを1回だけ呼び出し、1ページを返します。[公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)は総ページ数、総件数、現在ページを返さないため、本providerもそれらを提供できません。同じ検索条件へ応答の最新 `pagination_key` を追加して1ページずつ継続し、キーが返らなくなった応答で全件取得完了と判断します。

`cursor` は日本時間の当日差分取得に使う公式の不透明値です。対象は `fins_summary`、`fins_details`、`td_list` の3 datasetで、ページング時は最終ページにだけ返ります。値を解釈・加工せず受け渡し、次回は同じ日本時間当日の `date` と併せて指定します。Standardプラン・アドオンなしではcursor入力を公開せず、自動追跡、永続化、自動差分収集も行いません。詳細は [J-Quants API v2 対応状況](docs/jquants.md) と [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md) を参照してください。

kabus-controllerで登録中の先物・オプションすべての板情報を収集する例です。

```powershell
$body = @{
  provider = 'kabus-controller'
  dataset = 'market_data'
  parameters = @{}
} | ConvertTo-Json -Depth 5

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/collect `
  -ContentType 'application/json' `
  -Body $body
```

個別銘柄は `dataset = 'symbol_market_data'` とし、`parameters.symbol` へ銘柄コードを指定します。入力の完全な仕様は、実行中サーバーの `datalist` を確認してください。

Polymarketで市場・イベントを検索する例です。APIキーは不要です。

```powershell
$body = @{
  provider = 'polymarket'
  dataset = 'search'
  parameters = @{
    query = 'Bitcoin'
  }
} | ConvertTo-Json -Depth 5

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/collect `
  -ContentType 'application/json' `
  -Body $body
```

Polymarket providerも1回の `collect` につき1回の公開GETだけを実行します。検索は `page`、Gammaのイベント・市場一覧は応答の `next_cursor` を次回の `after_cursor` へ、CLOB市場一覧は応答の `next_cursor` を同名の次回入力へ、Data APIの一覧は `offset` を進めて呼び出し側が継続します。応答を自動結合しません。ページング対象のmetadataには `pagination.mode` と `pagination.total_pages_known` を常に付け、上流が総ページ数を返す場合だけ `total_pages_known=true` と実値を返します。詳細は [Polymarket公開API対応状況](docs/polymarket.md) を参照してください。

## MCPクライアント設定

MCPクライアントには、Streamable HTTPエンドポイントとして次を登録します。

```text
http://127.0.0.1:8080/mcp
```

MCPの初期指示と `collect` のtool descriptionには、設定上有効な全データソースの識別子、表示名、対象地域・資産・データ種別を含む概要が自動掲載されます。モデルには、この会話で一覧を未確認の場合、最初の `collect` より前に `datalist` を呼んで全providerを比較するよう案内します。一般知識、掲載順、dataset件数から特定providerを暗黙の既定値にはせず、選択したprovider、dataset、理由を利用者へ示します。

`datalist` は一覧の固定階層、`collect` は収集結果の共通外枠を `outputSchema` として公開します。`collect.data` はproviderとdatasetに固有のため任意のJSON値です。

```json
{
  "provider": "225225jp",
  "dataset": "us_equities",
  "parameters": {
    "session": "pre",
    "universe": "nasdaq100",
    "symbols": ["AAPL", "MSFT"],
    "limit": 20
  }
}
```

## Python provider

同梱の `conf/default.toml` では、現在yfinanceとinvestingpyが両方とも有効です。そのまま起動する場合はPython環境を構築してください。利用しないproviderは `enabled=false` に変更できます。

依存ライブラリを仮想環境へインストールします。

```powershell
python -m venv .venv
./.venv/Scripts/python.exe -m pip install -r python/requirements.lock.txt
```

`python/requirements.lock.txt` は通常の再現インストール用です。`python/requirements.txt` は直接依存だけを記載した更新用入力であり、日常のセットアップには使いません。公開配布する場合は、利用環境ごとのhash付きlockとSBOMも生成することを推奨します。

`conf/zz-python.local.toml` など、`default.toml` より後へ並ぶローカル設定を作成します。

```toml
[providers.yfinance]
enabled = true

[providers.investingpy]
enabled = false

[python]
executable = ".venv/Scripts/python.exe"
script = "python/collector.py"
timeout = "60s"
max_response_bytes = 16777216
max_concurrent_processes = 2
```

`[providers.yfinance].enabled` と `[providers.investingpy].enabled` は独立しています。同梱の `conf/default.toml` では両方とも `true` であり、`true` にしたproviderだけが `datalist` に掲載されます。利用しないproviderは `false` にします。トップレベルの `[python]` は両providerで共有する実行設定です。

`max_concurrent_processes` はyfinanceとinvestingpyで共有するPython子プロセス専用枠で、既定値は2、範囲は1～8です。PythonのCPU・メモリ消費を抑え、専用枠待ちの時間もPython処理の `timeout` に含めます。

yfinanceの価格履歴要求例です。

```json
{
  "provider": "yfinance",
  "dataset": "history",
  "parameters": {
    "ticker": "MSFT",
    "period": "1mo",
    "interval": "1d",
    "auto_adjust": true,
    "repair": false
  }
}
```

Python側はdataset、product、関数をすべて固定許可リストで選びます。入力から任意のPython関数やシェル文字列を実行する機能はありません。pandas、NumPy、日時、NaNなどは標準JSONへ正規化し、MultiIndex相当のtupleキーは安定した文字列へ変換します。未対応objectと正規化後のキー衝突は、推測で文字列化せずエラーにします。

Python providerの応答metadataには `source_name`、`source_url`、`unofficial_client`、`terms_url` が含まれます。取得結果の利用可否を判断する際は、ライブラリ名だけでなく実際のデータ取得元とその規約も確認してください。

## J-Quants provider

`jquants` はJ-Quants API v2へGoから直接HTTPS接続します。Standardプラン、アドオンなしの設定では、17データAPIとBulk API 2件の合計19 datasetが `datalist` に掲載されます。詳細な30件の対応表は [J-Quants API v2 対応状況](docs/jquants.md) に集約しています。

同梱の `conf/default.toml` だけでは `jquants` が無効なため、`datalist` には `225225jp`、`kabus-controller`、`polymarket`、`yfinance`、`investingpy` の5 providerが掲載されます。Git管理外のローカル設定で `jquants` を有効化すると、6 providerが掲載されます。

`conf/zz-jquants.local.toml` のように `default.toml` より後へ並ぶ、Git管理外の `conf/*.local.toml` を作成します。実際のAPIキーを `default.toml`、`conf.toml.sample`、文書、コミット対象のファイルに記載しないでください。

```toml
[providers.jquants]
enabled = true
base_url = "https://api.jquants.com"
api_key = "YOUR_JQUANTS_API_KEY"
plan = "standard"
addons = []
timeout = "30s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

BulkとTDnetのダウンロード系datasetは署名付きURLだけを返し、ファイル本体の取得、展開、保存は行いません。

全J-Quants要求をプロセス内で共通の単一FIFOキューへ受付順に入れ、基本・財務・株価分足／ティック・TDnetの独立quotaで [公式レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)の50%に抑えて開始します。実効上限は基本枠がFree 2.5、Light 30、Standard 60、Premium 250要求/分、追加枠が財務30、株価分足・ティック30、TDnet 50要求/分です。429の自動再試行は行いません。

`max_response_bytes` は既定16 MiB、設定範囲1～64 MiBで、未圧縮本文、Gzipヘッダーを含む圧縮本文、Gzip展開後本文の上限として使います。HTTPリダイレクトは通常どおり追跡し、同一originでは `x-api-key` を維持し、異なるoriginでは同ヘッダーだけを除去します。通信エラーではAPIキーの完全一致だけを伏せ、URLやqueryなどの診断情報は保持するため、queryへ独自の秘密値を入れないでください。

## kabus-controller provider

`kabus-controller` は、KabusControllerの `http://10.10.100.1:8080` へGoから直接接続します。APIキー、Python、外部SDKは不要です。外部provider識別子は大文字小文字を区別する `kabus-controller` で、表示名は `KabusController` です。

```toml
[providers.kabus-controller]
enabled = true
base_url = "http://10.10.100.1:8080"
timeout = "15s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

公開する18 datasetは、従来の登録一覧・登録板6件に加え、詳細ランキング、規制、派生商品コード解決、NT同限月ペア、任意板、登録済みOPチェーン、銘柄情報、優先市場、為替、信用プレミアム料、注文ソフトリミット、controller既知登録数から求める残枠上限を扱います。通常は1 GET、NTペアとOPチェーンは2 GET、登録容量は3 GETを全て成功させてから合成します。OPチェーンは選択脚の気配・出来高・時刻の利用可否と登録一覧の基準時刻を返し、登録容量は重複を除いた既知symbol数から残枠上限を計算します。入力、固定パス、鮮度metadata、登録副作用は [kabus-controller対応状況](docs/kabus-controller.md) を参照してください。

既定の `base_url` はLAN内の平文HTTPです。KabusControllerを信頼できないネットワークへ公開せず、アドレスが異なる環境では `conf/zz-kabus-controller.local.toml` など後順位のローカル設定でオリジンだけを上書きしてください。上流へ `X-API-KEY` 等の認証情報を送らず取引操作も行いませんが、銘柄指定GETによるAPI銘柄登録は起こり得ます。既存登録を保護するため自動解除はしません。

## Polymarket provider

`polymarket` は公開Gamma、CLOB、Data APIへGoから直接HTTPS接続します。APIキー、ウォレット署名、Pythonは不要です。事前検証PJで確認した10機能を移植し、公開読取専用APIを27データセット追加した合計37データセットを `datalist` に掲載します。

```toml
[providers.polymarket]
enabled = true
gamma_base_url = "https://gamma-api.polymarket.com"
clob_base_url = "https://clob.polymarket.com"
data_base_url = "https://data-api.polymarket.com"
timeout = "15s"
user_agent = "MarketDataCollector/0.1"
max_response_bytes = 16777216
```

全Polymarket要求をプロセス内で共通の単一FIFOキューへ入れ、1件ずつ、[公式レートリミット](https://docs.polymarket.com/api-reference/rate-limits)の50%以下で開始します。429を自動再試行しません。JSONは `UseNumber` で復号し、巨大なIDや小数を不用意に `float64` へ変換せず標準JSONへ正規化します。`Accept-Encoding: gzip` を送り、HTTP本文上限は既定16 MiB、設定範囲1～64 MiBとして、圧縮前と展開後の双方へ適用します。

## 設定

`conf` 直下の `.toml` をファイル名昇順で読みます。後のファイルで指定した項目だけが前の設定を上書きします。未知の設定項目は起動エラーになります。

225225.jpへの通信期限とUser-Agentは `[providers.nikkei225jp]` の `timeout` と `user_agent` で設定します。`user_agent` は225225.jpへ送る、利用元を識別可能にする文字列です。通常レスポンス本文の既定上限は4 MiB、チャート本文は32 MiBで、上流レスポンスをローカルに保持しません。J-Quantsの有効状態、API接続、契約範囲は `[providers.jquants]`、KabusControllerの有効状態とAPIオリジンは `[providers.kabus-controller]`、Polymarketの有効状態と3 APIへのHTTP接続は `[providers.polymarket]` にまとめます。yfinanceとinvestingpyの有効状態は各providerセクション、共有子プロセス設定はトップレベル `[python]` に分離しています。

待受は `[SYSTEM].Port` だけで指定し、Host設定はありません。サーバーは常に全インターフェースで待ち受けます。Origin制限はなく、CORSは `Access-Control-Allow-Origin: *` です。

このため、サーバーへ到達可能な利用者は全員、RESTとMCPから収集処理を実行できます。API上は読取専用でも、要求ごとに外部providerへ通信するため、上流負荷と利用規約・データ利用条件のリスクは残ります。J-Quantsを有効にした場合は、到達可能な第三者が設定済みAPIキーの利用枠を消費し、取得データを閲覧できます。

意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御してください。OS側のCPU・メモリ・プロセス制限も併用します。

詳細は [docs/configuration.md](docs/configuration.md) を参照してください。

## 重要な利用上の注意

- 225225.jpは公開REST APIではなく、画面用の内部JavaScript/JSONを参照します。URLと形式は予告なく変更される可能性があります。
- J-Quants APIのAPIキーを公開応答やログに含めないでください。取得データを第三者が閲覧できる形で公開する前に、J-Quantsの契約とデータ利用条件を確認してください。
- kabus-controller providerは固定許可したGETだけを実行します。銘柄指定情報GETはAPI登録銘柄リストを変更し得るため、既定の平文HTTP接続先とKabusController自体をLAN外へ公開せず、取得情報の利用条件を運用者が確認してください。
- Polymarket providerは公開情報だけを読み取りますが、公開ウォレットの情報、予測市場データ、利用地域にはPolymarketの規約と地域制限が適用されます。注文・キャンセル・入出金・認証付きAPIは実装していません。
- yfinanceはYahoo公式SDKではありません。yfinance自身が研究・教育および個人利用に関する注意を示しています。一般公開、組織共有、商用利用の前にYahooとデータ権利者の条件を確認してください。
- `investingpy` というPyPIパッケージは使用しません。外部識別子だけを `investingpy` とし、非公式OSS `investpy==1.0.8` を使います。investpyプロジェクト自身も、Investing.com側の変更により正常動作しない旨を警告しているため、動作は保証されません。
詳細と一次資料へのリンクは [docs/providers.md](docs/providers.md) にまとめています。

## テスト

通常テストは外部サイトへ接続しません。

```powershell
./test.ps1
```

個別に実行する場合は次のとおりです。

```powershell
go test ./...
python -m unittest discover -s python -p "test_*.py"
go vet ./...
go build .
```

225225.jpの実サイト確認は、明示的に環境変数を設定した場合だけ実行されます。

```powershell
$env:LIVE_225225 = '1'
go test ./internal/provider/nikkei225jp -run Live -v
```

## リポジトリ構成

```text
.
├── conf/                         TOML設定とサンプル
├── docs/                         設計、API、MCP、provider仕様
│   ├── jquants.md                J-Quants API v2対応状況
│   ├── kabus-controller.md       KabusController固定GET・合成dataset対応状況
│   ├── polymarket.md             Polymarket公開API対応状況
│   └── setup-guide.md            構築、配置、Python依存の手順書
├── dist/                         OS・CPU別の配布物
├── internal/
│   ├── config/                   TOML読込と検証
│   ├── domain/                   REST/MCP共通DTOとエラー
│   ├── httpserver/               共通HTTP境界
│   ├── mcpserver/                Streamable HTTP MCP adapter
│   ├── provider/                 provider契約と各取得実装
│   │   └── polymarket/           公開Gamma/CLOB/Data APIのGo実装
│   ├── restapi/                  REST adapter
│   └── service/                  接続方式共通ユースケース
├── python/                       yfinance/investpy標準入出力adapter
├── main.go                       `go run .` の起動点
└── test.ps1                      一括検証
```

設計の詳細は [docs/architecture.md](docs/architecture.md)、REST仕様は [docs/rest-api.md](docs/rest-api.md)、MCP仕様は [docs/mcp.md](docs/mcp.md) を参照してください。

J-Quantsのdataset、プラン条件、仕様確認日は [docs/jquants.md](docs/jquants.md)、KabusControllerのdatasetと固定パスは [docs/kabus-controller.md](docs/kabus-controller.md)、Polymarketのdataset、非対応範囲、仕様確認日は [docs/polymarket.md](docs/polymarket.md) を参照してください。
