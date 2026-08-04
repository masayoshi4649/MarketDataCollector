// Package restapi は、共通収集サービスを読み取り専用REST APIとして公開します。
package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
	"github.com/masayoshi4649/MarketDataCollector/internal/strictjson"
)

// Service は、REST APIが利用する共通ユースケースを表します。
type Service interface {
	DataList() domain.DataList
	Collect(context.Context, domain.CollectRequest) (domain.CollectResponse, error)
}

const defaultMaxRequestBytes = int64(1024 * 1024)

// Server は、RESTルーティングと安全なJSON応答を管理します。
type Server struct {
	service         Service
	logger          *slog.Logger
	maxRequestBytes int64
	handler         http.Handler
}

// New は、datalist、collect、healthzを公開するRESTハンドラーを生成します。
//
// 引数:
//   - service Service: MCPと共有する収集ユースケース。
//   - maxRequestBytes int64: collect要求本文の最大バイト数。0は1 MiB。
//   - logger *slog.Logger: 内部原因だけを記録するロガー。nilは既定ロガー。
//
// 返り値:
//   - *Server: http.Serverへ登録できるRESTサーバー。
//   - error: service未設定または本文上限が不正な場合のエラー。
func New(service Service, maxRequestBytes int64, logger *slog.Logger) (*Server, error) {
	if service == nil {
		return nil, errors.New("REST API用サービスがありません")
	}
	if maxRequestBytes == 0 {
		maxRequestBytes = defaultMaxRequestBytes
	}
	if maxRequestBytes < 1 {
		return nil, errors.New("REST API要求本文上限は1以上にしてください")
	}
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{service: service, logger: logger, maxRequestBytes: maxRequestBytes}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/datalist", server.dataList)
	mux.HandleFunc("POST /api/collect", server.collect)
	mux.HandleFunc("/api/", server.notFound)
	server.handler = server.responseHeaders(mux)
	return server, nil
}

// Handler は、RESTルートを含むHTTPハンドラーを返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - http.Handler: /healthzと/api配下を処理するハンドラー。
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ----------------------------------------

// health は、外部通信せずプロセスの稼働状態を返します。
//
// 引数:
//   - writer http.ResponseWriter: JSON応答の書き込み先。
//   - request *http.Request: GET /healthz要求。
//
// 返り値:
//   - なし。HTTP 200のJSONを直接書き込む。
func (s *Server) health(writer http.ResponseWriter, request *http.Request) {
	_ = request
	s.writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

// dataList は、providerとdatasetの固定仕様を返します。
//
// 引数:
//   - writer http.ResponseWriter: JSON応答の書き込み先。
//   - request *http.Request: GET /api/datalist要求。
//
// 返り値:
//   - なし。共通サービスの一覧を直接書き込む。
func (s *Server) dataList(writer http.ResponseWriter, request *http.Request) {
	_ = request
	s.writeJSON(writer, http.StatusOK, s.service.DataList())
}

// collect は、JSON要求を検証して要求時収集を実行します。
//
// 引数:
//   - writer http.ResponseWriter: JSON応答の書き込み先。
//   - request *http.Request: POST /api/collect要求。
//
// 返り値:
//   - なし。成功または共通エラーをJSONで直接書き込む。
func (s *Server) collect(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		s.writeError(writer, http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "Content-Typeはapplication/jsonを指定してください")
		return
	}
	encoding := strings.TrimSpace(request.Header.Get("Content-Encoding"))
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		s.writeError(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_CONTENT_ENCODING", "圧縮された要求は受け付けていません")
		return
	}
	if request.ContentLength > s.maxRequestBytes {
		s.writeError(writer, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "要求本文が上限を超えています")
		return
	}

	limitedBody := http.MaxBytesReader(writer, request.Body, s.maxRequestBytes)
	defer limitedBody.Close()
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			s.writeError(writer, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "要求本文が上限を超えています")
			return
		}
		s.writeError(writer, http.StatusBadRequest, string(domain.ErrorInvalidArgument), "JSON要求本文を読み取れません")
		return
	}
	var input domain.CollectRequest
	if err := strictjson.DecodeObject(body, &input, "provider", "dataset", "parameters"); err != nil {
		s.writeError(writer, http.StatusBadRequest, string(domain.ErrorInvalidArgument), "JSON要求本文が不正です")
		return
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawFields); err != nil {
		s.writeError(writer, http.StatusBadRequest, string(domain.ErrorInvalidArgument), "JSON要求本文が不正です")
		return
	}
	if rawParameters, exists := rawFields["parameters"]; exists && bytes.Equal(bytes.TrimSpace(rawParameters), []byte("null")) {
		s.writeError(writer, http.StatusBadRequest, string(domain.ErrorInvalidArgument), "parametersはJSONオブジェクトにしてください")
		return
	}

	result, err := s.service.Collect(request.Context(), input)
	if err != nil {
		s.handleServiceError(writer, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, result)
}

