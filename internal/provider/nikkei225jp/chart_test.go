package nikkei225jp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const valid1DaySingleBody = "var Bdata=\"1\",Max=\"2\",Min=\"0\",Ldata=\"1.5\",Per=\"+1.0\",Time=\"08/02\",STtime=\"2026-08-02-09-00\",Start=\"9:00\",End=\"15:30\",opF=\"0\",Pline=\"1\",Area=\"J\",Rang=\"6.5\",Keta=\"2\";\n" +
	"var Cdata = [[1700000000000,1.5],[1700000010000,1.75]];"

// TestFetchChart60Minutes は、60分ティックをコード別系列へ変換して絞り込みます。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChart60Minutes(t *testing.T) {
	t.Parallel()

	const body = "var DATAm=[[511,1700000000000,150.1],[111,1700000000000,30000],[511,1700000010000,150.2]];"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != chart60MinutesPath {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if request.Header.Get("Referer") != DefaultReferer {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchChart(context.Background(), ChartRange60Minutes, []string{"511"})
	if err != nil {
		t.Fatalf("FetchChart() error = %v", err)
	}
	if data.Range != ChartRange60Minutes || len(data.Sources) != 1 || len(data.Series) != 1 {
		t.Fatalf("data = %+v", data)
	}
	if data.Series[0].Code != "511" || data.Series[0].Name != "USD/JPY" {
		t.Errorf("Series[0] = %+v", data.Series[0])
	}
	if len(data.Series[0].Points) != 2 ||
		data.Series[0].Points[1].TimestampMillis != 1700000010000 ||
		data.Series[0].Points[1].Value != 150.2 {
		t.Errorf("Points = %+v", data.Series[0].Points)
	}
}

// TestFetchChart6Hours は、複数のSコード代入を数値コード順へ変換します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChart6Hours(t *testing.T) {
	t.Parallel()

	const body = "var S511 = [[1700000000000,150.1],[1700000010000,150.2]];\n" +
		"var S111=[[1700000000000,30000],[1700000010000,30001]];"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != chart6HoursPath {
			t.Errorf("request path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/javascript")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchChart(context.Background(), ChartRange6Hours, nil)
	if err != nil {
		t.Fatalf("FetchChart() error = %v", err)
	}
	if len(data.Series) != 2 || data.Series[0].Code != "111" || data.Series[1].Code != "511" {
		t.Fatalf("Series = %+v", data.Series)
	}
}

// TestFetchChart1DaySparse は、差分時刻と疎列を欠測のまま系列へ展開します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChart1DaySparse(t *testing.T) {
	t.Parallel()

	const body = "var DATAm=[[1700000000000,150,30000],[1,151,,200],[2,,30001]];"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != chart1DayPath {
			t.Errorf("request path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchChart(context.Background(), ChartRange1Day, nil)
	if err != nil {
		t.Fatalf("FetchChart() error = %v", err)
	}
	if len(data.Series) != len(chart1DayColumnCodes) {
		t.Fatalf("len(Series) = %d", len(data.Series))
	}

	dollarYen := findChartSeries(t, data.Series, "511")
	if len(dollarYen.Points) != 2 ||
		dollarYen.Points[1].TimestampMillis != 1700000010000 {
		t.Errorf("ドル円 Points = %+v", dollarYen.Points)
	}
	nikkei := findChartSeries(t, data.Series, "111")
	if len(nikkei.Points) != 2 ||
		nikkei.Points[1].TimestampMillis != 1700000030000 {
		t.Errorf("日本225 Points = %+v", nikkei.Points)
	}
	cfd := findChartSeries(t, data.Series, "191")
	if len(cfd.Points) != 1 || cfd.Points[0].TimestampMillis != 1700000010000 {
		t.Errorf("CFD日本225 Points = %+v", cfd.Points)
	}
}

// TestFetchChart1DayUsesSingleCodeResource は、確認済み1銘柄で小容量パスを選択します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChart1DayUsesSingleCodeResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		path string
	}{
		{code: "111", path: "/_data/_nfsDATA/json/111_24min.json"},
		{code: "151", path: "/_data/_nfsDATA/json/151_24.json"},
		{code: "643", path: "/_data/_nfsDATA/json/643_24.json"},
		{code: "811", path: "/_data/_nfsDATA/json/811_24.json"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.code, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.URL.Path != test.path {
					t.Errorf("request path = %q", request.URL.Path)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(valid1DaySingleBody))
			}))
			defer server.Close()

			client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			data, err := client.FetchChart(context.Background(), ChartRange1Day, []string{test.code})
			if err != nil {
				t.Fatalf("FetchChart() error = %v", err)
			}
			if requests.Load() != 1 || len(data.Series) != 1 || data.Series[0].Code != test.code {
				t.Errorf("requests = %d, data = %+v", requests.Load(), data)
			}
			if len(data.Series[0].Points) != 2 {
				t.Errorf("Points = %+v", data.Series[0].Points)
			}
		})
	}
}

