package nikkei225jp

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CryptoAsset は、仮想通貨一覧に含まれる1銘柄分の数値を表します。
//
// 主な特徴:
//   - 国内取扱、時価総額、価格、各期間の変化率は空欄の場合にnilで保持する
//   - MarketCapHundredMillionJPYの単位は配信元の表示に合わせた億円とする
//   - 文字列項目はJavaScriptとして評価せず配信値をそのまま保持する
type CryptoAsset struct {
	Code                       string   `json:"code"`
	Symbol                     string   `json:"symbol"`
	NameJapanese               string   `json:"name_ja"`
	NameEnglish                string   `json:"name_en"`
	AvailableInJapan           *bool    `json:"available_in_japan"`
	MarketCapHundredMillionJPY *float64 `json:"market_cap_100m_jpy"`
	PriceJPY                   *float64 `json:"price_jpy"`
	Change24HoursPercent       *float64 `json:"change_24h_percent"`
	ChangeOneWeekPercent       *float64 `json:"change_1w_percent"`
	ChangeThreeMonthsPercent   *float64 `json:"change_3m_percent"`
	ChangeOneYearPercent       *float64 `json:"change_1y_percent"`
}

// CryptoAssetData は、仮想通貨一覧の全銘柄と配信元の更新表示を表します。
//
// 主な特徴:
//   - Assetsは銘柄コードの数値順で格納する
//   - CoinCountは配信元の宣言値と解析済み銘柄数が一致した場合だけ保持する
//   - LastModifiedはLastModCoin変数の原文を保持する
type CryptoAssetData struct {
	LastModified string        `json:"last_modified"`
	CoinCount    int           `json:"coin_count"`
	Assets       []CryptoAsset `json:"assets"`
}

// ----------------------------------------

var compactCurrentLinePattern = regexp.MustCompile(`(?m)^A\[(\d+)\]="([^"\r\n]*)";\r?$`)

var cryptoAssetDocumentPattern = regexp.MustCompile(
	`(?s)^\s*var\s+CO\s*=\s*\[\s*\]\s*;\s*(.*?)\s*` +
		`var\s+LastModCoin\s*=\s*"([^"\r\n]*)"\s*;\s*` +
		`var\s+Coincount\s*=\s*(\d+)\s*;\s*$`,
)

var cryptoAssetAssignmentPattern = regexp.MustCompile(`CO\[(\d+)\]\s*=\s*"([^"\r\n]*)"\s*;`)

// ----------------------------------------

// parseCompactCurrent は、A[code]形式の5項目JavaScript代入を現在値へ変換します。
//
// 引数:
//   - body []byte: ajax_fx_table.jsとして配信されたJavaScript本文。
//
// 返り値:
//   - []CurrentQuote: 銘柄コードの数値順に並べた現在値。高値と安値はnil。
//   - error: 行形式、列数、数値、重複コードに異常がある場合のエラー。
func parseCompactCurrent(body []byte) ([]CurrentQuote, error) {
	matches := compactCurrentLinePattern.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, errors.New("簡易現在値の代入行がありません")
	}
	if remainder := strings.TrimSpace(string(compactCurrentLinePattern.ReplaceAll(body, nil))); remainder != "" {
		return nil, errors.New("未対応のJavaScript記述が含まれています")
	}

	quotes := make([]CurrentQuote, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		code := string(match[1])
		if _, exists := seen[code]; exists {
			return nil, fmt.Errorf("銘柄コード%sが重複しています", code)
		}
		seen[code] = struct{}{}

		fields := strings.Split(string(match[2]), "_")
		if len(fields) != 5 {
			return nil, fmt.Errorf("銘柄コード%sの列数が%dです", code, len(fields))
		}

		value, err := parseOptionalFloat(fields[0])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの現在値が不正です: %w", code, err)
		}
		change, err := parseOptionalFloat(fields[1])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの前日比が不正です: %w", code, err)
		}
		changePercent, err := parseOptionalFloat(fields[2])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの騰落率が不正です: %w", code, err)
		}
		displayStatus, err := parseOptionalInt(fields[4])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%sの表示状態が不正です: %w", code, err)
		}

		quotes = append(quotes, CurrentQuote{
			Code:          code,
			Name:          InstrumentName(code),
			Value:         value,
			Change:        change,
			ChangePercent: changePercent,
			MarketTime:    fields[3],
			DisplayStatus: displayStatus,
			High:          nil,
			Low:           nil,
		})
	}

	sort.Slice(quotes, func(i, j int) bool {
		return numericCodeLess(quotes[i].Code, quotes[j].Code)
	})
	return quotes, nil
}

