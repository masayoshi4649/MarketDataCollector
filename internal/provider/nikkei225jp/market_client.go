package nikkei225jp

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

var commoditySuffixNames = []string{"oilM1", "oilM2", "oilM3", "gldM1", "gldM2"}

// FetchMarketCurrent は、指定市場の現在値を内部配信1件から取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - section MarketSection: 取得する市場分類。
//   - codes []string: 出力対象の数値コード。空の場合は配信全件。
//
// 返り値:
//   - MarketCurrentData: 市場情報、HTTP付帯情報、現在値一覧。
//   - error: 市場、コード、通信、本文形式のいずれかに異常がある場合のエラー。
func (c *Client) FetchMarketCurrent(
	ctx context.Context,
	section MarketSection,
	codes []string,
) (MarketCurrentData, error) {
	config, exists := marketSectionConfigs[section]
	if !exists {
		return MarketCurrentData{}, fmt.Errorf("未対応の市場分類です: %q", section)
	}
	if config.currentPath == "" {
		return MarketCurrentData{}, fmt.Errorf("%sには現在値配信がありません", config.info.Name)
	}
	selectedCodes, err := normalizeChartCodes(codes)
	if err != nil {
		return MarketCurrentData{}, err
	}

	release, err := c.acquireRequestSlot(ctx)
	if err != nil {
		return MarketCurrentData{}, err
	}
	defer release()

	requestURL, err := c.resolveResourceURL(config.currentPath)
	if err != nil {
		return MarketCurrentData{}, err
	}
	body, metadata, err := c.fetchWithRefererLocked(
		ctx,
		requestURL,
		config.info.Name+"現在値",
		c.maxResponseBytes,
		config.info.PageURL,
	)
	if err != nil {
		return MarketCurrentData{}, err
	}
	quotes, err := parseCurrent(body)
	if err != nil {
		return MarketCurrentData{}, fmt.Errorf("%s現在値本文を解析できません: %w", config.info.Name, err)
	}
	quotes, err = filterCurrentQuotes(quotes, selectedCodes)
	if err != nil {
		return MarketCurrentData{}, err
	}

	return MarketCurrentData{
		Section:  section,
		Name:     config.info.Name,
		PageURL:  config.info.PageURL,
		Metadata: metadata,
		Quotes:   quotes,
	}, nil
}

// filterCurrentQuotes は、指定コードだけを現在値一覧へ残します。
//
// 引数:
//   - quotes []CurrentQuote: 配信本文に含まれる現在値一覧。
//   - selectedCodes []string: 数値順に正規化済みの指定コード。空なら全件。
//
// 返り値:
//   - []CurrentQuote: 元の数値順を保って絞り込んだ現在値一覧。
//   - error: 指定コードが配信本文にない場合のエラー。
func filterCurrentQuotes(
	quotes []CurrentQuote,
	selectedCodes []string,
) ([]CurrentQuote, error) {
	if len(selectedCodes) == 0 {
		return quotes, nil
	}
	selected := make(map[string]struct{}, len(selectedCodes))
	for _, code := range selectedCodes {
		selected[code] = struct{}{}
	}
	found := make(map[string]struct{}, len(selectedCodes))
	filtered := make([]CurrentQuote, 0, len(selectedCodes))
	for _, quote := range quotes {
		if _, exists := selected[quote.Code]; exists {
			filtered = append(filtered, quote)
			found[quote.Code] = struct{}{}
		}
	}
	if len(found) != len(selectedCodes) {
		missing := make([]string, 0, len(selectedCodes)-len(found))
		for _, code := range selectedCodes {
			if _, exists := found[code]; !exists {
				missing = append(missing, code)
			}
		}
		return nil, newInputError(fmt.Sprintf("現在値本文に指定コードがありません: %s", strings.Join(missing, ",")))
	}
	return filtered, nil
}

// ----------------------------------------

