package nikkei225jp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const marketClientCurrentBody = "A[511]=\"157.619_-1.902_-1.19_05:59_0_160.877_157.297\";\n" +
	"A[111]=\"64362.02_+2494.59_+4.03_07/31_0_65364.73_61948.23\";\n" +
	"A[112]=\"4003.30_+50.80_+1.29_07/31_0_4052.27_3951.22\";\n"

const marketClientSingleChartBody = "var Bdata=\"1\",Max=\"2\",Min=\"0\",Ldata=\"1.5\",Per=\"+1.0\",Time=\"08/02\",STtime=\"2026-08-02-09-00\",Start=\"9:00\",End=\"15:30\",opF=\"0\",Pline=\"1\",Area=\"J\",Rang=\"6.5\",Keta=\"2\";\n" +
	"var Cdata=[[1700000000000,1.5],[1700000010000,1.75]];"

// TestFetchMarketCurrentUsesSectionReferer は、市場ごとの現在値パスとRefererを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchMarketCurrentUsesSectionReferer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		section     MarketSection
		requestPath string
		referer     string
	}{
		{name: "世界主要市場", section: MarketSectionTop, requestPath: DefaultCurrentPath, referer: "https://225225.jp/"},
		{name: "日経先物", section: MarketSectionNikkeiFutures, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_cme.js", referer: "https://225225.jp/2nk/"},
		{name: "日本市場", section: MarketSectionJapan, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_nikkei.js", referer: "https://225225.jp/1jp/"},
		{name: "米国市場", section: MarketSectionUS, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_dow.js", referer: "https://225225.jp/3ny/"},
		{name: "欧州市場", section: MarketSectionEurope, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_euro.js", referer: "https://225225.jp/6ec/"},
		{name: "アジア市場", section: MarketSectionAsia, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_asia.js", referer: "https://225225.jp/7as/"},
		{name: "商品先物", section: MarketSectionCommodities, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_oil.js", referer: "https://225225.jp/5cx/"},
		{name: "為替", section: MarketSectionFX, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_fx.js", referer: "https://225225.jp/4fx/"},
		{name: "暗号資産", section: MarketSectionBitcoin, requestPath: "/_data/_nfsDATA/ajaxindex/ajax_bitcoin.js", referer: "https://225225.jp/bitcoin/"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet {
					t.Errorf("Method = %q", request.Method)
				}
				if request.URL.Path != test.requestPath {
					t.Errorf("Path = %q, want %q", request.URL.Path, test.requestPath)
				}
				if request.Header.Get("Referer") != test.referer {
					t.Errorf("Referer = %q, want %q", request.Header.Get("Referer"), test.referer)
				}
				writer.Header().Set("Content-Type", "application/javascript")
				_, _ = writer.Write([]byte(marketClientCurrentBody))
			}))
			defer server.Close()

			client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			data, err := client.FetchMarketCurrent(context.Background(), test.section, nil)
			if err != nil {
				t.Fatalf("FetchMarketCurrent() error = %v", err)
			}
			if data.Section != test.section || data.PageURL != test.referer || len(data.Quotes) != 3 {
				t.Errorf("data = %+v", data)
			}
			if !strings.HasSuffix(data.Metadata.SourceURL, test.requestPath) {
				t.Errorf("SourceURL = %q", data.Metadata.SourceURL)
			}
		})
	}
}

// TestFetchMarketCurrentFiltersCodes は、重複指定を除いて必要な現在値だけを返すことを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchMarketCurrentFiltersCodes(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/_data/_nfsDATA/ajaxindex/ajax_nikkei.js" {
			t.Errorf("Path = %q", request.URL.Path)
		}
		if request.Header.Get("Referer") != "https://225225.jp/1jp/" {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/javascript")
		_, _ = writer.Write([]byte(marketClientCurrentBody))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchMarketCurrent(
		context.Background(),
		MarketSectionJapan,
		[]string{"511", "111", "511"},
	)
	if err != nil {
		t.Fatalf("FetchMarketCurrent() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Errorf("requests = %d, want 1", requests.Load())
	}
	if len(data.Quotes) != 2 || data.Quotes[0].Code != "111" || data.Quotes[1].Code != "511" {
		t.Errorf("Quotes = %+v", data.Quotes)
	}
}

// TestFetchMarketChartCompositeRestoresTimestamps は、複合チャートの絶対・差分時刻をHTTP層経由で復元します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchMarketChartCompositeRestoresTimestamps(t *testing.T) {
	t.Parallel()

	const body = `var DATAm=[
		[1700000000000,100,200],
		[18,101,201],
		[1700001000000,102,null]
	];`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_data/_nfsDATA/hs_data/hs_CHART3.json" {
			t.Errorf("Path = %q", request.URL.Path)
		}
		if request.Header.Get("Referer") != "https://225225.jp/1jp/" {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchMarketChart(
		context.Background(),
		MarketSectionJapan,
		MarketChartRangeIntraday,
		[]string{"112", "111"},
	)
	if err != nil {
		t.Fatalf("FetchMarketChart() error = %v", err)
	}
	if len(data.Sources) != 1 || len(data.Series) != 2 {
		t.Fatalf("data = %+v", data)
	}
	nikkei := findChartSeries(t, data.Series, "111")
	if len(nikkei.Points) != 3 {
		t.Fatalf("日本225 Points = %+v", nikkei.Points)
	}
	if nikkei.Points[0].TimestampMillis != 1700000000000 ||
		nikkei.Points[1].TimestampMillis != 1700000180000 ||
		nikkei.Points[2].TimestampMillis != 1700001000000 {
		t.Errorf("日本225 Points = %+v", nikkei.Points)
	}
	topix := findChartSeries(t, data.Series, "112")
	if len(topix.Points) != 2 || topix.Points[1].TimestampMillis != 1700000180000 {
		t.Errorf("TOPIX Points = %+v", topix.Points)
	}
}

// TestFetchMarketChartPrefersSingleResource は、1コード指定時に小容量パスを複合配信より優先します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchMarketChartPrefersSingleResource(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/_data/_nfsDATA/json/411_24min.json" {
			t.Errorf("Path = %q", request.URL.Path)
		}
		if request.Header.Get("Referer") != "https://225225.jp/6ec/" {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(marketClientSingleChartBody))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchMarketChart(
		context.Background(),
		MarketSectionEurope,
		MarketChartRangeIntraday,
		[]string{"411"},
	)
	if err != nil {
		t.Fatalf("FetchMarketChart() error = %v", err)
	}
	if requests.Load() != 1 || len(data.Sources) != 1 || len(data.Series) != 1 {
		t.Fatalf("requests = %d, data = %+v", requests.Load(), data)
	}
	if data.Series[0].Code != "411" || data.Series[0].Name != "フランスCAC40" || len(data.Series[0].Points) != 2 {
		t.Errorf("Series = %+v", data.Series)
	}
	if !strings.HasSuffix(data.Sources[0].SourceURL, "/_data/_nfsDATA/json/411_24min.json") {
		t.Errorf("SourceURL = %q", data.Sources[0].SourceURL)
	}
}

