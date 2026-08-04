package nikkei225jp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	chart60MinutesPath = "/_data/_nfsDATA/hs_data/hs_tick2.json"
	chart6HoursPath    = "/_data/_nfsDATA/hs_data/hs_nk_s3.json"
	chart1DayPath      = "/_data/_nfsDATA/hs_data/hs_TOP4.json"
	chart6MonthsFormat = "/_data/_nfsDATA/HS_DATA_DAY/S%s.json"

	maxChartRows    = 250000
	maxChartColumns = 64
)

var chart1DayColumnCodes = []string{
	"511",
	"111",
	"191",
	"413",
	"732",
	"211",
	"731",
	"621",
	"921",
	"1001",
}

var chart1DaySinglePaths = map[string]string{
	"111": "/_data/_nfsDATA/json/111_24min.json",
	"151": "/_data/_nfsDATA/json/151_24.json",
	"643": "/_data/_nfsDATA/json/643_24.json",
	"811": "/_data/_nfsDATA/json/811_24.json",
}

var chart6MonthsCodes = map[string]struct{}{
	"111":  {},
	"211":  {},
	"321":  {},
	"413":  {},
	"511":  {},
	"514":  {},
	"523":  {},
	"621":  {},
	"921":  {},
	"1001": {},
}

var chartMetadataNames = []string{
	"Bdata",
	"Max",
	"Min",
	"Ldata",
	"Per",
	"Time",
	"STtime",
	"Start",
	"End",
	"opF",
	"Pline",
	"Area",
	"Rang",
	"Keta",
}

type chartScriptParser struct {
	body     []byte
	position int
}

// ----------------------------------------

// FetchChart は、指定期間のチャートを内部数値配信から取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - chartRange ChartRange: 取得するチャート期間。
//   - codes []string: 出力対象の銘柄コード。空の場合は期間ごとの既定対象。
//
// 返り値:
//   - ChartData: 期間、取得元、銘柄別系列を含むチャートデータ。
//   - error: 期間、コード、通信、本文形式のいずれかに異常がある場合のエラー。
func (c *Client) FetchChart(
	ctx context.Context,
	chartRange ChartRange,
	codes []string,
) (ChartData, error) {
	selectedCodes, err := normalizeChartCodes(codes)
	if err != nil {
		return ChartData{}, err
	}
	if err := validateChartSelection(chartRange, selectedCodes); err != nil {
		return ChartData{}, err
	}

	release, err := c.acquireRequestSlot(ctx)
	if err != nil {
		return ChartData{}, err
	}
	defer release()

	switch chartRange {
	case ChartRange60Minutes:
		return c.fetch60MinutesChartLocked(ctx, selectedCodes)
	case ChartRange6Hours:
		return c.fetch6HoursChartLocked(ctx, selectedCodes)
	case ChartRange1Day:
		return c.fetch1DayChartLocked(ctx, selectedCodes)
	case ChartRange6Months:
		return c.fetch6MonthsChartLocked(ctx, selectedCodes)
	default:
		return ChartData{}, fmt.Errorf("未対応のチャート期間です: %q", chartRange)
	}
}

// normalizeChartCodes は、指定コードを検証して重複のない数値順へ整えます。
//
// 引数:
//   - codes []string: 利用者が指定した銘柄コード。
//
// 返り値:
//   - []string: 重複を除いて数値順に並べたコード。未指定の場合はnil。
//   - error: 空文字または数字以外を含むコードがある場合のエラー。
func normalizeChartCodes(codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	unique := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if !isDecimalCode(code) {
			return nil, newInputError(fmt.Sprintf("不正なチャート銘柄コードです: %q", code))
		}
		unique[code] = struct{}{}
	}

	normalized := make([]string, 0, len(unique))
	for code := range unique {
		normalized = append(normalized, code)
	}
	sortCodes(normalized)
	return normalized, nil
}

