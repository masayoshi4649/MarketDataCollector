"""市場情報ライブラリを安全に呼び出す標準入出力アダプター。"""

from __future__ import annotations

import contextlib
import datetime as datetime_module
import importlib
import importlib.metadata
import json
import math
import re
import sys
from collections.abc import Mapping
from decimal import Decimal
from enum import Enum
from typing import Any


PROVIDER_IMPORT_NAMES = {
    "yfinance": "yfinance",
    "investingpy": "investpy",
}

PROVIDER_SOURCE_METADATA = {
    "yfinance": {
        "source_name": "Yahoo Finance",
        "source_url": "https://finance.yahoo.com/",
        "unofficial_client": True,
        "terms_url": "https://legal.yahoo.com/us/en/yahoo/terms/product-atos/apiforydn/index.html",
    },
    "investingpy": {
        "source_name": "Investing.com",
        "source_url": "https://www.investing.com/",
        "unofficial_client": True,
        "terms_url": "https://cdn.investing.com/about-us/terms_and_conditions.pdf",
    },
}

YFINANCE_DATASETS = frozenset(
    {
        "quote",
        "history",
        "actions",
        "financials",
        "analysis",
        "holders",
        "options",
        "news",
        "search",
        "download",
    }
)

INVESTPY_DATASETS = frozenset(
    {
        "search",
        "recent",
        "historical",
        "information",
        "overview",
        "economic_calendar",
        "technical_indicators",
        "moving_averages",
        "pivot_points",
    }
)

PROVIDER_DATASETS = {
    "yfinance": YFINANCE_DATASETS,
    "investingpy": INVESTPY_DATASETS,
}

EXIT_INVALID_ARGUMENT = 2
EXIT_PROVIDER_UNAVAILABLE = 3
EXIT_UPSTREAM_ERROR = 4

MAX_YFINANCE_TICKERS = 100
MAX_YFINANCE_RESULT_COUNT = 100
MAX_YFINANCE_THREADS = 32
MAX_PROVIDER_TIMEOUT_SECONDS = 300.0
MAX_INVESTPY_RESULTS = 1000
MAX_FILTER_VALUES = 100
MAX_TEXT_LENGTH = 4096
MAX_FILTER_TEXT_LENGTH = 256
MAX_TICKER_LENGTH = 128

