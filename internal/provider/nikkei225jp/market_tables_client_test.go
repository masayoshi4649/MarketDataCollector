package nikkei225jp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const marketTableFXBody = "A[523]=\"1.15251_-0.00008_-0.01_05:59_0\";\n" +
	"A[501]=\"99.8372_-0.1609_-0.16_05:59_0\";\n" +
	"A[511]=\"157.619_-1.902_-1.19_05:59_0\";\n"

const marketTableCryptoBody = `var CO=[];
CO[1001]="BTC_ビットコイン_Bitcoin_1_1992571_9934257_-0.16_-5.54_-20.82_-40.26_";
var LastModCoin="22:17";
var Coincount=1;`

// TestMarketTableFetchersUseFixedRoutes は、各数値表が固定パスと対応ページのRefererだけを使うことを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestMarketTableFetchersUseFixedRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		referer string
		body    string
		fetch   func(*Client) error
	}{
		{
			name:    "日経225構成銘柄",
			path:    "/_data/_nfsDATA/min/country_jp_nk225N.js",
			referer: "https://225225.jp/1jp/",
			body:    validJapanComponentsBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchJapanComponents(context.Background())
				return err
			},
		},
		{
			name:    "日経225寄与度",
			path:    "/_data/_nfsDATA/min/country_jp_kiyo10N.js",
			referer: "https://225225.jp/1jp/",
			body:    validJapanContributionsBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchJapanContributions(context.Background())
				return err
			},
		},
		{
			name:    "東証33業種",
			path:    "/_data/_nfsDATA/min/country_jp_gyo.js",
			referer: "https://225225.jp/1jp/",
			body:    buildJapanIndustryBody("0"),
			fetch: func(client *Client) error {
				_, _, err := client.FetchJapanIndustries(context.Background())
				return err
			},
		},
		{
			name:    "日本株ランキング",
			path:    "/_data/_nfsDATA/min/country_jp_ranking.js",
			referer: "https://225225.jp/1jp/",
			body:    validJapanRankingBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchMarketRankings(context.Background(), MarketSectionJapan)
				return err
			},
		},
		{
			name:    "米国株通常取引",
			path:    "/_data/_nfsDATA/min/country_ny.js",
			referer: "https://225225.jp/3ny/",
			body:    validUSEquityBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchUSEquities(context.Background(), USMarketSessionRegular)
				return err
			},
		},
		{
			name:    "米国株プレ市場",
			path:    "/_data/_nfsDATA/min/country_ny_pre.js",
			referer: "https://225225.jp/3ny/",
			body:    validUSPreEquityBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchUSEquities(context.Background(), USMarketSessionPre)
				return err
			},
		},
		{
			name:    "米国株アフター市場",
			path:    "/_data/_nfsDATA/min/country_ny_after.js",
			referer: "https://225225.jp/3ny/",
			body:    strings.ReplaceAll(validUSPreEquityBody, "_pre", "_after"),
			fetch: func(client *Client) error {
				_, _, err := client.FetchUSEquities(context.Background(), USMarketSessionAfter)
				return err
			},
		},
		{
			name:    "米国業種指数",
			path:    "/_data/_nfsDATA/min/country_ny_gyo.js",
			referer: "https://225225.jp/3ny/",
			body:    validUSIndustryBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchUSIndustries(context.Background())
				return err
			},
		},
		{
			name:    "米国株ランキング",
			path:    "/_data/_nfsDATA/min/country_ny_ranking.js",
			referer: "https://225225.jp/3ny/",
			body:    validUSRankingBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchMarketRankings(context.Background(), MarketSectionUS)
				return err
			},
		},
		{
			name:    "日本株ADR・PTS",
			path:    "/_data/_nfsDATA/adr/_adr_all.js",
			referer: "https://225225.jp/3ny/adr.php",
			body:    validADRBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchADR(context.Background())
				return err
			},
		},
		{
			name:    "為替レート",
			path:    "/_data/_nfsDATA/ajaxindex/ajax_fx_table.js",
			referer: "https://225225.jp/4fx/",
			body:    marketTableFXBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchFXRates(context.Background(), nil)
				return err
			},
		},
		{
			name:    "暗号資産一覧",
			path:    "/_data/_nfsDATA/min/coin_table_DWMY.js",
			referer: "https://225225.jp/bitcoin/",
			body:    marketTableCryptoBody,
			fetch: func(client *Client) error {
				_, _, err := client.FetchCryptoAssets(context.Background())
				return err
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodGet {
					t.Errorf("HTTPメソッド = %s", request.Method)
				}
				if request.URL.Path != test.path || request.URL.RawQuery != "" {
					t.Errorf("要求先 = %s", request.URL.RequestURI())
				}
				if request.Header.Get("Referer") != test.referer {
					t.Errorf("Referer = %q", request.Header.Get("Referer"))
				}
				writer.Header().Set("Content-Type", "application/javascript")
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if err := test.fetch(client); err != nil {
				t.Fatalf("数値表取得 error = %v", err)
			}
			if requests.Load() != 1 {
				t.Errorf("HTTP要求回数 = %d", requests.Load())
			}
		})
	}
}