// isDecimalCode は、文字列がASCII数字だけで構成されているか確認します。
//
// 引数:
//   - code string: 検証する銘柄コード。
//
// 返り値:
//   - bool: 1文字以上のASCII数字だけで構成される場合はtrue。
func isDecimalCode(code string) bool {
	if code == "" {
		return false
	}
	for index := 0; index < len(code); index++ {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	return true
}

// sortCodes は、銘柄コードを数値として比較して昇順に並べます。
//
// 引数:
//   - codes []string: 並べ替える銘柄コード。
//
// 返り値:
//   - なし。引数のスライスを直接更新する。
func sortCodes(codes []string) {
	sort.Slice(codes, func(leftIndex, rightIndex int) bool {
		left, leftErr := strconv.ParseUint(codes[leftIndex], 10, 64)
		right, rightErr := strconv.ParseUint(codes[rightIndex], 10, 64)
		if leftErr != nil || rightErr != nil {
			return codes[leftIndex] < codes[rightIndex]
		}
		return left < right
	})
}

// validateChartSelection は、期間ごとに事前判定できるコード制約を検証します。
//
// 引数:
//   - chartRange ChartRange: 取得するチャート期間。
//   - codes []string: 正規化済みの指定コード。
//
// 返り値:
//   - error: 未対応期間、1日複合データにないコード、長期許可対象外コードのエラー。
func validateChartSelection(chartRange ChartRange, codes []string) error {
	switch chartRange {
	case ChartRange60Minutes, ChartRange6Hours:
		return nil
	case ChartRange1Day:
		if len(codes) == 1 {
			if _, optimized := chart1DaySinglePaths[codes[0]]; optimized {
				return nil
			}
		}
		available := make(map[string]struct{}, len(chart1DayColumnCodes))
		for _, code := range chart1DayColumnCodes {
			available[code] = struct{}{}
		}
		for _, code := range codes {
			if _, exists := available[code]; !exists {
				return fmt.Errorf("1日チャートに指定コードがありません: %s", code)
			}
		}
		return nil
	case ChartRange6Months:
		for _, code := range codes {
			if _, allowed := chart6MonthsCodes[code]; !allowed {
				return fmt.Errorf("6か月チャートで許可されていないコードです: %s", code)
			}
		}
		return nil
	default:
		return fmt.Errorf("未対応のチャート期間です: %q", chartRange)
	}
}

// ----------------------------------------

// fetch60MinutesChartLocked は、60分画面用のティックデータを取得して解析します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - codes []string: 正規化済みの出力対象コード。nilの場合は全件。
//
// 返り値:
//   - ChartData: 60分チャートの取得元と銘柄別系列。
//   - error: 通信、本文解析、コード絞り込みに失敗した場合のエラー。
func (c *Client) fetch60MinutesChartLocked(
	ctx context.Context,
	codes []string,
) (ChartData, error) {
	body, metadata, err := c.fetchChartResourceLocked(
		ctx,
		chart60MinutesPath,
		"60分チャート",
	)
	if err != nil {
		return ChartData{}, err
	}
	series, err := parse60MinutesChart(body)
	if err != nil {
		return ChartData{}, fmt.Errorf("60分チャート本文を解析できません: %w", err)
	}
	series, err = filterChartSeries(series, codes)
	if err != nil {
		return ChartData{}, err
	}
	return ChartData{
		Range:   ChartRange60Minutes,
		Sources: []ResponseMetadata{metadata},
		Series:  series,
	}, nil
}

// fetch6HoursChartLocked は、6時間画面用の銘柄別データを取得して解析します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - codes []string: 正規化済みの出力対象コード。nilの場合は全件。
//
// 返り値:
//   - ChartData: 6時間チャートの取得元と銘柄別系列。
//   - error: 通信、本文解析、コード絞り込みに失敗した場合のエラー。
func (c *Client) fetch6HoursChartLocked(
	ctx context.Context,
	codes []string,
) (ChartData, error) {
	body, metadata, err := c.fetchChartResourceLocked(
		ctx,
		chart6HoursPath,
		"6時間チャート",
	)
	if err != nil {
		return ChartData{}, err
	}
	series, err := parseAssignedSeriesChart(body)
	if err != nil {
		return ChartData{}, fmt.Errorf("6時間チャート本文を解析できません: %w", err)
	}
	series, err = filterChartSeries(series, codes)
	if err != nil {
		return ChartData{}, err
	}
	return ChartData{
		Range:   ChartRange6Hours,
		Sources: []ResponseMetadata{metadata},
		Series:  series,
	}, nil
}

// fetch1DayChartLocked は、当日の複合データまたは単一銘柄用小容量データを取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - codes []string: 正規化済みの出力対象コード。nilの場合は複合全件。
//
// 返り値:
//   - ChartData: 1日チャートの取得元と銘柄別系列。
//   - error: 通信、本文解析、コード絞り込みに失敗した場合のエラー。
func (c *Client) fetch1DayChartLocked(
	ctx context.Context,
	codes []string,
) (ChartData, error) {
	if len(codes) == 1 {
		if resourcePath, optimized := chart1DaySinglePaths[codes[0]]; optimized {
			return c.fetch1DaySingleChartLocked(ctx, codes[0], resourcePath)
		}
	}

	body, metadata, err := c.fetchChartResourceLocked(
		ctx,
		chart1DayPath,
		"1日複合チャート",
	)
	if err != nil {
		return ChartData{}, err
	}
	series, err := parse1DayChart(body)
	if err != nil {
		return ChartData{}, fmt.Errorf("1日複合チャート本文を解析できません: %w", err)
	}
	series, err = filterChartSeries(series, codes)
	if err != nil {
		return ChartData{}, err
	}
	return ChartData{
		Range:   ChartRange1Day,
		Sources: []ResponseMetadata{metadata},
		Series:  series,
	}, nil
}

// fetch1DaySingleChartLocked は、単一銘柄用の小容量な当日データを取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - code string: 取得対象の銘柄コード。
//   - resourcePath string: コードに対応する検証済み固定パス。
//
// 返り値:
//   - ChartData: 1銘柄だけを含む1日チャート。
//   - error: 通信または本文解析に失敗した場合のエラー。
func (c *Client) fetch1DaySingleChartLocked(
	ctx context.Context,
	code string,
	resourcePath string,
) (ChartData, error) {
	body, metadata, err := c.fetchChartResourceLocked(
		ctx,
		resourcePath,
		"1日単一チャート",
	)
	if err != nil {
		return ChartData{}, err
	}
	points, err := parse1DaySingleChart(body)
	if err != nil {
		return ChartData{}, fmt.Errorf("1日単一チャート本文を解析できません: %w", err)
	}
	return ChartData{
		Range:   ChartRange1Day,
		Sources: []ResponseMetadata{metadata},
		Series: []ChartSeries{{
			Code:   code,
			Name:   InstrumentName(code),
			Points: points,
		}},
	}, nil
}

// fetch6MonthsChartLocked は、許可された銘柄の日次データをコードごとに取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - codes []string: 正規化済みの出力対象コード。nilの場合は111だけを取得する。
//
// 返り値:
//   - ChartData: 6か月チャートの取得元一覧と銘柄別系列。
//   - error: 通信、本文解析、配信コード照合に失敗した場合のエラー。
func (c *Client) fetch6MonthsChartLocked(
	ctx context.Context,
	codes []string,
) (ChartData, error) {
	targetCodes := codes
	if len(targetCodes) == 0 {
		targetCodes = []string{"111"}
	}

	result := ChartData{
		Range:   ChartRange6Months,
		Sources: make([]ResponseMetadata, 0, len(targetCodes)),
		Series:  make([]ChartSeries, 0, len(targetCodes)),
	}
	for _, code := range targetCodes {
		resourcePath := fmt.Sprintf(chart6MonthsFormat, code)
		body, metadata, err := c.fetchChartResourceLocked(
			ctx,
			resourcePath,
			"6か月チャート",
		)
		if err != nil {
			return ChartData{}, err
		}
		series, err := parseAssignedSeriesChart(body)
		if err != nil {
			return ChartData{}, fmt.Errorf("6か月チャート本文を解析できません: %w", err)
		}
		if len(series) != 1 || series[0].Code != code {
			return ChartData{}, fmt.Errorf("6か月チャートの配信コードが指定%sと一致しません", code)
		}
		result.Sources = append(result.Sources, metadata)
		result.Series = append(result.Series, series[0])
	}
	return result, nil
}

// fetchChartResourceLocked は、同一ホストのチャート資材を1回取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - resourcePath string: 取得対象の固定パス。
//   - resourceName string: エラー表示に利用する資材名。
//
// 返り値:
//   - []byte: 200応答の本文。
//   - ResponseMetadata: HTTP応答の付帯情報。
//   - error: URL解決またはHTTP取得に失敗した場合のエラー。
func (c *Client) fetchChartResourceLocked(
	ctx context.Context,
	resourcePath string,
	resourceName string,
) ([]byte, ResponseMetadata, error) {
	requestURL, err := c.resolveResourceURL(resourcePath)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	body, metadata, err := c.fetchLocked(
		ctx,
		requestURL,
		resourceName,
		c.maxChartResponseBytes,
	)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	return body, metadata, nil
}

// ----------------------------------------

// parse60MinutesChart は、DATAmのコード・時刻・値の3列を銘柄別系列へ変換します。
//
// 引数:
//   - body []byte: var DATAm形式のJavaScript本文。
//
// 返り値:
//   - []ChartSeries: 銘柄コード順に並べた60分系列。
//   - error: 代入、列数、コード、時刻、値が不正な場合のエラー。
func parse60MinutesChart(body []byte) ([]ChartSeries, error) {
	rows, err := parseSingleMatrixAssignment(body, "DATAm")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("DATAmに行がありません")
	}

	pointsByCode := make(map[string][]ChartPoint)
	for rowIndex, row := range rows {
		if len(row) != 3 {
			return nil, fmt.Errorf("DATAmの%d行目の列数が%dです", rowIndex+1, len(row))
		}
		codeNumber, err := requiredPositiveInteger(row[0], "銘柄コード")
		if err != nil {
			return nil, fmt.Errorf("DATAmの%d行目の銘柄コードが不正です: %w", rowIndex+1, err)
		}
		timestamp, err := requiredPositiveInteger(row[1], "時刻")
		if err != nil {
			return nil, fmt.Errorf("DATAmの%d行目の時刻が不正です: %w", rowIndex+1, err)
		}
		if row[2] == nil {
			return nil, fmt.Errorf("DATAmの%d行目の値がありません", rowIndex+1)
		}

		code := strconv.FormatInt(codeNumber, 10)
		points, err := appendChartPoint(pointsByCode[code], timestamp, *row[2])
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%s: %w", code, err)
		}
		pointsByCode[code] = points
	}
	return chartSeriesFromMap(pointsByCode), nil
}

