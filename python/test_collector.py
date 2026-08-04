"""Python収集アダプターの外部通信を行わない単体テスト。"""

from __future__ import annotations

import datetime
import io
import json
import math
import pathlib
import sys
import unittest
from collections import namedtuple
from decimal import Decimal
from types import SimpleNamespace
from unittest import mock


sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import collector


# ----------------------------------------
# 正規化用の偽オブジェクト
# ----------------------------------------


class FakeNumpyScalar:
    """NumPyスカラー相当の偽オブジェクト。"""

    __module__ = "numpy"

    def __init__(self, value):
        """偽スカラーを初期化する。

        機能:
            itemで返す値を保持する。
        引数:
            value (Any): itemで返す値。
        返り値:
            None: 返り値はない。
        """

        self.value = value

    def item(self):
        """保持したPython標準値を返す。

        機能:
            NumPyスカラーのitemと同じ変換境界を再現する。
        引数:
            なし。
        返り値:
            Any: 初期化時に保持した値。
        """

        return self.value


class FakeNumpyArray:
    """NumPy配列相当の偽オブジェクト。"""

    __module__ = "numpy"

    def __init__(self, values):
        """偽配列を初期化する。

        機能:
            tolistで返す複数要素を保持する。
        引数:
            values (list[Any]): 配列に含める値。
        返り値:
            None: 返り値はない。
        """

        self.values = values

    def item(self):
        """複数要素を単一値にできないエラーを再現する。

        機能:
            実NumPy配列の複数要素に対するitem失敗を再現する。
        引数:
            なし。
        返り値:
            Any: 正常な返り値はない。
        """

        raise ValueError("複数要素は単一値へ変換できません。")

    def tolist(self):
        """保持した値をPython標準配列として返す。

        機能:
            NumPy配列のtolist変換境界を再現する。
        引数:
            なし。
        返り値:
            list[Any]: 初期化時に保持した配列。
        """

        return self.values


class FakeDataFrame:
    """pandasのDataFrame相当の偽オブジェクト。"""

    __module__ = "pandas.core.frame"

    def __init__(self, records):
        """偽DataFrameを初期化する。

        機能:
            records形式へ変換する行データを保持する。
        引数:
            records (list[dict[str, Any]]): 表を構成する行データ。
        返り値:
            None: 返り値はない。
        """

        self.records = records
        self.reset_called = False

    def reset_index(self):
        """インデックスを列へ戻した表を返す。

        機能:
            実DataFrameのreset_index呼び出しを記録する。
        引数:
            なし。
        返り値:
            FakeDataFrame: この偽DataFrame自身。
        """

        self.reset_called = True
        return self

    def to_dict(self, orient):
        """表を指定形式の対応表へ変換する。

        機能:
            records指定を確認し、保持した行データを返す。
        引数:
            orient (str): 要求された変換形式。
        返り値:
            list[dict[str, Any]]: 保持した行データ。
        """

        if orient != "records":
            raise AssertionError("orientにはrecordsを指定してください。")
        return self.records


# ----------------------------------------
# yfinance用の偽モジュール
# ----------------------------------------