YFINANCE_PERIODS = frozenset(
    {"1d", "5d", "1mo", "3mo", "6mo", "1y", "2y", "5y", "10y", "ytd", "max"}
)
YFINANCE_INTERVALS = frozenset(
    {"1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "5d", "1wk", "1mo", "3mo"}
)
INVESTPY_ORDER_VALUES = frozenset({"ascending", "descending"})
INVESTPY_PRICE_INTERVALS = frozenset({"Daily", "Weekly", "Monthly"})
INVESTPY_TECHNICAL_INTERVALS = frozenset(
    {"5mins", "15mins", "30mins", "1hour", "5hours", "daily", "weekly", "monthly"}
)
INVESTPY_TIME_FILTERS = frozenset({"time_only", "time_remain"})
INVESTPY_IMPORTANCES = frozenset({"low", "medium", "high"})

INVESTPY_PRODUCT_ARGUMENTS = {
    "stock": "stock",
    "etf": "etf",
    "fund": "fund",
    "index": "index",
    "currency_cross": "currency_cross",
    "commodity": "commodity",
    "bond": "bond",
    "certificate": "certificate",
    "crypto": "crypto",
}

INVESTPY_SEARCH_PRODUCTS = {
    "stock": "stocks",
    "etf": "etfs",
    "fund": "funds",
    "index": "indices",
    "currency_cross": "currencies",
    "commodity": "commodities",
    "bond": "bonds",
    "certificate": "certificates",
    "crypto": "cryptos",
}

INVESTPY_NAMED_FUNCTIONS = {
    "recent": {
        "stock": "get_stock_recent_data",
        "etf": "get_etf_recent_data",
        "fund": "get_fund_recent_data",
        "index": "get_index_recent_data",
        "currency_cross": "get_currency_cross_recent_data",
        "commodity": "get_commodity_recent_data",
        "bond": "get_bond_recent_data",
        "certificate": "get_certificate_recent_data",
        "crypto": "get_crypto_recent_data",
    },
    "historical": {
        "stock": "get_stock_historical_data",
        "etf": "get_etf_historical_data",
        "fund": "get_fund_historical_data",
        "index": "get_index_historical_data",
        "currency_cross": "get_currency_cross_historical_data",
        "commodity": "get_commodity_historical_data",
        "bond": "get_bond_historical_data",
        "certificate": "get_certificate_historical_data",
        "crypto": "get_crypto_historical_data",
    },
    "information": {
        "stock": "get_stock_information",
        "etf": "get_etf_information",
        "fund": "get_fund_information",
        "index": "get_index_information",
        "currency_cross": "get_currency_cross_information",
        "commodity": "get_commodity_information",
        "bond": "get_bond_information",
        "certificate": "get_certificate_information",
        "crypto": "get_crypto_information",
    },
}

INVESTPY_TECHNICAL_FUNCTIONS = {
    "technical_indicators": "technical_indicators",
    "moving_averages": "moving_averages",
    "pivot_points": "pivot_points",
}

INVESTPY_TECHNICAL_PRODUCTS = {
    "stock": "stock",
    "etf": "etf",
    "fund": "fund",
    "index": "index",
    "currency_cross": "currency_cross",
    "commodity": "commodity",
    "bond": "bond",
    "certificate": "certificate",
}

INVESTPY_TECHNICAL_COUNTRY_OPTIONAL_PRODUCTS = frozenset(
    {"currency_cross", "commodity"}
)

INVESTPY_OVERVIEW_FUNCTIONS = {
    "stock": "get_stocks_overview",
    "etf": "get_etfs_overview",
    "fund": "get_funds_overview",
    "index": "get_indices_overview",
    "currency_cross": "get_currency_crosses_overview",
    "commodity": "get_commodities_overview",
    "bond": "get_bonds_overview",
    "certificate": "get_certificates_overview",
    "crypto": "get_cryptos_overview",
}

INVESTPY_COUNTRY_REQUIRED_PRODUCTS = frozenset(
    {"stock", "etf", "fund", "index", "certificate"}
)

INVESTPY_NAMED_COUNTRY_PRODUCTS = frozenset(
    {"stock", "etf", "fund", "index", "commodity", "certificate"}
)


class CollectorError(Exception):
    """Python実行環境または収集結果を利用できないことを表すエラー。"""


class InputError(CollectorError):
    """利用者がリクエストを修正できる入力エラー。"""


# ----------------------------------------
# 入力検証
# ----------------------------------------


def _validate_request(payload: Any) -> tuple[str, str, dict[str, Any]]:
    """収集リクエスト全体を検証する。

    機能:
        JSONから復元した値が所定のオブジェクト構造か確認する。
    引数:
        payload (Any): JSONから復元した入力値。
    返り値:
        tuple[str, str, dict[str, Any]]: provider、dataset、parametersの組。
    """

    if not isinstance(payload, Mapping):
        raise InputError("入力JSONの最上位はオブジェクトにしてください。")

    allowed_keys = {"provider", "dataset", "parameters"}
    unknown_keys = sorted(str(key) for key in payload.keys() if key not in allowed_keys)
    if unknown_keys:
        raise InputError(
            f"入力JSONに未対応の項目があります: {', '.join(unknown_keys)}"
        )

    provider = payload.get("provider")
    dataset = payload.get("dataset")
    parameters = payload.get("parameters")

    if not isinstance(provider, str):
        providers = ", ".join(sorted(PROVIDER_DATASETS))
        raise InputError(f"providerは次のいずれかにしてください: {providers}")
    if provider not in PROVIDER_DATASETS:
        providers = ", ".join(sorted(PROVIDER_DATASETS))
        raise InputError(
            f"provider「{provider[:128]}」は未対応です。次のいずれかにしてください: {providers}"
        )
    if not isinstance(dataset, str) or dataset not in PROVIDER_DATASETS[provider]:
        datasets = ", ".join(sorted(PROVIDER_DATASETS[provider]))
        raise InputError(
            f"provider「{provider}」のdatasetは次のいずれかにしてください: {datasets}"
        )
    if not isinstance(parameters, Mapping):
        raise InputError("parametersはオブジェクトにしてください。")

    return provider, dataset, dict(parameters)


def _validate_parameters(
    parameters: Mapping[str, Any],
    *,
    required: frozenset[str] = frozenset(),
    optional: frozenset[str] = frozenset(),
) -> dict[str, Any]:
    """データセット固有のパラメーター名と必須項目を検証する。

    機能:
        未許可パラメーターを拒否し、必須パラメーターの存在を確認する。
    引数:
        parameters (Mapping[str, Any]): 検証するパラメーター。
        required (frozenset[str]): 必須パラメーター名。
        optional (frozenset[str]): 任意パラメーター名。
    返り値:
        dict[str, Any]: 検証済みパラメーターの複製。
    """

    allowed = required | optional
    unknown = sorted(str(key) for key in parameters.keys() if key not in allowed)
    if unknown:
        raise InputError(f"未対応のパラメーターがあります: {', '.join(unknown)}")

    missing = sorted(
        name
        for name in required
        if name not in parameters or parameters[name] is None or parameters[name] == ""
    )
    if missing:
        raise InputError(f"必須パラメーターがありません: {', '.join(missing)}")

    return dict(parameters)


def _require_text(
    value: Any,
    parameter_name: str,
    maximum_length: int = MAX_TEXT_LENGTH,
) -> str:
    """値が空でない文字列であることを検証する。

    機能:
        銘柄名、検索語、日付などの文字列入力を検証する。
    引数:
        value (Any): 検証対象の値。
        parameter_name (str): エラー表示に使うパラメーター名。
        maximum_length (int): 許可する最大文字数。
    返り値:
        str: 検証済み文字列。
    """

    if (
        not isinstance(value, str)
        or not value.strip()
        or len(value) > maximum_length
    ):
        raise InputError(
            f"{parameter_name}は1文字以上{maximum_length}文字以下の文字列にしてください。"
        )
    return value


def _require_product(value: Any, supported_products: Mapping[str, str]) -> str:
    """商品種別がデータセットの許可リスト内か検証する。

    機能:
        入力値から任意の関数名が選ばれないよう商品種別を制限する。
    引数:
        value (Any): productとして指定された値。
        supported_products (Mapping[str, str]): 商品種別と固定関数名の対応。
    返り値:
        str: 検証済みの商品種別。
    """

    if not isinstance(value, str) or value not in supported_products:
        products = ", ".join(sorted(supported_products))
        raise InputError(f"productは次のいずれかにしてください: {products}")
    return value


def _require_integer(
    value: Any,
    parameter_name: str,
    minimum: int,
    maximum: int,
) -> int:
    """値が指定範囲の整数であることを検証する。

    機能:
        boolを整数として扱わず、件数やスレッド数の上下限を強制する。
    引数:
        value (Any): 検証対象の値。
        parameter_name (str): エラー表示に使うパラメーター名。
        minimum (int): 許可する最小値。
        maximum (int): 許可する最大値。
    返り値:
        int: 検証済み整数。
    """

    if (
        isinstance(value, bool)
        or not isinstance(value, int)
        or value < minimum
        or value > maximum
    ):
        raise InputError(
            f"{parameter_name}は{minimum}以上{maximum}以下の整数にしてください。"
        )
    return value


def _require_number(
    value: Any,
    parameter_name: str,
    minimum_exclusive: float,
    maximum: float,
) -> float:
    """値が有限な数値範囲にあることを検証する。

    機能:
        上流timeoutなどの数値入力へ有限性と上限を適用する。
    引数:
        value (Any): 検証対象の値。
        parameter_name (str): エラー表示に使うパラメーター名。
        minimum_exclusive (float): この値より大きい必要がある下限。
        maximum (float): 許可する最大値。
    返り値:
        float: 検証済み数値。
    """

    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise InputError(f"{parameter_name}は数値にしてください。")
    normalized = float(value)
    if not math.isfinite(normalized) or not minimum_exclusive < normalized <= maximum:
        raise InputError(
            f"{parameter_name}は{minimum_exclusive}より大きく{maximum}以下にしてください。"
        )
    return normalized


def _require_boolean(value: Any, parameter_name: str) -> bool:
    """値が真偽値であることを検証する。

    機能:
        Pythonの暗黙変換を行わずJSON booleanだけを受理する。
    引数:
        value (Any): 検証対象の値。
        parameter_name (str): エラー表示に使うパラメーター名。
    返り値:
        bool: 検証済み真偽値。
    """

    if not isinstance(value, bool):
        raise InputError(f"{parameter_name}はbooleanにしてください。")
    return value


def _require_choice(value: Any, parameter_name: str, allowed: frozenset[str]) -> str:
    """文字列値が固定許可リスト内か検証する。

    機能:
        period、interval、order等を公開仕様と同じ値に制限する。
    引数:
        value (Any): 検証対象の値。
        parameter_name (str): エラー表示に使うパラメーター名。
        allowed (frozenset[str]): 許可する文字列集合。
    返り値:
        str: 検証済み文字列。
    """

    if not isinstance(value, str) or value not in allowed:
        choices = "、".join(sorted(allowed))
        raise InputError(f"{parameter_name}は次のいずれかにしてください: {choices}")
    return value


def _require_text_list(
    value: Any,
    parameter_name: str,
    maximum_items: int = MAX_FILTER_VALUES,
) -> list[str]:
    """空でない文字列配列と最大件数を検証する。

    機能:
        国、重要度、分類などの配列が資源上限内か確認する。
    引数:
        value (Any): 検証対象の値。
        parameter_name (str): エラー表示に使うパラメーター名。
        maximum_items (int): 許可する最大要素数。
    返り値:
        list[str]: 検証済み文字列配列の複製。
    """

    if (
        not isinstance(value, list)
        or not value
        or len(value) > maximum_items
        or not all(
            isinstance(item, str)
            and item.strip()
            and len(item) <= MAX_FILTER_TEXT_LENGTH
            for item in value
        )
    ):
        raise InputError(
            f"{parameter_name}は1件以上{maximum_items}件以下、各{MAX_FILTER_TEXT_LENGTH}文字以下の文字列配列にしてください。"
        )
    return list(value)


# ----------------------------------------
# ライブラリ読み込み
# ----------------------------------------


def _load_provider_module(
    provider: str, injected_modules: Mapping[str, Any] | None
) -> Any:
    """許可済みproviderに対応するPythonモジュールを遅延読み込みする。

    機能:
        テスト用注入モジュールを優先し、通常時だけ対象ライブラリを読み込む。
    引数:
        provider (str): 検証済みprovider名。
        injected_modules (Mapping[str, Any] | None): テスト用モジュール対応表。
    返り値:
        Any: 読み込んだライブラリモジュール。
    """

    import_name = PROVIDER_IMPORT_NAMES[provider]
    if injected_modules is not None:
        if provider in injected_modules:
            return injected_modules[provider]
        if import_name in injected_modules:
            return injected_modules[import_name]

    try:
        return importlib.import_module(import_name)
    except ImportError as error:
        raise CollectorError(
            f"必要なライブラリ「{import_name}」を読み込めません。"
            "python/requirements.lock.txtをインストールしてください。"
        ) from error


def _library_version(provider: str, module: Any) -> str:
    """利用したライブラリのバージョンを取得する。

    機能:
        モジュール属性を優先し、無い場合はパッケージ情報から取得する。
    引数:
        provider (str): 検証済みprovider名。
        module (Any): 読み込み済みライブラリモジュール。
    返り値:
        str: ライブラリのバージョン。不明な場合は「不明」。
    """

    module_version = getattr(module, "__version__", None)
    if module_version is not None:
        return str(module_version)

    import_name = PROVIDER_IMPORT_NAMES[provider]
    try:
        return importlib.metadata.version(import_name)
    except importlib.metadata.PackageNotFoundError:
        return "不明"


def _fixed_function(module: Any, function_name: str) -> Any:
    """固定許可リストで選択済みのライブラリ関数を取得する。

    機能:
        ライブラリに必要な関数が存在し、呼び出し可能か確認する。
    引数:
        module (Any): 対象ライブラリモジュール。
        function_name (str): 内部の固定表から選ばれた関数名。
    返り値:
        Any: 呼び出し可能なライブラリ関数。
    """

    function = getattr(module, function_name, None)
    if not callable(function):
        raise CollectorError(
            f"インストール済みライブラリに必要な関数「{function_name}」がありません。"
        )
    return function


# ----------------------------------------
# yfinance収集処理
# ----------------------------------------


def _yfinance_ticker(module: Any, ticker_name: Any) -> Any:
    """yfinanceのTickerオブジェクトを生成する。

    機能:
        tickerを検証し、固定のTicker生成機能だけを呼び出す。
    引数:
        module (Any): yfinanceモジュール。
        ticker_name (Any): 銘柄ティッカー候補。
    返り値:
        Any: yfinanceのTickerオブジェクト。
    """

    ticker = _require_text(ticker_name, "ticker", MAX_TICKER_LENGTH)
    ticker_factory = _fixed_function(module, "Ticker")
    return ticker_factory(ticker)


def _fixed_member(
    target: Any,
    getter_name: str,
    property_name: str,
    keyword_arguments: Mapping[str, Any] | None = None,
) -> Any:
    """固定済みgetterまたは固定済みプロパティから値を取得する。

    機能:
        yfinanceの版差を吸収しつつ、入力由来ではないメンバーだけを参照する。
    引数:
        target (Any): Tickerオブジェクト。
        getter_name (str): 内部固定表で選択されたgetter名。
        property_name (str): 内部固定表で選択された代替プロパティ名。
        keyword_arguments (Mapping[str, Any] | None): getterへ渡す固定済み引数。
    返り値:
        Any: getterまたはプロパティから得た値。
    """

    getter = getattr(target, getter_name, None)
    if callable(getter):
        return getter(**dict(keyword_arguments or {}))
    try:
        return getattr(target, property_name)
    except AttributeError as error:
        raise CollectorError(
            f"インストール済みyfinanceに必要な機能「{getter_name}」がありません。"
        ) from error


def _collect_yfinance_quote(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄の基本情報を収集する。

    機能:
        固定のget_info機能からquoteデータを取得する。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickerを含む入力パラメーター。
    返り値:
        Any: yfinanceが返した銘柄基本情報。
    """

    values = _validate_parameters(parameters, required=frozenset({"ticker"}))
    ticker = _yfinance_ticker(module, values["ticker"])
    return _fixed_member(ticker, "get_info", "info")


def _collect_yfinance_history(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄の価格履歴を収集する。

    機能:
        許可済みの期間・間隔・補正オプションだけをhistoryへ渡す。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickerと履歴条件。
    返り値:
        Any: yfinanceが返した価格履歴。
    """

    optional = frozenset(
        {
            "period",
            "start",
            "end",
            "interval",
            "prepost",
            "actions",
            "auto_adjust",
            "back_adjust",
            "repair",
            "keepna",
            "rounding",
            "timeout",
            "raise_errors",
        }
    )
    values = _validate_parameters(
        parameters, required=frozenset({"ticker"}), optional=optional
    )
    ticker = _yfinance_ticker(module, values.pop("ticker"))
    if "period" in values:
        _require_choice(values["period"], "period", YFINANCE_PERIODS)
    if "interval" in values:
        _require_choice(values["interval"], "interval", YFINANCE_INTERVALS)
    for argument_name in ("start", "end"):
        if argument_name in values:
            _require_text(values[argument_name], argument_name)
    for argument_name in (
        "prepost",
        "actions",
        "auto_adjust",
        "back_adjust",
        "repair",
        "keepna",
        "rounding",
        "raise_errors",
    ):
        if argument_name in values:
            _require_boolean(values[argument_name], argument_name)
    if "timeout" in values:
        _require_number(
            values["timeout"], "timeout", 0.0, MAX_PROVIDER_TIMEOUT_SECONDS
        )
    history = getattr(ticker, "history", None)
    if not callable(history):
        raise CollectorError("インストール済みyfinanceにhistory機能がありません。")
    return history(**values)


def _collect_yfinance_actions(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄の配当・分割履歴を収集する。

    機能:
        固定のget_actions機能から企業行動データを取得する。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickerと任意のperiod。
    返り値:
        Any: yfinanceが返した企業行動データ。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"ticker"}),
        optional=frozenset({"period"}),
    )
    ticker = _yfinance_ticker(module, values.pop("ticker"))
    if "period" in values:
        _require_choice(values["period"], "period", YFINANCE_PERIODS)
    return _fixed_member(ticker, "get_actions", "actions", values)