// parseCryptoAssets は、CO[code]形式のJavaScript代入と件数情報を仮想通貨一覧へ変換します。
//
// 引数:
//   - body []byte: coin_table_DWMY.jsとして配信されたJavaScript本文。
//
// 返り値:
//   - CryptoAssetData: 銘柄コード順の仮想通貨一覧、更新表示、宣言件数。
//   - error: 文書構造、列数、数値、フラグ、重複コード、件数整合に異常がある場合のエラー。
func parseCryptoAssets(body []byte) (CryptoAssetData, error) {
	documentMatch := cryptoAssetDocumentPattern.FindSubmatch(body)
	if documentMatch == nil {
		return CryptoAssetData{}, errors.New("仮想通貨一覧の文書構造が不正です")
	}

	assignmentBody := documentMatch[1]
	matches := cryptoAssetAssignmentPattern.FindAllSubmatch(assignmentBody, -1)
	if len(matches) == 0 {
		return CryptoAssetData{}, errors.New("仮想通貨一覧の代入行がありません")
	}
	if remainder := strings.TrimSpace(string(cryptoAssetAssignmentPattern.ReplaceAll(assignmentBody, nil))); remainder != "" {
		return CryptoAssetData{}, errors.New("未対応のJavaScript記述が含まれています")
	}

	declaredCount, err := strconv.Atoi(string(documentMatch[3]))
	if err != nil {
		return CryptoAssetData{}, fmt.Errorf("仮想通貨の宣言件数が不正です: %w", err)
	}

	assets := make([]CryptoAsset, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		asset, err := parseCryptoAsset(string(match[1]), string(match[2]))
		if err != nil {
			return CryptoAssetData{}, err
		}
		if _, exists := seen[asset.Code]; exists {
			return CryptoAssetData{}, fmt.Errorf("仮想通貨コード%sが重複しています", asset.Code)
		}
		seen[asset.Code] = struct{}{}
		assets = append(assets, asset)
	}

	if declaredCount != len(assets) {
		return CryptoAssetData{}, fmt.Errorf(
			"仮想通貨の宣言件数%dと解析件数%dが一致しません",
			declaredCount,
			len(assets),
		)
	}

	sort.Slice(assets, func(i, j int) bool {
		return numericCodeLess(assets[i].Code, assets[j].Code)
	})
	return CryptoAssetData{
		LastModified: string(documentMatch[2]),
		CoinCount:    declaredCount,
		Assets:       assets,
	}, nil
}

// parseCryptoAsset は、1件分のアンダースコア区切り値を型付きの仮想通貨情報へ変換します。
//
// 引数:
//   - code string: サイト内部の仮想通貨コード。
//   - raw string: 末尾区切りを含む10項目の配信値。
//
// 返り値:
//   - CryptoAsset: 文字列、国内取扱、有限数へ正規化した仮想通貨情報。
//   - error: 列数、必須文字列、フラグ、数値のいずれかが不正な場合のエラー。
func parseCryptoAsset(code string, raw string) (CryptoAsset, error) {
	fields := strings.Split(raw, "_")
	if len(fields) != 11 || fields[10] != "" {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの列数または末尾区切りが不正です", code)
	}
	if fields[0] == "" || fields[1] == "" || fields[2] == "" {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの名称項目が空です", code)
	}

	availableInJapan, err := parseOptionalBooleanFlag(fields[3])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの国内取扱フラグが不正です: %w", code, err)
	}
	marketCap, err := parseOptionalFloat(fields[4])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの時価総額が不正です: %w", code, err)
	}
	price, err := parseOptionalFloat(fields[5])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの価格が不正です: %w", code, err)
	}
	change24Hours, err := parseOptionalFloat(fields[6])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの24時間変化率が不正です: %w", code, err)
	}
	changeOneWeek, err := parseOptionalFloat(fields[7])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの1週間変化率が不正です: %w", code, err)
	}
	changeThreeMonths, err := parseOptionalFloat(fields[8])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの3か月変化率が不正です: %w", code, err)
	}
	changeOneYear, err := parseOptionalFloat(fields[9])
	if err != nil {
		return CryptoAsset{}, fmt.Errorf("仮想通貨コード%sの1年変化率が不正です: %w", code, err)
	}

	return CryptoAsset{
		Code:                       code,
		Symbol:                     fields[0],
		NameJapanese:               fields[1],
		NameEnglish:                fields[2],
		AvailableInJapan:           availableInJapan,
		MarketCapHundredMillionJPY: marketCap,
		PriceJPY:                   price,
		Change24HoursPercent:       change24Hours,
		ChangeOneWeekPercent:       changeOneWeek,
		ChangeThreeMonthsPercent:   changeThreeMonths,
		ChangeOneYearPercent:       changeOneYear,
	}, nil
}

// parseOptionalBooleanFlag は、空欄、0、1だけを任意の真偽値として解析します。
//
// 引数:
//   - raw string: 空文字、0、1のいずれか。
//
// 返り値:
//   - *bool: 空欄ならnil、0ならfalse、1ならtrueへのポインター。
//   - error: 定義外のフラグ値だった場合のエラー。
func parseOptionalBooleanFlag(raw string) (*bool, error) {
	if raw == "" {
		return nil, nil
	}
	var value bool
	switch raw {
	case "0":
		value = false
	case "1":
		value = true
	default:
		return nil, fmt.Errorf("0または1ではありません: %s", raw)
	}
	return &value, nil
}

// numericCodeLess は、数字だけのコードを数値順で比較し、解析不能時は文字列順で比較します。
//
// 引数:
//   - left string: 左辺のサイト内部コード。
//   - right string: 右辺のサイト内部コード。
//
// 返り値:
//   - bool: leftをrightより前へ並べる場合はtrue。
func numericCodeLess(left, right string) bool {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	if leftErr != nil || rightErr != nil {
		return left < right
	}
	return leftNumber < rightNumber
}