// FetchMarketChart は、市場別の短期または日足全履歴チャートを取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - section MarketSection: 取得する市場分類。
//   - chartRange MarketChartRange: intradayまたはhistory。
//   - codes []string: 出力対象コード。空の場合は短期全系列、長期は既定1コード。
//
// 返り値:
//   - MarketChartData: 市場情報、取得元、銘柄別系列。
//   - error: 市場、範囲、コード、通信、本文形式のいずれかに異常がある場合のエラー。
func (c *Client) FetchMarketChart(
	ctx context.Context,
	section MarketSection,
	chartRange MarketChartRange,
	codes []string,
) (MarketChartData, error) {
	config, exists := marketSectionConfigs[section]
	if !exists {
		return MarketChartData{}, fmt.Errorf("未対応の市場分類です: %q", section)
	}
	selectedCodes, err := normalizeMarketCodes(codes)
	if err != nil {
		return MarketChartData{}, err
	}

	release, err := c.acquireRequestSlot(ctx)
	if err != nil {
		return MarketChartData{}, err
	}
	defer release()

	switch chartRange {
	case MarketChartRangeIntraday:
		return c.fetchMarketIntradayLocked(ctx, section, config, selectedCodes)
	case MarketChartRangeHistory:
		return c.fetchMarketHistoryLocked(ctx, section, config, selectedCodes)
	default:
		return MarketChartData{}, fmt.Errorf("未対応の市場チャート範囲です: %q", chartRange)
	}
}

// normalizeMarketCodes は、英数字とアンダースコアのコードを重複なしの順序へ整えます。
//
// 引数:
//   - codes []string: 利用者が指定した数値または合成系列コード。
//
// 返り値:
//   - []string: 重複を除いて安定順に並べたコード。未指定の場合はnil。
//   - error: 空文字または許可文字以外を含むコードがある場合のエラー。
func normalizeMarketCodes(codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	unique := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if code == "" {
			return nil, fmt.Errorf("不正な市場チャートコードです: %q", code)
		}
		for index := 0; index < len(code); index++ {
			value := code[index]
			if (value < '0' || value > '9') &&
				(value < 'a' || value > 'z') &&
				(value < 'A' || value > 'Z') && value != '_' {
				return nil, fmt.Errorf("不正な市場チャートコードです: %q", code)
			}
		}
		unique[code] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for code := range unique {
		result = append(result, code)
	}
	sort.Slice(result, func(leftIndex, rightIndex int) bool {
		left := result[leftIndex]
		right := result[rightIndex]
		leftNumeric := isDecimalCode(left)
		rightNumeric := isDecimalCode(right)
		if leftNumeric && rightNumeric {
			copyCodes := []string{left, right}
			sortCodes(copyCodes)
			return copyCodes[0] == left && left != right
		}
		if leftNumeric != rightNumeric {
			return leftNumeric
		}
		return left < right
	})
	return result, nil
}

// fetchMarketIntradayLocked は、単一小容量または複合短期チャートを取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - section MarketSection: 取得する市場分類。
//   - config marketSectionConfig: 検証済みの市場内部設定。
//   - codes []string: 正規化済みの出力対象コード。空なら複合全系列。
//
// 返り値:
//   - MarketChartData: 短期チャートの取得元と銘柄別系列。
//   - error: 対応、通信、本文解析、絞り込みに失敗した場合のエラー。
func (c *Client) fetchMarketIntradayLocked(
	ctx context.Context,
	section MarketSection,
	config marketSectionConfig,
	codes []string,
) (MarketChartData, error) {
	if len(codes) == 1 {
		if resourcePath, exists := config.intradaySinglePaths[codes[0]]; exists {
			return c.fetchMarketSingleIntradayLocked(ctx, section, config, codes[0], resourcePath)
		}
	}
	if config.intradayPath == "" {
		return MarketChartData{}, fmt.Errorf("%sには短期チャート配信がありません", config.info.Name)
	}
	if err := validateAllowedMarketCodes(codes, config.intradayCodes, "短期チャート"); err != nil {
		return MarketChartData{}, err
	}

	body, metadata, err := c.fetchMarketResourceLocked(
		ctx,
		config.intradayPath,
		config.info.PageURL,
		config.info.Name+"短期チャート",
	)
	if err != nil {
		return MarketChartData{}, err
	}
	suffixNames := []string(nil)
	if section == MarketSectionCommodities {
		suffixNames = commoditySuffixNames
	}
	series, chartMetadata, err := parseMarketIntradayChart(body, config.intradayCodes, suffixNames)
	if err != nil {
		return MarketChartData{}, fmt.Errorf("%s短期チャート本文を解析できません: %w", config.info.Name, err)
	}
	applyMarketSeriesNames(series, config)
	series, err = filterChartSeries(series, codes)
	if err != nil {
		return MarketChartData{}, err
	}

	return MarketChartData{
		Section:  section,
		Name:     config.info.Name,
		PageURL:  config.info.PageURL,
		Range:    MarketChartRangeIntraday,
		Sources:  []ResponseMetadata{metadata},
		Metadata: chartMetadata,
		Series:   series,
	}, nil
}