def _collect_yfinance_financials(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄の財務諸表を収集する。

    機能:
        種別と頻度の許可リストから固定getterを選び、財務諸表を取得する。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): ticker、statement、frequency。
    返り値:
        Any: 選択した財務諸表、または全財務諸表の対応表。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"ticker"}),
        optional=frozenset({"statement", "frequency"}),
    )
    ticker = _yfinance_ticker(module, values["ticker"])
    statement = values.get("statement", "all")
    frequency = values.get("frequency", "annual")

    statement_members = {
        "income": ("get_income_stmt", "financials", "quarterly_financials"),
        "balance_sheet": (
            "get_balance_sheet",
            "balance_sheet",
            "quarterly_balance_sheet",
        ),
        "cash_flow": ("get_cashflow", "cashflow", "quarterly_cashflow"),
    }
    if statement != "all" and statement not in statement_members:
        raise InputError(
            "statementはall、income、balance_sheet、cash_flowのいずれかにしてください。"
        )
    frequency_map = {"annual": "yearly", "quarterly": "quarterly"}
    if frequency not in frequency_map:
        raise InputError("frequencyはannualまたはquarterlyにしてください。")

    selected_statements = (
        statement_members.keys() if statement == "all" else (statement,)
    )
    result: dict[str, Any] = {}
    for statement_name in selected_statements:
        getter_name, annual_property, quarterly_property = statement_members[
            statement_name
        ]
        property_name = (
            annual_property if frequency == "annual" else quarterly_property
        )
        result[statement_name] = _fixed_member(
            ticker,
            getter_name,
            property_name,
            {"freq": frequency_map[frequency]},
        )

    if statement != "all":
        return result[statement]
    return result


