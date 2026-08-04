package nikkei225

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider/nikkei225jp"
)

/*
TestCollectorRejectsInvalidSourceSelectionBeforeHTTP は、上流通信前の入力検証を確認します。

機能:
  - 現在値非対応分類、チャート範囲、チャートコード、米国取引セッションを拒否する
  - 利用者が修正できる失敗をINVALID_ARGUMENTへ分類する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorRejectsInvalidSourceSelectionBeforeHTTP(t *testing.T) {
	collector, err := New(&nikkei225jp.Client{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	testCases := []struct {
		name       string
		dataset    string
		parameters map[string]any
	}{
		{name: "現在値配信なし", dataset: "current", parameters: map[string]any{"section": "adr"}},
		{name: "未知チャート範囲", dataset: "chart", parameters: map[string]any{"range": "year"}},
		{name: "許可外チャートコード", dataset: "chart", parameters: map[string]any{"codes": []any{"../../secret"}}},
		{name: "単一専用コードの複数指定", dataset: "chart", parameters: map[string]any{"section": "top", "codes": []any{"151", "643"}}},
		{name: "未知米国セッション", dataset: "us_equities", parameters: map[string]any{"session": "overnight"}},
		{name: "大文字違いの入力項目", dataset: "current", parameters: map[string]any{"Section": "japan"}},
		{name: "JSON安全整数超過", dataset: "chart", parameters: map[string]any{"from_millis": int64(9007199254740992)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, collectErr := collector.Collect(context.Background(), testCase.dataset, testCase.parameters)
			var serviceErr *domain.ServiceError
			if !errors.As(collectErr, &serviceErr) || serviceErr.Kind != domain.ErrorInvalidArgument {
				t.Fatalf("Collect() error = %v, INVALID_ARGUMENTを期待", collectErr)
			}
		})
	}
}

// ----------------------------------------

/*
TestDescriptorSectionAllowListsMatchAvailability は、datalistの市場分類候補を検証します。

機能:
  - currentには現在値配信がある分類だけを掲載する
  - chartには短期または履歴チャート配信がある分類だけを掲載する
  - 追加数値表だけのadrを両方から除外する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestDescriptorSectionAllowListsMatchAvailability(t *testing.T) {
	collector, err := New(&nikkei225jp.Client{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	datasets := collector.Descriptor().Datasets
	for _, datasetName := range []string{"current", "chart"} {
		var allowed []string
		for _, dataset := range datasets {
			if dataset.Name != datasetName {
				continue
			}
			for _, parameter := range dataset.Parameters {
				if parameter.Name == "section" {
					allowed = parameter.Allowed
				}
			}
		}
		if len(allowed) == 0 {
			t.Fatalf("dataset %qのsection候補がありません", datasetName)
		}
		if slices.Contains(allowed, "adr") {
			t.Errorf("dataset %qのsection候補に配信なしのadrがあります", datasetName)
		}
	}
}

// ----------------------------------------

/*
TestNewRejectsTypedNilClient は、生成済みCollectorが常に利用可能な依存関係を持つことを検証します。

機能:
  - Client interfaceへ格納されたnilポインターを起動時に拒否する
  - Collect時のnilポインター参照を防止する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewRejectsTypedNilClient(t *testing.T) {
	var client *nikkei225jp.Client
	if _, err := New(client); err == nil {
		t.Fatal("New() error = nil, 型付きnil clientの拒否を期待")
	}
}

// ----------------------------------------

/*
TestCollectorClassifiesMissingCurrentCodesAsInvalidArgument は、配信にない指定コードの分類を確認します。

機能:
  - currentとfx_ratesで配信本文にないコードを指定する
  - 取得クライアントのInputErrorをINVALID_ARGUMENTへ変換する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestCollectorClassifiesMissingCurrentCodesAsInvalidArgument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/javascript")
		switch request.URL.Path {
		case nikkei225jp.DefaultCurrentPath:
			_, _ = writer.Write([]byte("A[111]=\"64362.02_+2494.59_+4.03_07/31_0_65364.73_61948.23\";"))
		case "/_data/_nfsDATA/ajaxindex/ajax_fx_table.js":
			_, _ = writer.Write([]byte("A[511]=\"157.619_-1.902_-1.19_05:59_0\";"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := nikkei225jp.NewClient(nikkei225jp.Config{
		BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	collector, err := New(client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, dataset := range []string{"current", "fx_rates"} {
		t.Run(dataset, func(t *testing.T) {
			_, collectErr := collector.Collect(
				context.Background(),
				dataset,
				map[string]any{"codes": []any{"999"}},
			)
			var serviceErr *domain.ServiceError
			if !errors.As(collectErr, &serviceErr) || serviceErr.Kind != domain.ErrorInvalidArgument {
				t.Fatalf("Collect() error = %v, INVALID_ARGUMENTを期待", collectErr)
			}
		})
	}
}
