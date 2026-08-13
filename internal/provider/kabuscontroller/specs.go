// Package kabuscontroller は、KabusControllerとkabuステーション情報APIを共通収集サービスへ接続します。
package kabuscontroller

import "github.com/masayoshi4649/MarketDataCollector/internal/domain"

const (
	// DefaultBaseURL は、KabusController APIの既定オリジンです。
	DefaultBaseURL = "http://10.10.100.1:8080"
	// DefaultUserAgent は、上流へ送信する既定のUser-Agentです。
	DefaultUserAgent = "MarketDataCollector/0.1"
	// DefaultMaxResponseBytes は、JSON応答本文の既定上限です。
	DefaultMaxResponseBytes int64 = 16 * 1024 * 1024
)

type parameterType string

const (
	typeString  parameterType = "string"
	typeInteger parameterType = "integer"
	typeBoolean parameterType = "boolean"
)

type parameterValidator string

const (
	validatorNone             parameterValidator = "none"
	validatorControllerSymbol parameterValidator = "controller_symbol"
	validatorSecuritySymbol   parameterValidator = "security_symbol"
)

type endpointKind string

const (
	kindFixed            endpointKind = "fixed"
	kindControllerSymbol endpointKind = "controller_symbol"
	kindSymbolExchange   endpointKind = "symbol_exchange"
	kindPlainSymbol      endpointKind = "plain_symbol"
	kindPair             endpointKind = "pair"
	kindResolver         endpointKind = "resolver"
	kindComposite        endpointKind = "composite"
)

type parameterSpec struct {
	Name        string
	Type        parameterType
	Required    bool
	Description string
	Allowed     []string
	Default     any
	Minimum     *float64
	Maximum     *float64
	Validator   parameterValidator
	QueryName   string
}

type endpointSpec struct {
	Dataset           string
	Description       string
	Path              string
	Kind              endpointKind
	Parameters        []parameterSpec
	NotFound          bool
	MayRegisterSymbol bool
	StandardInfo      bool
	BidAskReversed    bool
}