def _collect_yfinance_analysis(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄のアナリスト分析を収集する。

    機能:
        sectionの許可リストから固定getterを選び、分析情報を取得する。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickerと任意のsection。
    返り値:
        Any: 選択した分析情報、または全分析情報の対応表。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"ticker"}),
        optional=frozenset({"section"}),
    )
    ticker = _yfinance_ticker(module, values["ticker"])
    section = values.get("section", "all")
    section_members = {
        "analyst_price_targets": (
            "get_analyst_price_targets",
            "analyst_price_targets",
        ),
        "earnings_estimate": ("get_earnings_estimate", "earnings_estimate"),
        "revenue_estimate": ("get_revenue_estimate", "revenue_estimate"),
        "earnings_history": ("get_earnings_history", "earnings_history"),
        "eps_trend": ("get_eps_trend", "eps_trend"),
        "eps_revisions": ("get_eps_revisions", "eps_revisions"),
        "growth_estimates": ("get_growth_estimates", "growth_estimates"),
        "recommendations": ("get_recommendations", "recommendations"),
    }
    if section != "all" and section not in section_members:
        sections = ", ".join(["all", *section_members.keys()])
        raise InputError(f"sectionは次のいずれかにしてください: {sections}")

    selected_sections = section_members.keys() if section == "all" else (section,)
    result = {
        section_name: _fixed_member(
            ticker,
            section_members[section_name][0],
            section_members[section_name][1],
        )
        for section_name in selected_sections
    }
    if section != "all":
        return result[section]
    return result


def _collect_yfinance_holders(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄の保有者情報を収集する。

    機能:
        sectionの許可リストから固定getterを選び、保有者情報を取得する。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickerと任意のsection。
    返り値:
        Any: 選択した保有者情報、または全保有者情報の対応表。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"ticker"}),
        optional=frozenset({"section"}),
    )
    ticker = _yfinance_ticker(module, values["ticker"])
    section = values.get("section", "all")
    section_members = {
        "major": ("get_major_holders", "major_holders"),
        "institutional": ("get_institutional_holders", "institutional_holders"),
        "mutualfund": ("get_mutualfund_holders", "mutualfund_holders"),
        "insider_transactions": (
            "get_insider_transactions",
            "insider_transactions",
        ),
        "insider_purchases": ("get_insider_purchases", "insider_purchases"),
        "insider_roster": ("get_insider_roster_holders", "insider_roster_holders"),
    }
    if section != "all" and section not in section_members:
        sections = ", ".join(["all", *section_members.keys()])
        raise InputError(f"sectionは次のいずれかにしてください: {sections}")

    selected_sections = section_members.keys() if section == "all" else (section,)
    result = {
        section_name: _fixed_member(
            ticker,
            section_members[section_name][0],
            section_members[section_name][1],
        )
        for section_name in selected_sections
    }
    if section != "all":
        return result[section]
    return result


def _collect_yfinance_options(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄のオプション満期またはチェーンを収集する。

    機能:
        date未指定時は満期一覧、指定時は固定のoption_chainを取得する。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickerと任意のdate。
    返り値:
        Any: 満期一覧またはオプションチェーン。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"ticker"}),
        optional=frozenset({"date"}),
    )
    ticker = _yfinance_ticker(module, values["ticker"])
    if "date" not in values:
        try:
            return ticker.options
        except AttributeError as error:
            raise CollectorError(
                "インストール済みyfinanceにoptions機能がありません。"
            ) from error

    option_date = _require_text(values["date"], "date")
    option_chain = getattr(ticker, "option_chain", None)
    if not callable(option_chain):
        raise CollectorError("インストール済みyfinanceにoption_chain機能がありません。")
    return option_chain(option_date)