// parseAssignedSeriesChart は、複数のvar S{code}代入を銘柄別系列へ変換します。
//
// 引数:
//   - body []byte: var S{code}形式のJavaScript本文。
//
// 返り値:
//   - []ChartSeries: 銘柄コード順に並べた系列。
//   - error: 代入名、行形式、時刻、値が不正な場合のエラー。
func parseAssignedSeriesChart(body []byte) ([]ChartSeries, error) {
	assignments, err := parseSeriesAssignments(body)
	if err != nil {
		return nil, err
	}
	pointsByCode := make(map[string][]ChartPoint, len(assignments))
	for code, rows := range assignments {
		points, err := chartPointsFromPairs(rows)
		if err != nil {
			return nil, fmt.Errorf("銘柄コード%s: %w", code, err)
		}
		pointsByCode[code] = points
	}
	return chartSeriesFromMap(pointsByCode), nil
}

// parse1DayChart は、時刻差分と疎列を持つDATAmを銘柄別系列へ変換します。
//
// 引数:
//   - body []byte: var DATAm形式のJavaScript本文。
//
// 返り値:
//   - []ChartSeries: 固定10列を銘柄コード順に並べた当日系列。
//   - error: 時刻差分、列数、値、時刻順が不正な場合のエラー。
func parse1DayChart(body []byte) ([]ChartSeries, error) {
	rows, err := parseSingleMatrixAssignment(body, "DATAm")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("DATAmに行がありません")
	}

	pointsByCode := make(map[string][]ChartPoint, len(chart1DayColumnCodes))
	for _, code := range chart1DayColumnCodes {
		pointsByCode[code] = []ChartPoint{}
	}

	var timestamp int64
	for rowIndex, row := range rows {
		if len(row) == 0 || len(row) > len(chart1DayColumnCodes)+1 {
			return nil, fmt.Errorf("DATAmの%d行目の列数が%dです", rowIndex+1, len(row))
		}
		if rowIndex == 0 {
			timestamp, err = requiredPositiveInteger(row[0], "絶対時刻")
		} else {
			var delta int64
			delta, err = requiredPositiveInteger(row[0], "時刻差分")
			if err == nil {
				timestamp, err = addTimestampDelta(timestamp, delta)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("DATAmの%d行目の時刻が不正です: %w", rowIndex+1, err)
		}

		for columnIndex := 1; columnIndex < len(row); columnIndex++ {
			if row[columnIndex] == nil {
				continue
			}
			code := chart1DayColumnCodes[columnIndex-1]
			points, appendErr := appendChartPoint(
				pointsByCode[code],
				timestamp,
				*row[columnIndex],
			)
			if appendErr != nil {
				return nil, fmt.Errorf("銘柄コード%s: %w", code, appendErr)
			}
			pointsByCode[code] = points
		}
	}
	return chartSeriesFromMap(pointsByCode), nil
}

