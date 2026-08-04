# MarketDataCollector

市場情報を要求時に収集し、同じ入出力仕様で REST API と HTTP MCP から返す Go サーバーです。

初期 provider は次の3種類です。

- `225225jp`: 225225.jp の現在値、チャート、日経225構成銘柄、ランキングなど13データセット
- `yfinance`: 価格、企業行動、財務、分析、保有者、オプション、ニュース、検索など10データセット
- `investingpy`: 外部識別子は要件に合わせてこの名前を使い、Pythonでは非公式OSS `investpy==1.0.8` の情報取得機能を利用

データは保存せず、`collect` 要求を受けた時点で取得します。225225.jpの上流レスポンスもローカルに保持せず、取得を伴う要求ごとに上流へ接続します。

## RESTとMCPの対応

標準 MCP の Streamable HTTP は1つの transport URI 内で tool 名により操作を識別します。そのため独自の `/mcp/collect` は作らず、次のように操作名と共通サービスを対応させています。

| 機能 | REST | MCP |
| --- | --- | --- |
| データセット一覧 | `GET /api/datalist` | `POST /mcp` 内の `datalist` tool |
| 要求時収集 | `POST /api/collect` | `POST /mcp` 内の `collect` tool |
| 死活監視 | `GET /healthz` | 対応なし |

`datalist` と `collect` は、RESTとMCPの両方から同じ `internal/service` を呼びます。provider名、dataset名、parameters、返却値、service以降のエラー分類は接続方式で変わりません。HTTP形式不正とJSON-RPC Schema違反のようなtransport境界エラーだけは、それぞれの標準形式で返します。

`datalist` に掲載するのは設定で `enabled=true` のproviderだけです。`enabled=false` のproviderを `collect` に指定した場合は、存在しないproviderと同じ `NOT_FOUND` を返します。

## 必要環境

- Go 1.24.2 以上
- Python providerを使う場合だけPython 3.12以上と `python/requirements.lock.txt` の依存ライブラリ

Python依存の固定版はCPython 3.14 / Windowsで検証しています。現在のlockはPython 3.12未満には導入できません。別のPython・OSで利用する場合は、その環境でもインストールと単体テストを確認してください。

## 起動

ビルド済み配布物の配置、Pythonの要否、仮想環境、依存パッケージ、Windows/Linux別の起動方法は [構築・配置手順書](docs/setup-guide.md) を参照してください。

リポジトリルートで次を実行します。

現在の `conf/default.toml` はPython providerを有効にしているため、先にPython環境を構築するか、使用しないPython providerを `enabled=false` にしてください。

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

## MCPクライアント設定

MCPクライアントには、Streamable HTTPエンドポイントとして次を登録します。

```text
http://127.0.0.1:8080/mcp
```

最初に `datalist` を呼び、返されたproviderとdatasetを `collect` へ指定します。

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

`conf/90-python.local.toml` などを作成します。

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

## 設定

`conf` 直下の `.toml` をファイル名昇順で読みます。後のファイルで指定した項目だけが前の設定を上書きします。未知の設定項目は起動エラーになります。

225225.jpへの通信期限とUser-Agentは `[providers.nikkei225jp]` の `timeout` と `user_agent` で設定します。`user_agent` は225225.jpへ送る、利用元を識別可能にする文字列です。通常レスポンス本文の既定上限は4 MiB、チャート本文は32 MiBで、上流レスポンスをローカルに保持しません。yfinanceとinvestingpyの有効状態は各providerセクション、共有子プロセス設定はトップレベル `[python]` に分離しています。

待受は `[SYSTEM].Port` だけで指定し、Host設定はありません。サーバーは常に全インターフェースで待ち受けます。Origin制限はなく、CORSは `Access-Control-Allow-Origin: *` です。

このため、サーバーへ到達可能な利用者は全員、RESTとMCPから収集処理を実行できます。API上は読取専用でも、要求ごとに外部providerへ通信するため、上流負荷と利用規約・データ利用条件のリスクは残ります。

意図しない利用者から隔離する場合は、OSファイアウォール、コンテナや仮想ネットワーク、TLS・レート制限を提供するリバースプロキシで到達範囲を制御してください。OS側のCPU・メモリ・プロセス制限も併用します。

詳細は [docs/configuration.md](docs/configuration.md) を参照してください。

## 重要な利用上の注意

- 225225.jpは公開REST APIではなく、画面用の内部JavaScript/JSONを参照します。URLと形式は予告なく変更される可能性があります。
- yfinanceはYahoo公式SDKではありません。yfinance自身が研究・教育および個人利用に関する注意を示しています。一般公開、組織共有、商用利用の前にYahooとデータ権利者の条件を確認してください。
- `investingpy` というPyPIパッケージは使用しません。外部識別子だけを `investingpy` とし、非公式OSS `investpy==1.0.8` を使います。investpyプロジェクト自身も、Investing.com側の変更により正常動作しない旨を警告しているため、動作は保証されません。
- Investing.comは公開APIを提供していない旨を案内しています。Webページの自動抽出には同社の規約とデータ権利者の条件が適用されるため、書面許諾のない自動取得を前提にしないでください。

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
│   └── setup-guide.md            構築、配置、Python依存の手順書
├── dist/                         OS・CPU別の配布物
├── internal/
│   ├── config/                   TOML読込と検証
│   ├── domain/                   REST/MCP共通DTOとエラー
│   ├── httpserver/               共通HTTP境界
│   ├── mcpserver/                Streamable HTTP MCP adapter
│   ├── provider/                 provider契約と各取得実装
│   ├── restapi/                  REST adapter
│   └── service/                  接続方式共通ユースケース
├── python/                       yfinance/investpy標準入出力adapter
├── main.go                       `go run .` の起動点
└── test.ps1                      一括検証
```

設計の詳細は [docs/architecture.md](docs/architecture.md)、REST仕様は [docs/rest-api.md](docs/rest-api.md)、MCP仕様は [docs/mcp.md](docs/mcp.md) を参照してください。
