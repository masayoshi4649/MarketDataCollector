package nikkei225jp

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const marketAbsoluteTimestampThreshold = int64(1_000_000_000_000)

// ----------------------------------------

// parseMarketIntradayChart は、市場別のDATAm配信を銘柄別の時系列へ変換します。
//
// 先頭行の第1列は絶対Unixミリ秒として扱います。2行目以降は、
// 1,000,000,000,000以上なら絶対Unixミリ秒、未満の正整数なら
// 直前時刻に対する差分として10,000ミリ秒を乗じて復元します。
// 疎な値列を許可し、columnCodesで指定された順序を返り値でも維持します。
// suffixNamesに明示された末尾の文字列代入だけを追加情報として許可します。
//
// 引数:
//   - body []byte: var DATAm代入と任意の許可済み文字列代入を含むJavaScript本文。
//   - columnCodes []string: DATAmの第2列以降へ順番に対応させる系列コード。
//   - suffixNames []string: DATAmの後で文字列代入を許可する変数名。
//
// 返り値:
//   - []ChartSeries: columnCodesと同じ順序で並ぶ市場内時系列。
//   - map[string]string: 実際に存在した許可済み末尾変数と文字列値。
//   - error: 定義、JavaScript構文、列数、時刻、数値に異常がある場合のエラー。
func parseMarketIntradayChart(
	body []byte,
	columnCodes []string,
	suffixNames []string,
) ([]ChartSeries, map[string]string, error) {
	allowedSuffixes, err := validateMarketChartDefinitions(columnCodes, suffixNames)
	if err != nil {
		return nil, nil, err
	}

	// ----------------------------------------

	parser := newChartScriptParser(body)
	if err := parser.expectKeyword("var"); err != nil {
		return nil, nil, err
	}
	identifier, err := parser.parseIdentifier()
	if err != nil {
		return nil, nil, err
	}
	if identifier != "DATAm" {
		return nil, nil, fmt.Errorf("想定外の配列名です: %s", identifier)
	}
	if err := parser.expectByte('='); err != nil {
		return nil, nil, err
	}
	rows, err := parser.parseMatrix()
	if err != nil {
		return nil, nil, err
	}
	if err := parser.finishAssignment(false); err != nil {
		return nil, nil, err
	}

	// ----------------------------------------

	suffixes, err := parseMarketSuffixAssignments(parser, allowedSuffixes)
	if err != nil {
		return nil, nil, err
	}
	series, err := marketSeriesFromRows(rows, columnCodes)
	if err != nil {
		return nil, nil, err
	}
	return series, suffixes, nil
}

// ----------------------------------------

// validateMarketChartDefinitions は、列コードと末尾変数の呼び出し側定義を検証します。
//
// 引数:
//   - columnCodes []string: DATAmの値列へ対応させる系列コード。
//   - suffixNames []string: 末尾で文字列代入を許可するJavaScript識別子。
//
// 返り値:
//   - map[string]struct{}: 重複を除いた許可済み末尾変数名の集合。
//   - error: 空や重複した系列コード、不正や重複した末尾変数名がある場合のエラー。
func validateMarketChartDefinitions(
	columnCodes []string,
	suffixNames []string,
) (map[string]struct{}, error) {
	if len(columnCodes) == 0 {
		return nil, errors.New("市場チャートの列コードがありません")
	}
	if len(columnCodes) > maxChartColumns-1 {
		return nil, fmt.Errorf("市場チャートの列コード数が上限%dを超えました", maxChartColumns-1)
	}

	seenCodes := make(map[string]struct{}, len(columnCodes))
	for _, code := range columnCodes {
		if code == "" || strings.TrimSpace(code) != code {
			return nil, fmt.Errorf("不正な市場チャート列コードです: %q", code)
		}
		if _, exists := seenCodes[code]; exists {
			return nil, fmt.Errorf("市場チャート列コードが重複しています: %s", code)
		}
		seenCodes[code] = struct{}{}
	}

	allowedSuffixes := make(map[string]struct{}, len(suffixNames))
	for _, name := range suffixNames {
		if !isMarketIdentifier(name) {
			return nil, fmt.Errorf("不正な末尾変数名です: %q", name)
		}
		if _, exists := allowedSuffixes[name]; exists {
			return nil, fmt.Errorf("末尾変数名が重複しています: %s", name)
		}
		allowedSuffixes[name] = struct{}{}
	}
	return allowedSuffixes, nil
}