// notFound は、未定義APIまたは既知APIへの非対応メソッドを共通JSON形式で拒否します。
//
// 引数:
//   - writer http.ResponseWriter: JSON応答の書き込み先。
//   - request *http.Request: /api配下への要求。
//
// 返り値:
//   - なし。既知パスはHTTP 405、それ以外はHTTP 404を直接書き込む。
func (s *Server) notFound(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/datalist":
		writer.Header().Set("Allow", http.MethodGet)
		s.writeError(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "このAPIはGETだけを受け付けます")
	case "/api/collect":
		writer.Header().Set("Allow", http.MethodPost)
		s.writeError(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "このAPIはPOSTだけを受け付けます")
	default:
		s.writeError(writer, http.StatusNotFound, string(domain.ErrorNotFound), "APIが見つかりません")
	}
}

// ----------------------------------------

// handleServiceError は、共通失敗分類をHTTP状態コードへ変換します。
//
// 引数:
//   - writer http.ResponseWriter: JSONエラーの書き込み先。
//   - err error: 共通サービスから返されたエラー。
//
// 返り値:
//   - なし。分類済みHTTPエラーを直接書き込む。
func (s *Server) handleServiceError(writer http.ResponseWriter, err error) {
	var serviceErr *domain.ServiceError
	if !errors.As(err, &serviceErr) {
		s.logger.Error("REST APIの収集処理で未分類エラーが発生しました", "error", err)
		s.writeError(writer, http.StatusInternalServerError, string(domain.ErrorInternal), "内部処理に失敗しました")
		return
	}

	status := http.StatusInternalServerError
	switch serviceErr.Kind {
	case domain.ErrorInvalidArgument:
		status = http.StatusBadRequest
	case domain.ErrorNotFound:
		status = http.StatusNotFound
	case domain.ErrorProviderUnavailable:
		status = http.StatusServiceUnavailable
		writer.Header().Set("Retry-After", "1")
	case domain.ErrorUpstream:
		status = http.StatusBadGateway
	case domain.ErrorTimeout:
		status = http.StatusGatewayTimeout
	case domain.ErrorInternal:
		status = http.StatusInternalServerError
	}
	if serviceErr.Cause != nil {
		s.logger.Error(
			"REST APIの収集処理に失敗しました",
			"code", serviceErr.Kind,
			"error", serviceErr.Cause,
		)
	}
	s.writeError(writer, status, string(serviceErr.Kind), serviceErr.Message)
}

// responseHeaders は、全REST応答へ保存・MIME推測防止ヘッダーを追加します。
//
// 引数:
//   - next http.Handler: ヘッダー設定後に呼び出すルートハンドラー。
//
// 返り値:
//   - http.Handler: 共通応答ヘッダーを付けるハンドラー。
func (s *Server) responseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}

// writeJSON は、ステータスとJSON値を安全に書き込みます。
//
// 引数:
//   - writer http.ResponseWriter: 応答の書き込み先。
//   - status int: 書き込むHTTP状態コード。
//   - value any: JSONへ変換する応答値。
//
// 返り値:
//   - なし。変換失敗時はHTTP 500の固定JSONを返してログへ記録する。
func (s *Server) writeJSON(writer http.ResponseWriter, status int, value any) {
	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(value); err != nil {
		s.logger.Error("REST APIのJSON応答を生成できません", "error", err)
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte("{\"error\":{\"code\":\"INTERNAL\",\"message\":\"JSON応答を生成できません\"}}\n"))
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if _, err := writer.Write(buffer.Bytes()); err != nil {
		s.logger.Error("REST APIのJSON応答を書き込めません", "error", err)
	}
}

// writeError は、全RESTエラーを同じJSON形式で返します。
//
// 引数:
//   - writer http.ResponseWriter: 応答の書き込み先。
//   - status int: HTTP状態コード。
//   - code string: 機械判定用の安定したエラー分類。
//   - message string: 利用者へ公開できる日本語メッセージ。
//
// 返り値:
//   - なし。errorオブジェクトをJSONで直接書き込む。
func (s *Server) writeError(writer http.ResponseWriter, status int, code string, message string) {
	s.writeJSON(writer, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
