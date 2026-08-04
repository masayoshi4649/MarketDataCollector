package nikkei225jp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const maxTableRows = 10000

var japanIndustryNames = []string{
	"水産・農林業",
	"鉱業",
	"建設業",
	"食料品",
	"繊維製品",
	"パルプ・紙",
	"化学",
	"医薬品",
	"石油・石炭製品",
	"ゴム製品",
	"ガラス・土石製品",
	"鉄鋼",
	"非鉄金属",
	"金属製品",
	"機械",
	"電気機器",
	"輸送用機器",
	"精密機器",
	"その他製品",
	"電気・ガス業",
	"陸運業",
	"海運業",
	"空運業",
	"倉庫・運輸関連業",
	"情報・通信業",
	"卸売業",
	"小売業",
	"銀行業",
	"証券・商品先物取引業",
	"保険業",
	"その他金融業",
	"不動産業",
	"サービス業",
}

var japanIndustryCodes = []string{
	"0050", "1050", "2050", "3050", "3100", "3150", "3200", "3250",
	"3300", "3350", "3400", "3450", "3500", "3550", "3600", "3650",
	"3700", "3750", "3800", "4050", "5050", "5100", "5150", "5200",
	"5250", "6050", "6100", "7050", "7100", "7150", "7200", "8050",
	"9050",
}

// JapanComponent は、日経225構成銘柄1件の正規化済み数値を表します。
//
// 主な特徴:
//   - PriceとDeemedPriceを区別して保持する
//   - WeightPercentとContributionはサイト配信値を数値化して保持する
//   - IndustryCodeはサイト内部の業種番号を文字列のまま保持する
type JapanComponent struct {
	Code            string  `json:"code"`
	IndustryCode    string  `json:"industry_code"`
	Name            string  `json:"name"`
	EnglishName     string  `json:"english_name,omitempty"`
	Price           float64 `json:"price"`
	DeemedPrice     float64 `json:"deemed_price"`
	WeightPercent   float64 `json:"weight_percent"`
	ChangePercent   float64 `json:"change_percent"`
	Change          float64 `json:"change"`
	ContributionYen float64 `json:"contribution_yen"`
}

// JapanComponentData は、日経225構成銘柄一覧と集計値を表します。
//
// 主な特徴:
//   - Componentsは配信配列の順番を保持する
//   - UpCount、DownCount、UnchangedCountは配信元の集計値を保持する
//   - DivisorとNikkeiValueは有限数値だけを保持する
type JapanComponentData struct {
	UpdatedAt      string           `json:"updated_at"`
	UpCount        int              `json:"up_count"`
	DownCount      int              `json:"down_count"`
	UnchangedCount int              `json:"unchanged_count"`
	TotalDeemed    float64          `json:"total_deemed"`
	Divisor        float64          `json:"divisor"`
	NikkeiValue    float64          `json:"nikkei_value"`
	Components     []JapanComponent `json:"components"`
}

// JapanContribution は、日経225への寄与度上位または下位の1件を表します。
//
// 主な特徴:
//   - Directionはtopまたはbottomを保持する
//   - Rankは配信配列の添字を1始まりへ変換する
//   - ContributionYenは日経平均を動かした金額を表す
type JapanContribution struct {
	Direction       string  `json:"direction"`
	Rank            int     `json:"rank"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	ContributionYen float64 `json:"contribution_yen"`
	Price           float64 `json:"price"`
	Change          float64 `json:"change"`
	ChangePercent   float64 `json:"change_percent"`
}

// JapanContributionData は、寄与度上位・下位一覧と更新時刻を表します。
//
// 主な特徴:
//   - TopとBottomを別々の配列で保持する
//   - 各配列は配信順位の順番を保持する
type JapanContributionData struct {
	UpdatedAt string              `json:"updated_at"`
	Top       []JapanContribution `json:"top"`
	Bottom    []JapanContribution `json:"bottom"`
}

// JapanIndustry は、東証33業種の1業種分の数値を表します。
//
// 主な特徴:
//   - Positionは配信上の順番を1始まりで保持する
//   - CodeとNameは東証33業種の固定順へ対応付ける
type JapanIndustry struct {
	Position int     `json:"position"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Change   float64 `json:"change"`
}

// JapanIndustryData は、東証33業種一覧と更新日時を表します。
//
// 主な特徴:
//   - UpdatedDateとUpdatedTimeを分離して保持する
//   - Industriesは東証の固定順を保持する
type JapanIndustryData struct {
	UpdatedDate string          `json:"updated_date"`
	UpdatedTime string          `json:"updated_time"`
	Industries  []JapanIndustry `json:"industries"`
}

// MarketRanking は、日本株または米国株ランキングの1件を表します。
//
// 主な特徴:
//   - Kindはgainers、losers、activeのいずれかを保持する
//   - 過去順位がハイフンの場合はnilを保持する
//   - ExchangeとVolumeは市場ごとに存在する側だけを設定する
type MarketRanking struct {
	Market         string   `json:"market"`
	Kind           string   `json:"kind"`
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Rank           int      `json:"rank"`
	PreviousRank   *int     `json:"previous_rank,omitempty"`
	TwoDaysAgoRank *int     `json:"two_days_ago_rank,omitempty"`
	Value          float64  `json:"value"`
	ChangePercent  float64  `json:"change_percent"`
	Change         float64  `json:"change"`
	Exchange       string   `json:"exchange,omitempty"`
	Volume         *float64 `json:"volume,omitempty"`
	MarketTime     string   `json:"market_time"`
}

// MarketRankingData は、3種類のランキングと更新時刻を表します。
//
// 主な特徴:
//   - Gainers、Losers、Activeを配信配列ごとに分離する
//   - Marketはjapanまたはusを保持する
type MarketRankingData struct {
	Market    string          `json:"market"`
	UpdatedAt string          `json:"updated_at"`
	Gainers   []MarketRanking `json:"gainers"`
	Losers    []MarketRanking `json:"losers"`
	Active    []MarketRanking `json:"active"`
}

// USEquity は、米国主要銘柄配信の1行を表します。
//
// 主な特徴:
//   - Universeはfang_plus、dow30、nasdaq100、otherのいずれかを保持する
//   - 同一SymbolでもUniverseが異なる行は別行として保持する
//   - ContributionはDOW30かつDivisorが得られた場合だけ設定する
type USEquity struct {
	Universe        string   `json:"universe"`
	Symbol          string   `json:"symbol"`
	Name            string   `json:"name"`
	EnglishName     string   `json:"english_name,omitempty"`
	IndustryCode    int      `json:"industry_code"`
	Price           float64  `json:"price"`
	Change          float64  `json:"change"`
	ChangePercent   float64  `json:"change_percent"`
	Volume          *float64 `json:"volume,omitempty"`
	WeightPercent   float64  `json:"weight_percent"`
	DowContribution *float64 `json:"dow_contribution,omitempty"`
}

// USEquityData は、米国主要銘柄一覧と取引セッション情報を表します。
//
// 主な特徴:
//   - Sessionはregular、pre、afterのいずれかを保持する
//   - Divisorは通常市場の配信に存在する場合だけ設定する
//   - Equitiesは意図的な指数間重複を保持する
type USEquityData struct {
	Session   string     `json:"session"`
	UpdatedAt string     `json:"updated_at"`
	Market    string     `json:"market,omitempty"`
	Divisor   *float64   `json:"divisor,omitempty"`
	Equities  []USEquity `json:"equities"`
}