// parse1DaySingleChart は、許可済みメタ情報とCdataの単一銘柄本文を解析します。
//
// 引数:
//   - body []byte: 固定メタ代入とvar Cdata代入を含むJavaScript本文。
//
// 返り値:
//   - []ChartPoint: 時刻の非降順に並べた当日系列。
//   - error: メタ代入、Cdata、時刻、値が不正な場合のエラー。
func parse1DaySingleChart(body []byte) ([]ChartPoint, error) {
	parser := newChartScriptParser(body)
	if err := parser.parseMetadataAssignments(); err != nil {
		return nil, err
	}
	if err := parser.expectKeyword("var"); err != nil {
		return nil, err
	}
	identifier, err := parser.parseIdentifier()
	if err != nil {
		return nil, err
	}
	if identifier != "Cdata" {
		return nil, fmt.Errorf("想定外の配列名です: %s", identifier)
	}
	if err := parser.expectByte('='); err != nil {
		return nil, err
	}
	rows, err := parser.parseMatrix()
	if err != nil {
		return nil, err
	}
	if err := parser.finishAssignment(true); err != nil {
		return nil, err
	}
	return chartPointsFromPairs(rows)
}

// chartPointsFromPairs は、時刻と値の2列行を時系列ポイントへ変換します。
//
// 引数:
//   - rows [][]*float64: 時刻と値を持つ数値行。
//
// 返り値:
//   - []ChartPoint: 時刻の非降順に並べたポイント。
//   - error: 列数、欠測、時刻順が不正な場合のエラー。
func chartPointsFromPairs(rows [][]*float64) ([]ChartPoint, error) {
	points := make([]ChartPoint, 0, len(rows))
	for rowIndex, row := range rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("%d行目の列数が%dです", rowIndex+1, len(row))
		}
		timestamp, err := requiredPositiveInteger(row[0], "時刻")
		if err != nil {
			return nil, fmt.Errorf("%d行目の時刻が不正です: %w", rowIndex+1, err)
		}
		if row[1] == nil {
			return nil, fmt.Errorf("%d行目の値がありません", rowIndex+1)
		}
		points, err = appendChartPoint(points, timestamp, *row[1])
		if err != nil {
			return nil, fmt.Errorf("%d行目: %w", rowIndex+1, err)
		}
	}
	return points, nil
}

