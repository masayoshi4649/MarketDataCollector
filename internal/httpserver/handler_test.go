package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

/*
TestHandlerRoutesAndAllowsCORS は、HTTPルートとCORS境界を検証します。

機能:
  - healthz、REST、MCPを正しいhandlerへ渡すことを確認する
  - 任意OriginへCORS応答を返すことを確認する
  - OPTIONS要求をOrigin設定なしで処理する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestHandlerRoutesAndAllowsCORS(t *testing.T) {
	rest := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("rest"))
	})
	mcp := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("mcp"))
	})
	handler, err := NewHandler(rest, mcp, Options{RequestTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	healthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Errorf("healthz状態 = %d, 200を期待", healthRecorder.Code)
	}

	for _, path := range []string{"/api/datalist", "/mcp"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Origin", "https://any-client.example")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("%sの状態 = %d, 200を期待", path, recorder.Code)
		}
		if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%sのCORS許可元 = %q, *を期待", path, recorder.Header().Get("Access-Control-Allow-Origin"))
		}
	}

	preflightRequest := httptest.NewRequest(http.MethodOptions, "/api/collect", nil)
	preflightRequest.Header.Set("Origin", "https://browser.example")
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, preflightRequest)
	if preflight.Code != http.StatusNoContent {
		t.Errorf("OPTIONS状態 = %d, 204を期待", preflight.Code)
	}
	if headers := preflight.Header().Get("Access-Control-Allow-Headers"); headers != "Content-Type, MCP-Protocol-Version" {
		t.Errorf("CORS許可ヘッダー = %q", headers)
	}
}

// ----------------------------------------

/*
TestHandlerDoesNotTreatMCPSubpathsAsProtocolEndpoint は、MCP URI境界を検証します。

機能:
  - 標準MCPの正確な/mcpだけをprotocol handlerへ渡す
  - /mcp/collectなど独自経路を404にして標準仕様との混同を防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestHandlerDoesNotTreatMCPSubpathsAsProtocolEndpoint(t *testing.T) {
	handler, err := NewHandler(
		http.NotFoundHandler(),
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}),
		Options{RequestTimeout: time.Second},
	)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp/collect", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("/mcp/collect状態 = %d, 404を期待", recorder.Code)
	}
}

// ----------------------------------------

/*
TestHandlerDoesNotApplyCommonConcurrencyLimit は、HTTP全体の同時実行枠を持たないことを検証します。

機能:
  - 2件の収集要求を同時に後段handlerへ到達させる
  - アプリケーション共通枠による503応答や待機がないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestHandlerDoesNotApplyCommonConcurrencyLimit(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	rest := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started <- struct{}{}
		<-release
		writer.WriteHeader(http.StatusOK)
	})
	handler, err := NewHandler(
		rest,
		http.NotFoundHandler(),
		Options{RequestTimeout: time.Second},
	)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			handler.ServeHTTP(
				httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/hold", nil),
			)
			done <- struct{}{}
		}()
	}
	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		select {
		case <-started:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("後段へ到達した要求数 = %d, 2を期待", requestNumber-1)
		}
	}
	close(release)
	<-done
	<-done
}