// isMarketIdentifier は、文字列が限定パーサーで許可するASCII識別子か確認します。
//
// 引数:
//   - name string: 検証する末尾変数名。
//
// 返り値:
//   - bool: 先頭と継続文字がJavaScript識別子の限定規則に一致する場合はtrue。
func isMarketIdentifier(name string) bool {
	if name == "" || !isIdentifierStart(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !isIdentifierPart(name[index]) {
			return false
		}
	}
	return true
}

// marketSeriesFromRows は、疎な数値行列を定義順の時系列へ変換します。
//
// 2行目以降の時刻値は、1,000,000,000,000以上なら絶対Unixミリ秒、
// 未満なら10,000ミリ秒単位の差分として解釈します。どちらの形式でも
// 行全体の時刻が非降順であることを検証します。
//
// 引数:
//   - rows [][]*float64: 第1列に時刻、第2列以降に疎な系列値を持つ行列。
//   - columnCodes []string: 値列へ順番に対応させる系列コード。
//
// 返り値:
//   - []ChartSeries: columnCodesの順序を維持した時系列。
//   - error: 行、列、時刻、値が不正な場合のエラー。
func marketSeriesFromRows(
	rows [][]*float64,
	columnCodes []string,
) ([]ChartSeries, error) {
	if len(rows) == 0 {
		return nil, errors.New("DATAmに行がありません")
	}

	series := make([]ChartSeries, len(columnCodes))
	for index, code := range columnCodes {
		series[index] = ChartSeries{
			Code:   code,
			Name:   InstrumentName(code),
			Points: make([]ChartPoint, 0),
		}
	}

	var timestamp int64
	for rowIndex, row := range rows {
		if len(row) == 0 || len(row) > len(columnCodes)+1 {
			return nil, fmt.Errorf("DATAmの%d行目の列数が%dです", rowIndex+1, len(row))
		}

		rawTimestamp, err := requiredPositiveInteger(row[0], "時刻")
		if err != nil {
			return nil, fmt.Errorf("DATAmの%d行目の時刻が不正です: %w", rowIndex+1, err)
		}
		previousTimestamp := timestamp
		if rowIndex == 0 || rawTimestamp >= marketAbsoluteTimestampThreshold {
			timestamp = rawTimestamp
		} else {
			timestamp, err = addTimestampDelta(timestamp, rawTimestamp)
			if err != nil {
				return nil, fmt.Errorf("DATAmの%d行目の時刻差分が不正です: %w", rowIndex+1, err)
			}
		}
		if rowIndex > 0 && timestamp < previousTimestamp {
			return nil, fmt.Errorf("DATAmの%d行目の時刻が非降順ではありません", rowIndex+1)
		}

		for columnIndex := 1; columnIndex < len(row); columnIndex++ {
			if row[columnIndex] == nil {
				continue
			}
			points, appendErr := appendChartPoint(
				series[columnIndex-1].Points,
				timestamp,
				*row[columnIndex],
			)
			if appendErr != nil {
				return nil, fmt.Errorf("系列%s: %w", columnCodes[columnIndex-1], appendErr)
			}
			series[columnIndex-1].Points = points
		}
	}
	return series, nil
}

// ----------------------------------------

// parseMarketSuffixAssignments は、DATAm後方の許可済み文字列代入を解析します。
//
// 引数:
//   - parser *chartScriptParser: DATAm代入直後を指す限定JavaScriptパーサー。
//   - allowedNames map[string]struct{}: 文字列代入を許可する変数名の集合。
//
// 返り値:
//   - map[string]string: 本文に存在した許可済み変数と文字列値。
//   - error: 未知変数、重複、式、余分なJavaScriptがある場合のエラー。
func parseMarketSuffixAssignments(
	parser *chartScriptParser,
	allowedNames map[string]struct{},
) (map[string]string, error) {
	values := make(map[string]string)
	parser.skipWhitespace()
	if parser.atEnd() {
		return values, nil
	}
	if len(allowedNames) == 0 {
		return nil, parser.errorf("余分なJavaScript記述があります")
	}
	if err := parser.expectKeyword("var"); err != nil {
		return nil, err
	}

	for {
		name, err := parser.parseIdentifier()
		if err != nil {
			return nil, err
		}
		if _, allowed := allowedNames[name]; !allowed {
			return nil, fmt.Errorf("想定外の末尾変数名です: %s", name)
		}
		if _, exists := values[name]; exists {
			return nil, fmt.Errorf("末尾変数%sの代入が重複しています", name)
		}
		if err := parser.expectByte('='); err != nil {
			return nil, err
		}
		value, err := parser.parseMarketQuotedString()
		if err != nil {
			return nil, err
		}
		values[name] = value

		parser.skipWhitespace()
		if parser.consumeByte(';') {
			break
		}
		if !parser.consumeByte(',') {
			return nil, parser.errorf("末尾文字列代入の後にカンマまたはセミコロンが必要です")
		}
	}

	parser.skipWhitespace()
	if !parser.atEnd() {
		return nil, parser.errorf("余分なJavaScript記述があります")
	}
	return values, nil
}