// appendChartPoint は、時刻の非降順を検証してポイントを追加します。
//
// 引数:
//   - points []ChartPoint: 追加前の時系列ポイント。
//   - timestamp int64: 追加するUnixミリ秒。
//   - value float64: 追加する有限数値。
//
// 返り値:
//   - []ChartPoint: 新しいポイントを末尾へ追加したスライス。
//   - error: 値が有限でない場合、または時刻が前の点より古い場合のエラー。
func appendChartPoint(
	points []ChartPoint,
	timestamp int64,
	value float64,
) ([]ChartPoint, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, errors.New("値が有限ではありません")
	}
	if len(points) > 0 && timestamp < points[len(points)-1].TimestampMillis {
		return nil, errors.New("時刻が非降順ではありません")
	}
	return append(points, ChartPoint{TimestampMillis: timestamp, Value: value}), nil
}

// requiredInteger は、必須の有限整数をint64へ変換します。
//
// 引数:
//   - value *float64: 数値行から取得した値。
//   - fieldName string: エラー表示に利用する項目名。
//
// 返り値:
//   - int64: 検証済みの整数。
//   - error: 欠測、小数、int64範囲外の場合のエラー。
func requiredInteger(value *float64, fieldName string) (int64, error) {
	if value == nil {
		return 0, fmt.Errorf("%sがありません", fieldName)
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || math.Trunc(*value) != *value {
		return 0, fmt.Errorf("%sが有限整数ではありません", fieldName)
	}
	const int64UpperBound = float64(uint64(1) << 63)
	if *value < -int64UpperBound || *value >= int64UpperBound {
		return 0, fmt.Errorf("%sがint64の範囲外です", fieldName)
	}
	return int64(*value), nil
}

// requiredPositiveInteger は、必須の正整数をint64へ変換します。
//
// 引数:
//   - value *float64: 数値行から取得した値。
//   - fieldName string: エラー表示に利用する項目名。
//
// 返り値:
//   - int64: 1以上の検証済み整数。
//   - error: 欠測、非整数、0以下、int64範囲外の場合のエラー。
func requiredPositiveInteger(value *float64, fieldName string) (int64, error) {
	integer, err := requiredInteger(value, fieldName)
	if err != nil {
		return 0, err
	}
	if integer <= 0 {
		return 0, fmt.Errorf("%sは1以上にしてください", fieldName)
	}
	return integer, nil
}

// addTimestampDelta は、10秒単位の差分をUnixミリ秒へ安全に加算します。
//
// 引数:
//   - timestamp int64: 差分適用前のUnixミリ秒。
//   - delta int64: 10秒単位の正の差分。
//
// 返り値:
//   - int64: 差分適用後のUnixミリ秒。
//   - error: 乗算または加算がint64を超える場合のエラー。
func addTimestampDelta(timestamp int64, delta int64) (int64, error) {
	const deltaMillis = int64(10000)
	if delta > math.MaxInt64/deltaMillis {
		return 0, errors.New("時刻差分の乗算がint64を超えます")
	}
	increment := delta * deltaMillis
	if timestamp > math.MaxInt64-increment {
		return 0, errors.New("時刻差分の加算がint64を超えます")
	}
	return timestamp + increment, nil
}

// chartSeriesFromMap は、コード別ポイントを表示名付き系列へ変換します。
//
// 引数:
//   - pointsByCode map[string][]ChartPoint: コードをキーにしたポイント一覧。
//
// 返り値:
//   - []ChartSeries: 銘柄コードの数値順に並べた系列。
func chartSeriesFromMap(pointsByCode map[string][]ChartPoint) []ChartSeries {
	codes := make([]string, 0, len(pointsByCode))
	for code := range pointsByCode {
		codes = append(codes, code)
	}
	sortCodes(codes)

	series := make([]ChartSeries, 0, len(codes))
	for _, code := range codes {
		series = append(series, ChartSeries{
			Code:   code,
			Name:   InstrumentName(code),
			Points: pointsByCode[code],
		})
	}
	return series
}

// filterChartSeries は、指定コードだけを系列へ残して欠落指定を検出します。
//
// 引数:
//   - series []ChartSeries: 取得本文に含まれる全系列。
//   - selectedCodes []string: 正規化済みの出力対象コード。nilの場合は全件。
//
// 返り値:
//   - []ChartSeries: 元の数値順を保って絞り込んだ系列。
//   - error: 指定コードが本文に存在しない場合のエラー。
func filterChartSeries(
	series []ChartSeries,
	selectedCodes []string,
) ([]ChartSeries, error) {
	if len(selectedCodes) == 0 {
		return series, nil
	}

	selected := make(map[string]struct{}, len(selectedCodes))
	for _, code := range selectedCodes {
		selected[code] = struct{}{}
	}
	found := make(map[string]struct{}, len(selectedCodes))
	filtered := make([]ChartSeries, 0, len(selectedCodes))
	for _, item := range series {
		if _, exists := selected[item.Code]; exists {
			filtered = append(filtered, item)
			found[item.Code] = struct{}{}
		}
	}
	if len(found) != len(selectedCodes) {
		missing := make([]string, 0, len(selectedCodes)-len(found))
		for _, code := range selectedCodes {
			if _, exists := found[code]; !exists {
				missing = append(missing, code)
			}
		}
		return nil, fmt.Errorf("チャート本文に指定コードがありません: %s", strings.Join(missing, ","))
	}
	return filtered, nil
}

// ----------------------------------------

// newChartScriptParser は、BOMを除いた限定JavaScriptパーサーを生成します。
//
// 引数:
//   - body []byte: 解析するJavaScript本文。
//
// 返り値:
//   - *chartScriptParser: 配列と許可済み代入だけを読むパーサー。
func newChartScriptParser(body []byte) *chartScriptParser {
	if bytes.HasPrefix(body, []byte{0xef, 0xbb, 0xbf}) {
		body = body[3:]
	}
	return &chartScriptParser{body: body}
}

// parseSingleMatrixAssignment は、単一のvar配列代入だけを解析します。
//
// 引数:
//   - body []byte: 解析するJavaScript本文。
//   - expectedName string: 許可する代入先変数名。
//
// 返り値:
//   - [][]*float64: 疎要素をnilで保持した数値行列。
//   - error: 変数名、配列、終端に異常がある場合のエラー。
func parseSingleMatrixAssignment(
	body []byte,
	expectedName string,
) ([][]*float64, error) {
	parser := newChartScriptParser(body)
	if err := parser.expectKeyword("var"); err != nil {
		return nil, err
	}
	identifier, err := parser.parseIdentifier()
	if err != nil {
		return nil, err
	}
	if identifier != expectedName {
		return nil, fmt.Errorf("想定外の配列名です: %s", identifier)
	}
	if err := parser.expectByte('='); err != nil {
		return nil, err
	}
	rows, err := parser.parseMatrix()
	if err != nil {
		return nil, err
	}
	if err := parser.finishAssignment(true); err != nil {
		return nil, err
	}
	return rows, nil
}

// parseSeriesAssignments は、複数のvar S{code}配列代入を解析します。
//
// 引数:
//   - body []byte: 解析するJavaScript本文。
//
// 返り値:
//   - map[string][][]*float64: コードをキーにした数値行列。
//   - error: 変数名、重複、配列、余分な記述に異常がある場合のエラー。
func parseSeriesAssignments(body []byte) (map[string][][]*float64, error) {
	parser := newChartScriptParser(body)
	assignments := make(map[string][][]*float64)
	for {
		parser.skipWhitespace()
		if parser.atEnd() {
			break
		}
		if err := parser.expectKeyword("var"); err != nil {
			return nil, err
		}
		identifier, err := parser.parseIdentifier()
		if err != nil {
			return nil, err
		}
		if len(identifier) < 2 || identifier[0] != 'S' || !isDecimalCode(identifier[1:]) {
			return nil, fmt.Errorf("想定外の系列名です: %s", identifier)
		}
		code := identifier[1:]
		if _, exists := assignments[code]; exists {
			return nil, fmt.Errorf("銘柄コード%sの代入が重複しています", code)
		}
		if err := parser.expectByte('='); err != nil {
			return nil, err
		}
		rows, err := parser.parseMatrix()
		if err != nil {
			return nil, err
		}
		if err := parser.finishAssignment(false); err != nil {
			return nil, err
		}
		assignments[code] = rows
	}
	if len(assignments) == 0 {
		return nil, errors.New("S{code}形式の代入がありません")
	}
	return assignments, nil
}

// parseMetadataAssignments は、単一銘柄本文の固定メタ代入を厳密に読み飛ばします。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - error: メタ名、順序、文字列値、区切りが想定形式と異なる場合のエラー。
func (p *chartScriptParser) parseMetadataAssignments() error {
	if err := p.expectKeyword("var"); err != nil {
		return err
	}
	for index, expectedName := range chartMetadataNames {
		identifier, err := p.parseIdentifier()
		if err != nil {
			return err
		}
		if identifier != expectedName {
			return fmt.Errorf("想定外のメタ名です: %s", identifier)
		}
		if err := p.expectByte('='); err != nil {
			return err
		}
		if _, err := p.parseString(); err != nil {
			return err
		}
		if index < len(chartMetadataNames)-1 {
			if err := p.expectByte(','); err != nil {
				return err
			}
		}
	}
	if err := p.expectByte(';'); err != nil {
		return err
	}
	return nil
}

// parseMatrix は、数値、null、空スロットだけで構成される二次元配列を解析します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - [][]*float64: nullと空スロットをnilで保持した数値行列。
//   - error: 配列構文、行数、列数、数値が不正な場合のエラー。
func (p *chartScriptParser) parseMatrix() ([][]*float64, error) {
	if err := p.expectByte('['); err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.consumeByte(']') {
		return [][]*float64{}, nil
	}

	rows := make([][]*float64, 0)
	for {
		p.skipWhitespace()
		if p.peekByte() != '[' {
			return nil, p.errorf("行配列が必要です")
		}
		row, err := p.parseNumericRow()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		if len(rows) > maxChartRows {
			return nil, p.errorf("チャート行数が上限%dを超えました", maxChartRows)
		}

		p.skipWhitespace()
		if p.consumeByte(']') {
			return rows, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("行の後にカンマまたは閉じ角括弧が必要です")
		}
		p.skipWhitespace()
		if p.consumeByte(']') {
			return rows, nil
		}
	}
}

// parseNumericRow は、疎要素を許可した一行の数値配列を解析します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - []*float64: 数値へのポインターと欠測を表すnilの一覧。
//   - error: 数値構文、区切り、列数が不正な場合のエラー。
func (p *chartScriptParser) parseNumericRow() ([]*float64, error) {
	if err := p.expectByte('['); err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.consumeByte(']') {
		return []*float64{}, nil
	}

	row := make([]*float64, 0)
	for {
		p.skipWhitespace()
		if p.consumeByte(',') {
			row = append(row, nil)
			if len(row) > maxChartColumns {
				return nil, p.errorf("チャート列数が上限%dを超えました", maxChartColumns)
			}
			p.skipWhitespace()
			if p.consumeByte(']') {
				return row, nil
			}
			continue
		}
		if p.consumeByte(']') {
			return row, nil
		}

		value, err := p.parseOptionalNumber()
		if err != nil {
			return nil, err
		}
		row = append(row, value)
		if len(row) > maxChartColumns {
			return nil, p.errorf("チャート列数が上限%dを超えました", maxChartColumns)
		}

		p.skipWhitespace()
		if p.consumeByte(']') {
			return row, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("値の後にカンマまたは閉じ角括弧が必要です")
		}
		p.skipWhitespace()
		if p.consumeByte(']') {
			return row, nil
		}
	}
}

// parseOptionalNumber は、有限数値またはnullを解析します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - *float64: 有限数値へのポインター。nullの場合はnil。
//   - error: 対応外トークン、非有限値、数値構文異常の場合のエラー。
func (p *chartScriptParser) parseOptionalNumber() (*float64, error) {
	p.skipWhitespace()
	if p.consumeKeyword("null") {
		return nil, nil
	}

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
		return nil, p.errorf("数値またはnullが必要です")
	}
	if p.peekByte() == 'e' || p.peekByte() == 'E' {
		p.position++
		if p.peekByte() == '+' || p.peekByte() == '-' {
			p.position++
		}
		if p.consumeDigits() == 0 {
			return nil, p.errorf("指数部に数字が必要です")
		}
	}

	raw := string(p.body[start:p.position])
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, p.errorf("数値を解析できません: %s", raw)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, p.errorf("有限ではない数値です: %s", raw)
	}
	return &value, nil
}