var endpointSpecs = []endpointSpec{
	{
		Dataset:     "future_registrations",
		Description: "KabusControllerへ登録されている先物一覧を取得します。",
		Path:        "/api/trade/registrations/future",
		Kind:        kindFixed,
	},
	{
		Dataset:     "option_registrations",
		Description: "KabusControllerへ登録されているオプション一覧を取得します。",
		Path:        "/api/trade/registrations/option",
		Kind:        kindFixed,
	},
	{
		Dataset:        "market_data",
		Description:    "登録中の先物・オプションすべての板情報を取得します。",
		Path:           "/api/trade/market-data",
		Kind:           kindFixed,
		BidAskReversed: true,
	},
	{
		Dataset:        "future_market_data",
		Description:    "登録中の先物だけの板情報を取得します。",
		Path:           "/api/trade/market-data/future",
		Kind:           kindFixed,
		BidAskReversed: true,
	},
	{
		Dataset:        "option_market_data",
		Description:    "登録中のオプションだけの板情報を取得します。",
		Path:           "/api/trade/market-data/option",
		Kind:           kindFixed,
		BidAskReversed: true,
	},
	{
		Dataset:        "symbol_market_data",
		Description:    "指定した先物またはオプション1銘柄の板情報を取得します。",
		Path:           "/api/trade/market-data/:symbol",
		Kind:           kindControllerSymbol,
		NotFound:       true,
		BidAskReversed: true,
		Parameters: []parameterSpec{
			{
				Name:        "symbol",
				Type:        typeString,
				Required:    true,
				Description: "KabusControllerへ登録済みの先物またはオプションの銘柄コードです。",
				Validator:   validatorControllerSymbol,
			},
		},
	},
	{
		Dataset:      "kabus_ranking",
		Description:  "kabuステーションが保持する当日の詳細ランキングを取得します。平日7:53頃から9:00過ぎ頃は株価・業種ランキングが空になる場合があり、信用ランキングは毎週第3営業日7:55頃に更新されます。",
		Path:         "/kabusapi/ranking",
		Kind:         kindFixed,
		StandardInfo: true,
		Parameters: []parameterSpec{
			{
				Name:        "ranking_type",
				Type:        typeString,
				Required:    true,
				Description: "ランキング種別です。1は値上がり率、2は値下がり率、3は売買高、4は売買代金、5はTICK回数、6は売買高急増、7は売買代金急増、8～13は信用情報、14～15は業種別騰落率です。",
				Allowed:     []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"},
				Validator:   validatorNone,
				QueryName:   "Type",
			},
			{
				Name:        "exchange_division",
				Type:        typeString,
				Description: "対象市場です。業種別ランキングでは指定しても上流で無視されます。",
				Allowed:     []string{"ALL", "T", "TP", "TS", "TG", "M", "FK", "S"},
				Default:     "ALL",
				Validator:   validatorNone,
				QueryName:   "ExchangeDivision",
			},
		},
	},
	{
		Dataset:           "kabus_regulations",
		Description:       "指定した国内株式の取引規制と空売り規制を取得します。取得により上流のAPI登録銘柄リストへ自動登録される場合があります。",
		Path:              "/kabusapi/regulations/:symbol",
		Kind:              kindSymbolExchange,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		Parameters: []parameterSpec{
			securitySymbolParameter("規制情報を取得する国内株式の銘柄コードです。"),
			stockExchangeParameter(),
		},
	},
	{
		Dataset:           "derivative_symbol_resolver",
		Description:       "商品、限月、コール・プット、権利行使価格または限週から、先物・オプションの銘柄コードを解決します。kindに応じた条件付き入力を検証します。取得により解決銘柄が上流のAPI登録銘柄リストへ自動登録される場合があります。",
		Path:              "/kabusapi/symbolname",
		Kind:              kindResolver,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		Parameters: []parameterSpec{
			{
				Name:        "kind",
				Type:        typeString,
				Required:    true,
				Description: "解決するデリバティブの種類です。",
				Allowed:     []string{"future", "option", "mini_option_weekly"},
				Validator:   validatorNone,
			},
			{
				Name:        "product_code",
				Type:        typeString,
				Description: "futureまたはoptionで指定する商品コードです。mini_option_weeklyでは指定しません。",
				Allowed: []string{
					"NK225", "NK225mini", "TOPIX", "TOPIXmini", "GROWTH", "JPX400",
					"DOW", "VI", "Core30", "REIT", "NK225micro", "NK225op", "NK225miniop",
				},
				Validator: validatorNone,
			},
			{
				Name:        "deriv_month",
				Type:        typeInteger,
				Required:    true,
				Description: "限月です。0は上流が選ぶ直近限月、明示指定はYYYYMM形式です。",
				Minimum:     numberPointer(0),
				Maximum:     numberPointer(999999),
				Validator:   validatorNone,
				QueryName:   "DerivMonth",
			},
			{
				Name:        "put_or_call",
				Type:        typeString,
				Description: "optionまたはmini_option_weeklyで必須となるプット・コール区分です。",
				Allowed:     []string{"P", "C"},
				Validator:   validatorNone,
				QueryName:   "PutOrCall",
			},
			{
				Name:        "strike_price",
				Type:        typeInteger,
				Description: "optionまたはmini_option_weeklyで必須となる権利行使価格です。0は解決時点のATMを表します。",
				Minimum:     numberPointer(0),
				Maximum:     numberPointer(2147483647),
				Validator:   validatorNone,
				QueryName:   "StrikePrice",
			},
			{
				Name:        "deriv_weekly",
				Type:        typeInteger,
				Description: "mini_option_weeklyで必須となる限週です。0は指定限月の直近限週です。",
				Allowed:     []string{"0", "1", "3", "4", "5"},
				Validator:   validatorNone,
				QueryName:   "DerivWeekly",
			},
		},
	},
	{
		Dataset:           "nt_pair_symbol_resolver",
		Description:       "明示した同一限月についてミニTOPIXと日経225miniまたはmicroの銘柄コードを2要求で解決します。直近限月を意味する0は受け付けません。各要求は解決銘柄をAPI登録し得て、2脚目失敗時にも1脚目の登録は残る場合があります。",
		Kind:              kindComposite,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		Parameters: []parameterSpec{
			{
				Name:        "deriv_month",
				Type:        typeInteger,
				Required:    true,
				Description: "両脚へ共通指定するYYYYMM形式の限月です。0は指定できません。",
				Minimum:     numberPointer(200001),
				Maximum:     numberPointer(299912),
				Validator:   validatorNone,
				QueryName:   "DerivMonth",
			},
			{
				Name:        "nikkei_product_code",
				Type:        typeString,
				Description: "日経側の先物商品コードです。",
				Allowed:     []string{"NK225mini", "NK225micro"},
				Default:     "NK225mini",
				Validator:   validatorNone,
				QueryName:   "FutureCode",
			},
		},
	},
	{
		Dataset:           "arbitrary_board_snapshot",
		Description:       "任意の現物・先物・オプション1銘柄の時価・板情報を取得します。取得により上流のAPI登録銘柄リストへ自動登録される場合があります。売買方向はBuy1とSell1を正として解釈します。",
		Path:              "/kabusapi/board/:symbol",
		Kind:              kindSymbolExchange,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		BidAskReversed:    true,
		Parameters: []parameterSpec{
			securitySymbolParameter("板情報を取得する現物・先物・オプションの銘柄コードです。"),
			allExchangeParameter(),
		},
	},
	{
		Dataset:        "option_chain_snapshot",
		Description:    "KabusControllerへ登録済みのオプション一覧と板情報を結合し、指定限月・中心権利行使価格の周辺をコール・プット別に返します。未登録銘柄の自動登録や建玉の推測は行いません。",
		Kind:           kindComposite,
		NotFound:       true,
		BidAskReversed: true,
		Parameters: []parameterSpec{
			{
				Name:        "option_code",
				Type:        typeString,
				Required:    true,
				Description: "オプションの商品コードです。",
				Allowed:     []string{"NK225op", "NK225miniop"},
				Validator:   validatorNone,
			},
			{
				Name:        "deriv_month",
				Type:        typeInteger,
				Required:    true,
				Description: "抽出するYYYYMM形式の限月です。",
				Minimum:     numberPointer(200001),
				Maximum:     numberPointer(299912),
				Validator:   validatorNone,
			},
			{
				Name:        "center_strike",
				Type:        typeInteger,
				Required:    true,
				Description: "チェーン中央とする権利行使価格を明示します。実際のATMは自動判定しません。",
				Minimum:     numberPointer(1),
				Maximum:     numberPointer(2147483647),
				Validator:   validatorNone,
			},
			{
				Name:        "strikes_each_side",
				Type:        typeInteger,
				Description: "中心価格の上下それぞれに含める権利行使価格の本数です。",
				Default:     5,
				Minimum:     numberPointer(0),
				Maximum:     numberPointer(20),
				Validator:   validatorNone,
			},
		},
	},
	{
		Dataset:           "kabus_symbol_info",
		Description:       "指定した銘柄の基本情報を取得します。追加情報には時価総額、発行済み株式数、決算期日、清算値が含まれます。取得により上流のAPI登録銘柄リストへ自動登録される場合があります。",
		Path:              "/kabusapi/symbol/:symbol",
		Kind:              kindSymbolExchange,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		Parameters: []parameterSpec{
			securitySymbolParameter("銘柄情報を取得する現物・先物・オプションの銘柄コードです。"),
			allExchangeParameter(),
			{
				Name:        "add_info",
				Type:        typeBoolean,
				Description: "時価総額、発行済み株式数、決算期日、清算値を追加する場合はtrueです。",
				Default:     true,
				Validator:   validatorNone,
				QueryName:   "addinfo",
			},
		},
	},
	{
		Dataset:           "kabus_primary_exchange",
		Description:       "国内株式の優先市場を取得します。取得により上流のAPI登録銘柄リストへ自動登録される場合があります。",
		Path:              "/kabusapi/primaryexchange/:symbol",
		Kind:              kindPlainSymbol,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		Parameters: []parameterSpec{
			securitySymbolParameter("優先市場を取得する国内株式の銘柄コードです。"),
		},
	},
	{
		Dataset:      "kabus_fx_snapshot",
		Description:  "指定した通貨ペアのBid、Ask、スプレッド、前日比、時刻を取得します。時刻には日付が含まれません。",
		Path:         "/kabusapi/exchange/:pair",
		Kind:         kindPair,
		NotFound:     true,
		StandardInfo: true,
		Parameters: []parameterSpec{
			{
				Name:        "pair",
				Type:        typeString,
				Description: "取得する通貨ペアです。",
				Allowed: []string{
					"usdjpy", "eurjpy", "gbpjpy", "audjpy", "chfjpy", "cadjpy",
					"nzdjpy", "zarjpy", "eurusd", "gbpusd", "audusd",
				},
				Default:   "usdjpy",
				Validator: validatorNone,
			},
		},
	},
	{
		Dataset:           "kabus_margin_premium",
		Description:       "指定した国内株式の一般信用・デイトレ信用のプレミアム料を取得します。取得により上流のAPI登録銘柄リストへ自動登録される場合があります。",
		Path:              "/kabusapi/margin/marginpremium/:symbol",
		Kind:              kindPlainSymbol,
		NotFound:          true,
		MayRegisterSymbol: true,
		StandardInfo:      true,
		Parameters: []parameterSpec{
			securitySymbolParameter("プレミアム料を取得する国内株式の銘柄コードです。"),
		},
	},
	{
		Dataset:      "kabus_api_soft_limits",
		Description:  "kabuステーションの現物・信用・先物・オプションにおける1注文当たりの上限とバージョンを取得します。API登録銘柄数や登録残枠ではありません。",
		Path:         "/kabusapi/apisoftlimit",
		Kind:         kindFixed,
		StandardInfo: true,
	},
	{
		Dataset:     "kabus_api_capacity",
		Description: "kabuステーションの1注文当たりの上限と、KabusControllerが把握する先物・オプション登録件数からAPI登録残枠の上限を返します。株式、PUSH、他クライアントの登録は把握できないため正確な残枠ではありません。",
		Kind:        kindComposite,
	},
}