// TestFetchChart6Months は、許可コードごとの長期資材を直列取得します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChart6Months(t *testing.T) {
	t.Parallel()

	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestedPaths = append(requestedPaths, request.URL.Path)
		code := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/_data/_nfsDATA/HS_DATA_DAY/S"), ".json")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, "var S%s = [[1700000000000,1],[1700000010000,2]];", code)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchChart(
		context.Background(),
		ChartRange6Months,
		[]string{"523", "111"},
	)
	if err != nil {
		t.Fatalf("FetchChart() error = %v", err)
	}
	expectedPaths := []string{
		"/_data/_nfsDATA/HS_DATA_DAY/S111.json",
		"/_data/_nfsDATA/HS_DATA_DAY/S523.json",
	}
	if strings.Join(requestedPaths, ",") != strings.Join(expectedPaths, ",") {
		t.Errorf("requestedPaths = %v", requestedPaths)
	}
	if len(data.Sources) != 2 || len(data.Series) != 2 ||
		data.Series[0].Code != "111" || data.Series[1].Code != "523" {
		t.Errorf("data = %+v", data)
	}
}

// TestFetchChart6MonthsDefaultsTo111 は、長期コード未指定時に111だけを取得します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChart6MonthsDefaultsTo111(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_data/_nfsDATA/HS_DATA_DAY/S111.json" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("var S111 = [[1700000000000,1]];"))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchChart(context.Background(), ChartRange6Months, nil)
	if err != nil {
		t.Fatalf("FetchChart() error = %v", err)
	}
	if len(data.Series) != 1 || data.Series[0].Code != "111" {
		t.Errorf("Series = %+v", data.Series)
	}
}

// TestFetchChartRejectsInvalidSelectionBeforeRequest は、既知の不正指定を通信前に拒否します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChartRejectsInvalidSelectionBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	tests := []struct {
		name       string
		chartRange ChartRange
		codes      []string
	}{
		{name: "未対応期間", chartRange: "1y"},
		{name: "不正文字", chartRange: ChartRange60Minutes, codes: []string{" 111"}},
		{name: "1日複合にないコード", chartRange: ChartRange1Day, codes: []string{"321"}},
		{name: "6か月許可対象外", chartRange: ChartRange6Months, codes: []string{"999"}},
	}
	for _, test := range tests {
		if _, err := client.FetchChart(context.Background(), test.chartRange, test.codes); err == nil {
			t.Errorf("%s: FetchChart() error = nil", test.name)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("要求回数 = %d", requests.Load())
	}
}

// TestFetchChartRejectsMissingDynamicCode は、動的配信にない指定コードをエラーにします。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChartRejectsMissingDynamicCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("var DATAm=[[111,1700000000000,1]];"))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.FetchChart(context.Background(), ChartRange60Minutes, []string{"999"})
	if err == nil || !strings.Contains(err.Error(), "指定コード") {
		t.Fatalf("FetchChart() error = %v", err)
	}
}

// TestChartParserKeepsDuplicateTimestamp は、配信元の同一時刻点を失わず保持します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestChartParserKeepsDuplicateTimestamp(t *testing.T) {
	t.Parallel()

	series, err := parseAssignedSeriesChart([]byte(
		"var S1001=[[1700000000000,1],[1700000000000,1]];",
	))
	if err != nil {
		t.Fatalf("parseAssignedSeriesChart() error = %v", err)
	}
	if len(series) != 1 || len(series[0].Points) != 2 {
		t.Fatalf("Series = %+v", series)
	}
}