def _collect_yfinance_news(module: Any, parameters: Mapping[str, Any]) -> Any:
    """銘柄に関連するニュースを収集する。

    機能:
        countとtabだけを許可し、固定のget_news機能を呼び出す。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): ticker、count、tab。
    返り値:
        Any: yfinanceが返したニュース一覧。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"ticker"}),
        optional=frozenset({"count", "tab"}),
    )
    ticker = _yfinance_ticker(module, values.pop("ticker"))
    if "count" in values:
        _require_integer(
            values["count"], "count", 1, MAX_YFINANCE_RESULT_COUNT
        )
    if "tab" in values and values["tab"] not in {
        "news",
        "all",
        "press releases",
    }:
        raise InputError("tabはnews、all、press releasesのいずれかにしてください。")
    return _fixed_member(ticker, "get_news", "news", values)


def _collect_yfinance_search(module: Any, parameters: Mapping[str, Any]) -> Any:
    """yfinanceの横断検索結果を収集する。

    機能:
        queryと検索件数関連の許可済み引数だけをSearchへ渡す。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): queryと検索条件。
    返り値:
        Any: quote、news、list、research、navの検索結果。
    """

    optional = frozenset(
        {
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
        }
    )
    values = _validate_parameters(
        parameters, required=frozenset({"query"}), optional=optional
    )
    query = _require_text(values.pop("query"), "query", 512)
    for argument_name in ("max_results", "news_count", "lists_count", "recommended"):
        if argument_name in values:
            _require_integer(
                values[argument_name],
                argument_name,
                0,
                MAX_YFINANCE_RESULT_COUNT,
            )
    for argument_name in (
        "include_cb",
        "include_nav_links",
        "include_research",
        "include_cultural_assets",
        "enable_fuzzy_query",
        "raise_errors",
    ):
        if argument_name in values:
            _require_boolean(values[argument_name], argument_name)
    if "timeout" in values:
        _require_number(
            values["timeout"], "timeout", 0.0, MAX_PROVIDER_TIMEOUT_SECONDS
        )
    search_factory = _fixed_function(module, "Search")
    search_result = search_factory(query, **values)
    if isinstance(search_result, Mapping):
        return search_result

    result = {}
    for member_name in ("quotes", "news", "lists", "research", "nav"):
        member = getattr(search_result, member_name, None)
        if member is not None:
            result[member_name] = member
    return result if result else search_result


def _collect_yfinance_download(module: Any, parameters: Mapping[str, Any]) -> Any:
    """複数銘柄の価格データを一括収集する。

    機能:
        tickersと許可済みのdownload引数だけを固定関数へ渡す。
    引数:
        module (Any): yfinanceモジュール。
        parameters (Mapping[str, Any]): tickersとダウンロード条件。
    返り値:
        Any: yfinanceが返した価格データ。
    """

    optional = frozenset(
        {
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
        }
    )
    values = _validate_parameters(
        parameters, required=frozenset({"tickers"}), optional=optional
    )
    tickers = values["tickers"]
    valid_tickers = isinstance(tickers, str) and bool(tickers.strip())
    ticker_count = 0
    if valid_tickers:
        ticker_values = [
            ticker for ticker in re.split(r"[\s,]+", tickers.strip()) if ticker
        ]
        ticker_count = len(ticker_values)
        valid_tickers = all(
            len(ticker) <= MAX_TICKER_LENGTH for ticker in ticker_values
        )
    if isinstance(tickers, list):
        valid_tickers = bool(tickers) and all(
            isinstance(ticker, str)
            and bool(ticker.strip())
            and len(ticker) <= MAX_TICKER_LENGTH
            for ticker in tickers
        )
        ticker_count = len(tickers)
    if not valid_tickers or ticker_count > MAX_YFINANCE_TICKERS:
        raise InputError(
            f"tickersは1件以上{MAX_YFINANCE_TICKERS}件以下の銘柄文字列または文字列配列にしてください。"
        )
    if "period" in values:
        _require_choice(values["period"], "period", YFINANCE_PERIODS)
    if "interval" in values:
        _require_choice(values["interval"], "interval", YFINANCE_INTERVALS)
    if "group_by" in values:
        _require_choice(
            values["group_by"], "group_by", frozenset({"column", "ticker"})
        )
    for argument_name in ("start", "end"):
        if argument_name in values:
            _require_text(values[argument_name], argument_name)
    for argument_name in (
        "actions",
        "ignore_tz",
        "auto_adjust",
        "back_adjust",
        "repair",
        "keepna",
        "progress",
        "prepost",
        "rounding",
        "multi_level_index",
    ):
        if argument_name in values:
            _require_boolean(values[argument_name], argument_name)
    if "threads" in values and not isinstance(values["threads"], bool):
        _require_integer(values["threads"], "threads", 1, MAX_YFINANCE_THREADS)
    if "timeout" in values:
        _require_number(
            values["timeout"], "timeout", 0.0, MAX_PROVIDER_TIMEOUT_SECONDS
        )
    download = _fixed_function(module, "download")
    return download(**values)


YFINANCE_HANDLERS = {
    "quote": _collect_yfinance_quote,
    "history": _collect_yfinance_history,
    "actions": _collect_yfinance_actions,
    "financials": _collect_yfinance_financials,
    "analysis": _collect_yfinance_analysis,
    "holders": _collect_yfinance_holders,
    "options": _collect_yfinance_options,
    "news": _collect_yfinance_news,
    "search": _collect_yfinance_search,
    "download": _collect_yfinance_download,
}


def _dispatch_yfinance(
    module: Any, dataset: str, parameters: Mapping[str, Any]
) -> Any:
    """yfinanceの固定ハンドラーへリクエストを振り分ける。

    機能:
        検証済みdatasetに対応する明示許可済みハンドラーだけを実行する。
    引数:
        module (Any): yfinanceモジュール。
        dataset (str): 検証済みデータセット名。
        parameters (Mapping[str, Any]): データセット固有パラメーター。
    返り値:
        Any: yfinanceハンドラーの収集結果。
    """

    return YFINANCE_HANDLERS[dataset](module, parameters)


# ----------------------------------------
# investpy収集処理
# ----------------------------------------


def _investpy_country(
    product: str, values: Mapping[str, Any], *, required: bool = True
) -> str | None:
    """商品種別に応じてcountryを検証する。

    機能:
        国指定が必要な商品でcountryを必須化し、指定値を文字列として検証する。
    引数:
        product (str): 検証済みの商品種別。
        values (Mapping[str, Any]): 入力パラメーター。
        required (bool): 国指定必須ルールを適用するかどうか。
    返り値:
        str | None: 検証済みcountry。指定がなければNone。
    """

    country = values.get("country")
    if required and product in INVESTPY_COUNTRY_REQUIRED_PRODUCTS and country is None:
        raise InputError(f"product「{product}」ではcountryが必須です。")
    if country is None:
        return None
    if product not in INVESTPY_NAMED_COUNTRY_PRODUCTS:
        raise InputError(f"product「{product}」ではcountryを指定できません。")
    return _require_text(country, "country")


def _collect_investpy_search(module: Any, parameters: Mapping[str, Any]) -> Any:
    """investpyの銘柄横断検索を実行する。

    機能:
        商品種別を検索用固定値へ変換し、固定のsearch_quotes関数を呼び出す。
    引数:
        module (Any): investpyモジュール。
        parameters (Mapping[str, Any]): product、query、country、n_results。
    返り値:
        Any: investpyが返した検索結果。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"product", "query"}),
        optional=frozenset({"country", "n_results"}),
    )
    product = _require_product(values["product"], INVESTPY_SEARCH_PRODUCTS)
    query = _require_text(values["query"], "query")
    keyword_arguments: dict[str, Any] = {
        "text": query,
        "products": [INVESTPY_SEARCH_PRODUCTS[product]],
    }
    if "country" in values:
        country = values["country"]
        if isinstance(country, str):
            country = [_require_text(country, "country")]
        else:
            country = _require_text_list(country, "country")
        keyword_arguments["countries"] = country
    if "n_results" in values:
        keyword_arguments["n_results"] = _require_integer(
            values["n_results"], "n_results", 1, MAX_INVESTPY_RESULTS
        )
    search_quotes = _fixed_function(module, "search_quotes")
    return search_quotes(**keyword_arguments)


