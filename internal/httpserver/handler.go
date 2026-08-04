// Package httpserver は、REST APIとMCPを単一HTTPサーバーへ安全に合成します。
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Options は、全接続方式へ共通適用するHTTP境界設定です。
type Options struct {
	RequestTimeout time.Duration
}

// NewHandler は、MCPとRESTを同一ポートへ合成して共通制御を追加します。
//
// 引数:
//   - restHandler http.Handler: /healthzと/api配下を処理するRESTハンドラー。
//   - mcpHandler http.Handler: 正確な/mcpだけを処理するStreamable HTTPハンドラー。
//   - options Options: 要求期限の設定。
//
// 返り値:
//   - http.Handler: http.Serverへ設定する最上位ハンドラー。
//   - error: ハンドラー未設定または制限値が不正な場合のエラー。
func NewHandler(restHandler http.Handler, mcpHandler http.Handler, options Options) (http.Handler, error) {
	if restHandler == nil {
		return nil, errors.New("RESTハンドラーがありません")
	}
	if mcpHandler == nil {
		return nil, errors.New("MCPハンドラーがありません")
	}
	if options.RequestTimeout <= 0 {
		return nil, errors.New("要求期限は0秒より長くしてください")
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", restHandler)
	mux.Handle("/api/", restHandler)
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/", notFound)

	protected := &boundaryHandler{
		next:           mux,
		requestTimeout: options.RequestTimeout,
	}
	return protected, nil
}

// ----------------------------------------

type boundaryHandler struct {
	next           http.Handler
	requestTimeout time.Duration
}

// ServeHTTP は、CORSと要求期限を全接続方式へ適用します。
//
// 引数:
//   - writer http.ResponseWriter: 検証失敗または後段応答の書き込み先。
//   - request *http.Request: RESTまたはMCPのHTTP要求。
//
// 返り値:
//   - なし。検証成功時だけ後段ハンドラーを呼び出す。
func (h *boundaryHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")

	if request.Header.Get("Origin") != "" {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version")
		writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	}
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	if request.URL.Path == "/healthz" {
		h.next.ServeHTTP(writer, request)
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	h.next.ServeHTTP(writer, request.WithContext(ctx))
}

// notFound は、未定義ルートをJSON形式で拒否します。
//
// 引数:
//   - writer http.ResponseWriter: HTTP 404の書き込み先。
//   - request *http.Request: 未定義ルートへの要求。
//
// 返り値:
//   - なし。HTTP 404を直接書き込む。
func notFound(writer http.ResponseWriter, request *http.Request) {
	_ = request
	writeBoundaryError(writer, http.StatusNotFound, "NOT_FOUND", "URIが見つかりません")
}

// writeBoundaryError は、接続方式を問わず小さなJSONエラーを返します。
//
// 引数:
//   - writer http.ResponseWriter: エラー応答の書き込み先。
//   - status int: HTTP状態コード。
//   - code string: 機械判定用の安定分類。
//   - message string: 利用者へ公開できる日本語メッセージ。
//
// 返り値:
//   - なし。JSON応答を直接書き込む。
func writeBoundaryError(
	writer http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