// USIndustry は、米国業種指数配信の1件を表します。
//
// 主な特徴:
//   - GroupはGYO1またはGYO2を保持する
//   - Codeはサイト内部の業種指数コードを保持する
type USIndustry struct {
	Group         string  `json:"group"`
	Position      int     `json:"position"`
	Code          string  `json:"code"`
	Value         float64 `json:"value"`
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"change_percent"`
	MarketTime    string  `json:"market_time"`
}

// USIndustryData は、米国業種指数一覧と更新時刻を表します。
//
// 主な特徴:
//   - IndustriesはGYO1、GYO2の順番で格納する
//   - UpdatedAtは配信末尾の更新時刻を保持する
type USIndustryData struct {
	UpdatedAt  string       `json:"updated_at"`
	Industries []USIndustry `json:"industries"`
}

// ADRQuote は、日本株1銘柄のADR、PTS、東証価格を表します。
//
// 主な特徴:
//   - 配信上空欄になり得る数値はnilで保持する
//   - ADRVsTokyoPercentとPTSVsTokyoPercentは必要な元値が揃う場合だけ算出する
//   - MainはShu変数に含まれる主要銘柄かを表す
type ADRQuote struct {
	Code               string   `json:"code"`
	ADRSymbol          string   `json:"adr_symbol"`
	Name               string   `json:"name"`
	IndustryCode       string   `json:"industry_code"`
	EnglishName        string   `json:"english_name,omitempty"`
	Market             string   `json:"market"`
	ConversionRatio    float64  `json:"conversion_ratio"`
	TokyoDate          string   `json:"tokyo_date"`
	TokyoPrice         *float64 `json:"tokyo_price,omitempty"`
	TokyoChange        *float64 `json:"tokyo_change,omitempty"`
	TokyoChangePercent *float64 `json:"tokyo_change_percent,omitempty"`
	TokyoStatus        string   `json:"tokyo_status"`
	ADRMarketTime      string   `json:"adr_market_time"`
	ADRPrice           *float64 `json:"adr_price,omitempty"`
	ADRChange          *float64 `json:"adr_change,omitempty"`
	ADRChangePercent   *float64 `json:"adr_change_percent,omitempty"`
	ADRVolume          *float64 `json:"adr_volume,omitempty"`
	ADRYen             *float64 `json:"adr_yen,omitempty"`
	USDJPY             *float64 `json:"usd_jpy,omitempty"`
	PTSMarketTime      string   `json:"pts_market_time"`
	PTSPrice           *float64 `json:"pts_price,omitempty"`
	PTSVolume          *float64 `json:"pts_volume,omitempty"`
	Sponsorship        string   `json:"sponsorship,omitempty"`
	DisplayFlag        string   `json:"display_flag,omitempty"`
	ADRVsTokyoPercent  *float64 `json:"adr_vs_tokyo_percent,omitempty"`
	PTSVsTokyoPercent  *float64 `json:"pts_vs_tokyo_percent,omitempty"`
	Main               bool     `json:"main"`
}

// ADRData は、ADR一覧と主要銘柄コードを表します。
//
// 主な特徴:
//   - MainCodesはShu変数の順番を保持する
//   - QuotesはA0配列の順番を保持する
type ADRData struct {
	MainCodes []string   `json:"main_codes"`
	Quotes    []ADRQuote `json:"quotes"`
}

// ----------------------------------------

type tableScriptParser struct {
	body     []byte
	position int
}

type indexedTableValues struct {
	values map[int]string
	max    int
}

// newIndexedTableValues は、添字重複を検証する一時格納領域を生成します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - *indexedTableValues: 空の添字付き文字列格納領域。
func newIndexedTableValues() *indexedTableValues {
	return &indexedTableValues{values: make(map[int]string), max: -1}
}

// add は、添字付き文字列を重複検証して追加します。
//
// 引数:
//   - index int: 配信配列の添字。
//   - value string: 添字に対応する行文字列。
//
// 返り値:
//   - error: 添字が範囲外または重複する場合のエラー。
func (v *indexedTableValues) add(index int, value string) error {
	if index < 0 || index >= maxTableRows {
		return fmt.Errorf("配列添字が範囲外です: %d", index)
	}
	if _, exists := v.values[index]; exists {
		return fmt.Errorf("配列添字%dが重複しています", index)
	}
	v.values[index] = value
	if index > v.max {
		v.max = index
	}
	return nil
}

// ordered は、添字の欠落を検証して順番どおりの文字列を返します。
//
// 引数:
//   - なし。格納済みの添字と文字列を利用する。
//
// 返り値:
//   - []string: 0から始まる添字順の文字列。
//   - error: 先頭または途中の添字が欠落する場合のエラー。
func (v *indexedTableValues) ordered() ([]string, error) {
	if v.max < 0 {
		return []string{}, nil
	}
	result := make([]string, v.max+1)
	for index := 0; index <= v.max; index++ {
		value, exists := v.values[index]
		if !exists {
			return nil, fmt.Errorf("配列添字%dが欠落しています", index)
		}
		result[index] = value
	}
	return result, nil
}

// newTableScriptParser は、厳密な配信JavaScriptパーサーを生成します。
//
// 引数:
//   - body []byte: 解析対象のJavaScript本文。
//
// 返り値:
//   - *tableScriptParser: 本文先頭を指すパーサー。
func newTableScriptParser(body []byte) *tableScriptParser {
	return &tableScriptParser{body: body}
}

// expectKeyword は、識別子境界を含めて指定キーワードを消費します。
//
// 引数:
//   - keyword string: 現在位置に必要なキーワード。
//
// 返り値:
//   - error: キーワードが存在しない場合のエラー。
func (p *tableScriptParser) expectKeyword(keyword string) error {
	p.skipWhitespace()
	if !bytes.HasPrefix(p.body[p.position:], []byte(keyword)) {
		return p.errorf("%sが必要です", keyword)
	}
	end := p.position + len(keyword)
	if end < len(p.body) && isTableIdentifierPart(p.body[end]) {
		return p.errorf("%sの識別子境界が不正です", keyword)
	}
	p.position = end
	return nil
}

// parseIdentifier は、ASCIIのJavaScript識別子を解析します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - string: 解析した識別子。
//   - error: 識別子が存在しない場合のエラー。
func (p *tableScriptParser) parseIdentifier() (string, error) {
	p.skipWhitespace()
	start := p.position
	if !isTableIdentifierStart(p.peekByte()) {
		return "", p.errorf("識別子が必要です")
	}
	p.position++
	for isTableIdentifierPart(p.peekByte()) {
		p.position++
	}
	return string(p.body[start:p.position]), nil
}

// parseString は、JSON互換の二重引用符付き文字列を解析します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - string: エスケープを復元したUTF-8文字列。
//   - error: 文字列構文またはUTF-8が不正な場合のエラー。
func (p *tableScriptParser) parseString() (string, error) {
	p.skipWhitespace()
	if p.peekByte() != '"' {
		return "", p.errorf("二重引用符付き文字列が必要です")
	}
	start := p.position
	p.position++
	escaped := false
	for !p.atEnd() {
		current := p.body[p.position]
		p.position++
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' {
			escaped = true
			continue
		}
		if current == '"' {
			var result string
			if err := json.Unmarshal(p.body[start:p.position], &result); err != nil {
				return "", p.errorf("文字列を解析できません")
			}
			return result, nil
		}
	}
	return "", p.errorf("文字列が閉じていません")
}

// parseSingleQuotedString は、単純な単一引用符付き文字列を解析します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - string: 引用符内の文字列。
//   - error: 文字列が閉じていない場合またはエスケープを含む場合のエラー。
func (p *tableScriptParser) parseSingleQuotedString() (string, error) {
	p.skipWhitespace()
	if p.peekByte() != '\'' {
		return "", p.errorf("単一引用符付き文字列が必要です")
	}
	p.position++
	start := p.position
	for !p.atEnd() {
		current := p.body[p.position]
		if current == '\\' {
			return "", p.errorf("単一引用符文字列のエスケープは未対応です")
		}
		if current == '\'' {
			result := string(p.body[start:p.position])
			p.position++
			return result, nil
		}
		p.position++
	}
	return "", p.errorf("単一引用符文字列が閉じていません")
}

// parseNumberToken は、符号・小数・指数を含む数値トークンを解析します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - string: 検証前の数値文字列。
//   - error: 数値構文が不正な場合のエラー。
func (p *tableScriptParser) parseNumberToken() (string, error) {
	p.skipWhitespace()
	start := p.position
	if p.peekByte() == '+' || p.peekByte() == '-' {
		p.position++
	}
	digits := p.consumeDigits()
	if p.peekByte() == '.' {
		p.position++
		digits += p.consumeDigits()
	}
	if digits == 0 {
		p.position = start
		return "", p.errorf("数値が必要です")
	}
	if p.peekByte() == 'e' || p.peekByte() == 'E' {
		p.position++
		if p.peekByte() == '+' || p.peekByte() == '-' {
			p.position++
		}
		if p.consumeDigits() == 0 {
			return "", p.errorf("指数部に数字が必要です")
		}
	}
	return string(p.body[start:p.position]), nil
}

// parseFloat は、JavaScript本文の有限なfloat64を解析します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - float64: 解析した有限数値。
//   - error: 数値構文または有限性が不正な場合のエラー。
func (p *tableScriptParser) parseFloat() (float64, error) {
	raw, err := p.parseNumberToken()
	if err != nil {
		return 0, err
	}
	return parseFiniteNumber(raw, "JavaScript数値")
}

// parseInt は、JavaScript本文の10進整数を解析します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - int: 解析した整数。
//   - error: 小数、指数、範囲外の場合のエラー。
func (p *tableScriptParser) parseInt() (int, error) {
	raw, err := p.parseNumberToken()
	if err != nil {
		return 0, err
	}
	if strings.ContainsAny(raw, ".eE") {
		return 0, p.errorf("整数が必要です: %s", raw)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, p.errorf("整数を解析できません: %s", raw)
	}
	return value, nil
}

// expectByte は、空白を読み飛ばして指定記号を消費します。
//
// 引数:
//   - expected byte: 現在位置に必要なASCII記号。
//
// 返り値:
//   - error: 指定記号が存在しない場合のエラー。
func (p *tableScriptParser) expectByte(expected byte) error {
	p.skipWhitespace()
	if p.peekByte() != expected {
		return p.errorf("%qが必要です", expected)
	}
	p.position++
	return nil
}

// consumeByte は、現在位置が指定記号なら消費します。
//
// 引数:
//   - expected byte: 消費を試みるASCII記号。
//
// 返り値:
//   - bool: 指定記号を消費した場合はtrue。
func (p *tableScriptParser) consumeByte(expected byte) bool {
	p.skipWhitespace()
	if p.peekByte() != expected {
		return false
	}
	p.position++
	return true
}

// consumeDigits は、現在位置から連続するASCII数字を消費します。
//
// 引数:
//   - なし。現在位置から解析する。
//
// 返り値:
//   - int: 消費した数字の文字数。
func (p *tableScriptParser) consumeDigits() int {
	start := p.position
	for p.peekByte() >= '0' && p.peekByte() <= '9' {
		p.position++
	}
	return p.position - start
}

// skipWhitespace は、現在位置からASCII空白を読み飛ばします。
//
// 引数:
//   - なし。パーサー自身の位置を更新する。
//
// 返り値:
//   - なし。
func (p *tableScriptParser) skipWhitespace() {
	for !p.atEnd() {
		switch p.body[p.position] {
		case ' ', '\t', '\r', '\n':
			p.position++
		default:
			return
		}
	}
}

// peekByte は、現在位置のバイトを読み取ります。
//
// 引数:
//   - なし。現在位置を変更しない。
//
// 返り値:
//   - byte: 現在位置のバイト。本文末尾の場合は0。
func (p *tableScriptParser) peekByte() byte {
	if p.atEnd() {
		return 0
	}
	return p.body[p.position]
}

// atEnd は、現在位置が本文末尾か確認します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - bool: 本文末尾へ到達している場合はtrue。
func (p *tableScriptParser) atEnd() bool {
	return p.position >= len(p.body)
}

// finish は、空白以外の余分なJavaScriptがないことを検証します。
//
// 引数:
//   - なし。現在位置から本文末尾までを検証する。
//
// 返り値:
//   - error: 余分なJavaScript記述が存在する場合のエラー。
func (p *tableScriptParser) finish() error {
	p.skipWhitespace()
	if !p.atEnd() {
		return p.errorf("余分なJavaScript記述があります")
	}
	return nil
}

// errorf は、現在位置を含む解析エラーを生成します。
//
// 引数:
//   - format string: エラーメッセージの書式。
//   - arguments ...any: 書式へ埋め込む値。
//
// 返り値:
//   - error: 現在バイト位置を含むエラー。
func (p *tableScriptParser) errorf(format string, arguments ...any) error {
	return fmt.Errorf("バイト%d: %s", p.position, fmt.Sprintf(format, arguments...))
}

// isTableIdentifierStart は、識別子先頭として許可するASCIIバイトを判定します。
//
// 引数:
//   - value byte: 判定するバイト。
//
// 返り値:
//   - bool: ASCII英字、アンダースコア、ドル記号の場合はtrue。
func isTableIdentifierStart(value byte) bool {
	return value == '_' || value == '$' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z')
}

// isTableIdentifierPart は、識別子の継続文字として許可するASCIIバイトを判定します。
//
// 引数:
//   - value byte: 判定するバイト。
//
// 返り値:
//   - bool: 識別子先頭文字またはASCII数字の場合はtrue。
func isTableIdentifierPart(value byte) bool {
	return isTableIdentifierStart(value) || (value >= '0' && value <= '9')
}

// parseFiniteNumber は、文字列を有限なfloat64へ変換します。
//
// 引数:
//   - raw string: 数値を表す文字列。
//   - field string: エラー表示に使う項目名。
//
// 返り値:
//   - float64: 解析した有限数値。
//   - error: 空文字、構文異常、NaN、無限大の場合のエラー。
func parseFiniteNumber(raw string, field string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%sが空です", field)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%sが有限数値ではありません: %q", field, raw)
	}
	return value, nil
}

// parsePercentNumber は、任意の末尾パーセント記号を除いて有限数値へ変換します。
//
// 引数:
//   - raw string: パーセント値を表す文字列。
//   - field string: エラー表示に使う項目名。
//
// 返り値:
//   - float64: パーセント単位の有限数値。
//   - error: 数値が不正な場合のエラー。
func parsePercentNumber(raw string, field string) (float64, error) {
	return parseFiniteNumber(strings.TrimSuffix(strings.TrimSpace(raw), "%"), field)
}

// parseOptionalFiniteNumber は、空欄またはハイフンをnilとして有限数値を解析します。
//
// 引数:
//   - raw string: 任意数値を表す文字列。
//   - field string: エラー表示に使う項目名。
//
// 返り値:
//   - *float64: 有限数値へのポインター。空欄またはハイフンの場合はnil。
//   - error: 空欄以外の値が有限数値ではない場合のエラー。
func parseOptionalFiniteNumber(raw string, field string) (*float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "-" {
		return nil, nil
	}
	value, err := parseFiniteNumber(strings.ReplaceAll(trimmed, ",", ""), field)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// parseOptionalPercentNumber は、空欄またはハイフンをnilとしてパーセント値を解析します。
//
// 引数:
//   - raw string: 任意のパーセント値。
//   - field string: エラー表示に使う項目名。
//
// 返り値:
//   - *float64: パーセント単位の有限数値。空欄またはハイフンの場合はnil。
//   - error: 値が有限数値ではない場合のエラー。
func parseOptionalPercentNumber(raw string, field string) (*float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "-" {
		return nil, nil
	}
	value, err := parsePercentNumber(trimmed, field)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// parseOptionalRank は、ランキングの任意順位を解析します。
//
// 引数:
//   - raw string: 順位またはハイフン。
//   - field string: エラー表示に使う項目名。
//
// 返り値:
//   - *int: 正の順位。ハイフンの場合はnil。
//   - error: 正の10進整数ではない場合のエラー。
func parseOptionalRank(raw string, field string) (*int, error) {
	if strings.TrimSpace(raw) == "-" {
		return nil, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("%sが正の整数ではありません: %q", field, raw)
	}
	return &value, nil
}

// ----------------------------------------

// parseJapanComponents は、N2配信を日経225構成銘柄一覧へ正規化します。
//
// 引数:
//   - body []byte: country_jp_nk225N.jsの本文。
//
// 返り値:
//   - JapanComponentData: 構成銘柄と配信集計値。
//   - error: JavaScript構文、列数、重複、有限数値の検証に失敗した場合のエラー。
func parseJapanComponents(body []byte) (JapanComponentData, error) {
	p := newTableScriptParser(body)
	if err := p.expectKeyword("var"); err != nil {
		return JapanComponentData{}, err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "N2" {
		return JapanComponentData{}, p.errorf("N2宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return JapanComponentData{}, err
	}
	if err := p.expectKeyword("new"); err != nil {
		return JapanComponentData{}, err
	}
	if err := p.expectKeyword("Array"); err != nil {
		return JapanComponentData{}, err
	}
	for _, expected := range []byte{'(', ')', ';'} {
		if err := p.expectByte(expected); err != nil {
			return JapanComponentData{}, err
		}
	}

	rows := newIndexedTableValues()
	metadata := make(map[string]string)
	for {
		p.skipWhitespace()
		if p.atEnd() {
			break
		}
		identifier, err := p.parseIdentifier()
		if err != nil {
			return JapanComponentData{}, err
		}
		if identifier == "N2" {
			if err := p.expectByte('['); err != nil {
				return JapanComponentData{}, err
			}
			index, err := p.parseInt()
			if err != nil {
				return JapanComponentData{}, err
			}
			if err := p.expectByte(']'); err != nil {
				return JapanComponentData{}, err
			}
			if err := p.expectByte('='); err != nil {
				return JapanComponentData{}, err
			}
			row, err := p.parseString()
			if err != nil {
				return JapanComponentData{}, err
			}
			if err := p.expectByte(';'); err != nil {
				return JapanComponentData{}, err
			}
			if err := rows.add(index, row); err != nil {
				return JapanComponentData{}, err
			}
			continue
		}

		if _, known := map[string]struct{}{
			"LastTime": {}, "CntUp": {}, "CntDwn": {}, "CntEvn": {},
			"N225total": {}, "N225josuu": {}, "N225kabuka": {},
		}[identifier]; !known {
			return JapanComponentData{}, p.errorf("未知の変数です: %s", identifier)
		}
		if _, duplicate := metadata[identifier]; duplicate {
			return JapanComponentData{}, p.errorf("変数%sが重複しています", identifier)
		}
		if err := p.expectByte('='); err != nil {
			return JapanComponentData{}, err
		}
		var raw string
		if identifier == "LastTime" {
			raw, err = p.parseString()
		} else {
			raw, err = p.parseNumberToken()
		}
		if err != nil {
			return JapanComponentData{}, err
		}
		if err := p.expectByte(';'); err != nil {
			return JapanComponentData{}, err
		}
		metadata[identifier] = raw
	}

	orderedRows, err := rows.ordered()
	if err != nil {
		return JapanComponentData{}, err
	}
	if len(orderedRows) == 0 {
		return JapanComponentData{}, fmt.Errorf("N2配列が空です")
	}
	components := make([]JapanComponent, 0, len(orderedRows))
	seenCodes := make(map[string]struct{}, len(orderedRows))
	for index, row := range orderedRows {
		component, err := parseJapanComponentRow(row)
		if err != nil {
			return JapanComponentData{}, fmt.Errorf("N2[%d]: %w", index, err)
		}
		if _, duplicate := seenCodes[component.Code]; duplicate {
			return JapanComponentData{}, fmt.Errorf("構成銘柄コード%sが重複しています", component.Code)
		}
		seenCodes[component.Code] = struct{}{}
		components = append(components, component)
	}

	requiredMetadata := []string{
		"LastTime", "CntUp", "CntDwn", "CntEvn", "N225total", "N225josuu", "N225kabuka",
	}
	for _, required := range requiredMetadata {
		if _, exists := metadata[required]; !exists {
			return JapanComponentData{}, fmt.Errorf("必須変数%sがありません", required)
		}
	}
	upCount, err := parseNonNegativeInt(metadata["CntUp"], "CntUp")
	if err != nil {
		return JapanComponentData{}, err
	}
	downCount, err := parseNonNegativeInt(metadata["CntDwn"], "CntDwn")
	if err != nil {
		return JapanComponentData{}, err
	}
	unchangedCount, err := parseNonNegativeInt(metadata["CntEvn"], "CntEvn")
	if err != nil {
		return JapanComponentData{}, err
	}
	if upCount+downCount+unchangedCount != len(components) {
		return JapanComponentData{}, fmt.Errorf("騰落銘柄数%dが構成銘柄数%dと一致しません", upCount+downCount+unchangedCount, len(components))
	}
	totalDeemed, err := parseFiniteNumber(metadata["N225total"], "N225total")
	if err != nil {
		return JapanComponentData{}, err
	}
	divisor, err := parseFiniteNumber(metadata["N225josuu"], "N225josuu")
	if err != nil || divisor <= 0 {
		return JapanComponentData{}, fmt.Errorf("N225josuuが正の有限数値ではありません")
	}
	nikkeiValue, err := parseFiniteNumber(metadata["N225kabuka"], "N225kabuka")
	if err != nil {
		return JapanComponentData{}, err
	}
	return JapanComponentData{
		UpdatedAt:      metadata["LastTime"],
		UpCount:        upCount,
		DownCount:      downCount,
		UnchangedCount: unchangedCount,
		TotalDeemed:    totalDeemed,
		Divisor:        divisor,
		NikkeiValue:    nikkeiValue,
		Components:     components,
	}, nil
}

// parseJapanComponentRow は、N2の1行を10列の構成銘柄へ変換します。
//
// 引数:
//   - row string: 二重アンダースコア区切りのN2行。
//
// 返り値:
//   - JapanComponent: 正規化済み構成銘柄。
//   - error: 列数、必須文字列、有限数値が不正な場合のエラー。
func parseJapanComponentRow(row string) (JapanComponent, error) {
	fields := strings.Split(row, "__")
	if len(fields) != 10 {
		return JapanComponent{}, fmt.Errorf("列数が10ではありません: %d", len(fields))
	}
	if fields[0] == "" || fields[1] == "" || fields[2] == "" {
		return JapanComponent{}, fmt.Errorf("コード、業種、名称は空にできません")
	}
	price, err := parseFiniteNumber(fields[3], "現在値")
	if err != nil {
		return JapanComponent{}, err
	}
	deemedPrice, err := parseFiniteNumber(fields[4], "みなし値")
	if err != nil {
		return JapanComponent{}, err
	}
	weight, err := parsePercentNumber(fields[5], "構成率")
	if err != nil {
		return JapanComponent{}, err
	}
	changePercent, err := parsePercentNumber(fields[6], "前日比率")
	if err != nil {
		return JapanComponent{}, err
	}
	change, err := parseFiniteNumber(fields[7], "前日比")
	if err != nil {
		return JapanComponent{}, err
	}
	contribution, err := parseFiniteNumber(fields[8], "寄与度")
	if err != nil {
		return JapanComponent{}, err
	}
	return JapanComponent{
		Code:            fields[0],
		IndustryCode:    fields[1],
		Name:            fields[2],
		EnglishName:     fields[9],
		Price:           price,
		DeemedPrice:     deemedPrice,
		WeightPercent:   weight,
		ChangePercent:   changePercent,
		Change:          change,
		ContributionYen: contribution,
	}, nil
}

// parseNonNegativeInt は、文字列を0以上の10進整数へ変換します。
//
// 引数:
//   - raw string: 整数を表す文字列。
//   - field string: エラー表示に使う項目名。
//
// 返り値:
//   - int: 解析した0以上の整数。
//   - error: 構文異常または負数の場合のエラー。
func parseNonNegativeInt(raw string, field string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%sが0以上の整数ではありません: %q", field, raw)
	}
	return value, nil
}

// parseJapanContributions は、寄与度上位・下位配信を正規化します。
//
// 引数:
//   - body []byte: country_jp_kiyo10N.jsの本文。
//
// 返り値:
//   - JapanContributionData: 更新時刻と上位・下位一覧。
//   - error: 宣言、添字、列数、重複、有限数値が不正な場合のエラー。
func parseJapanContributions(body []byte) (JapanContributionData, error) {
	p := newTableScriptParser(body)
	if err := p.expectKeyword("var"); err != nil {
		return JapanContributionData{}, err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "LastTime2" {
		return JapanContributionData{}, p.errorf("LastTime2宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return JapanContributionData{}, err
	}
	updatedAt, err := p.parseString()
	if err != nil {
		return JapanContributionData{}, err
	}
	if err := p.expectByte(';'); err != nil {
		return JapanContributionData{}, err
	}
	for _, arrayName := range []string{"top10", "las10"} {
		if err := parseNewArrayDeclaration(p, arrayName); err != nil {
			return JapanContributionData{}, err
		}
	}

	arrays := map[string]*indexedTableValues{
		"top10": newIndexedTableValues(),
		"las10": newIndexedTableValues(),
	}
	for {
		p.skipWhitespace()
		if p.atEnd() {
			break
		}
		arrayName, err := p.parseIdentifier()
		if err != nil {
			return JapanContributionData{}, err
		}
		array, exists := arrays[arrayName]
		if !exists {
			return JapanContributionData{}, p.errorf("未知の配列です: %s", arrayName)
		}
		index, row, err := parseIndexedStringAssignment(p)
		if err != nil {
			return JapanContributionData{}, err
		}
		if err := array.add(index, row); err != nil {
			return JapanContributionData{}, err
		}
	}

	top, err := parseJapanContributionRows(arrays["top10"], "top")
	if err != nil {
		return JapanContributionData{}, err
	}
	bottom, err := parseJapanContributionRows(arrays["las10"], "bottom")
	if err != nil {
		return JapanContributionData{}, err
	}
	return JapanContributionData{UpdatedAt: updatedAt, Top: top, Bottom: bottom}, nil
}

// parseNewArrayDeclaration は、var name = new Array();形式を検証します。
//
// 引数:
//   - p *tableScriptParser: 解析位置を保持するパーサー。
//   - expectedName string: 宣言に必要な配列名。
//
// 返り値:
//   - error: 宣言形式または配列名が不正な場合のエラー。
func parseNewArrayDeclaration(p *tableScriptParser, expectedName string) error {
	if err := p.expectKeyword("var"); err != nil {
		return err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != expectedName {
		return p.errorf("%s配列宣言が必要です", expectedName)
	}
	if err := p.expectByte('='); err != nil {
		return err
	}
	if err := p.expectKeyword("new"); err != nil {
		return err
	}
	if err := p.expectKeyword("Array"); err != nil {
		return err
	}
	for _, expected := range []byte{'(', ')', ';'} {
		if err := p.expectByte(expected); err != nil {
			return err
		}
	}
	return nil
}

// parseIndexedStringAssignment は、識別子後の[index]="value";部分を解析します。
//
// 引数:
//   - p *tableScriptParser: 配列名直後を指すパーサー。
//
// 返り値:
//   - int: 配列添字。
//   - string: 代入された文字列。
//   - error: 添字または代入構文が不正な場合のエラー。
func parseIndexedStringAssignment(p *tableScriptParser) (int, string, error) {
	if err := p.expectByte('['); err != nil {
		return 0, "", err
	}
	index, err := p.parseInt()
	if err != nil {
		return 0, "", err
	}
	if err := p.expectByte(']'); err != nil {
		return 0, "", err
	}
	if err := p.expectByte('='); err != nil {
		return 0, "", err
	}
	value, err := p.parseString()
	if err != nil {
		return 0, "", err
	}
	if err := p.expectByte(';'); err != nil {
		return 0, "", err
	}
	return index, value, nil
}

// parseJapanContributionRows は、添字付き寄与度行を正規化します。
//
// 引数:
//   - rows *indexedTableValues: 添字付きの寄与度行。
//   - direction string: topまたはbottom。
//
// 返り値:
//   - []JapanContribution: 配信順位順の寄与度一覧。
//   - error: 添字欠落、列数、コード重複、有限数値が不正な場合のエラー。
func parseJapanContributionRows(rows *indexedTableValues, direction string) ([]JapanContribution, error) {
	ordered, err := rows.ordered()
	if err != nil {
		return nil, err
	}
	result := make([]JapanContribution, 0, len(ordered))
	seenCodes := make(map[string]struct{}, len(ordered))
	for index, row := range ordered {
		fields := strings.Split(row, "__")
		if len(fields) != 6 {
			return nil, fmt.Errorf("%s[%d]の列数が6ではありません: %d", direction, index, len(fields))
		}
		if fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("%s[%d]のコードまたは名称が空です", direction, index)
		}
		if _, duplicate := seenCodes[fields[0]]; duplicate {
			return nil, fmt.Errorf("%sのコード%sが重複しています", direction, fields[0])
		}
		seenCodes[fields[0]] = struct{}{}
		contribution, err := parseFiniteNumber(fields[2], "寄与度")
		if err != nil {
			return nil, err
		}
		price, err := parseFiniteNumber(fields[3], "現在値")
		if err != nil {
			return nil, err
		}
		change, err := parseFiniteNumber(fields[4], "前日比")
		if err != nil {
			return nil, err
		}
		changePercent, err := parsePercentNumber(fields[5], "前日比率")
		if err != nil {
			return nil, err
		}
		result = append(result, JapanContribution{
			Direction:       direction,
			Rank:            index + 1,
			Code:            fields[0],
			Name:            fields[1],
			ContributionYen: contribution,
			Price:           price,
			Change:          change,
			ChangePercent:   changePercent,
		})
	}
	return result, nil
}

// parseJapanIndustries は、東証33業種の指数値と変化を正規化します。
//
// 引数:
//   - body []byte: country_jp_gyo.jsの本文。
//
// 返り値:
//   - JapanIndustryData: 更新日時と33業種一覧。
//   - error: 変数、split記述、列数、有限数値が不正な場合のエラー。
func parseJapanIndustries(body []byte) (JapanIndustryData, error) {
	p := newTableScriptParser(body)
	values := make(map[string]string)
	for _, expectedName := range []string{"ModDate", "ModTime", "G1", "G2"} {
		name, err := p.parseIdentifier()
		if err != nil || name != expectedName {
			return JapanIndustryData{}, p.errorf("%s代入が必要です", expectedName)
		}
		if err := p.expectByte('='); err != nil {
			return JapanIndustryData{}, err
		}
		value, err := p.parseString()
		if err != nil {
			return JapanIndustryData{}, err
		}
		if err := p.expectByte(';'); err != nil {
			return JapanIndustryData{}, err
		}
		values[name] = value
	}
	for _, name := range []string{"G1", "G2"} {
		if err := parseSplitAssignment(p, name, name); err != nil {
			return JapanIndustryData{}, err
		}
	}
	if err := p.finish(); err != nil {
		return JapanIndustryData{}, err
	}
	levelFields, err := parseTrailingUnderscoreFields(values["G1"], len(japanIndustryNames), "G1")
	if err != nil {
		return JapanIndustryData{}, err
	}
	changeFields, err := parseTrailingUnderscoreFields(values["G2"], len(japanIndustryNames), "G2")
	if err != nil {
		return JapanIndustryData{}, err
	}
	industries := make([]JapanIndustry, 0, len(levelFields))
	for index := range levelFields {
		value, err := parseFiniteNumber(levelFields[index], "業種指数")
		if err != nil {
			return JapanIndustryData{}, fmt.Errorf("G1[%d]: %w", index, err)
		}
		change, err := parseFiniteNumber(changeFields[index], "業種変化")
		if err != nil {
			return JapanIndustryData{}, fmt.Errorf("G2[%d]: %w", index, err)
		}
		industries = append(industries, JapanIndustry{
			Position: index + 1,
			Code:     japanIndustryCodes[index],
			Name:     japanIndustryNames[index],
			Value:    value,
			Change:   change,
		})
	}
	return JapanIndustryData{
		UpdatedDate: values["ModDate"],
		UpdatedTime: values["ModTime"],
		Industries:  industries,
	}, nil
}

// parseSplitAssignment は、target=source.split("_");形式を検証します。
//
// 引数:
//   - p *tableScriptParser: 解析位置を保持するパーサー。
//   - target string: 左辺に必要な識別子。
//   - source string: splitを呼び出す識別子。
//
// 返り値:
//   - error: 指定形式と一致しない場合のエラー。
func parseSplitAssignment(p *tableScriptParser, target string, source string) error {
	actualTarget, err := p.parseIdentifier()
	if err != nil || actualTarget != target {
		return p.errorf("%sのsplit代入が必要です", target)
	}
	if err := p.expectByte('='); err != nil {
		return err
	}
	actualSource, err := p.parseIdentifier()
	if err != nil || actualSource != source {
		return p.errorf("split元は%sである必要があります", source)
	}
	if err := p.expectByte('.'); err != nil {
		return err
	}
	if err := p.expectKeyword("split"); err != nil {
		return err
	}
	if err := p.expectByte('('); err != nil {
		return err
	}
	delimiter, err := p.parseString()
	if err != nil || delimiter != "_" {
		return p.errorf("split区切りはアンダースコアである必要があります")
	}
	for _, expected := range []byte{')', ';'} {
		if err := p.expectByte(expected); err != nil {
			return err
		}
	}
	return nil
}

// parseTrailingUnderscoreFields は、末尾アンダースコア付き列を分割します。
//
// 引数:
//   - raw string: アンダースコア区切りの文字列。
//   - expected int: 必要な実データ列数。
//   - field string: エラー表示に使う変数名。
//
// 返り値:
//   - []string: 末尾空列を除いた列一覧。
//   - error: 末尾区切りまたは列数が不正な場合のエラー。
func parseTrailingUnderscoreFields(raw string, expected int, field string) ([]string, error) {
	fields := strings.Split(raw, "_")
	if len(fields) != expected+1 || fields[len(fields)-1] != "" {
		return nil, fmt.Errorf("%sの列数または末尾区切りが不正です", field)
	}
	return fields[:expected], nil
}

// parseJapanRankings は、日本株ランキング配信を正規化します。
//
// 引数:
//   - body []byte: country_jp_ranking.jsの本文。
//
// 返り値:
//   - MarketRankingData: 日本株の値上がり、値下がり、売買活況一覧。
//   - error: 宣言、列数、順位、重複、有限数値が不正な場合のエラー。
func parseJapanRankings(body []byte) (MarketRankingData, error) {
	return parseMarketRankings(body, "japan")
}

// parseUSRankings は、米国株ランキング配信を正規化します。
//
// 引数:
//   - body []byte: country_ny_ranking.jsの本文。
//
// 返り値:
//   - MarketRankingData: 米国株の値上がり、値下がり、出来高上位一覧。
//   - error: 宣言、列数、順位、重複、有限数値が不正な場合のエラー。
func parseUSRankings(body []byte) (MarketRankingData, error) {
	return parseMarketRankings(body, "us")
}

// parseMarketRankings は、共通形式の日本株・米国株ランキングを解析します。
//
// 引数:
//   - body []byte: ランキングJavaScript本文。
//   - market string: japanまたはus。
//
// 返り値:
//   - MarketRankingData: 市場別に正規化した3種類のランキング。
//   - error: 市場指定または配信形式が不正な場合のエラー。
func parseMarketRankings(body []byte, market string) (MarketRankingData, error) {
	if market != "japan" && market != "us" {
		return MarketRankingData{}, fmt.Errorf("未対応のランキング市場です: %q", market)
	}
	p := newTableScriptParser(body)
	if err := p.expectKeyword("var"); err != nil {
		return MarketRankingData{}, err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "UPDATE_TIME" {
		return MarketRankingData{}, p.errorf("UPDATE_TIME宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return MarketRankingData{}, err
	}
	updatedAt, err := p.parseString()
	if err != nil {
		return MarketRankingData{}, err
	}
	if err := p.expectByte(';'); err != nil {
		return MarketRankingData{}, err
	}

	arrays := map[string]*indexedTableValues{
		"RANK_up": newIndexedTableValues(),
		"RANK_dw": newIndexedTableValues(),
		"RANK_bi": newIndexedTableValues(),
	}
	for _, expectedName := range []string{"RANK_up", "RANK_dw", "RANK_bi"} {
		if err := parseEmptyArrayDeclaration(p, expectedName); err != nil {
			return MarketRankingData{}, err
		}
	}
	for {
		p.skipWhitespace()
		if p.atEnd() {
			break
		}
		arrayName, err := p.parseIdentifier()
		if err != nil {
			return MarketRankingData{}, err
		}
		array, exists := arrays[arrayName]
		if !exists {
			return MarketRankingData{}, p.errorf("未知のランキング配列です: %s", arrayName)
		}
		index, row, err := parseIndexedStringAssignment(p)
		if err != nil {
			return MarketRankingData{}, err
		}
		if err := array.add(index, row); err != nil {
			return MarketRankingData{}, err
		}
	}

	gainers, err := parseRankingRows(arrays["RANK_up"], market, "gainers")
	if err != nil {
		return MarketRankingData{}, err
	}
	losers, err := parseRankingRows(arrays["RANK_dw"], market, "losers")
	if err != nil {
		return MarketRankingData{}, err
	}
	active, err := parseRankingRows(arrays["RANK_bi"], market, "active")
	if err != nil {
		return MarketRankingData{}, err
	}
	return MarketRankingData{
		Market:    market,
		UpdatedAt: updatedAt,
		Gainers:   gainers,
		Losers:    losers,
		Active:    active,
	}, nil
}

// parseEmptyArrayDeclaration は、var name=[];形式を検証します。
//
// 引数:
//   - p *tableScriptParser: 解析位置を保持するパーサー。
//   - expectedName string: 必要な配列名。
//
// 返り値:
//   - error: 宣言または配列名が不正な場合のエラー。
func parseEmptyArrayDeclaration(p *tableScriptParser, expectedName string) error {
	if err := p.expectKeyword("var"); err != nil {
		return err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != expectedName {
		return p.errorf("%s配列宣言が必要です", expectedName)
	}
	for _, expected := range []byte{'=', '[', ']', ';'} {
		if err := p.expectByte(expected); err != nil {
			return err
		}
	}
	return nil
}

// parseRankingRows は、ランキング行を市場別の構造体へ変換します。
//
// 引数:
//   - rows *indexedTableValues: 添字付きランキング行。
//   - market string: japanまたはus。
//   - kind string: gainers、losers、activeのいずれか。
//
// 返り値:
//   - []MarketRanking: 配信順位順のランキング。
//   - error: 添字、列数、順位、コード重複、有限数値が不正な場合のエラー。
func parseRankingRows(rows *indexedTableValues, market string, kind string) ([]MarketRanking, error) {
	ordered, err := rows.ordered()
	if err != nil {
		return nil, err
	}
	result := make([]MarketRanking, 0, len(ordered))
	seenCodes := make(map[string]struct{}, len(ordered))
	for index, row := range ordered {
		fields := strings.Split(row, "_")
		if len(fields) != 10 {
			return nil, fmt.Errorf("%s[%d]の列数が10ではありません: %d", kind, index, len(fields))
		}
		if fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("%s[%d]のコードまたは名称が空です", kind, index)
		}
		if _, duplicate := seenCodes[fields[0]]; duplicate {
			return nil, fmt.Errorf("%sのコード%sが重複しています", kind, fields[0])
		}
		seenCodes[fields[0]] = struct{}{}
		rank, err := strconv.Atoi(fields[2])
		if err != nil || rank != index+1 {
			return nil, fmt.Errorf("%s[%d]の当日順位が添字と一致しません", kind, index)
		}
		previousRank, err := parseOptionalRank(fields[3], "前日順位")
		if err != nil {
			return nil, err
		}
		twoDaysAgoRank, err := parseOptionalRank(fields[4], "前々日順位")
		if err != nil {
			return nil, err
		}
		value, err := parseFiniteNumber(fields[5], "現在値")
		if err != nil {
			return nil, err
		}
		changePercent, err := parsePercentNumber(fields[6], "前日比率")
		if err != nil {
			return nil, err
		}
		change, err := parseFiniteNumber(fields[7], "前日比")
		if err != nil {
			return nil, err
		}
		item := MarketRanking{
			Market:         market,
			Kind:           kind,
			Code:           fields[0],
			Name:           fields[1],
			Rank:           rank,
			PreviousRank:   previousRank,
			TwoDaysAgoRank: twoDaysAgoRank,
			Value:          value,
			ChangePercent:  changePercent,
			Change:         change,
			MarketTime:     fields[9],
		}
		if market == "japan" {
			if fields[8] == "" {
				return nil, fmt.Errorf("%s[%d]の市場区分が空です", kind, index)
			}
			item.Exchange = fields[8]
		} else {
			volume, err := parseOptionalFiniteNumber(fields[8], "出来高")
			if err != nil || volume == nil {
				return nil, fmt.Errorf("%s[%d]の出来高が不正です", kind, index)
			}
			item.Volume = volume
		}
		result = append(result, item)
	}
	return result, nil
}

// ----------------------------------------

// parseUSEquities は、通常・プレ・アフター共通の米国主要銘柄配信を解析します。
//
// 引数:
//   - body []byte: country_ny.js、country_ny_pre.js、country_ny_after.jsのいずれか。
//
// 返り値:
//   - USEquityData: 取引セッションと正規化済み銘柄一覧。
//   - error: 変数名、JSON風配列、必須キー、重複、有限数値が不正な場合のエラー。
func parseUSEquities(body []byte) (USEquityData, error) {
	p := newTableScriptParser(body)
	if err := p.expectKeyword("var"); err != nil {
		return USEquityData{}, err
	}
	timeVariable, err := p.parseIdentifier()
	if err != nil {
		return USEquityData{}, err
	}
	session, suffix, err := parseUSSessionVariable(timeVariable)
	if err != nil {
		return USEquityData{}, err
	}
	if err := p.expectByte('='); err != nil {
		return USEquityData{}, err
	}
	updatedAt, err := p.parseString()
	if err != nil {
		return USEquityData{}, err
	}
	if err := p.expectByte(';'); err != nil {
		return USEquityData{}, err
	}

	result := USEquityData{Session: session, UpdatedAt: updatedAt}
	if session == "regular" {
		market, divisor, err := parseUSRegularMetadata(p)
		if err != nil {
			return USEquityData{}, err
		}
		result.Market = market
		result.Divisor = &divisor
	}

	if err := p.expectKeyword("var"); err != nil {
		return USEquityData{}, err
	}
	dataVariable, err := p.parseIdentifier()
	if err != nil || dataVariable != "stockData"+suffix {
		return USEquityData{}, p.errorf("stockData%s宣言が必要です", suffix)
	}
	if err := p.expectByte('='); err != nil {
		return USEquityData{}, err
	}
	equities, err := parseUSEquityArray(p, session, result.Divisor)
	if err != nil {
		return USEquityData{}, err
	}
	if err := p.expectByte(';'); err != nil {
		return USEquityData{}, err
	}
	if err := p.finish(); err != nil {
		return USEquityData{}, err
	}
	result.Equities = equities
	return result, nil
}

// parseUSSessionVariable は、更新時刻変数から取引セッションと接尾辞を決定します。
//
// 引数:
//   - variable string: 更新時刻のJavaScript変数名。
//
// 返り値:
//   - string: regular、pre、afterのいずれか。
//   - string: 対応する変数接尾辞。
//   - error: 未知の変数名の場合のエラー。
func parseUSSessionVariable(variable string) (string, string, error) {
	switch variable {
	case "stockDataTime":
		return "regular", "", nil
	case "stockDataTime_pre":
		return "pre", "_pre", nil
	case "stockDataTime_after":
		return "after", "_after", nil
	default:
		return "", "", fmt.Errorf("未知の米国市場更新時刻変数です: %s", variable)
	}
}

// parseUSRegularMetadata は、通常市場だけに含まれる市場状態と除数を解析します。
//
// 引数:
//   - p *tableScriptParser: 更新時刻代入後を指すパーサー。
//
// 返り値:
//   - string: stockDataMarketの文字列。
//   - float64: 正のstockDataDivisor。
//   - error: 変数名、文字列、除数が不正な場合のエラー。
func parseUSRegularMetadata(p *tableScriptParser) (string, float64, error) {
	if err := p.expectKeyword("var"); err != nil {
		return "", 0, err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "stockDataMarket" {
		return "", 0, p.errorf("stockDataMarket宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return "", 0, err
	}
	market, err := p.parseString()
	if err != nil || market == "" {
		return "", 0, p.errorf("stockDataMarketが不正です")
	}
	if err := p.expectByte(';'); err != nil {
		return "", 0, err
	}
	if err := p.expectKeyword("var"); err != nil {
		return "", 0, err
	}
	name, err = p.parseIdentifier()
	if err != nil || name != "stockDataDivisor" {
		return "", 0, p.errorf("stockDataDivisor宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return "", 0, err
	}
	divisor, err := p.parseFloat()
	if err != nil || divisor <= 0 {
		return "", 0, p.errorf("stockDataDivisorは正の有限数値である必要があります")
	}
	if err := p.expectByte(';'); err != nil {
		return "", 0, err
	}
	return market, divisor, nil
}

// parseUSEquityArray は、米国主要銘柄のJSON風オブジェクト配列を解析します。
//
// 引数:
//   - p *tableScriptParser: 開き角括弧の直前を指すパーサー。
//   - session string: regular、pre、afterのいずれか。
//   - divisor *float64: 通常市場のDOW寄与度除数。存在しない場合はnil。
//
// 返り値:
//   - []USEquity: 配信順と指数間重複を保持した銘柄一覧。
//   - error: 配列、オブジェクト、同一指数内重複が不正な場合のエラー。
func parseUSEquityArray(
	p *tableScriptParser,
	session string,
	divisor *float64,
) ([]USEquity, error) {
	if err := p.expectByte('['); err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.consumeByte(']') {
		return nil, fmt.Errorf("米国銘柄配列が空です")
	}
	result := make([]USEquity, 0, 200)
	seen := make(map[string]struct{})
	for {
		if len(result) >= maxTableRows {
			return nil, p.errorf("米国銘柄行数が上限を超えました")
		}
		equity, flag, err := parseUSEquityObject(p, session)
		if err != nil {
			return nil, fmt.Errorf("米国銘柄[%d]: %w", len(result), err)
		}
		identity := strconv.Itoa(flag) + "\x00" + equity.Symbol
		if _, duplicate := seen[identity]; duplicate {
			return nil, fmt.Errorf("米国銘柄%sが%s内で重複しています", equity.Symbol, equity.Universe)
		}
		seen[identity] = struct{}{}
		if flag == 30 && divisor != nil {
			contribution := equity.Change / *divisor
			if math.IsNaN(contribution) || math.IsInf(contribution, 0) {
				return nil, fmt.Errorf("米国銘柄%sのDOW寄与度が有限ではありません", equity.Symbol)
			}
			equity.DowContribution = &contribution
		}
		result = append(result, equity)
		if p.consumeByte(']') {
			break
		}
		if err := p.expectByte(','); err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if p.peekByte() == ']' {
			return nil, p.errorf("米国銘柄配列の末尾カンマは許可されません")
		}
	}
	return result, nil
}

// parseUSEquityObject は、米国主要銘柄の1オブジェクトを厳密解析します。
//
// 引数:
//   - p *tableScriptParser: 開き波括弧の直前を指すパーサー。
//   - session string: regular、pre、afterのいずれか。
//
// 返り値:
//   - USEquity: 正規化済みの米国銘柄。
//   - int: 配信上のFフラグ。
//   - error: キー重複、未知キー、必須キー、型、有限数値が不正な場合のエラー。
func parseUSEquityObject(p *tableScriptParser, session string) (USEquity, int, error) {
	if err := p.expectByte('{'); err != nil {
		return USEquity{}, 0, err
	}
	p.skipWhitespace()
	if p.peekByte() == '}' {
		return USEquity{}, 0, p.errorf("米国銘柄オブジェクトが空です")
	}
	var result USEquity
	flag := -1
	seen := make(map[string]struct{})
	for {
		p.skipWhitespace()
		if p.consumeByte('}') {
			break
		}
		key, err := p.parseString()
		if err != nil {
			return USEquity{}, 0, err
		}
		if _, duplicate := seen[key]; duplicate {
			return USEquity{}, 0, p.errorf("オブジェクトキー%sが重複しています", key)
		}
		seen[key] = struct{}{}
		if err := p.expectByte(':'); err != nil {
			return USEquity{}, 0, err
		}
		switch key {
		case "F":
			flag, err = p.parseInt()
		case "S":
			result.Symbol, err = p.parseString()
		case "J":
			result.Name, err = p.parseString()
		case "E":
			result.EnglishName, err = p.parseString()
		case "G":
			result.IndustryCode, err = p.parseInt()
		case "V":
			result.Price, err = p.parseFloat()
		case "Z":
			result.Change, err = p.parseFloat()
		case "P":
			result.ChangePercent, err = p.parseFloat()
		case "D":
			var volume float64
			volume, err = p.parseFloat()
			result.Volume = &volume
		case "K":
			result.WeightPercent, err = p.parseFloat()
		default:
			return USEquity{}, 0, p.errorf("未知の米国銘柄キーです: %s", key)
		}
		if err != nil {
			return USEquity{}, 0, err
		}
		if p.consumeByte('}') {
			break
		}
		if err := p.expectByte(','); err != nil {
			return USEquity{}, 0, err
		}
		p.skipWhitespace()
		if p.peekByte() == '}' {
			return USEquity{}, 0, p.errorf("米国銘柄オブジェクトの末尾カンマは許可されません")
		}
	}
	required := []string{"F", "S", "J", "G", "V", "Z", "P", "K"}
	if session == "regular" {
		required = append(required, "E", "D")
	} else if _, hasEnglish := seen["E"]; hasEnglish {
		return USEquity{}, 0, fmt.Errorf("%s配信にEキーは許可されません", session)
	} else if _, hasVolume := seen["D"]; hasVolume {
		return USEquity{}, 0, fmt.Errorf("%s配信にDキーは許可されません", session)
	}
	for _, key := range required {
		if _, exists := seen[key]; !exists {
			return USEquity{}, 0, fmt.Errorf("必須キー%sがありません", key)
		}
	}
	if result.Symbol == "" || result.Name == "" {
		return USEquity{}, 0, fmt.Errorf("Symbolまたは名称が空です")
	}
	if result.Volume != nil && *result.Volume < 0 {
		return USEquity{}, 0, fmt.Errorf("出来高が負数です")
	}
	if result.WeightPercent < 0 {
		return USEquity{}, 0, fmt.Errorf("構成率が負数です")
	}
	universe, err := usUniverseName(flag)
	if err != nil {
		return USEquity{}, 0, err
	}
	result.Universe = universe
	return result, flag, nil
}

// usUniverseName は、Fフラグを利用者向けの指数区分名へ変換します。
//
// 引数:
//   - flag int: 配信オブジェクトのF値。
//
// 返り値:
//   - string: fang_plus、dow30、nasdaq100、otherのいずれか。
//   - error: 未知のF値の場合のエラー。
func usUniverseName(flag int) (string, error) {
	switch flag {
	case 0:
		return "other", nil
	case 10:
		return "fang_plus", nil
	case 30:
		return "dow30", nil
	case 100:
		return "nasdaq100", nil
	default:
		return "", fmt.Errorf("未知の米国銘柄Fフラグです: %d", flag)
	}
}

// parseUSIndustries は、米国業種指数GYO1・GYO2配信を正規化します。
//
// 引数:
//   - body []byte: country_ny_gyo.jsの本文。
//
// 返り値:
//   - USIndustryData: 更新時刻と業種指数一覧。
//   - error: 宣言、添字、split記述、列数、重複、有限数値が不正な場合のエラー。
func parseUSIndustries(body []byte) (USIndustryData, error) {
	p := newTableScriptParser(body)
	if err := p.expectKeyword("var"); err != nil {
		return USIndustryData{}, err
	}
	for index, expectedName := range []string{"GYO1", "GYO2"} {
		name, err := p.parseIdentifier()
		if err != nil || name != expectedName {
			return USIndustryData{}, p.errorf("%s宣言が必要です", expectedName)
		}
		for _, expected := range []byte{'=', '[', ']'} {
			if err := p.expectByte(expected); err != nil {
				return USIndustryData{}, err
			}
		}
		if index == 0 {
			if err := p.expectByte(','); err != nil {
				return USIndustryData{}, err
			}
		}
	}
	if err := p.expectByte(';'); err != nil {
		return USIndustryData{}, err
	}

	arrays := map[string]*indexedTableValues{
		"GYO1": newIndexedTableValues(),
		"GYO2": newIndexedTableValues(),
	}
	for {
		p.skipWhitespace()
		if !bytes.HasPrefix(p.body[p.position:], []byte("GYO1")) &&
			!bytes.HasPrefix(p.body[p.position:], []byte("GYO2")) {
			break
		}
		name, err := p.parseIdentifier()
		if err != nil {
			return USIndustryData{}, err
		}
		array, exists := arrays[name]
		if !exists {
			return USIndustryData{}, p.errorf("未知の米国業種配列です: %s", name)
		}
		index, row, err := parseIndexedStringAssignment(p)
		if err != nil {
			return USIndustryData{}, err
		}
		if err := array.add(index, row); err != nil {
			return USIndustryData{}, err
		}
	}
	if err := parseUSIndustrySplitLoop(p, "Y", "GYO1"); err != nil {
		return USIndustryData{}, err
	}
	if err := parseUSIndustrySplitLoop(p, "X", "GYO2"); err != nil {
		return USIndustryData{}, err
	}
	if err := p.expectKeyword("var"); err != nil {
		return USIndustryData{}, err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "GyoModTime" {
		return USIndustryData{}, p.errorf("GyoModTime宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return USIndustryData{}, err
	}
	updatedAt, err := p.parseString()
	if err != nil {
		return USIndustryData{}, err
	}
	if err := p.expectByte(';'); err != nil {
		return USIndustryData{}, err
	}
	if err := p.finish(); err != nil {
		return USIndustryData{}, err
	}

	result := USIndustryData{UpdatedAt: updatedAt}
	seenCodes := make(map[string]struct{})
	for _, group := range []string{"GYO1", "GYO2"} {
		ordered, err := arrays[group].ordered()
		if err != nil {
			return USIndustryData{}, err
		}
		for index, row := range ordered {
			industry, err := parseUSIndustryRow(row, group, index)
			if err != nil {
				return USIndustryData{}, err
			}
			if _, duplicate := seenCodes[industry.Code]; duplicate {
				return USIndustryData{}, fmt.Errorf("米国業種コード%sが重複しています", industry.Code)
			}
			seenCodes[industry.Code] = struct{}{}
			result.Industries = append(result.Industries, industry)
		}
	}
	if len(result.Industries) == 0 {
		return USIndustryData{}, fmt.Errorf("米国業種配列が空です")
	}
	return result, nil
}

// parseUSIndustrySplitLoop は、配信末尾のsplit変換for文を厳密検証します。
//
// 引数:
//   - p *tableScriptParser: forキーワードの直前を指すパーサー。
//   - loopVariable string: YまたはXのループ変数。
//   - arrayName string: GYO1またはGYO2。
//
// 返り値:
//   - error: for文が既知のsplit形式と一致しない場合のエラー。
func parseUSIndustrySplitLoop(
	p *tableScriptParser,
	loopVariable string,
	arrayName string,
) error {
	if err := p.expectKeyword("for"); err != nil {
		return err
	}
	if err := p.expectByte('('); err != nil {
		return err
	}
	variable, err := p.parseIdentifier()
	if err != nil || variable != loopVariable {
		return p.errorf("for変数%sが必要です", loopVariable)
	}
	if err := p.expectKeyword("in"); err != nil {
		return err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != arrayName {
		return p.errorf("for対象%sが必要です", arrayName)
	}
	if err := p.expectByte(')'); err != nil {
		return err
	}
	for occurrence := 0; occurrence < 2; occurrence++ {
		name, err = p.parseIdentifier()
		if err != nil || name != arrayName {
			return p.errorf("split対象%sが必要です", arrayName)
		}
		if err := p.expectByte('['); err != nil {
			return err
		}
		variable, err = p.parseIdentifier()
		if err != nil || variable != loopVariable {
			return p.errorf("配列添字%sが必要です", loopVariable)
		}
		if err := p.expectByte(']'); err != nil {
			return err
		}
		if occurrence == 0 {
			if err := p.expectByte('='); err != nil {
				return err
			}
		}
	}
	if err := p.expectByte('.'); err != nil {
		return err
	}
	if err := p.expectKeyword("split"); err != nil {
		return err
	}
	if err := p.expectByte('('); err != nil {
		return err
	}
	delimiter, err := p.parseSingleQuotedString()
	if err != nil || delimiter != "_" {
		return p.errorf("split区切りはアンダースコアである必要があります")
	}
	for _, expected := range []byte{')', ';'} {
		if err := p.expectByte(expected); err != nil {
			return err
		}
	}
	return nil
}

// parseUSIndustryRow は、米国業種配信の1行を5列へ正規化します。
//
// 引数:
//   - row string: アンダースコア区切りの業種行。
//   - group string: GYO1またはGYO2。
//   - index int: 配信配列の添字。
//
// 返り値:
//   - USIndustry: 正規化済み業種指数。
//   - error: 列数、コード、有限数値が不正な場合のエラー。
func parseUSIndustryRow(row string, group string, index int) (USIndustry, error) {
	fields := strings.Split(row, "_")
	if len(fields) != 5 {
		return USIndustry{}, fmt.Errorf("%s[%d]の列数が5ではありません: %d", group, index, len(fields))
	}
	if fields[0] == "" {
		return USIndustry{}, fmt.Errorf("%s[%d]のコードが空です", group, index)
	}
	value, err := parseFiniteNumber(fields[1], "業種指数")
	if err != nil {
		return USIndustry{}, err
	}
	change, err := parseFiniteNumber(fields[2], "業種変化")
	if err != nil {
		return USIndustry{}, err
	}
	changePercent, err := parsePercentNumber(fields[3], "業種変化率")
	if err != nil {
		return USIndustry{}, err
	}
	return USIndustry{
		Group:         group,
		Position:      index + 1,
		Code:          fields[0],
		Value:         value,
		Change:        change,
		ChangePercent: changePercent,
		MarketTime:    fields[4],
	}, nil
}

// ----------------------------------------

// parseADRData は、ADR一覧A0と主要銘柄Shuを正規化します。
//
// 引数:
//   - body []byte: _adr_all.jsの本文。
//
// 返り値:
//   - ADRData: 主要コードとADR・PTS・東証価格一覧。
//   - error: JavaScript構文、24列、重複、有限数値、主要コード照合が不正な場合のエラー。
func parseADRData(body []byte) (ADRData, error) {
	p := newTableScriptParser(body)
	if err := p.expectKeyword("var"); err != nil {
		return ADRData{}, err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "Shu" {
		return ADRData{}, p.errorf("Shu宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return ADRData{}, err
	}
	mainRaw, err := p.parseString()
	if err != nil {
		return ADRData{}, err
	}
	if err := p.expectByte(';'); err != nil {
		return ADRData{}, err
	}
	mainCodes, mainSet, err := parseADRMainCodes(mainRaw)
	if err != nil {
		return ADRData{}, err
	}
	if err := parseADRArrayDeclaration(p); err != nil {
		return ADRData{}, err
	}
	name, err = p.parseIdentifier()
	if err != nil || name != "q" {
		return ADRData{}, p.errorf("q初期化が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return ADRData{}, err
	}
	initialIndex, err := p.parseInt()
	if err != nil || initialIndex != 0 {
		return ADRData{}, p.errorf("qは0で初期化する必要があります")
	}
	if err := p.expectByte(';'); err != nil {
		return ADRData{}, err
	}

	result := ADRData{MainCodes: mainCodes}
	seenQuotes := make(map[string]struct{})
	seenCodes := make(map[string]struct{})
	for {
		p.skipWhitespace()
		if p.atEnd() {
			break
		}
		if len(result.Quotes) >= maxTableRows {
			return ADRData{}, p.errorf("ADR行数が上限を超えました")
		}
		name, err = p.parseIdentifier()
		if err != nil || name != "A0" {
			return ADRData{}, p.errorf("A0[q]代入が必要です")
		}
		if err := p.expectByte('['); err != nil {
			return ADRData{}, err
		}
		indexName, err := p.parseIdentifier()
		if err != nil || indexName != "q" {
			return ADRData{}, p.errorf("A0の添字はqである必要があります")
		}
		if err := p.expectByte(']'); err != nil {
			return ADRData{}, err
		}
		if err := p.expectByte('='); err != nil {
			return ADRData{}, err
		}
		row, err := p.parseString()
		if err != nil {
			return ADRData{}, err
		}
		if err := p.expectByte(';'); err != nil {
			return ADRData{}, err
		}
		name, err = p.parseIdentifier()
		if err != nil || name != "q" {
			return ADRData{}, p.errorf("A0代入後にq++が必要です")
		}
		for increment := 0; increment < 2; increment++ {
			if err := p.expectByte('+'); err != nil {
				return ADRData{}, err
			}
		}
		if err := p.expectByte(';'); err != nil {
			return ADRData{}, err
		}

		quote, err := parseADRRow(row)
		if err != nil {
			return ADRData{}, fmt.Errorf("A0[%d]: %w", len(result.Quotes), err)
		}
		identity := quote.Code + "\x00" + quote.ADRSymbol
		if _, duplicate := seenQuotes[identity]; duplicate {
			return ADRData{}, fmt.Errorf("ADR銘柄%s:%sが重複しています", quote.Code, quote.ADRSymbol)
		}
		seenQuotes[identity] = struct{}{}
		seenCodes[quote.Code] = struct{}{}
		_, quote.Main = mainSet[quote.Code]
		result.Quotes = append(result.Quotes, quote)
	}
	if len(result.Quotes) == 0 {
		return ADRData{}, fmt.Errorf("A0配列が空です")
	}
	for _, code := range result.MainCodes {
		if _, exists := seenCodes[code]; !exists {
			return ADRData{}, fmt.Errorf("Shuの主要コード%sがA0に存在しません", code)
		}
	}
	return result, nil
}

// parseADRMainCodes は、Shuのカンマ区切り主要コードを検証します。
//
// 引数:
//   - raw string: カンマ区切りの主要銘柄コード。
//
// 返り値:
//   - []string: 配信順を保持した主要コード。
//   - map[string]struct{}: 主要コード判定用集合。
//   - error: 空コードまたは重複コードがある場合のエラー。
func parseADRMainCodes(raw string) ([]string, map[string]struct{}, error) {
	if raw == "" {
		return nil, nil, fmt.Errorf("Shuが空です")
	}
	codes := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if code == "" {
			return nil, nil, fmt.Errorf("Shuに空コードがあります")
		}
		if _, duplicate := seen[code]; duplicate {
			return nil, nil, fmt.Errorf("Shuのコード%sが重複しています", code)
		}
		seen[code] = struct{}{}
	}
	return codes, seen, nil
}

// parseADRArrayDeclaration は、var A0=new Array();宣言を検証します。
//
// 引数:
//   - p *tableScriptParser: A0宣言の直前を指すパーサー。
//
// 返り値:
//   - error: 宣言形式が一致しない場合のエラー。
func parseADRArrayDeclaration(p *tableScriptParser) error {
	if err := p.expectKeyword("var"); err != nil {
		return err
	}
	name, err := p.parseIdentifier()
	if err != nil || name != "A0" {
		return p.errorf("A0宣言が必要です")
	}
	if err := p.expectByte('='); err != nil {
		return err
	}
	if err := p.expectKeyword("new"); err != nil {
		return err
	}
	if err := p.expectKeyword("Array"); err != nil {
		return err
	}
	for _, expected := range []byte{'(', ')', ';'} {
		if err := p.expectByte(expected); err != nil {
			return err
		}
	}
	return nil
}

// parseADRRow は、A0の24列をADRQuoteへ正規化します。
//
// 引数:
//   - row string: アンダースコア区切りのA0行。
//
// 返り値:
//   - ADRQuote: 正規化済みADR、PTS、東証価格。
//   - error: 列数、必須文字列、倍率、任意数値が不正な場合のエラー。
func parseADRRow(row string) (ADRQuote, error) {
	fields := strings.Split(row, "_")
	if len(fields) != 24 {
		return ADRQuote{}, fmt.Errorf("列数が24ではありません: %d", len(fields))
	}
	for _, index := range []int{0, 1, 2, 3, 4, 5} {
		if fields[index] == "" {
			return ADRQuote{}, fmt.Errorf("必須文字列の列%dが空です", index)
		}
	}
	conversionRatio, err := parseFiniteNumber(fields[6], "円換算倍率")
	if err != nil || conversionRatio <= 0 {
		return ADRQuote{}, fmt.Errorf("円換算倍率が正の有限数値ではありません")
	}
	tokyoPrice, err := parseOptionalFiniteNumber(fields[8], "東証価格")
	if err != nil {
		return ADRQuote{}, err
	}
	tokyoChange, err := parseOptionalFiniteNumber(fields[9], "東証前日比")
	if err != nil {
		return ADRQuote{}, err
	}
	tokyoChangePercent, err := parseOptionalPercentNumber(fields[10], "東証前日比率")
	if err != nil {
		return ADRQuote{}, err
	}
	adrPrice, err := parseOptionalFiniteNumber(fields[13], "ADR価格")
	if err != nil {
		return ADRQuote{}, err
	}
	adrChange, err := parseOptionalFiniteNumber(fields[14], "ADR前日比")
	if err != nil {
		return ADRQuote{}, err
	}
	adrChangePercent, err := parseOptionalPercentNumber(fields[15], "ADR前日比率")
	if err != nil {
		return ADRQuote{}, err
	}
	adrVolume, err := parseOptionalFiniteNumber(fields[16], "ADR出来高")
	if err != nil {
		return ADRQuote{}, err
	}
	adrYen, err := parseOptionalFiniteNumber(fields[17], "ADR円換算")
	if err != nil {
		return ADRQuote{}, err
	}
	usdJPY, err := parseOptionalFiniteNumber(fields[18], "USDJPY")
	if err != nil {
		return ADRQuote{}, err
	}
	ptsPrice, err := parseOptionalFiniteNumber(fields[20], "PTS価格")
	if err != nil {
		return ADRQuote{}, err
	}
	ptsVolume, err := parseOptionalFiniteNumber(fields[21], "PTS出来高")
	if err != nil {
		return ADRQuote{}, err
	}
	for field, value := range map[string]*float64{
		"ADR出来高": adrVolume,
		"PTS出来高": ptsVolume,
	} {
		if value != nil && *value < 0 {
			return ADRQuote{}, fmt.Errorf("%sが負数です", field)
		}
	}
	if fields[22] != "S" && fields[22] != "U" {
		return ADRQuote{}, fmt.Errorf("スポンサー区分がSまたはUではありません: %q", fields[22])
	}
	quote := ADRQuote{
		Code:               fields[0],
		ADRSymbol:          fields[1],
		Name:               fields[2],
		IndustryCode:       fields[3],
		EnglishName:        fields[4],
		Market:             fields[5],
		ConversionRatio:    conversionRatio,
		TokyoDate:          fields[7],
		TokyoPrice:         tokyoPrice,
		TokyoChange:        tokyoChange,
		TokyoChangePercent: tokyoChangePercent,
		TokyoStatus:        fields[11],
		ADRMarketTime:      fields[12],
		ADRPrice:           adrPrice,
		ADRChange:          adrChange,
		ADRChangePercent:   adrChangePercent,
		ADRVolume:          adrVolume,
		ADRYen:             adrYen,
		USDJPY:             usdJPY,
		PTSMarketTime:      fields[19],
		PTSPrice:           ptsPrice,
		PTSVolume:          ptsVolume,
		Sponsorship:        fields[22],
		DisplayFlag:        fields[23],
	}
	quote.ADRVsTokyoPercent = calculateRelativePercent(adrYen, tokyoPrice)
	quote.PTSVsTokyoPercent = calculateRelativePercent(ptsPrice, tokyoPrice)
	return quote, nil
}

// calculateRelativePercent は、比較値と基準値から差率を算出します。
//
// 引数:
//   - value *float64: 比較する値。nilの場合は算出しない。
//   - base *float64: 基準値。nilまたは0の場合は算出しない。
//
// 返り値:
//   - *float64: (value-base)/base*100の有限値。算出不能の場合はnil。
func calculateRelativePercent(value *float64, base *float64) *float64 {
	if value == nil || base == nil || *base == 0 {
		return nil
	}
	result := (*value - *base) / *base * 100
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return nil
	}
	return &result
}