def _collect_investpy_named(
    module: Any, dataset: str, parameters: Mapping[str, Any]
) -> Any:
    """商品名を指定するinvestpyデータセットを収集する。

    機能:
        datasetとproductの二段階許可リストから固定関数を選択して実行する。
    引数:
        module (Any): investpyモジュール。
        dataset (str): recent、historical、informationのいずれか。
        parameters (Mapping[str, Any]): product、name、countryおよび期間条件。
    返り値:
        Any: investpyが返した市場データまたは商品情報。
    """

    optional_by_dataset = {
        "recent": frozenset({"country", "order", "interval"}),
        "historical": frozenset({"country", "order", "interval"}),
        "information": frozenset({"country"}),
    }
    required = {"product", "name"}
    if dataset == "historical":
        required.update({"from_date", "to_date"})
    values = _validate_parameters(
        parameters,
        required=frozenset(required),
        optional=optional_by_dataset[dataset],
    )

    function_map = INVESTPY_NAMED_FUNCTIONS[dataset]
    product = _require_product(values.pop("product"), function_map)
    name = _require_text(values.pop("name"), "name")
    country = _investpy_country(product, values)

    keyword_arguments = {INVESTPY_PRODUCT_ARGUMENTS[product]: name}
    if country is not None:
        keyword_arguments["country"] = country
    for argument_name in ("from_date", "to_date", "order", "interval"):
        if argument_name in values:
            argument_value = values[argument_name]
            if argument_name in {"from_date", "to_date"}:
                argument_value = _require_text(argument_value, argument_name)
            elif argument_name == "order":
                argument_value = _require_choice(
                    argument_value, argument_name, INVESTPY_ORDER_VALUES
                )
            elif argument_name == "interval":
                argument_value = _require_choice(
                    argument_value, argument_name, INVESTPY_PRICE_INTERVALS
                )
            keyword_arguments[argument_name] = argument_value

    function = _fixed_function(module, function_map[product])
    return function(**keyword_arguments)


def _collect_investpy_overview(module: Any, parameters: Mapping[str, Any]) -> Any:
    """商品種別ごとの市場概要を収集する。

    機能:
        商品種別別の許可パラメーターを検証して固定overview関数を実行する。
    引数:
        module (Any): investpyモジュール。
        parameters (Mapping[str, Any]): productと概要条件。
    返り値:
        Any: investpyが返した市場概要。
    """

    product_value = parameters.get("product")
    product = _require_product(product_value, INVESTPY_OVERVIEW_FUNCTIONS)
    required_by_product = {
        "stock": frozenset({"product", "country"}),
        "etf": frozenset({"product", "country"}),
        "fund": frozenset({"product", "country"}),
        "index": frozenset({"product", "country"}),
        "currency_cross": frozenset({"product", "currency"}),
        "commodity": frozenset({"product", "group"}),
        "bond": frozenset({"product", "country"}),
        "certificate": frozenset({"product", "country"}),
        "crypto": frozenset({"product"}),
    }
    optional_by_product = {
        "stock": frozenset({"n_results"}),
        "etf": frozenset({"n_results"}),
        "fund": frozenset({"n_results"}),
        "index": frozenset({"n_results"}),
        "currency_cross": frozenset({"n_results"}),
        "commodity": frozenset({"n_results"}),
        "bond": frozenset(),
        "certificate": frozenset({"n_results"}),
        "crypto": frozenset({"n_results"}),
    }
    values = _validate_parameters(
        parameters,
        required=required_by_product[product],
        optional=optional_by_product[product],
    )
    values.pop("product")

    for argument_name in ("country", "currency", "group"):
        if argument_name in values:
            values[argument_name] = _require_text(values[argument_name], argument_name)
    if "n_results" in values:
        n_results = values["n_results"]
        allows_all = product == "crypto" and n_results is None
        if not allows_all and (
            isinstance(n_results, bool)
            or not isinstance(n_results, int)
            or not 1 <= n_results <= 1000
        ):
            raise InputError("n_resultsは1以上1000以下の整数にしてください。")

    function = _fixed_function(module, INVESTPY_OVERVIEW_FUNCTIONS[product])
    return function(**values)


def _collect_investpy_economic_calendar(
    module: Any, parameters: Mapping[str, Any]
) -> Any:
    """経済指標カレンダーを収集する。

    機能:
        許可済みの絞り込み条件だけを固定economic_calendar関数へ渡す。
    引数:
        module (Any): investpyモジュール。
        parameters (Mapping[str, Any]): 日時、国、重要度、分類の条件。
    返り値:
        Any: investpyが返した経済指標カレンダー。
    """

    optional = frozenset(
        {
            "time_zone",
            "time_filter",
            "countries",
            "importances",
            "categories",
            "from_date",
            "to_date",
        }
    )
    values = _validate_parameters(parameters, optional=optional)
    for argument_name in ("time_zone", "from_date", "to_date"):
        if argument_name in values:
            values[argument_name] = _require_text(
                values[argument_name], argument_name
            )
    if "time_filter" in values:
        values["time_filter"] = _require_choice(
            values["time_filter"], "time_filter", INVESTPY_TIME_FILTERS
        )
    for argument_name in ("countries", "importances", "categories"):
        if argument_name in values:
            values[argument_name] = _require_text_list(
                values[argument_name], argument_name
            )
    if "importances" in values:
        unknown_importances = sorted(
            set(values["importances"]) - INVESTPY_IMPORTANCES
        )
        if unknown_importances:
            raise InputError(
                "importancesに未対応の値があります: "
                + "、".join(unknown_importances)
            )
    economic_calendar = _fixed_function(module, "economic_calendar")
    return economic_calendar(**values)


