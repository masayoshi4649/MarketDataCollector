# Python収集アダプター

`collector.py` は収集要求ごとにGoプロセスから起動される、Python 3.12以上の標準入出力アダプターです。1つの子プロセスが標準入力のJSONを1件処理し、成功時は標準出力へJSONを1件返します。ライブラリが出力した付随メッセージとエラーは標準エラーへ送ります。

## セットアップ

```powershell
python -m pip install -r python/requirements.lock.txt
```

`requirements.lock.txt` はCPython 3.14 / Windowsで依存解決と検証を行った通常セットアップ用の固定版です。現在のlockはPython 3.12未満には導入できません。`requirements.txt` はyfinanceとinvestpyという直接依存だけを記載したlock更新用入力であり、再現インストールには使いません。lockはversionだけを固定してhashを含まないため、公開配布では対象環境ごとのhash付きlockとSBOMを生成することを推奨します。

PyPI上の `investingpy` という名前のパッケージは使用しません。外部provider名は要件に合わせて `investingpy` ですが、インストール名とimport名は非公式OSSの `investpy==1.0.8` です。investpyはInvesting.comが提供する公式クライアントではありません。

`investpy==1.0.8` は参照先サイトの変更に追随しておらず、現在の上流サイトに対する動作は保証できません。呼び出し失敗はアダプターの非0終了として扱われます。

利用するproviderだけをTOMLで個別に有効化します。同梱の `conf/default.toml` では、現在両方とも `true` です。

```toml
[providers.yfinance]
enabled = true

[providers.investingpy]
enabled = false

[python]
executable = ".venv/Scripts/python.exe"
script = "python/collector.py"
max_concurrent_processes = 2
```

各providerの `enabled` は独立しています。同梱の `conf/default.toml` では両方とも `true` です。`true` のproviderだけが `datalist` に掲載され、`false` のproviderを `collect` に指定すると `NOT_FOUND` になります。トップレベルの `[python]` が共有実行設定です。`max_concurrent_processes` はyfinanceとinvestingpyで共有する専用枠で、既定値は2、範囲は1～8です。枠待ちもPython処理の `timeout` に含まれます。

Investing.comは公開APIを提供していないと案内しており、Webページの自動抽出には同社とデータ権利者の規約が適用されます。利用条件と書面許諾を確認できない環境では `[providers.investingpy].enabled` を `true` にしないでください。一次資料は [Provider仕様](../docs/providers.md#investingpy) にまとめています。

## 入出力

入力例:

```json
{"provider":"yfinance","dataset":"history","parameters":{"ticker":"AAPL","period":"1mo","interval":"1d"}}
```

成功時の出力形式:

```json
{"data":[],"metadata":{"library":"yfinance","library_version":"1.5.2","source_name":"Yahoo Finance","source_url":"https://finance.yahoo.com/","unofficial_client":true,"terms_url":"https://legal.yahoo.com/us/en/yahoo/terms/product-atos/apiforydn/index.html"}}
```

失敗時も標準出力へ次の構造化JSONを1件返します。公開可能な概要だけを `message` に入れ、内部詳細やライブラリの付随出力は標準エラーへ送ります。

```json
{"error":{"kind":"INVALID_ARGUMENT","message":"入力が不正です。"}}
```

終了コードは次のとおりです。

| 終了コード | `kind` | 内容 |
| --- | --- | --- |
| `0` | なし | 成功 |
| `2` | `INVALID_ARGUMENT` | 入力JSON、provider、dataset、parametersが不正 |
| `3` | `PROVIDER_UNAVAILABLE` | import、実行環境、外部返却値の安全な正規化に失敗 |
| `4` | `UPSTREAM_ERROR` | provider関数または上流通信に失敗 |

標準入力、標準出力、標準エラーはOS localeによらずUTF-8 strictへ固定します。Go側はPythonを `-I` で起動し、Windows実行、PATH、一時ディレクトリ、locale・timezone、TLS証明書に必要な環境変数だけを渡します。この分離はOS sandboxではないため、公開運用ではOSまたはコンテナ側でもCPU、メモリ、プロセス数、ファイル・ネットワーク権限を制限してください。

成功時のmetadataには `library`、`library_version` に加えて、実際の取得元を示す `source_name`、`source_url`、非公式clientであることを示す `unofficial_client`、規約確認先の `terms_url` を含めます。

## 許可データセット

- `yfinance`: `quote`、`history`、`actions`、`financials`、`analysis`、`holders`、`options`、`news`、`search`、`download`
- `investingpy`: `search`、`recent`、`historical`、`information`、`overview`、`economic_calendar`、`technical_indicators`、`moving_averages`、`pivot_points`

`yfinance` は通常 `ticker`、`search` は `query`、`download` は `tickers` を使います。`investingpy` は商品を扱うデータセットで `product` を必須とし、`stock`、`etf`、`fund`、`index`、`currency_cross`、`commodity`、`bond`、`certificate`、`crypto` の明示許可リストから選択します。テクニカル分析系は `certificate` に対応しますが、`crypto` には対応しません。

`investingpy` の `overview` は、`currency_cross` では `currency`、`commodity` では `group`、`bond` では `country` を必須とします。`stock`、`etf`、`fund`、`index`、`certificate` でも `country` が必須です。`crypto` は `n_results` だけを任意指定できます。

`recent`、`historical`、`information` の `country` は、stock、etf、fund、index、certificateで必須、commodityで任意、currency_cross、bond、cryptoでは指定禁止です。`search` では商品種別にかかわらず任意の絞り込みです。テクニカル分析系ではcurrency_crossとcommodityだけが任意で、それ以外の対応商品では必須です。

## 返却値の正規化

pandas DataFrame/Series、MultiIndex、NumPy値、日時、欠測値を標準JSON値へ明示的に変換します。tuple/MultiIndex相当のmapping keyは、各要素を正規化したJSON文字列という安定形式にします。次の値は推測で文字列化せず、終了コード3のエラーにします。

- 未対応のobjectまたはmapping key型
- 循環参照
- 非有限数のmapping key
- 変換後に同じ文字列となるmapping keyの衝突

## 単体テスト

テストは偽モジュールを注入するため、外部通信もライブラリのインストールも不要です。

```powershell
python -m unittest discover -s python -p "test_*.py"
```
