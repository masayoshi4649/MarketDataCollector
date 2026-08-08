# MarketDataCollector 構築・配置手順書

## 1. この手順書の対象

この手順書では、次の2通りを説明します。

- `dist` にあるビルド済みバイナリをWindowsまたはLinuxへ配置して起動する
- ソースコードからWindows amd64版とLinux amd64版をビルドする

REST APIとHTTP MCPは同じプロセスで起動します。データベースや常駐Python workerは使用しません。

## 2. Pythonが必要になる条件

Pythonの要否は、使用するproviderで決まります。

| 使用するprovider                   | Python | Pythonパッケージ |
| ---------------------------------- | ------ | ---------------- |
| `225225jp` だけ                    | 不要   | 不要             |
| `jquants`                          | 不要   | 不要             |
| `polymarket`                       | 不要   | 不要             |
| `yfinance`                         | 必要   | 必要             |
| `investingpy`                      | 必要   | 必要             |
| `yfinance` と `investingpy` の両方 | 必要   | 必要             |

配布物の `conf/default.toml` では、現在 `yfinance` と `investingpy` が両方とも `enabled=true` です。その設定を変更せずに起動する場合は、Python環境を先に構築してください。

Pythonを使用しない場合は、`conf/zz-runtime.local.toml` を作成して次の内容を保存します。`default.toml` より後から読み込まれるこのファイルで両providerを無効にすれば、Python本体、仮想環境、Pythonパッケージはすべて不要です。

```toml
[providers.yfinance]
enabled = false

[providers.investingpy]
enabled = false
```

`jquants` と `polymarket` はGoネイティブproviderのため、他のproviderと同時に有効化してもPythonの要否に影響しません。

### 2.1 J-Quantsを利用する場合

J-QuantsのサブスクリプションとAPIキーを用意します。実APIキーは、Git管理外の `conf/*.local.toml` にだけ保存してください。`conf/default.toml`、`conf/conf.toml.sample`、文書、その他の追跡対象ファイルに実際のキーを記載してはいけません。

`conf/zz-jquants.local.toml` を作成し、次の値を契約と実行環境に合わせます。この名前は `default.toml` より後に読み込まれるため、既定値を確実に上書きします。`YOUR_JQUANTS_API_KEY` は説明用のプレースホルダーです。

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

`plan` は `free`、`light`、`standard`、`premium` から契約中のものを指定します。`addons` には契約済みの `minute` または `tdnet` だけを指定し、Freeプランでは空配列にします。Standardプラン、アドオンなしでは、17データAPIとBulk API 2件の合計19 datasetが `datalist` に掲載されます。詳細な30件の対応表は [J-Quants API v2 対応状況](jquants.md) を参照してください。

`max_response_bytes` は既定16 MiB、設定範囲1～64 MiBです。未圧縮本文、Gzipヘッダーを含む圧縮本文、Gzip展開後本文のいずれにも適用されます。通常のHTTPリダイレクトは追跡し、同一originでは `x-api-key` を維持し、異なるoriginでは同ヘッダーだけを除去します。