class FakeTicker:
    """yfinanceのTicker相当の偽オブジェクト。"""

    def __init__(self, ticker, calls):
        """偽Tickerを初期化する。

        機能:
            tickerと呼び出し記録先を保持し、固定プロパティを用意する。
        引数:
            ticker (str): 銘柄ティッカー。
            calls (list[Any]): 呼び出し記録先。
        返り値:
            None: 返り値はない。
        """

        self.ticker = ticker
        self.calls = calls
        self.options = ("2026-08-21", "2026-09-18")

    def get_info(self):
        """偽の銘柄基本情報を返す。

        機能:
            quoteの固定getter呼び出しを記録する。
        引数:
            なし。
        返り値:
            dict[str, Any]: 偽の銘柄基本情報。
        """

        self.calls.append(("get_info", self.ticker, {}))
        return {"symbol": self.ticker, "price": FakeNumpyScalar(123)}

    def history(self, **keyword_arguments):
        """偽の価格履歴を返す。

        機能:
            historyへ渡された許可済み引数を記録する。
        引数:
            keyword_arguments (Any): 履歴取得条件。
        返り値:
            FakeDataFrame: 偽の価格履歴。
        """

        self.calls.append(("history", self.ticker, keyword_arguments))
        return FakeDataFrame(
            [
                {
                    "Date": datetime.datetime(2026, 8, 1, 9, 0),
                    "Close": math.nan,
                }
            ]
        )

    def get_actions(self, **keyword_arguments):
        """偽の企業行動履歴を返す。

        機能:
            get_actionsへ渡された引数を記録する。
        引数:
            keyword_arguments (Any): 企業行動取得条件。
        返り値:
            list[dict[str, Any]]: 偽の企業行動履歴。
        """

        self.calls.append(("get_actions", self.ticker, keyword_arguments))
        return [{"Dividends": 1.0}]

    def get_income_stmt(self, **keyword_arguments):
        """偽の損益計算書を返す。

        機能:
            get_income_stmtへ渡された引数を記録する。
        引数:
            keyword_arguments (Any): 財務諸表取得条件。
        返り値:
            dict[str, int]: 偽の損益計算書。
        """

        self.calls.append(("get_income_stmt", self.ticker, keyword_arguments))
        return {"Revenue": 100}

    def get_earnings_estimate(self):
        """偽の利益予測を返す。

        機能:
            固定された分析getterの呼び出しを記録する。
        引数:
            なし。
        返り値:
            dict[str, int]: 偽の利益予測。
        """

        self.calls.append(("get_earnings_estimate", self.ticker, {}))
        return {"avg": 10}

    def get_institutional_holders(self):
        """偽の機関投資家情報を返す。

        機能:
            固定された保有者getterの呼び出しを記録する。
        引数:
            なし。
        返り値:
            list[dict[str, str]]: 偽の機関投資家情報。
        """

        self.calls.append(("get_institutional_holders", self.ticker, {}))
        return [{"Holder": "機関A"}]

    def option_chain(self, option_date):
        """偽のオプションチェーンを返す。

        機能:
            option_chainへ渡された満期日を記録する。
        引数:
            option_date (str): オプション満期日。
        返り値:
            tuple[Any, Any]: callsとputsを持つ名前付きタプル。
        """

        self.calls.append(("option_chain", self.ticker, {"date": option_date}))
        chain_type = namedtuple("OptionChain", ["calls", "puts"])
        return chain_type([{"strike": 100}], [{"strike": 90}])

    def get_news(self, **keyword_arguments):
        """偽のニュース一覧を返す。

        機能:
            get_newsへ渡された引数を記録する。
        引数:
            keyword_arguments (Any): ニュース取得条件。
        返り値:
            list[dict[str, str]]: 偽のニュース一覧。
        """

        self.calls.append(("get_news", self.ticker, keyword_arguments))
        return [{"title": "市場ニュース"}]