def _collect_investpy_technical(
    module: Any, dataset: str, parameters: Mapping[str, Any]
) -> Any:
    """テクニカル分析データを収集する。

    機能:
        datasetとproductの固定許可リストから分析関数を選択して実行する。
    引数:
        module (Any): investpyモジュール。
        dataset (str): テクニカル分析データセット名。
        parameters (Mapping[str, Any]): product、name、country、interval。
    返り値:
        Any: investpyが返したテクニカル分析データ。
    """

    values = _validate_parameters(
        parameters,
        required=frozenset({"product", "name"}),
        optional=frozenset({"country", "interval"}),
    )
    product = _require_product(values.pop("product"), INVESTPY_TECHNICAL_PRODUCTS)
    name = _require_text(values.pop("name"), "name")
    country = values.get("country")
    if product not in INVESTPY_TECHNICAL_COUNTRY_OPTIONAL_PRODUCTS and country is None:
        raise InputError(f"product「{product}」ではcountryが必須です。")
    if country is not None:
        country = _require_text(country, "country")

    keyword_arguments = {
        "name": name,
        "country": country,
        "product_type": INVESTPY_TECHNICAL_PRODUCTS[product],
    }
    if "interval" in values:
        keyword_arguments["interval"] = _require_choice(
            values["interval"], "interval", INVESTPY_TECHNICAL_INTERVALS
        )

    function = _fixed_function(module, INVESTPY_TECHNICAL_FUNCTIONS[dataset])
    return function(**keyword_arguments)


def _dispatch_investpy(
    module: Any, dataset: str, parameters: Mapping[str, Any]
) -> Any:
    """investpyの固定ハンドラーへリクエストを振り分ける。

    機能:
        検証済みdatasetに対応する明示許可済み処理だけを実行する。
    引数:
        module (Any): investpyモジュール。
        dataset (str): 検証済みデータセット名。
        parameters (Mapping[str, Any]): データセット固有パラメーター。
    返り値:
        Any: investpyハンドラーの収集結果。
    """

    if dataset == "search":
        return _collect_investpy_search(module, parameters)
    if dataset in {"recent", "historical", "information"}:
        return _collect_investpy_named(module, dataset, parameters)
    if dataset == "overview":
        return _collect_investpy_overview(module, parameters)
    if dataset == "economic_calendar":
        return _collect_investpy_economic_calendar(module, parameters)
    if dataset in {"technical_indicators", "moving_averages", "pivot_points"}:
        return _collect_investpy_technical(module, dataset, parameters)
    raise CollectorError(f"未対応のinvestpyデータセットです: {dataset}")


# ----------------------------------------
# JSON正規化
# ----------------------------------------


def _normalize_mapping_key(key: Any) -> str:
    """対応表のキーを安定したJSON文字列へ変換する。

    機能:
        標準スカラー、日時、MultiIndex相当のタプルだけを明示形式へ変換する。
    引数:
        key (Any): JSONオブジェクトのキー候補。
    返り値:
        str: 安定形式へ変換したキー。
    """

    if isinstance(key, str):
        return key
    if isinstance(key, (datetime_module.date, datetime_module.time)):
        return key.isoformat()
    if key is None:
        return "null"
    if isinstance(key, bool):
        return "true" if key else "false"
    if isinstance(key, int):
        return str(key)
    if isinstance(key, float):
        if not math.isfinite(key):
            raise CollectorError("収集結果のキーに非有限数が含まれています。")
        return json.dumps(key, allow_nan=False, separators=(",", ":"))
    if isinstance(key, tuple):
        normalized_items = [_normalize_mapping_key(item) for item in key]
        return json.dumps(
            normalized_items,
            ensure_ascii=False,
            allow_nan=False,
            separators=(",", ":"),
        )
    raise CollectorError(
        f"収集結果に未対応のキー型が含まれています: {type(key).__name__}"
    )


def _normalize_mapping(value: Mapping[Any, Any], seen: set[int]) -> dict[str, Any]:
    """対応表をJSONオブジェクトへ正規化する。

    機能:
        キーを安定形式へ変換し、衝突を拒否して各値を標準JSON値へ変換する。
    引数:
        value (Mapping[Any, Any]): 変換する対応表。
        seen (set[int]): 循環参照検出に使う識別子集合。
    返り値:
        dict[str, Any]: JSON化可能な対応表。
    """

    identity = id(value)
    if identity in seen:
        raise CollectorError("収集結果に循環参照が含まれています。")
    seen.add(identity)
    try:
        result: dict[str, Any] = {}
        for key, item in value.items():
            normalized_key = _normalize_mapping_key(key)
            if normalized_key in result:
                raise CollectorError(
                    f"収集結果のキーが正規化後に重複しました: {normalized_key}"
                )
            result[normalized_key] = _normalize_json_value(item, seen)
        return result
    finally:
        seen.remove(identity)


def _normalize_sequence(value: Any, seen: set[int]) -> list[Any]:
    """配列形式の値をJSON配列へ正規化する。

    機能:
        リスト、タプル、集合の各要素を再帰的に標準JSON値へ変換する。
    引数:
        value (Any): 変換する配列形式の値。
        seen (set[int]): 循環参照検出に使う識別子集合。
    返り値:
        list[Any]: JSON化可能な配列。
    """

    identity = id(value)
    if identity in seen:
        raise CollectorError("収集結果に循環参照が含まれています。")
    seen.add(identity)
    try:
        items = value
        if isinstance(value, (set, frozenset)):
            items = sorted(value, key=lambda item: str(item))
        return [_normalize_json_value(item, seen) for item in items]
    finally:
        seen.remove(identity)


def _normalize_object(value: Any, seen: set[int]) -> Any:
    """ライブラリ固有オブジェクトをJSON値へ正規化する。

    機能:
        明示的なas_jsonまたはto_dictを持つproviderオブジェクトだけを変換する。
    引数:
        value (Any): 変換する任意オブジェクト。
        seen (set[int]): 循環参照検出に使う識別子集合。
    返り値:
        Any: JSON化可能な値。
    """

    identity = id(value)
    if identity in seen:
        raise CollectorError("収集結果に循環参照が含まれています。")
    seen.add(identity)
    try:
        as_json = getattr(value, "as_json", None)
        if callable(as_json):
            converted = as_json()
            if isinstance(converted, str):
                try:
                    converted = json.loads(converted)
                except json.JSONDecodeError as error:
                    raise CollectorError(
                        "providerオブジェクトのas_json結果がJSONではありません。"
                    ) from error
            return _normalize_json_value(converted, seen)

        to_dict = getattr(value, "to_dict", None)
        if callable(to_dict):
            return _normalize_json_value(to_dict(), seen)

        raise CollectorError(
            f"収集結果に未対応のオブジェクト型が含まれています: {type(value).__name__}"
        )
    finally:
        seen.remove(identity)