// fetchMarketSingleIntradayLocked は、確認済み小容量パスから1系列を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - section MarketSection: 取得する市場分類。
//   - config marketSectionConfig: 検証済みの市場内部設定。
//   - code string: 取得する許可済みコード。
//   - resourcePath string: コードに対応する固定パス。
//
// 返り値:
//   - MarketChartData: 1系列だけを含む短期チャート。
//   - error: 通信または本文解析に失敗した場合のエラー。
func (c *Client) fetchMarketSingleIntradayLocked(
	ctx context.Context,
	section MarketSection,
	config marketSectionConfig,
	code string,
	resourcePath string,
) (MarketChartData, error) {
	body, metadata, err := c.fetchMarketResourceLocked(
		ctx,
		resourcePath,
		config.info.PageURL,
		config.info.Name+"単一短期チャート",
	)
	if err != nil {
		return MarketChartData{}, err
	}
	points, err := parse1DaySingleChart(body)
	if err != nil {
		return MarketChartData{}, fmt.Errorf("%s単一短期チャート本文を解析できません: %w", config.info.Name, err)
	}
	name := marketInstrumentName(code, config)

	return MarketChartData{
		Section: section,
		Name:    config.info.Name,
		PageURL: config.info.PageURL,
		Range:   MarketChartRangeIntraday,
		Sources: []ResponseMetadata{metadata},
		Series: []ChartSeries{{
			Code:   code,
			Name:   name,
			Points: points,
		}},
	}, nil
}

// fetchMarketHistoryLocked は、許可された日足全履歴をコードごとに直列取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - section MarketSection: 取得する市場分類。
//   - config marketSectionConfig: 検証済みの市場内部設定。
//   - codes []string: 正規化済みの出力対象コード。空なら先頭の既定コード。
//
// 返り値:
//   - MarketChartData: 日足全履歴の取得元一覧と銘柄別系列。
//   - error: 対応、コード、通信、本文解析に失敗した場合のエラー。
func (c *Client) fetchMarketHistoryLocked(
	ctx context.Context,
	section MarketSection,
	config marketSectionConfig,
	codes []string,
) (MarketChartData, error) {
	if len(config.info.HistoryCodes) == 0 {
		return MarketChartData{}, fmt.Errorf("%sには長期日足配信がありません", config.info.Name)
	}
	targetCodes := codes
	if len(targetCodes) == 0 {
		targetCodes = []string{config.info.HistoryCodes[0]}
	}
	for _, code := range targetCodes {
		if !isDecimalCode(code) {
			return MarketChartData{}, fmt.Errorf("長期日足コードは数字だけにしてください: %q", code)
		}
		if _, allowed := config.historyCodes[code]; !allowed {
			return MarketChartData{}, fmt.Errorf("%sの長期日足で許可されていないコードです: %s", config.info.Name, code)
		}
	}

	result := MarketChartData{
		Section: section,
		Name:    config.info.Name,
		PageURL: config.info.PageURL,
		Range:   MarketChartRangeHistory,
		Sources: make([]ResponseMetadata, 0, len(targetCodes)),
		Series:  make([]ChartSeries, 0, len(targetCodes)),
	}
	for _, code := range targetCodes {
		resourcePath := fmt.Sprintf(chart6MonthsFormat, code)
		body, metadata, err := c.fetchMarketResourceLocked(
			ctx,
			resourcePath,
			config.info.PageURL,
			config.info.Name+"長期日足",
		)
		if err != nil {
			return MarketChartData{}, err
		}
		series, err := parseAssignedSeriesChart(body)
		if err != nil {
			return MarketChartData{}, fmt.Errorf("%s長期日足本文を解析できません: %w", config.info.Name, err)
		}
		if len(series) != 1 || series[0].Code != code {
			return MarketChartData{}, fmt.Errorf("長期日足の配信コードが指定%sと一致しません", code)
		}
		series[0].Name = marketInstrumentName(code, config)
		result.Sources = append(result.Sources, metadata)
		result.Series = append(result.Series, series[0])
	}
	return result, nil
}