class FakeYFinance:
    """yfinanceモジュール相当の偽オブジェクト。"""

    __version__ = "9.9.1"

    def __init__(self):
        """偽yfinanceモジュールを初期化する。

        機能:
            関数呼び出しの記録配列を用意する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        self.calls = []

    def Ticker(self, ticker):
        """偽Tickerを生成する。

        機能:
            Ticker生成を記録し、同じ記録先を持つ偽Tickerを返す。
        引数:
            ticker (str): 銘柄ティッカー。
        返り値:
            FakeTicker: 生成した偽Ticker。
        """

        self.calls.append(("Ticker", ticker, {}))
        return FakeTicker(ticker, self.calls)

    def Search(self, query, **keyword_arguments):
        """偽の横断検索を実行する。

        機能:
            Searchへ渡された検索語と条件を記録する。
        引数:
            query (str): 検索語。
            keyword_arguments (Any): 検索条件。
        返り値:
            SimpleNamespace: 固定検索結果を持つオブジェクト。
        """

        self.calls.append(("Search", query, keyword_arguments))
        return SimpleNamespace(
            quotes=[{"symbol": "AAPL"}],
            news=[{"title": "検索ニュース"}],
            lists=[],
            research=[],
            nav=[],
        )

    def download(self, **keyword_arguments):
        """偽の一括価格データを返す。

        機能:
            downloadへ渡された条件を記録する。
        引数:
            keyword_arguments (Any): 一括取得条件。
        返り値:
            dict[str, int]: 偽の価格データ。
        """

        self.calls.append(("download", None, keyword_arguments))
        return {"rows": 2}


# ----------------------------------------
# investpy用の偽モジュール
# ----------------------------------------


class FakeInvestpy:
    """investpyモジュール相当の偽オブジェクト。"""

    __version__ = "1.0.8-偽"

    def __init__(self):
        """偽investpyモジュールを初期化する。

        機能:
            関数呼び出しの記録配列を用意する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        self.calls = []

    def _record(self, function_name, keyword_arguments):
        """偽関数の呼び出しを記録する。

        機能:
            関数名とキーワード引数を記録し、共通の偽結果を返す。
        引数:
            function_name (str): 呼び出された固定関数名。
            keyword_arguments (dict[str, Any]): 関数へ渡された引数。
        返り値:
            dict[str, Any]: 関数名と引数を持つ偽結果。
        """

        self.calls.append((function_name, keyword_arguments))
        return {"function": function_name, "arguments": keyword_arguments}

    def search_quotes(self, **keyword_arguments):
        """偽の銘柄検索を実行する。

        機能:
            search_quotesの呼び出しを記録する。
        引数:
            keyword_arguments (Any): 検索条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("search_quotes", keyword_arguments)

    def get_currency_cross_recent_data(self, **keyword_arguments):
        """偽の通貨ペア直近データを返す。

        機能:
            固定recent関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 直近データ取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_currency_cross_recent_data", keyword_arguments)

    def get_commodity_recent_data(self, **keyword_arguments):
        """偽の商品直近データを返す。

        機能:
            countryを省略できるcommodity用固定関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 直近データ取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_commodity_recent_data", keyword_arguments)

    def get_stock_historical_data(self, **keyword_arguments):
        """偽の株価履歴を返す。

        機能:
            固定historical関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 履歴取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_stock_historical_data", keyword_arguments)

    def get_stock_information(self, **keyword_arguments):
        """偽の株式情報を返す。

        機能:
            固定information関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 情報取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_stock_information", keyword_arguments)

    def get_cryptos_overview(self, **keyword_arguments):
        """偽の暗号資産概要を返す。

        機能:
            固定overview関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 概要取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_cryptos_overview", keyword_arguments)

    def get_currency_crosses_overview(self, **keyword_arguments):
        """偽の通貨ペア概要を返す。

        機能:
            currencyを必須とする固定overview関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 概要取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_currency_crosses_overview", keyword_arguments)

    def get_commodities_overview(self, **keyword_arguments):
        """偽の商品概要を返す。

        機能:
            groupを必須とする固定overview関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 概要取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_commodities_overview", keyword_arguments)

    def get_bonds_overview(self, **keyword_arguments):
        """偽の債券概要を返す。

        機能:
            countryを必須とする固定overview関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 概要取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("get_bonds_overview", keyword_arguments)

    def economic_calendar(self, **keyword_arguments):
        """偽の経済指標カレンダーを返す。

        機能:
            固定economic_calendar関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): カレンダー取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("economic_calendar", keyword_arguments)

    def technical_indicators(self, **keyword_arguments):
        """偽のテクニカル指標を返す。

        機能:
            固定technical_indicators関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): テクニカル指標取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("technical_indicators", keyword_arguments)

    def moving_averages(self, **keyword_arguments):
        """偽の移動平均を返す。

        機能:
            固定moving_averages関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): 移動平均取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("moving_averages", keyword_arguments)

    def pivot_points(self, **keyword_arguments):
        """偽のピボットポイントを返す。

        機能:
            固定pivot_points関数の呼び出しを記録する。
        引数:
            keyword_arguments (Any): ピボットポイント取得条件。
        返り値:
            dict[str, Any]: 共通の偽結果。
        """

        return self._record("pivot_points", keyword_arguments)