// TestFetchMarketChartHistoryUsesAllowedCodesSerially は、許可済み日足を数値順かつ直列に取得します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchMarketChartHistoryUsesAllowedCodesSerially(t *testing.T) {
	t.Parallel()

	var active atomic.Int32
	var maximumActive atomic.Int32
	var requests atomic.Int32
	var pathsMutex sync.Mutex
	requestedPaths := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		currentActive := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximumActive.Load()
			if currentActive <= previous || maximumActive.CompareAndSwap(previous, currentActive) {
				break
			}
		}
		pathsMutex.Lock()
		requestedPaths = append(requestedPaths, request.URL.Path)
		pathsMutex.Unlock()
		if request.Header.Get("Referer") != "https://225225.jp/3ny/" {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		code := strings.TrimSuffix(
			strings.TrimPrefix(request.URL.Path, "/_data/_nfsDATA/HS_DATA_DAY/S"),
			".json",
		)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, "var S%s=[[1700000000000,1],[1700000010000,2]];", code)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	data, err := client.FetchMarketChart(
		context.Background(),
		MarketSectionUS,
		MarketChartRangeHistory,
		[]string{"811", "211"},
	)
	if err != nil {
		t.Fatalf("FetchMarketChart() error = %v", err)
	}
	pathsMutex.Lock()
	paths := append([]string(nil), requestedPaths...)
	pathsMutex.Unlock()
	wantPaths := []string{
		"/_data/_nfsDATA/HS_DATA_DAY/S211.json",
		"/_data/_nfsDATA/HS_DATA_DAY/S811.json",
	}
	if strings.Join(paths, ",") != strings.Join(wantPaths, ",") {
		t.Errorf("requestedPaths = %v, want %v", paths, wantPaths)
	}
	if maximumActive.Load() != 1 {
		t.Errorf("maximumActive = %d, want 1", maximumActive.Load())
	}
	if len(data.Sources) != 2 || len(data.Series) != 2 ||
		data.Series[0].Code != "211" || data.Series[1].Code != "811" {
		t.Errorf("data = %+v", data)
	}

	requestsBeforeInvalid := requests.Load()
	_, err = client.FetchMarketChart(
		context.Background(),
		MarketSectionUS,
		MarketChartRangeHistory,
		[]string{"999"},
	)
	if err == nil || !strings.Contains(err.Error(), "許可されていない") {
		t.Fatalf("未許可コード error = %v", err)
	}
	if requests.Load() != requestsBeforeInvalid {
		t.Errorf("未許可コードでHTTP要求されました: before=%d after=%d", requestsBeforeInvalid, requests.Load())
	}
}

// TestFetchMarketRejectsUnsupportedADRResources は、ADRの未対応現在値・チャートを通信前に拒否します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchMarketRejectsUnsupportedADRResources(t *testing.T) {
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
	_, err = client.FetchMarketCurrent(context.Background(), MarketSectionADR, nil)
	if err == nil || !strings.Contains(err.Error(), "現在値配信がありません") {
		t.Errorf("ADR現在値 error = %v", err)
	}
	_, err = client.FetchMarketChart(
		context.Background(),
		MarketSectionADR,
		MarketChartRangeIntraday,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "短期チャート配信がありません") {
		t.Errorf("ADR短期チャート error = %v", err)
	}
	_, err = client.FetchMarketChart(
		context.Background(),
		MarketSectionADR,
		MarketChartRangeHistory,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "長期日足配信がありません") {
		t.Errorf("ADR長期チャート error = %v", err)
	}
	if requests.Load() != 0 {
		t.Errorf("requests = %d, want 0", requests.Load())
	}
}
