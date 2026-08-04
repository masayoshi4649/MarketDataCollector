package nikkei225jp

import "time"

// ResponseMetadata は、取得したHTTPレスポンスの付帯情報を表します。
//
// 主な用途:
//   - 実際に参照したURLと取得時刻を記録する
//   - 上流が返した検証子とキャッシュ指示を記録する
//   - 取得した本文サイズを確認する
type ResponseMetadata struct {
	SourceURL     string    `json:"source_url"`
	FetchedAt     time.Time `json:"fetched_at"`
	LastModified  string    `json:"last_modified,omitempty"`
	ETag          string    `json:"etag,omitempty"`
	CacheControl  string    `json:"cache_control,omitempty"`
	ResponseBytes int64     `json:"response_bytes"`
}

// CurrentQuote は、サイト内部配信に含まれる1銘柄分の現在値を表します。
//
// 主な特徴:
//   - 空欄になり得る数値はnilで保持する
//   - MarketTimeはHH:mmまたはMM/DDの原文を保持する
//   - DisplayStatusはサイト内部の表示用数値であり、市場状態を保証しない
type CurrentQuote struct {
	Code          string   `json:"code"`
	Name          string   `json:"name,omitempty"`
	Value         *float64 `json:"value"`
	Change        *float64 `json:"change"`
	ChangePercent *float64 `json:"change_percent"`
	MarketTime    string   `json:"market_time"`
	DisplayStatus *int     `json:"display_status"`
	High          *float64 `json:"high"`
	Low           *float64 `json:"low"`
}

// CurrentData は、1回の取得で得た全銘柄の現在値と付帯情報を表します。
//
// 主な特徴:
//   - Quotesは銘柄コードの数値順で格納する
//   - Metadataには取得元とHTTP応答の付帯情報を格納する
type CurrentData struct {
	Metadata ResponseMetadata `json:"metadata"`
	Quotes   []CurrentQuote   `json:"quotes"`
}

// ----------------------------------------

// ChartRange は、取得するサイト上のチャート表示モードを表します。
//
// 主な特徴:
//   - 60mは60分画面で使うティック配信を表す
//   - 6hは6時間画面で使う銘柄別配信を表す
//   - 1dは1日画面で使う複合または単一銘柄配信を表す
//   - 6moは6か月画面で使う銘柄別の日足全履歴配信を表す
//   - 各配信は画面の表示幅より広い点列を含む場合がある
type ChartRange string

const (
	ChartRange60Minutes ChartRange = "60m"
	ChartRange6Hours    ChartRange = "6h"
	ChartRange1Day      ChartRange = "1d"
	ChartRange6Months   ChartRange = "6mo"
)

// ChartPoint は、チャート上の1時点の数値を表します。
//
// 主な特徴:
//   - TimestampMillisはUnixエポックからのミリ秒を保持する
//   - Valueは有限のfloat64だけを保持する
type ChartPoint struct {
	TimestampMillis int64   `json:"timestamp_millis"`
	Value           float64 `json:"value"`
}

// ChartSeries は、1銘柄分のチャート系列を表します。
//
// 主な特徴:
//   - Codeはサイト内部の銘柄コードを保持する
//   - Pointsは時刻の非降順で格納し、配信元の同一時刻点も保持する
type ChartSeries struct {
	Code   string       `json:"code"`
	Name   string       `json:"name,omitempty"`
	Points []ChartPoint `json:"points"`
}

// ChartData は、1回のチャート取得で得た系列と取得元情報を表します。
//
// 主な特徴:
//   - Sourcesは実際に参照したURLごとの付帯情報を保持する
//   - Seriesは銘柄コードの数値順で格納する
type ChartData struct {
	Range   ChartRange         `json:"range"`
	Sources []ResponseMetadata `json:"sources"`
	Series  []ChartSeries      `json:"series"`
}

var instrumentNames = map[string]string{
	"111":  "日本225",
	"112":  "TOPIX",
	"113":  "JPX日経インデックス400",
	"114":  "東証プライム",
	"115":  "東証スタンダード",
	"116":  "東証グロース",
	"121":  "グロース250",
	"136":  "225先物mini",
	"137":  "TOPIX先物",
	"138":  "グロース250先物",
	"141":  "東証REIT指数",
	"142":  "東証規模別指数 大型",
	"143":  "東証規模別指数 中型",
	"144":  "東証規模別指数 小型",
	"151":  "日本国債10年利回り",
	"161":  "日本VI",
	"162":  "NT倍率",
	"163":  "ドル建て225",
	"181":  "東証プライム 出来高",
	"182":  "東証プライム 売買代金",
	"183":  "東証スタンダード 出来高",
	"184":  "東証スタンダード 売買代金",
	"185":  "東証グロース 出来高",
	"186":  "東証グロース 売買代金",
	"191":  "CFD日本225",
	"211":  "NYダウ",
	"212":  "NASDAQ",
	"213":  "S&P500",
	"214":  "NAS100",
	"216":  "FANG+",
	"312":  "オーストラリア",
	"313":  "韓国KOSPI",
	"321":  "上海総合",
	"331":  "香港ハンセン",
	"341":  "シンガポールSTI",
	"342":  "台湾加権",
	"345":  "インドネシアJKSE",
	"346":  "ベトナムVN100",
	"347":  "タイSET",
	"352":  "インドNifty50",
	"411":  "フランスCAC40",
	"412":  "ドイツDAX",
	"413":  "FTSE100",
	"441":  "ロシアRTS",
	"481":  "欧州STOXX50",
	"501":  "ドルインデックス",
	"510":  "円インデックス",
	"511":  "USD/JPY",
	"514":  "EUR/JPY",
	"523":  "EUR/USD",
	"611":  "SOX",
	"621":  "VIX",
	"641":  "eMAXIS Slim全世界株式",
	"643":  "MAXIS全世界株式ETF",
	"644":  "VT",
	"645":  "VTI",
	"731":  "CFD NYダウ",
	"732":  "CFD FTSE100",
	"811":  "米国債10年利回り",
	"831":  "長期国債先物 大取",
	"921":  "WTI原油",
	"931":  "NY金",
	"1001": "ビットコイン",
}

// ----------------------------------------

// InstrumentName は、確認済みの銘柄コードに対応する表示名を返します。
//
// 引数:
//   - code string: サイト内部の銘柄コード。
//
// 返り値:
//   - string: 確認済みの表示名。未確認コードの場合は空文字。
func InstrumentName(code string) string {
	return instrumentNames[code]
}