# ----------------------------------------
# 収集処理のテスト
# ----------------------------------------


class CollectorDispatchTest(unittest.TestCase):
    """固定許可リストによる収集処理を検証するテスト。"""

    def _collect(self, provider, dataset, parameters, fake_module):
        """偽モジュールを使って1件の収集処理を実行する。

        機能:
            テスト用リクエストの共通組み立てとモジュール注入を行う。
        引数:
            provider (str): 外部provider名。
            dataset (str): データセット名。
            parameters (dict[str, Any]): 収集パラメーター。
            fake_module (Any): 注入する偽モジュール。
        返り値:
            dict[str, Any]: collector.collectのレスポンス。
        """

        return collector.collect(
            {
                "provider": provider,
                "dataset": dataset,
                "parameters": parameters,
            },
            {provider: fake_module},
        )

    def test_yfinance_quote_history_and_metadata(self):
        """yfinanceのquote、history、メタデータを検証する。

        機能:
            固定Ticker経由の呼び出しとDataFrame正規化を確認する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        fake_module = FakeYFinance()
        quote_response = self._collect(
            "yfinance", "quote", {"ticker": "AAPL"}, fake_module
        )
        self.assertEqual(quote_response["data"], {"symbol": "AAPL", "price": 123})
        self.assertEqual(
            quote_response["metadata"]["library"], "yfinance"
        )
        self.assertEqual(quote_response["metadata"]["library_version"], "9.9.1")
        self.assertEqual(quote_response["metadata"]["source_name"], "Yahoo Finance")
        self.assertTrue(quote_response["metadata"]["unofficial_client"])

        history_response = self._collect(
            "yfinance",
            "history",
            {"ticker": "MSFT", "period": "1mo", "interval": "1d"},
            fake_module,
        )
        self.assertEqual(
            history_response["data"],
            [{"Date": "2026-08-01T09:00:00", "Close": None}],
        )
        self.assertEqual(
            fake_module.calls[-1],
            ("history", "MSFT", {"period": "1mo", "interval": "1d"}),
        )

    def test_yfinance_fixed_dataset_handlers(self):
        """yfinanceの個別データセットが固定getterへ到達することを検証する。

        機能:
            actions、financials、analysis、holders、options、newsを一括確認する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        cases = (
            ("actions", {"ticker": "AAPL", "period": "5y"}, "get_actions"),
            (
                "financials",
                {"ticker": "AAPL", "statement": "income", "frequency": "annual"},
                "get_income_stmt",
            ),
            (
                "analysis",
                {"ticker": "AAPL", "section": "earnings_estimate"},
                "get_earnings_estimate",
            ),
            (
                "holders",
                {"ticker": "AAPL", "section": "institutional"},
                "get_institutional_holders",
            ),
            (
                "options",
                {"ticker": "AAPL", "date": "2026-08-21"},
                "option_chain",
            ),
            ("news", {"ticker": "AAPL", "count": 3, "tab": "news"}, "get_news"),
        )
        for dataset, parameters, expected_function in cases:
            with self.subTest(dataset=dataset):
                fake_module = FakeYFinance()
                self._collect("yfinance", dataset, parameters, fake_module)
                self.assertEqual(fake_module.calls[-1][0], expected_function)

    def test_yfinance_search_and_download(self):
        """yfinanceのモジュール直下固定関数を検証する。

        機能:
            searchとdownloadが許可済み引数だけを受け取ることを確認する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        fake_module = FakeYFinance()
        search_response = self._collect(
            "yfinance",
            "search",
            {"query": "Apple", "max_results": 2},
            fake_module,
        )
        self.assertEqual(search_response["data"]["quotes"][0]["symbol"], "AAPL")
        self.assertEqual(fake_module.calls[-1], ("Search", "Apple", {"max_results": 2}))

        self._collect(
            "yfinance",
            "download",
            {"tickers": ["AAPL", "MSFT"], "period": "5d"},
            fake_module,
        )
        self.assertEqual(
            fake_module.calls[-1],
            ("download", None, {"tickers": ["AAPL", "MSFT"], "period": "5d"}),
        )

    def test_investpy_dataset_dispatch(self):
        """investpyの全データセット分類を検証する。

        機能:
            検索、商品情報、概要、経済指標、テクニカル系の固定関数を確認する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        cases = (
            (
                "search",
                {"product": "stock", "query": "トヨタ", "country": "japan"},
                "search_quotes",
                {
                    "text": "トヨタ",
                    "products": ["stocks"],
                    "countries": ["japan"],
                },
            ),
            (
                "recent",
                {"product": "currency_cross", "name": "USD/JPY", "interval": "Daily"},
                "get_currency_cross_recent_data",
                {"currency_cross": "USD/JPY", "interval": "Daily"},
            ),
            (
                "recent",
                {"product": "commodity", "name": "gold"},
                "get_commodity_recent_data",
                {"commodity": "gold"},
            ),
            (
                "historical",
                {
                    "product": "stock",
                    "name": "Toyota Motor",
                    "country": "japan",
                    "from_date": "01/07/2026",
                    "to_date": "31/07/2026",
                },
                "get_stock_historical_data",
                {
                    "stock": "Toyota Motor",
                    "country": "japan",
                    "from_date": "01/07/2026",
                    "to_date": "31/07/2026",
                },
            ),
            (
                "information",
                {"product": "stock", "name": "Toyota Motor", "country": "japan"},
                "get_stock_information",
                {"stock": "Toyota Motor", "country": "japan"},
            ),
            (
                "overview",
                {"product": "crypto", "n_results": 5},
                "get_cryptos_overview",
                {"n_results": 5},
            ),
            (
                "overview",
                {"product": "currency_cross", "currency": "JPY", "n_results": 3},
                "get_currency_crosses_overview",
                {"currency": "JPY", "n_results": 3},
            ),
            (
                "overview",
                {"product": "commodity", "group": "metals"},
                "get_commodities_overview",
                {"group": "metals"},
            ),
            (
                "overview",
                {"product": "bond", "country": "japan"},
                "get_bonds_overview",
                {"country": "japan"},
            ),
            (
                "economic_calendar",
                {"countries": ["japan"], "importances": ["high"]},
                "economic_calendar",
                {"countries": ["japan"], "importances": ["high"]},
            ),
            (
                "technical_indicators",
                {"product": "currency_cross", "name": "USD/JPY"},
                "technical_indicators",
                {
                    "name": "USD/JPY",
                    "country": None,
                    "product_type": "currency_cross",
                },
            ),
            (
                "moving_averages",
                {
                    "product": "stock",
                    "name": "Toyota Motor",
                    "country": "japan",
                    "interval": "daily",
                },
                "moving_averages",
                {
                    "name": "Toyota Motor",
                    "country": "japan",
                    "product_type": "stock",
                    "interval": "daily",
                },
            ),
            (
                "pivot_points",
                {"product": "certificate", "name": "BNP Gold", "country": "france"},
                "pivot_points",
                {
                    "name": "BNP Gold",
                    "country": "france",
                    "product_type": "certificate",
                },
            ),
        )

        for dataset, parameters, function_name, expected_arguments in cases:
            with self.subTest(dataset=dataset):
                fake_module = FakeInvestpy()
                response = self._collect(
                    "investingpy", dataset, parameters, fake_module
                )
                self.assertEqual(fake_module.calls[-1], (function_name, expected_arguments))
                self.assertEqual(response["metadata"]["library"], "investpy")
                self.assertEqual(response["metadata"]["library_version"], "1.0.8-偽")

    def test_unknown_inputs_are_rejected_before_execution(self):
        """未知入力がライブラリ実行前に拒否されることを検証する。

        機能:
            dataset、parameter、productによる任意関数実行を防ぐ。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        fake_module = FakeInvestpy()
        invalid_requests = (
            {
                "provider": "investingpy",
                "dataset": "__dict__",
                "parameters": {},
            },
            {
                "provider": "investingpy",
                "dataset": "recent",
                "parameters": {
                    "product": "__dict__",
                    "name": "危険値",
                },
            },
            {
                "provider": "investingpy",
                "dataset": "recent",
                "parameters": {
                    "product": "crypto",
                    "name": "bitcoin",
                    "function": "任意関数",
                },
            },
            {
                "provider": "investingpy",
                "dataset": "technical_indicators",
                "parameters": {"product": "crypto", "name": "bitcoin"},
            },
        )
        for request in invalid_requests:
            with self.subTest(request=request):
                with self.assertRaises(collector.CollectorError):
                    collector.collect(request, {"investingpy": fake_module})
        self.assertEqual(fake_module.calls, [])

    def test_required_country_and_ticker_validation(self):
        """商品固有必須値とティッカー型の検証を確認する。

        機能:
            country欠落と空tickerをライブラリ呼び出し前に拒否する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        fake_investpy = FakeInvestpy()
        with self.assertRaisesRegex(collector.CollectorError, "countryが必須"):
            self._collect(
                "investingpy",
                "historical",
                {
                    "product": "stock",
                    "name": "Toyota Motor",
                    "from_date": "01/07/2026",
                    "to_date": "31/07/2026",
                },
                fake_investpy,
            )
        self.assertEqual(fake_investpy.calls, [])

        fake_yfinance = FakeYFinance()
        with self.assertRaisesRegex(collector.CollectorError, "ticker"):
            self._collect("yfinance", "quote", {"ticker": []}, fake_yfinance)
        self.assertEqual(fake_yfinance.calls, [])

    def test_resource_limits_and_conditional_country(self):
        """件数上限とinvestpyの商品別country条件を検証する。

        機能:
            過大な銘柄数、スレッド数、検索件数と非対応countryを拒否する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        invalid_cases = (
            (
                "yfinance",
                "download",
                {"tickers": [f"T{index}" for index in range(101)]},
                FakeYFinance(),
            ),
            (
                "yfinance",
                "download",
                {"tickers": ["AAPL"], "threads": 33},
                FakeYFinance(),
            ),
            (
                "yfinance",
                "search",
                {"query": "Apple", "max_results": 101},
                FakeYFinance(),
            ),
            (
                "investingpy",
                "recent",
                {"product": "bond", "name": "Japan 10Y", "country": "japan"},
                FakeInvestpy(),
            ),
            (
                "investingpy",
                "recent",
                {"product": "currency_cross", "name": "USD/JPY", "interval": "daily"},
                FakeInvestpy(),
            ),
        )
        for provider, dataset, parameters, fake_module in invalid_cases:
            with self.subTest(provider=provider, dataset=dataset, parameters=parameters):
                with self.assertRaises(collector.InputError):
                    self._collect(provider, dataset, parameters, fake_module)


class JsonNormalizationTest(unittest.TestCase):
    """標準JSONへの正規化処理を検証するテスト。"""

    def test_datetime_nan_decimal_numpy_and_named_tuple(self):
        """代表的なライブラリ値の正規化を検証する。

        機能:
            日時、非有限数、Decimal、NumPyスカラー、名前付きタプルを確認する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        pair_type = namedtuple("Pair", ["left", "right"])
        source = {
            "datetime": datetime.datetime(
                2026, 8, 2, 12, 30, tzinfo=datetime.timezone.utc
            ),
            "date": datetime.date(2026, 8, 2),
            "time": datetime.time(12, 30),
            "delta": datetime.timedelta(seconds=90),
            "nan": math.nan,
            "positive_infinity": math.inf,
            "decimal": Decimal("12.5"),
            "decimal_nan": Decimal("NaN"),
            "numpy": FakeNumpyScalar(7),
            "numpy_array": FakeNumpyArray([1, math.nan]),
            "pair": pair_type(1, 2),
        }
        normalized = collector.normalize_json(source)
        self.assertEqual(normalized["datetime"], "2026-08-02T12:30:00+00:00")
        self.assertEqual(normalized["date"], "2026-08-02")
        self.assertEqual(normalized["time"], "12:30:00")
        self.assertEqual(normalized["delta"], 90.0)
        self.assertIsNone(normalized["nan"])
        self.assertIsNone(normalized["positive_infinity"])
        self.assertEqual(normalized["decimal"], 12.5)
        self.assertIsNone(normalized["decimal_nan"])
        self.assertEqual(normalized["numpy"], 7)
        self.assertEqual(normalized["numpy_array"], [1, None])
        self.assertEqual(normalized["pair"], {"left": 1, "right": 2})
        json.dumps(normalized, allow_nan=False)

    def test_dataframe_and_cycle_detection(self):
        """pandas表形式と循環参照の処理を検証する。

        機能:
            DataFrameを行配列へ変換し、循環参照を明示エラーにする。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        frame = FakeDataFrame([{"index": 1, "value": math.nan}])
        self.assertEqual(
            collector.normalize_json(frame), [{"index": 1, "value": None}]
        )
        self.assertTrue(frame.reset_called)

        cyclic = []
        cyclic.append(cyclic)
        with self.assertRaisesRegex(collector.CollectorError, "循環参照"):
            collector.normalize_json(cyclic)

    def test_mapping_key_collision_and_unknown_object_are_rejected(self):
        """キー衝突と未対応オブジェクトの黙示変換を拒否する。

        機能:
            文字列化後に重複するキーと安定serializerのない型をエラーにする。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        with self.assertRaisesRegex(collector.CollectorError, "重複"):
            collector.normalize_json({1: "整数", "1": "文字列"})
        with self.assertRaisesRegex(collector.CollectorError, "未対応"):
            collector.normalize_json(SimpleNamespace(value=1))

        normalized = collector.normalize_json({("Open", "AAPL"): 123})
        self.assertEqual(normalized, {'["Open","AAPL"]': 123})


