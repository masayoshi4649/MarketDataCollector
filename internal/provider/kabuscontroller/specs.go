// Package kabuscontroller は、KabusControllerの読取専用APIを共通収集サービスへ接続します。
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

type endpointSpec struct {
	Dataset        string
	Description    string
	Path           string
	RequiresSymbol bool
}

var endpointSpecs = []endpointSpec{
	{
		Dataset:     "future_registrations",
		Description: "KabusControllerへ登録されている先物一覧を取得します。",
		Path:        "/api/trade/registrations/future",
	},
	{
		Dataset:     "option_registrations",
		Description: "KabusControllerへ登録されているオプション一覧を取得します。",
		Path:        "/api/trade/registrations/option",
	},
	{
		Dataset:     "market_data",
		Description: "登録中の先物・オプションすべての板情報を取得します。",
		Path:        "/api/trade/market-data",
	},
	{
		Dataset:     "future_market_data",
		Description: "登録中の先物だけの板情報を取得します。",
		Path:        "/api/trade/market-data/future",
	},
	{
		Dataset:     "option_market_data",
		Description: "登録中のオプションだけの板情報を取得します。",
		Path:        "/api/trade/market-data/option",
	},
	{
		Dataset:        "symbol_market_data",
		Description:    "指定した先物またはオプション1銘柄の板情報を取得します。",
		Path:           "/api/trade/market-data/:symbol",
		RequiresSymbol: true,
	},
}

// ----------------------------------------

/*
datasetDescriptor は、固定endpoint仕様をdatalist用の共通形式へ変換します。

機能:
  - 固定パスの説明をdatasetへ設定する
  - 個別銘柄datasetだけに必須のsymbol入力を公開する

引数:
  - spec endpointSpec: 変換する固定endpoint仕様

返り値:
  - domain.DatasetDescriptor: RESTとMCPのdatalistへ掲載するdataset仕様
*/
func datasetDescriptor(spec endpointSpec) domain.DatasetDescriptor {
	parameters := []domain.ParameterDescriptor{}
	if spec.RequiresSymbol {
		parameters = append(parameters, domain.ParameterDescriptor{
			Name:        "symbol",
			Type:        "string",
			Required:    true,
			Description: "板情報を取得する先物またはオプションの銘柄コードです。英数字、ピリオド、アンダースコア、ハイフンを使用できます。",
		})
	}
	return domain.DatasetDescriptor{
		Name:        spec.Dataset,
		Description: spec.Description,
		Parameters:  parameters,
	}
}