// parseString は、JSON互換の二重引用符付き文字列を解析します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - string: エスケープを復元した文字列。
//   - error: 引用符、エスケープ、UTF-8文字列が不正な場合のエラー。
func (p *chartScriptParser) parseString() (string, error) {
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
			raw := p.body[start:p.position]
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", p.errorf("文字列を解析できません")
			}
			return value, nil
		}
	}
	return "", p.errorf("文字列が閉じていません")
}

// parseIdentifier は、ASCII英字で始まる識別子を解析します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - string: 解析した識別子。
//   - error: 現在位置に識別子がない場合のエラー。
func (p *chartScriptParser) parseIdentifier() (string, error) {
	p.skipWhitespace()
	start := p.position
	if !isIdentifierStart(p.peekByte()) {
		return "", p.errorf("識別子が必要です")
	}
	p.position++
	for isIdentifierPart(p.peekByte()) {
		p.position++
	}
	return string(p.body[start:p.position]), nil
}

// expectKeyword は、識別子境界を含めて指定キーワードを消費します。
//
// 引数:
//   - keyword string: 現在位置に必要なキーワード。
//
// 返り値:
//   - error: 指定キーワードが存在しない場合のエラー。
func (p *chartScriptParser) expectKeyword(keyword string) error {
	p.skipWhitespace()
	if !p.consumeKeyword(keyword) {
		return p.errorf("%sが必要です", keyword)
	}
	return nil
}

