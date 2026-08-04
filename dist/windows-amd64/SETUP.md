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
| `yfinance`                         | 必要   | 必要             |
| `investingpy`                      | 必要   | 必要             |
| `yfinance` と `investingpy` の両方 | 必要   | 必要             |

配布物の `conf/default.toml` では、現在 `yfinance` と `investingpy` が両方とも `enabled=true` です。その設定を変更せずに起動する場合は、Python環境を先に構築してください。


Pythonを使用しない場合は、`conf/90-runtime.local.toml` を作成して次の内容を保存します。後から読み込まれるこのファイルで両providerを無効にすれば、Python本体、仮想環境、Pythonパッケージはすべて不要です。

```toml
[providers.yfinance]
enabled = false

[providers.investingpy]
enabled = false
```

## 3. 必要環境

### 3.1 ビルド済みバイナリを実行する場合

- Windows amd64、またはLinux amd64
- 225225.jpなど取得元へ接続できるHTTPS通信環境
- Linuxでは通常、OSのCA証明書一式
- Python providerを使用する場合だけ、64ビット版CPython 3.12以上

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
   ├─ conf/
   │  ├─ default.toml
   │  └─ conf.toml.sample
   └─ python/
      ├─ collector.py
      ├─ requirements.txt
      └─ requirements.lock.txt
```

`conf.toml.sample` は説明用であり、拡張子が `.sample` のため自動では読み込まれません。実際の変更値は `conf/90-runtime.local.toml` など、拡張子が `.toml` の別ファイルへ記載してください。

## 5. Windows amd64への配置

### 5.1 配置

`dist/windows-amd64` フォルダを、配置先へフォルダごとコピーします。以降は配置したフォルダをカレントディレクトリにして作業します。

```powershell
Set-Location C:\Services\MarketDataCollector
```

別の場所を使う場合は、上記パスを実際の配置先へ読み替えてください。

### 5.2 Pythonを使用しない場合

「2. Pythonが必要になる条件」にある内容で `conf/90-runtime.local.toml` を作成し、次を実行します。

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

`conf/90-runtime.local.toml` を作成し、仮想環境と同梱アダプターを指定します。

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

「2. Pythonが必要になる条件」にある内容で `conf/90-runtime.local.toml` を作成し、次を実行します。

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

`conf/90-runtime.local.toml` を作成します。

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

`healthz` が `{"status":"ok"}` を返し、`datalist` に `enabled=true` としたproviderだけが掲載されれば構築完了です。現在の同梱設定を変更していなければ、`225225jp`、`yfinance`、`investingpy` の3件が掲載されます。ただし `datalist` はPythonパッケージのimportまでは行わないため、Python側は前述の `pip check` とimport確認も実施してください。

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
- この手順書を `SETUP.md` という名前でコピー

## 10. 主な起動エラー

| 状況                             | 確認箇所                                           |
| -------------------------------- | -------------------------------------------------- |
| Python実行ファイルが見つからない | `python.executable` のパスと、providerの `enabled` |
| Pythonアダプターを参照できない   | `python.script` とカレントディレクトリ             |
| `PROVIDER_UNAVAILABLE`           | 仮想環境へのlock一式の導入、import確認、Python版   |
| TCP 8080番で起動できない         | 同じポートを使用中の別プロセス、`SYSTEM.Port`      |
| Linuxで実行を拒否される          | `chmod +x ./MarketDataCollector` の実行有無        |

`investpy==1.0.8` は参照先サイトの変更に追随しておらず、Python環境の構築に成功しても実データ取得が失敗する場合があります。