// fetchMarketResourceLocked は、ページ固有Refererで同一ホストの数値資材を取得します。
//
// 引数:
//   - ctx context.Context: HTTP要求のキャンセルと期限を制御するコンテキスト。
//   - resourcePath string: 取得対象の検証済み固定パス。
//   - referer string: 対応ページのURL。
//   - resourceName string: エラー表示に利用する資材名。
//
// 返り値:
//   - []byte: 200応答の本文。
//   - ResponseMetadata: HTTP応答の付帯情報。
//   - error: URL解決またはHTTP取得に失敗した場合のエラー。
func (c *Client) fetchMarketResourceLocked(
	ctx context.Context,
	resourcePath string,
	referer string,
	resourceName string,
) ([]byte, ResponseMetadata, error) {
	requestURL, err := c.resolveResourceURL(resourcePath)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	body, metadata, err := c.fetchWithRefererLocked(
		ctx,
		requestURL,
		resourceName,
		c.maxChartResponseBytes,
		referer,
	)
	if err != nil {
		return nil, ResponseMetadata{}, err
	}
	return body, metadata, nil
}

// validateAllowedMarketCodes は、指定コードが許可一覧だけで構成されるか確認します。
//
// 引数:
//   - selectedCodes []string: 利用者が指定したコード。空なら検証を省略。
//   - allowedCodes []string: 配信列として確認済みの許可コード。
//   - resourceName string: エラー表示に利用する資材名。
//
// 返り値:
//   - error: 許可一覧にないコードがある場合のエラー。
func validateAllowedMarketCodes(
	selectedCodes []string,
	allowedCodes []string,
	resourceName string,
) error {
	if len(selectedCodes) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(allowedCodes))
	for _, code := range allowedCodes {
		allowed[code] = struct{}{}
	}
	for _, code := range selectedCodes {
		if _, exists := allowed[code]; !exists {
			return fmt.Errorf("%sに指定コードがありません: %s", resourceName, code)
		}
	}
	return nil
}

// applyMarketSeriesNames は、市場固有名または共通銘柄名を系列へ設定します。
//
// 引数:
//   - series []ChartSeries: 名称を設定する系列一覧。
//   - config marketSectionConfig: 市場固有名称を含む設定。
//
// 返り値:
//   - なし。引数の系列を直接更新する。
func applyMarketSeriesNames(series []ChartSeries, config marketSectionConfig) {
	for index := range series {
		series[index].Name = marketInstrumentName(series[index].Code, config)
	}
}

// marketInstrumentName は、市場固有名を優先して系列表示名を返します。
//
// 引数:
//   - code string: サイト内部または合成系列コード。
//   - config marketSectionConfig: 市場固有名称を含む設定。
//
// 返り値:
//   - string: 確認済みの表示名。未確認の場合は空文字。
func marketInstrumentName(code string, config marketSectionConfig) string {
	if name := config.intradayNames[code]; name != "" {
		return name
	}
	return InstrumentName(code)
}