// consumeKeyword は、識別子境界を確認して指定キーワードを消費します。
//
// 引数:
//   - keyword string: 消費を試みるキーワード。
//
// 返り値:
//   - bool: キーワードを消費した場合はtrue。
func (p *chartScriptParser) consumeKeyword(keyword string) bool {
	if !bytes.HasPrefix(p.body[p.position:], []byte(keyword)) {
		return false
	}
	end := p.position + len(keyword)
	if end < len(p.body) && isIdentifierPart(p.body[end]) {
		return false
	}
	p.position = end
	return true
}

// expectByte は、空白を読み飛ばして指定バイトを消費します。
//
// 引数:
//   - expected byte: 現在位置に必要なASCII記号。
//
// 返り値:
//   - error: 指定記号が存在しない場合のエラー。
func (p *chartScriptParser) expectByte(expected byte) error {
	p.skipWhitespace()
	if !p.consumeByte(expected) {
		return p.errorf("%qが必要です", expected)
	}
	return nil
}

// consumeByte は、現在位置が指定バイトなら1文字進めます。
//
// 引数:
//   - expected byte: 消費を試みるASCII記号。
//
// 返り値:
//   - bool: 指定バイトを消費した場合はtrue。
func (p *chartScriptParser) consumeByte(expected byte) bool {
	if p.peekByte() != expected {
		return false
	}
	p.position++
	return true
}