// ----------------------------------------

/*
securitySymbolParameter は、kabuステーション標準情報API用の銘柄コード入力を生成します。

機能:
  - pathへ組み込む銘柄コードの共通型とvalidatorを定義する

引数:
  - description string: datalistへ掲載する用途別の説明

返り値:
  - parameterSpec: 必須のsecurity_symbol入力仕様
*/
func securitySymbolParameter(description string) parameterSpec {
	return parameterSpec{
		Name:        "symbol",
		Type:        typeString,
		Required:    true,
		Description: description,
		Validator:   validatorSecuritySymbol,
	}
}

// ----------------------------------------

/*
stockExchangeParameter は、国内株式市場だけを許可する市場入力を生成します。

機能:
  - 東証、名証、福証、札証の市場コードを公開する

引数:
  - なし

返り値:
  - parameterSpec: 東証を既定値とする国内株式市場入力仕様
*/
func stockExchangeParameter() parameterSpec {
	return parameterSpec{
		Name:        "exchange",
		Type:        typeString,
		Description: "市場コードです。1は東証、3は名証、5は福証、6は札証で、既定値は1です。",
		Allowed:     []string{"1", "3", "5", "6"},
		Default:     "1",
		Validator:   validatorNone,
	}
}

// ----------------------------------------

/*
allExchangeParameter は、現物とデリバティブで利用できる市場入力を生成します。

機能:
  - 国内株式4市場と日通し、日中、夜間の市場コードを公開する

引数:
  - なし

返り値:
  - parameterSpec: 必須の市場入力仕様
*/
func allExchangeParameter() parameterSpec {
	return parameterSpec{
		Name:        "exchange",
		Type:        typeString,
		Required:    true,
		Description: "市場コードです。株式は1、3、5、6、デリバティブは2、23、24を指定します。",
		Allowed:     []string{"1", "3", "5", "6", "2", "23", "24"},
		Validator:   validatorNone,
	}
}