// parseMarketQuotedString は、単一または二重引用符付きの限定文字列を解析します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - string: 基本エスケープとUnicodeエスケープを復元した文字列。
//   - error: 引用符、エスケープ、UTF-8、サロゲートペアが不正な場合のエラー。
func (p *chartScriptParser) parseMarketQuotedString() (string, error) {
	p.skipWhitespace()
	quote := p.peekByte()
	if quote == '"' {
		return p.parseString()
	}
	if quote != '\'' {
		return "", p.errorf("単一または二重引用符付き文字列が必要です")
	}
	p.position++

	var builder strings.Builder
	for !p.atEnd() {
		current := p.body[p.position]
		p.position++
		if current == quote {
			value := builder.String()
			if !utf8.ValidString(value) {
				return "", p.errorf("文字列がUTF-8ではありません")
			}
			return value, nil
		}
		if current < 0x20 {
			return "", p.errorf("文字列に制御文字があります")
		}
		if current != '\\' {
			builder.WriteByte(current)
			continue
		}
		if p.atEnd() {
			return "", p.errorf("文字列のエスケープが閉じていません")
		}
		escaped := p.body[p.position]
		p.position++
		switch escaped {
		case '\'', '"', '\\', '/':
			builder.WriteByte(escaped)
		case 'b':
			builder.WriteByte('\b')
		case 'f':
			builder.WriteByte('\f')
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		case 't':
			builder.WriteByte('\t')
		case 'u':
			decoded, err := p.parseMarketUnicodeEscape()
			if err != nil {
				return "", err
			}
			builder.WriteRune(decoded)
		default:
			return "", p.errorf("未対応の文字列エスケープです: \\%c", escaped)
		}
	}
	return "", p.errorf("文字列が閉じていません")
}

// parseMarketUnicodeEscape は、\uに続くUnicodeコード単位を解析します。
//
// 高位サロゲートの場合は、直後の\u低位サロゲートと組み合わせます。
// 単独の低位サロゲートと不完全なペアは拒否します。
//
// 引数:
//   - なし。パーサー位置は最初の16進数字を指しているものとする。
//
// 返り値:
//   - rune: 検証とサロゲート復元を終えたUnicodeコードポイント。
//   - error: 16進表記またはサロゲートペアが不正な場合のエラー。
func (p *chartScriptParser) parseMarketUnicodeEscape() (rune, error) {
	first, err := p.parseMarketHexUnit()
	if err != nil {
		return 0, err
	}
	firstRune := rune(first)
	if firstRune < 0xd800 || firstRune > 0xdfff {
		return firstRune, nil
	}
	if firstRune >= 0xdc00 {
		return 0, p.errorf("単独の低位サロゲートです")
	}
	if p.position+2 > len(p.body) || p.body[p.position] != '\\' || p.body[p.position+1] != 'u' {
		return 0, p.errorf("高位サロゲートに低位サロゲートが続いていません")
	}
	p.position += 2
	second, err := p.parseMarketHexUnit()
	if err != nil {
		return 0, err
	}
	secondRune := rune(second)
	if secondRune < 0xdc00 || secondRune > 0xdfff {
		return 0, p.errorf("低位サロゲートが不正です")
	}
	return utf16.DecodeRune(firstRune, secondRune), nil
}

// parseMarketHexUnit は、4桁の16進数をUnicodeコード単位へ変換します。
//
// 引数:
//   - なし。パーサー位置は最初の16進数字を指しているものとする。
//
// 返り値:
//   - uint16: 解析したUnicodeコード単位。
//   - error: 4桁未満または16進数字以外を含む場合のエラー。
func (p *chartScriptParser) parseMarketHexUnit() (uint16, error) {
	if p.position+4 > len(p.body) {
		return 0, p.errorf("Unicodeエスケープが4桁ありません")
	}
	var value uint16
	for count := 0; count < 4; count++ {
		current := p.body[p.position]
		p.position++
		var digit byte
		switch {
		case current >= '0' && current <= '9':
			digit = current - '0'
		case current >= 'a' && current <= 'f':
			digit = current - 'a' + 10
		case current >= 'A' && current <= 'F':
			digit = current - 'A' + 10
		default:
			return 0, p.errorf("Unicodeエスケープに16進数字以外があります")
		}
		value = value*16 + uint16(digit)
	}
	return value, nil
}