全J-Quants要求をプロセス内で共有する単一FIFOキューへ受付順に入れ、基本・財務・株価分足／ティック・TDnetの独立quotaで [公式レートリミット](https://jpx-jquants.com/ja/spec/rate-limits.md)の50%に抑えます。実効上限は基本枠がFree 2.5、Light 30、Standard 60、Premium 250要求/分、財務30、株価分足・ティック30、TDnet 50要求/分です。キューとquotaはプロセス内だけで共有され、429を自動再試行しません。

ページングは1要求1ページです。[公式ページング仕様](https://jpx-jquants.com/ja/spec/pagination.md)には総ページ数、総件数、現在ページがないため、同じ検索条件へ最新 `pagination_key` を指定して継続し、キーが返らなくなれば完了です。`cursor` は `fins_summary`、`fins_details`、`td_list` の日本時間当日差分取得用の不透明値で、最終ページにだけ返ります。値を解釈・加工せず受け渡しますが、自動追跡や永続化は行いません。Standardプラン・アドオンなしではcursor入力を公開しません。詳細は [公式cursor仕様](https://jpx-jquants.com/ja/spec/cursor.md) を参照してください。

このサーバーは全ネットワークインターフェースで待ち受け、CORSは `*` です。到達可能な第三者はAPIキーを知らなくてもJ-Quantsの利用枠を消費し、取得データを閲覧できます。データの第三者配信に該当するおそれがあるため、J-Quantsを有効化したサーバーをそのまま外部公開しないでください。

通信エラーではAPIキーの完全一致だけを伏せ、URLやqueryなどの診断情報は保持します。queryへ独自の秘密値を入れないでください。

### 2.2 Polymarketを利用する場合

Polymarket providerは同梱設定で `enabled=true` です。公開Gamma、CLOB、Data APIのGETだけを使うため、APIキー、wallet署名、Python、Node.js、追加パッケージは不要です。`enabled=true` でも起動時と `datalist` では上流通信せず、`collect` 時だけ通信します。

通常は既定値を保持します。隔離したテスト先または通信期限・本文上限を変更する場合は、`conf/zz-polymarket.local.toml` などへ次を記載します。

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

公式オリジンは通常変更しません。`Accept-Encoding: gzip` を送り、`max_response_bytes` は既定16 MiB、設定範囲1～64 MiBとして未圧縮・Gzip圧縮・展開後本文へ適用します。全要求をプロセス内の単一FIFOキューへ入れて1件ずつ、[公式レートリミット](https://docs.polymarket.com/api-reference/rate-limits)の50%以下で開始し、429を自動再試行しません。

1回の `collect` は1 GETです。検索は `page`、Gammaは応答の `next_cursor` を次回の `after_cursor` へ、CLOBは応答の `next_cursor` を同名の次回入力へ、Dataは `offset` を進めて呼び出し側が継続します。ページング対象では `total_pages_known` を常に返し、総ページ数の実値は上流が提供する場合だけ返します。Dataのoffset型応答は `has_more_known=false` とし、返却件数から完了や次のoffsetを推測しません。37 datasetと入力は `datalist`、固定パスと非対応範囲は [Polymarket公開API対応状況](polymarket.md) を確認してください。

## 3. 必要環境

### 3.1 ビルド済みバイナリを実行する場合

- Windows amd64、またはLinux amd64
- 225225.jpなど取得元へ接続できるHTTPS通信環境
- Linuxでは通常、OSのCA証明書一式
- Python providerを使用する場合だけ、64ビット版CPython 3.12以上
- J-Quants providerを使用する場合は、J-Quants APIへのHTTPS通信、契約プラン、有効なAPIキー
- Polymarket providerを使用する場合は、Gamma、CLOB、Data APIへのHTTPS通信。APIキーとPythonは不要

Pythonの固定依存はCPython 3.14 / Windowsで検証しています。現在の `requirements.lock.txt` はPython 3.12未満では導入できないため、手順上の下限を3.12とします。

### 3.2 ソースからビルドする場合

- Go 1.24.2以上
- Gitから取得する場合はGit
- Go moduleを取得できるHTTPS通信環境
- `test.ps1` を実行する場合はPowerShell、`rg`、Python 3.12以上

バイナリをビルドするだけならPythonは不要です。PythonはPython providerの実行、またはPython単体テストを実行するときに使用します。

## 4. 配布物の構成

```text
dist/
├─ linux-amd64/
│  ├─ MarketDataCollector
│  ├─ SETUP.md
│  ├─ jquants.md
│  ├─ polymarket.md
│  ├─ conf/
│  │  ├─ default.toml
│  │  └─ conf.toml.sample
│  └─ python/
│     ├─ collector.py
│     ├─ requirements.txt
│     └─ requirements.lock.txt
└─ windows-amd64/
   ├─ MarketDataCollector.exe
   ├─ SETUP.md
   ├─ jquants.md
   ├─ polymarket.md
   ├─ conf/
   │  ├─ default.toml
   │  └─ conf.toml.sample
   └─ python/
      ├─ collector.py
      ├─ requirements.txt
      └─ requirements.lock.txt
```

`conf.toml.sample` は説明用であり、拡張子が `.sample` のため自動では読み込まれません。実際の変更値は `default.toml` より後へ並ぶ `conf/zz-runtime.local.toml` など、拡張子が `.toml` の別ファイルへ記載してください。J-Quantsの実APIキーを記載するファイルは、必ず `conf/*.local.toml` としてGit管理から除外します。

## 5. Windows amd64への配置

### 5.1 配置

`dist/windows-amd64` フォルダを、配置先へフォルダごとコピーします。以降は配置したフォルダをカレントディレクトリにして作業します。

```powershell
Set-Location C:\Services\MarketDataCollector
```

別の場所を使う場合は、上記パスを実際の配置先へ読み替えてください。

### 5.2 Pythonを使用しない場合

「2. Pythonが必要になる条件」にある内容で `conf/zz-runtime.local.toml` を作成し、次を実行します。

```powershell
.\MarketDataCollector.exe
```

### 5.3 Pythonを使用する場合

64ビット版CPython 3.12以上をインストールします。次に、配布フォルダ直下へ専用仮想環境を作成し、固定依存をインストールします。

```powershell
py -3.14 --version
py -3.14 -m venv .venv
.\.venv\Scripts\python.exe --version
.\.venv\Scripts\python.exe -m pip install -r .\python\requirements.lock.txt
.\.venv\Scripts\python.exe -m pip check
```

Python 3.12または3.13を利用する場合は、1行目の `-3.14` を実際の版へ変更します。`py` コマンドがない環境では、インストールした `python.exe` の絶対パスで `-m venv .venv` を実行します。

`conf/zz-runtime.local.toml` を作成し、仮想環境と同梱アダプターを指定します。

```toml
[python]
executable = ".venv/Scripts/python.exe"
script = "python/collector.py"

[providers.yfinance]
enabled = true

[providers.investingpy]
enabled = true
```

利用しないproviderは `enabled=false` に変更します。ライブラリを確認してからサーバーを起動します。

```powershell
.\.venv\Scripts\python.exe -c "import yfinance, investpy; print('Python provider: OK')"
.\MarketDataCollector.exe
```

## 6. Linux amd64への配置

### 6.1 配置

`dist/linux-amd64` フォルダを、配置先へフォルダごとコピーします。次の例では `/opt/market-data-collector` を使用します。

```bash
cd /opt/market-data-collector
chmod +x ./MarketDataCollector
```

Linux側でHTTPS通信に必要なCA証明書が未導入の場合は、使用するディストリビューションのパッケージ管理機能で `ca-certificates` を導入してください。

### 6.2 Pythonを使用しない場合

「2. Pythonが必要になる条件」にある内容で `conf/zz-runtime.local.toml` を作成し、次を実行します。

```bash
./MarketDataCollector
```

### 6.3 Pythonを使用する場合

ディストリビューションのパッケージ管理機能で、64ビット版CPython 3.12以上、`venv`、`pip` を導入します。配布フォルダ直下へ専用仮想環境を作成し、固定依存をインストールします。

```bash
python3 --version
python3 -m venv .venv
./.venv/bin/python --version
./.venv/bin/python -m pip install -r ./python/requirements.lock.txt
./.venv/bin/python -m pip check
```

表示されたPython版が3.12未満の場合は処理を進めず、3.12以上を導入して `python3.12` や `python3.14` のような版付きコマンドで仮想環境を作成してください。

固定依存一式の実動作検証済み環境はWindowsです。Linuxでは対象サーバー上でインストールと後述のimport確認を必ず実施してください。利用するLinuxとPythonの組み合わせに対応するwheelがない場合、pipがソースビルドを選択し、C/C++コンパイラーや各ライブラリの開発用パッケージが追加で必要になることがあります。

`conf/zz-runtime.local.toml` を作成します。

```toml
[python]
executable = ".venv/bin/python"
script = "python/collector.py"

[providers.yfinance]
enabled = true

[providers.investingpy]
enabled = true
```

利用しないproviderは `enabled=false` に変更します。ライブラリを確認してからサーバーを起動します。

```bash
./.venv/bin/python -c "import yfinance, investpy; print('Python provider: OK')"
./MarketDataCollector
```

## 7. 必要なPythonパッケージ

アプリケーションが直接利用するパッケージは次の2つです。

| 公開provider名 | インストールするパッケージ | import名   | 固定版  |
| -------------- | -------------------------- | ---------- | ------- |
| `yfinance`     | `yfinance`                 | `yfinance` | `1.5.2` |
| `investingpy`  | `investpy`                 | `investpy` | `1.0.8` |

`investingpy` というPyPIパッケージは使用しません。外部へ公開するprovider識別子だけが `investingpy` であり、実際に導入するパッケージは `investpy` です。

直接依存だけを個別に導入せず、通常は次のコマンドで固定済み依存一式を導入します。

```text
python -m pip install -r python/requirements.lock.txt
```

`requirements.lock.txt` に固定されているパッケージは次のとおりです。

```text
beautifulsoup4==4.15.0
certifi==2026.7.22
cffi==2.1.0
charset-normalizer==3.4.9
curl_cffi==0.16.0
idna==3.18
investpy==1.0.8
lxml==6.1.1
multitasking==0.0.13
numpy==2.5.1
pandas==3.0.5
peewee==4.3.0
platformdirs==4.11.0
protobuf==7.35.1
pycparser==3.0
python-dateutil==2.9.0.post0
pytz==2026.3.post1
requests==2.34.2
setuptools==80.9.0
six==1.17.0
soupsieve==2.9.1
typing_extensions==4.16.0
tzdata==2026.3; sys_platform == "win32"
Unidecode==1.4.0
urllib3==2.7.0
websockets==17.0.1
yfinance==1.5.2
```

`requirements.txt` は直接依存だけを記載した更新用入力です。通常の構築では `requirements.lock.txt` を使用してください。

## 8. 起動と疎通確認

設定ディレクトリの既定値は、カレントディレクトリから見た `conf` です。Pythonの `executable` と `script` に相対パスを指定した場合もカレントディレクトリから解決されるため、通常は配布フォルダ直下で起動してください。

別の場所から起動する場合は、`-conf` に絶対パスを指定し、Pythonのパスも配置先に合わせて絶対パスへ変更します。

起動後、次を確認します。

### Windows PowerShell

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
Invoke-RestMethod http://127.0.0.1:8080/api/datalist
```

### Linux

```bash
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/api/datalist
```

`healthz` が `{"status":"ok"}` を返し、`datalist` に `enabled=true` としたproviderだけが掲載されれば構築完了です。現在の同梱設定を変更していなければ、`225225jp`、`polymarket`、`yfinance`、`investingpy` の4件が掲載され、`polymarket` 内には37 datasetがあります。Git管理外のローカル設定で `jquants` も有効化した場合は、5 providerが掲載され、Standardプラン、アドオンなしでは `jquants` 内に19 datasetが掲載されます。ただし `datalist` はPythonパッケージのimportまでは行わないため、Python側は前述の `pip check` とimport確認も実施してください。

既定の待受は全ネットワークインターフェースのTCP 8080番です。到達範囲はOSまたはネットワーク側のファイアウォールで調整してください。

## 9. ソースからのテストとビルド

ソースをまだ取得していない場合は、次のようにリポジトリを取得してプロジェクトルートへ移動します。ソースアーカイブを使用する場合は展開後のプロジェクトルートへ移動してください。

```text
git clone https://github.com/masayoshi4649/MarketDataCollector.git
cd MarketDataCollector
```

Goの直接依存は `github.com/BurntSushi/toml v1.5.0` と `github.com/modelcontextprotocol/go-sdk v1.3.1` です。間接依存を含む正確な版とchecksumは `go.mod` と `go.sum` で管理されるため、個別に導入せず次のコマンドで取得します。

```text
go mod download
```

### 9.1 テスト

Windowsでは、リポジトリルートで次を実行します。

```powershell
.\test.ps1
```

LinuxでPowerShell、`rg`、PATH上の `python` コマンドを用意した場合は、次のように同じスクリプトを実行できます。

```bash
pwsh -File ./test.ps1
```

PowerShellを導入しないLinux環境では、少なくとも次を個別に実行します。

```bash
go test ./...
go vet ./...
go mod tidy -diff
./.venv/bin/python -m unittest discover -s python -p "test_*.py"
mkdir -p .cache/test-build
go build -trimpath -o .cache/test-build/MarketDataCollector .
```

`test.ps1` はGoの整形確認、Go単体テスト、`go vet`、module差分確認、Python単体テスト、実行環境向けビルドを実行します。Pythonの基本単体テストは偽モジュールを使うため、yfinanceとinvestpyがなくても実行できます。ただし実ライブラリ契約テストは依存不足時にskipされます。配布前の完全確認ではlock一式を導入し、skipが発生していないことも確認してください。

### 9.2 Windowsから2種類をビルドする場合

```powershell
New-Item -ItemType Directory -Force dist\linux-amd64 | Out-Null
New-Item -ItemType Directory -Force dist\windows-amd64 | Out-Null

$env:CGO_ENABLED = '0'
$env:GOARCH = 'amd64'
$env:GOOS = 'linux'
go build -trimpath -o dist\linux-amd64\MarketDataCollector .

$env:GOOS = 'windows'
go build -trimpath -o dist\windows-amd64\MarketDataCollector.exe .
```

### 9.3 Linuxから2種類をビルドする場合

```bash
mkdir -p dist/linux-amd64 dist/windows-amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/linux-amd64/MarketDataCollector .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o dist/windows-amd64/MarketDataCollector.exe .
```

ビルド後は、各platformフォルダへ次を同じ相対配置でコピーします。

- `conf/default.toml`
- `conf/conf.toml.sample`
- `python/collector.py`
- `python/requirements.txt`
- `python/requirements.lock.txt`
- `docs/jquants.md` を `jquants.md` という名前でコピー
- `docs/polymarket.md` を `polymarket.md` という名前でコピー
- この手順書を `SETUP.md` という名前でコピー

## 10. 主な起動エラー

| 状況                                  | 確認箇所                                                             |
| ------------------------------------- | -------------------------------------------------------------------- |
| Python実行ファイルが見つからない      | `python.executable` のパスと、providerの `enabled`                   |
| Pythonアダプターを参照できない        | `python.script` とカレントディレクトリ                               |
| `PROVIDER_UNAVAILABLE`                | 仮想環境へのlock一式の導入、import確認、Python版                     |
| J-Quants有効化時に起動できない        | `conf/*.local.toml` の `providers.jquants.api_key`、`plan`、`addons` |
| J-Quants収集が `PROVIDER_UNAVAILABLE` | 契約プラン・アドオン、APIキー、利用枠、403/429応答                   |
| Polymarket収集が失敗する              | 3 APIへのHTTPS疎通、本文上限、公式変更、429応答、利用条件            |
| TCP 8080番で起動できない              | 同じポートを使用中の別プロセス、`SYSTEM.Port`                        |
| Linuxで実行を拒否される               | `chmod +x ./MarketDataCollector` の実行有無                          |
| 外部データを取得できない              | DNS、HTTPS、CA証明書、取得元の状態、利用条件                         |

`investpy==1.0.8` は参照先サイトの変更に追随しておらず、Python環境の構築に成功しても実データ取得が失敗する場合があります。