class StandardIoTest(unittest.TestCase):
    """標準入力と標準出力の境界を検証するテスト。"""

    def test_main_outputs_exactly_one_strict_json(self):
        """成功時の標準出力が厳密JSON1件だけであることを検証する。

        機能:
            遅延importを偽モジュールへ置換し、mainの出力を解析する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        standard_input = io.StringIO(
            json.dumps(
                {
                    "provider": "yfinance",
                    "dataset": "history",
                    "parameters": {"ticker": "AAPL", "period": "1d"},
                }
            )
        )
        standard_output = io.StringIO()
        standard_error = io.StringIO()
        with (
            mock.patch.object(sys, "stdin", standard_input),
            mock.patch.object(sys, "stdout", standard_output),
            mock.patch.object(sys, "stderr", standard_error),
            mock.patch.object(
                collector.importlib, "import_module", return_value=FakeYFinance()
            ) as import_module,
        ):
            exit_code = collector.main()

        self.assertEqual(exit_code, 0)
        self.assertEqual(import_module.call_args.args, ("yfinance",))
        lines = standard_output.getvalue().splitlines()
        self.assertEqual(len(lines), 1)
        response = json.loads(lines[0], parse_constant=lambda value: self.fail(value))
        self.assertIsNone(response["data"][0]["Close"])
        self.assertEqual(standard_error.getvalue(), "")

    def test_main_failure_uses_stderr_and_nonzero_exit(self):
        """失敗時の終了コードと標準エラーを検証する。

        機能:
            不正JSONで標準出力が空、標準エラーが日本語になることを確認する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        standard_output = io.StringIO()
        standard_error = io.StringIO()
        with (
            mock.patch.object(sys, "stdin", io.StringIO("{不正JSON")),
            mock.patch.object(sys, "stdout", standard_output),
            mock.patch.object(sys, "stderr", standard_error),
        ):
            exit_code = collector.main()

        self.assertEqual(exit_code, collector.EXIT_INVALID_ARGUMENT)
        response = json.loads(standard_output.getvalue())
        self.assertEqual(response["error"]["kind"], "INVALID_ARGUMENT")
        self.assertIn("入力JSONを解析できません", response["error"]["message"])
        self.assertEqual(standard_error.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