// ----------------------------------------

/*
numberPointer は、数値parameterの境界値をポインターへ変換します。

機能:
  - 未指定境界のnilと値が0の境界を区別できるようにする

引数:
  - value float64: 保持する境界値

返り値:
  - *float64: 渡された値を保持する新しいポインター
*/
func numberPointer(value float64) *float64 {
	return &value
}

// ----------------------------------------

/*
datasetDescriptor は、endpoint仕様をdatalist用の共通形式へ変換します。

機能:
  - datasetの説明と全公開入力を共通Descriptorへ設定する
  - Allowedを複製して固定endpoint仕様とのスライス共有を防ぐ

引数:
  - spec endpointSpec: 変換するendpoint仕様

返り値:
  - domain.DatasetDescriptor: RESTとMCPのdatalistへ掲載するdataset仕様
*/
func datasetDescriptor(spec endpointSpec) domain.DatasetDescriptor {
	parameters := make([]domain.ParameterDescriptor, 0, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		allowed := append([]string(nil), parameter.Allowed...)
		parameters = append(parameters, domain.ParameterDescriptor{
			Name:        parameter.Name,
			Type:        string(parameter.Type),
			Required:    parameter.Required,
			Description: parameter.Description,
			Allowed:     allowed,
			Default:     parameter.Default,
			Minimum:     cloneNumberPointer(parameter.Minimum),
			Maximum:     cloneNumberPointer(parameter.Maximum),
		})
	}
	return domain.DatasetDescriptor{
		Name:        spec.Dataset,
		Description: spec.Description,
		Parameters:  parameters,
	}
}

// ----------------------------------------

// cloneNumberPointer は、数値境界ポインターをDescriptor用に複製します。
//
// 主な特徴:
//   - nilを維持し、設定値は新しいポインターへ複製する
//   - 公開Descriptor変更が内部endpoint仕様へ伝播することを防ぐ
//
// 引数:
//   - source *float64: 複製する数値境界
//
// 返り値:
//   - *float64: 独立した数値境界またはnil
func cloneNumberPointer(source *float64) *float64 {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}