def _normalize_json_value(value: Any, seen: set[int]) -> Any:
    """値を標準JSONで表現可能な形式へ再帰的に変換する。

    機能:
        pandas、NumPy、日時、NaNを含むライブラリ返却値を安全に変換する。
    引数:
        value (Any): 正規化する値。
        seen (set[int]): 循環参照検出に使う識別子集合。
    返り値:
        Any: 標準JSONで表現可能な値。
    """

    if value is None or isinstance(value, (str, bool, int)):
        return value
    if isinstance(value, float):
        return value if math.isfinite(value) else None
    if isinstance(value, Decimal):
        return float(value) if value.is_finite() else None
    if isinstance(value, Enum):
        return _normalize_json_value(value.value, seen)

    value_type = type(value)
    module_name = value_type.__module__
    type_name = value_type.__name__
    if module_name.startswith("pandas") and type_name in {"NAType", "NaTType"}:
        return None

    if isinstance(value, datetime_module.datetime):
        return value.isoformat()
    if isinstance(value, datetime_module.date):
        return value.isoformat()
    if isinstance(value, datetime_module.time):
        return value.isoformat()
    if isinstance(value, datetime_module.timedelta):
        return value.total_seconds()
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    if isinstance(value, Mapping):
        return _normalize_mapping(value, seen)
    as_dict = getattr(value, "_asdict", None)
    if callable(as_dict):
        return _normalize_json_value(as_dict(), seen)
    if isinstance(value, (list, tuple, set, frozenset)):
        return _normalize_sequence(value, seen)

    if module_name.startswith("numpy"):
        item_method = getattr(value, "item", None)
        if callable(item_method):
            try:
                converted = item_method()
                if converted is not value:
                    return _normalize_json_value(converted, seen)
            except (TypeError, ValueError):
                pass
        list_method = getattr(value, "tolist", None)
        if callable(list_method):
            converted = list_method()
            if converted is not value:
                return _normalize_json_value(converted, seen)

    if module_name.startswith("pandas"):
        reset_index = getattr(value, "reset_index", None)
        table_to_dict = getattr(value, "to_dict", None)
        if callable(reset_index) and callable(table_to_dict):
            table = reset_index()
            to_dict = getattr(table, "to_dict", None)
            if callable(to_dict):
                return _normalize_json_value(to_dict(orient="records"), seen)
        if type_name in {"Series", "Index"}:
            to_dict = getattr(value, "to_dict", None)
            if callable(to_dict):
                return _normalize_json_value(to_dict(), seen)
            to_list = getattr(value, "to_list", None)
            if callable(to_list):
                return _normalize_json_value(to_list(), seen)
        to_datetime = getattr(value, "to_pydatetime", None)
        if callable(to_datetime):
            return _normalize_json_value(to_datetime(), seen)
        to_timedelta = getattr(value, "to_pytimedelta", None)
        if callable(to_timedelta):
            return _normalize_json_value(to_timedelta(), seen)
        item_method = getattr(value, "item", None)
        if callable(item_method):
            converted = item_method()
            if converted is not value:
                return _normalize_json_value(converted, seen)

    return _normalize_object(value, seen)


def normalize_json(value: Any) -> Any:
    """収集結果を標準JSON互換値へ正規化する。

    機能:
        pandas、NumPy、日時、NaNを標準jsonモジュールで出力できる値へ変換する。
    引数:
        value (Any): ライブラリから返された収集結果。
    返り値:
        Any: 標準JSON互換の値。
    """

    return _normalize_json_value(value, set())


# ----------------------------------------
# 公開処理と標準入出力
# ----------------------------------------


def collect(
    payload: Any, injected_modules: Mapping[str, Any] | None = None
) -> dict[str, Any]:
    """1件の収集リクエストを実行する。

    機能:
        入力検証、provider呼び出し、JSON正規化、メタデータ付与を一括実行する。
    引数:
        payload (Any): JSONから復元した収集リクエスト。
        injected_modules (Mapping[str, Any] | None): 単体テスト用モジュール対応表。
    返り値:
        dict[str, Any]: dataとmetadataを持つ標準JSON互換レスポンス。
    """

    provider, dataset, parameters = _validate_request(payload)
    module = _load_provider_module(provider, injected_modules)

    if provider == "yfinance":
        data = _dispatch_yfinance(module, dataset, parameters)
    else:
        data = _dispatch_investpy(module, dataset, parameters)

    return {
        "data": normalize_json(data),
        "metadata": {
            "library": PROVIDER_IMPORT_NAMES[provider],
            "library_version": _library_version(provider, module),
            **PROVIDER_SOURCE_METADATA[provider],
        },
    }


def _write_error(kind: str, message: str) -> None:
    """標準出力へ機械判定可能な失敗JSONを1件出力する。

    機能:
        Go側が入力、実行環境、上流障害を安全に分類できる固定形式を生成する。
    引数:
        kind (str): INVALID_ARGUMENT等の共通エラー分類。
        message (str): 利用者へ公開できる日本語メッセージ。
    返り値:
        None: 返り値はない。
    """

    encoded = json.dumps(
        {"error": {"kind": kind, "message": message}},
        ensure_ascii=False,
        allow_nan=False,
        separators=(",", ":"),
    )
    sys.stdout.write(f"{encoded}\n")


def _configure_standard_streams() -> None:
    """標準入出力をOSロケールに依存しないUTF-8へ固定する。

    機能:
        Windowsのcp932を含む既定文字コード差を吸収してGoとのJSON契約を保つ。
    引数:
        なし。
    返り値:
        None: 返り値はない。
    """

    for stream in (sys.stdin, sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if callable(reconfigure):
            reconfigure(encoding="utf-8", errors="strict")


def main() -> int:
    """標準入力から1件を読み、標準出力へ厳密JSONを1件出力する。

    機能:
        ライブラリの付随出力を標準エラーへ退避し、成功時だけJSONを出力する。
    引数:
        なし。
    返り値:
        int: 成功時は0、入力不正は2、実行環境不備は3、上流失敗は4。
    """

    try:
        _configure_standard_streams()
        raw_input = sys.stdin.read()
        if not raw_input.strip():
            raise InputError("標準入力にJSONを1件指定してください。")
        payload = json.loads(raw_input)
        with contextlib.redirect_stdout(sys.stderr):
            response = collect(payload)
        encoded = json.dumps(
            response,
            ensure_ascii=False,
            allow_nan=False,
            separators=(",", ":"),
        )
        sys.stdout.write(f"{encoded}\n")
        return 0
    except json.JSONDecodeError as error:
        _write_error(
            "INVALID_ARGUMENT",
            f"入力JSONを解析できません。位置: {error.pos}、理由: {error.msg}",
        )
        return EXIT_INVALID_ARGUMENT
    except InputError as error:
        _write_error("INVALID_ARGUMENT", str(error))
        return EXIT_INVALID_ARGUMENT
    except CollectorError as error:
        _write_error("PROVIDER_UNAVAILABLE", str(error))
        return EXIT_PROVIDER_UNAVAILABLE
    except Exception as error:
        sys.stderr.write(f"収集処理に失敗しました。詳細: {error}\n")
        _write_error("UPSTREAM_ERROR", "外部providerから情報を収集できません。")
        return EXIT_UPSTREAM_ERROR


if __name__ == "__main__":
    raise SystemExit(main())