// TestChartParsersRejectInvalidBodies は、余分な記述と不正な数値・時刻を拒否します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestChartParsersRejectInvalidBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parse func() error
	}{
		{
			name: "余分な記述",
			parse: func() error {
				_, err := parse60MinutesChart([]byte("var DATAm=[[111,1700000000000,1]];alert(1);"))
				return err
			},
		},
		{
			name: "非有限値",
			parse: func() error {
				_, err := parse60MinutesChart([]byte("var DATAm=[[111,1700000000000,1e999]];"))
				return err
			},
		},
		{
			name: "時刻逆転",
			parse: func() error {
				_, err := parseAssignedSeriesChart([]byte("var S111=[[1700000001000,1],[1700000000000,2]];"))
				return err
			},
		},
		{
			name: "代入重複",
			parse: func() error {
				_, err := parseAssignedSeriesChart([]byte("var S111=[[1700000000000,1]];\nvar S111=[[1700000010000,2]];"))
				return err
			},
		},
		{
			name: "時刻差分オーバーフロー",
			parse: func() error {
				_, err := parse1DayChart([]byte("var DATAm=[[9000000000000000000,1],[900000000000000000,2]];"))
				return err
			},
		},
		{
			name: "メタ順序違反",
			parse: func() error {
				_, err := parse1DaySingleChart([]byte(strings.Replace(valid1DaySingleBody, "Bdata", "Unknown", 1)))
				return err
			},
		},
		{
			name: "メタ後の余分な記述",
			parse: func() error {
				_, err := parse1DaySingleChart([]byte(valid1DaySingleBody + " var X=1;"))
				return err
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.parse(); err == nil {
				t.Error("parse() error = nil")
			}
		})
	}
}

// TestFetchChartAlwaysRequestsUpstream は、チャートも連続取得ごとに上流へ無条件GETすることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChartAlwaysRequestsUpstream(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestNumber := requests.Add(1)
		if request.Header.Get("If-Modified-Since") != "" || request.Header.Get("If-None-Match") != "" {
			t.Errorf("%d回目に条件付きヘッダーが送信されました", requestNumber)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("ETag", fmt.Sprintf("\"chart-%d\"", requestNumber))
		_, _ = fmt.Fprintf(writer, "var DATAm=[[111,1700000000000,%d]];", requestNumber)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	first, err := client.FetchChart(context.Background(), ChartRange60Minutes, nil)
	if err != nil {
		t.Fatalf("1回目のFetchChart() error = %v", err)
	}
	second, err := client.FetchChart(context.Background(), ChartRange60Minutes, nil)
	if err != nil {
		t.Fatalf("2回目のFetchChart() error = %v", err)
	}
	if requests.Load() != 2 {
		t.Errorf("HTTP要求回数 = %d, want 2", requests.Load())
	}
	if len(first.Series) != 1 || len(first.Series[0].Points) != 1 || first.Series[0].Points[0].Value != 1 {
		t.Errorf("1回目のSeries = %+v", first.Series)
	}
	if len(second.Series) != 1 || len(second.Series[0].Points) != 1 || second.Series[0].Points[0].Value != 2 {
		t.Errorf("2回目のSeries = %+v", second.Series)
	}
	if len(second.Sources) != 1 || second.Sources[0].ETag != "\"chart-2\"" {
		t.Errorf("2回目のSources = %+v", second.Sources)
	}
}

// TestFetchChartRejectsBodyLimit は、現在値と別のチャート本文上限を適用します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchChartRejectsBodyLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("var DATAm=[[111,1700000000000,1]];"))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:               server.URL,
		HTTPClient:            server.Client(),
		MaxChartResponseBytes: 8,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.FetchChart(context.Background(), ChartRange60Minutes, nil)
	if err == nil || !strings.Contains(err.Error(), "上限8バイト") {
		t.Fatalf("FetchChart() error = %v", err)
	}
}

// TestCurrentAndChartShareRequestSlot は、現在値取得中のチャート待機をキャンセルできます。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestCurrentAndChartShareRequestSlot(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var chartRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == DefaultCurrentPath {
			close(started)
			<-release
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte(validCurrentBody))
			return
		}
		chartRequests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("var DATAm=[[111,1700000000000,1]];"))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	currentDone := make(chan error, 1)
	go func() {
		_, fetchErr := client.FetchCurrent(context.Background())
		currentDone <- fetchErr
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.FetchChart(ctx, ChartRange60Minutes, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("FetchChart() error = %v", err)
	}
	if chartRequests.Load() != 0 {
		t.Errorf("チャート要求回数 = %d", chartRequests.Load())
	}
	close(release)
	if err := <-currentDone; err != nil {
		t.Errorf("FetchCurrent() error = %v", err)
	}
}

// findChartSeries は、テスト対象系列から指定コードを検索します。
//
// 引数:
//   - t *testing.T: 系列欠落時にテスト失敗を報告するコンテキスト。
//   - series []ChartSeries: 検索対象の系列一覧。
//   - code string: 検索する銘柄コード。
//
// 返り値:
//   - ChartSeries: 指定コードに一致した系列。
func findChartSeries(t *testing.T, series []ChartSeries, code string) ChartSeries {
	t.Helper()
	for _, item := range series {
		if item.Code == code {
			return item
		}
	}
	t.Fatalf("コード%sの系列がありません", code)
	return ChartSeries{}
}