// ----------------------------------------

// TestFetchFXRatesFiltersCodes は、為替表1回の取得結果を指定コードだけへ数値順に絞り込むことを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchFXRatesFiltersCodes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_data/_nfsDATA/ajaxindex/ajax_fx_table.js" {
			t.Errorf("要求パス = %s", request.URL.Path)
		}
		if request.Header.Get("Referer") != "https://225225.jp/4fx/" {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/javascript")
		_, _ = writer.Write([]byte(marketTableFXBody))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	quotes, metadata, err := client.FetchFXRates(
		context.Background(),
		[]string{"523", "511", "523"},
	)
	if err != nil {
		t.Fatalf("FetchFXRates() error = %v", err)
	}
	if len(quotes) != 2 || quotes[0].Code != "511" || quotes[1].Code != "523" {
		t.Errorf("Quotes = %+v", quotes)
	}
	if metadata.SourceURL != server.URL+"/_data/_nfsDATA/ajaxindex/ajax_fx_table.js" {
		t.Errorf("SourceURL = %q", metadata.SourceURL)
	}
}

// TestMarketTableFetchersRejectInvalidSelectionsBeforeRequest は、不正なセッション・市場・コードを通信前に拒否することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestMarketTableFetchersRejectInvalidSelectionsBeforeRequest(t *testing.T) {
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
		name string
		call func() error
		want string
	}{
		{
			name: "不正な米国株セッション",
			call: func() error {
				_, _, fetchErr := client.FetchUSEquities(context.Background(), USMarketSession("overnight"))
				return fetchErr
			},
			want: "セッション",
		},
		{
			name: "ランキング未対応市場",
			call: func() error {
				_, _, fetchErr := client.FetchMarketRankings(context.Background(), MarketSectionEurope)
				return fetchErr
			},
			want: "japanまたはus",
		},
		{
			name: "不正な為替コード",
			call: func() error {
				_, _, fetchErr := client.FetchFXRates(context.Background(), []string{"USD/JPY"})
				return fetchErr
			},
			want: "不正なチャート銘柄コード",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			callErr := test.call()
			if callErr == nil || !strings.Contains(callErr.Error(), test.want) {
				t.Fatalf("取得 error = %v, want %q", callErr, test.want)
			}
		})
	}
	if requests.Load() != 0 {
		t.Errorf("入力異常時のHTTP要求回数 = %d", requests.Load())
	}
}

// TestMarketTableParseFailureStillAllowsNextRequest は、本文解析に失敗した後も次回要求で上流へ再取得することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestMarketTableParseFailureStillAllowsNextRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_data/_nfsDATA/ajaxindex/ajax_fx_table.js" {
			t.Errorf("要求パス = %s", request.URL.Path)
		}
		if request.Header.Get("Referer") != "https://225225.jp/4fx/" {
			t.Errorf("Referer = %q", request.Header.Get("Referer"))
		}
		writer.Header().Set("Content-Type", "application/javascript")
		writer.Header().Set("Cache-Control", "public,max-age=10")
		writer.Header().Set("Last-Modified", "Sun, 02 Aug 2026 05:00:00 GMT")
		switch requests.Add(1) {
		case 1:
			_, _ = writer.Write([]byte(`A[511]="列不足";`))
		case 2:
			if request.Header.Get("If-Modified-Since") != "" || request.Header.Get("If-None-Match") != "" {
				t.Errorf("解析失敗後に条件付きヘッダーが送信されました")
			}
			_, _ = writer.Write([]byte(marketTableFXBody))
		default:
			t.Errorf("想定外のHTTP要求です")
			writer.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, _, err := client.FetchFXRates(context.Background(), nil); err == nil {
		t.Fatalf("解析失敗本文のFetchFXRates() error = nil")
	}
	quotes, metadata, err := client.FetchFXRates(context.Background(), []string{"511"})
	if err != nil {
		t.Fatalf("再取得のFetchFXRates() error = %v", err)
	}
	if requests.Load() != 2 {
		t.Errorf("HTTP要求回数 = %d", requests.Load())
	}
	if len(quotes) != 1 || quotes[0].Code != "511" {
		t.Errorf("再取得結果 = Metadata:%+v Quotes:%+v", metadata, quotes)
	}
}