// consumeDigits は、現在位置から連続するASCII数字を消費します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - int: 消費した数字の文字数。
func (p *chartScriptParser) consumeDigits() int {
	start := p.position
	for {
		current := p.peekByte()
		if current < '0' || current > '9' {
			return p.position - start
		}
		p.position++
	}
}

// finishAssignment は、セミコロンと本文終端を検証します。
//
// 引数:
//   - requireEnd bool: 代入後に空白以外を許可しない場合はtrue。
//
// 返り値:
//   - error: セミコロンがない場合、または余分な記述がある場合のエラー。
func (p *chartScriptParser) finishAssignment(requireEnd bool) error {
	if err := p.expectByte(';'); err != nil {
		return err
	}
	if !requireEnd {
		return nil
	}
	p.skipWhitespace()
	if !p.atEnd() {
		return p.errorf("余分なJavaScript記述があります")
	}
	return nil
}

// skipWhitespace は、JavaScript本文のASCII空白を読み飛ばします。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - なし。現在位置を最初の非空白へ進める。
func (p *chartScriptParser) skipWhitespace() {
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
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - byte: 現在位置のバイト。本文末尾の場合は0。
func (p *chartScriptParser) peekByte() byte {
	if p.atEnd() {
		return 0
	}
	return p.body[p.position]
}

// atEnd は、現在位置が本文末尾へ到達したか確認します。
//
// 引数:
//   - なし。パーサーが保持する本文と現在位置を利用する。
//
// 返り値:
//   - bool: 本文末尾の場合はtrue。
func (p *chartScriptParser) atEnd() bool {
	return p.position >= len(p.body)
}

// errorf は、現在のバイト位置を含む解析エラーを生成します。
//
// 引数:
//   - format string: fmt.Errorfへ渡す書式。
//   - arguments ...any: 書式へ埋め込む値。
//
// 返り値:
//   - error: 現在位置を含む解析エラー。
func (p *chartScriptParser) errorf(format string, arguments ...any) error {
	message := fmt.Sprintf(format, arguments...)
	return fmt.Errorf("バイト%d: %s", p.position, message)
}

// isIdentifierStart は、バイトが許可する識別子の先頭文字か確認します。
//
// 引数:
//   - value byte: 検証するASCIIバイト。
//
// 返り値:
//   - bool: ASCII英字、アンダースコア、ドル記号の場合はtrue。
func isIdentifierStart(value byte) bool {
	return value == '_' || value == '$' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z')
}

// isIdentifierPart は、バイトが許可する識別子の継続文字か確認します。
//
// 引数:
//   - value byte: 検証するASCIIバイト。
//
// 返り値:
//   - bool: 識別子先頭文字またはASCII数字の場合はtrue。
func isIdentifierPart(value byte) bool {
	return isIdentifierStart(value) || (value >= '0' && value <= '9')
}
