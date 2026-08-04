"""実ライブラリが導入済みの場合に版と関数契約を確認するテスト。"""

from __future__ import annotations

import inspect
import pathlib
import sys
import unittest


sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import collector

try:
    import investpy
    import numpy
    import pandas
    import yfinance
except ImportError:
    investpy = None
    numpy = None
    pandas = None
    yfinance = None


@unittest.skipIf(
    investpy is None or yfinance is None or pandas is None or numpy is None,
    "実Python provider依存が未導入のため契約テストを省略します。",
)
class InstalledLibraryContractTest(unittest.TestCase):
    """固定した実ライブラリの通信不要な契約を検証するテスト。"""

    def test_versions_and_investpy_country_signatures(self):
        """固定版とinvestpyの商品別country引数を検証する。

        機能:
            investpyとyfinanceの版を確認し、countryを受ける商品だけを識別する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        self.assertEqual(investpy.__version__, "1.0.8")
        self.assertEqual(yfinance.__version__, "1.5.2")
        country_supported = {
            "currency_cross": investpy.get_currency_cross_recent_data,
            "commodity": investpy.get_commodity_recent_data,
            "bond": investpy.get_bond_recent_data,
            "crypto": investpy.get_crypto_recent_data,
        }
        for product, function in country_supported.items():
            with self.subTest(product=product):
                parameters = inspect.signature(function).parameters
                self.assertEqual("country" in parameters, product == "commodity")

    def test_yfinance_search_and_download_allowlists_are_real_parameters(self):
        """yfinance許可引数が実関数のシグネチャに存在することを検証する。

        機能:
            Searchとdownloadへ渡す固定引数名の版ずれを通信なしで検出する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        search_parameters = inspect.signature(yfinance.Search).parameters
        for name in {
            "query",
            "max_results",
            "news_count",
            "lists_count",
            "include_cb",
            "include_nav_links",
            "include_research",
            "include_cultural_assets",
            "enable_fuzzy_query",
            "recommended",
            "timeout",
            "raise_errors",
        }:
            self.assertIn(name, search_parameters)

        download_parameters = inspect.signature(yfinance.download).parameters
        for name in {
            "tickers",
            "start",
            "end",
            "actions",
            "threads",
            "ignore_tz",
            "group_by",
            "auto_adjust",
            "back_adjust",
            "repair",
            "keepna",
            "progress",
            "period",
            "interval",
            "prepost",
            "rounding",
            "timeout",
            "multi_level_index",
        }:
            self.assertIn(name, download_parameters)

    def test_real_pandas_numpy_normalization(self):
        """実pandasとNumPyの値を安定したJSONへ正規化する。

        機能:
            MultiIndex列、NumPy scalar、NaNを実依存環境で変換する。
        引数:
            なし。
        返り値:
            None: 返り値はない。
        """

        frame = pandas.DataFrame(
            [[numpy.float64(123.5), numpy.nan]],
            columns=pandas.MultiIndex.from_tuples(
                [("Open", "AAPL"), ("Close", "AAPL")]
            ),
        )
        normalized = collector.normalize_json(frame)
        self.assertEqual(normalized[0]['["Open","AAPL"]'], 123.5)
        self.assertIsNone(normalized[0]['["Close","AAPL"]'])


if __name__ == "__main__":
    unittest.main()
