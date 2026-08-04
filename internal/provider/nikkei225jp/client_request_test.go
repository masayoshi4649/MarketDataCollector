package nikkei225jp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestFetchCurrentAlwaysRequestsUpstream は、連続取得が毎回無条件GETで上流へ到達することを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchCurrentAlwaysRequestsUpstream(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestNumber := requests.Add(1)
		if request.Header.Get("If-Modified-Since") != "" {
			t.Errorf("%d回目のIf-Modified-Since = %q", requestNumber, request.Header.Get("If-Modified-Since"))
		}
		if request.Header.Get("If-None-Match") != "" {
			t.Errorf("%d回目のIf-None-Match = %q", requestNumber, request.Header.Get("If-None-Match"))
		}

		writer.Header().Set("Content-Type", "application/javascript")
		writer.Header().Set("Cache-Control", "public,max-age=3600")
		writer.Header().Set("ETag", fmt.Sprintf("\"response-%d\"", requestNumber))
		writer.Header().Set("Last-Modified", "Sun, 02 Aug 2026 05:00:00 GMT")
		_, _ = fmt.Fprintf(
			writer,
			"A[111]=\"%d_+1_+1_07/31_0_65364.73_61948.23\";\n",
			requestNumber,
		)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	first, err := client.FetchCurrent(context.Background())
	if err != nil {
		t.Fatalf("1回目のFetchCurrent() error = %v", err)
	}
	second, err := client.FetchCurrent(context.Background())
	if err != nil {
		t.Fatalf("2回目のFetchCurrent() error = %v", err)
	}

	if requests.Load() != 2 {
		t.Errorf("HTTP要求回数 = %d, want 2", requests.Load())
	}
	if len(first.Quotes) != 1 || first.Quotes[0].Value == nil || *first.Quotes[0].Value != 1 {
		t.Errorf("1回目のQuotes = %+v", first.Quotes)
	}
	if len(second.Quotes) != 1 || second.Quotes[0].Value == nil || *second.Quotes[0].Value != 2 {
		t.Errorf("2回目のQuotes = %+v", second.Quotes)
	}
	if first.Metadata.ETag != "\"response-1\"" || second.Metadata.ETag != "\"response-2\"" {
		t.Errorf("ETag = %q, %q", first.Metadata.ETag, second.Metadata.ETag)
	}
	if second.Metadata.LastModified != "Sun, 02 Aug 2026 05:00:00 GMT" {
		t.Errorf("Last-Modified = %q", second.Metadata.LastModified)
	}
	if second.Metadata.CacheControl != "public,max-age=3600" {
		t.Errorf("Cache-Control = %q", second.Metadata.CacheControl)
	}
}

// ----------------------------------------

// TestFetchHistoryAlwaysRequestsUpstream は、長期日足も連続取得ごとに無条件GETすることを検証します。
//
// 引数:
//   - t *testing.T: テスト失敗を報告するテストコンテキスト。
//
// 返り値:
//   - なし。
func TestFetchHistoryAlwaysRequestsUpstream(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestNumber := requests.Add(1)
		if request.URL.Path != "/_data/_nfsDATA/HS_DATA_DAY/S111.json" {
			t.Errorf("要求パス = %q", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if request.Header.Get("If-Modified-Since") != "" || request.Header.Get("If-None-Match") != "" {
			t.Errorf("%d回目に条件付きヘッダーが送信されました", requestNumber)
		}

		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("ETag", fmt.Sprintf("\"history-%d\"", requestNumber))
		_, _ = fmt.Fprintf(writer, "var S111=[[1700000000000,%d]];", requestNumber)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	first, err := client.FetchChart(context.Background(), ChartRange6Months, []string{"111"})
	if err != nil {
		t.Fatalf("1回目のFetchChart() error = %v", err)
	}
	second, err := client.FetchChart(context.Background(), ChartRange6Months, []string{"111"})
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
	if len(second.Sources) != 1 || second.Sources[0].ETag != "\"history-2\"" {
		t.Errorf("2回目のSources = %+v", second.Sources)
	}
}
