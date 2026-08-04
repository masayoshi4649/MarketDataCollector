// Package mcpserver は、共通収集サービスをStreamable HTTP MCPとして公開します。
package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

const (
	serverName             = "market-data-collector"
	serverVersion          = "0.1.0"
	defaultMaxRequestBytes = int64(1024 * 1024)
)

// Service は、MCPがREST APIと共有するユースケースを表します。
type Service interface {
	DataList() domain.DataList
	Collect(context.Context, domain.CollectRequest) (domain.CollectResponse, error)
}

// DataListInput は、datalistツールが入力項目を持たないことを表します。
type DataListInput struct{}

// Server は、MCP protocol serverとHTTP境界を管理します。
type Server struct {
	service Service
	logger  *slog.Logger
	handler http.Handler
}

// New は、datalistとcollectを登録したStreamable HTTP MCPを生成します。
//
// 引数:
//   - service Service: REST APIと共有する収集ユースケース。
//   - maxRequestBytes int64: MCP要求本文の最大バイト数。0は1 MiB。
//   - logger *slog.Logger: SDKと内部原因の記録先。nilは既定ロガー。
//
// 返り値:
//   - *Server: /mcpへ登録できるMCPサーバー。
//   - error: service未設定または本文上限が不正な場合のエラー。
func New(service Service, maxRequestBytes int64, logger *slog.Logger) (*Server, error) {
	if service == nil {
		return nil, errors.New("MCP用サービスがありません")
	}
	if maxRequestBytes == 0 {
		maxRequestBytes = defaultMaxRequestBytes
	}
	if maxRequestBytes < 1 {
		return nil, errors.New("MCP要求本文上限は1以上にしてください")
	}
	if logger == nil {
		logger = slog.Default()
	}

	server := &Server{service: service, logger: logger}
	protocolServer := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{
			Logger: logger,
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
			GetSessionID: func() string {
				return ""
			},
		},
	)

	readOnly := true
	notDestructive := false
	openWorld := true
	mcp.AddTool(protocolServer, &mcp.Tool{
		Name:        "datalist",
		Description: "利用可能なprovider、dataset、入力項目を返します。外部通信は行いません。RESTのGET /api/datalistと同じ仕様です。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "市場データ一覧",
			ReadOnlyHint:    readOnly,
			DestructiveHint: &notDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   &notDestructive,
		},
	}, server.dataList)
	mcp.AddTool(protocolServer, &mcp.Tool{
		Name:        "collect",
		Description: "providerとdatasetを指定し、要求時に市場情報を収集して返します。RESTのPOST /api/collectと同じ入力・出力です。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "市場データ収集",
			ReadOnlyHint:    readOnly,
			DestructiveHint: &notDestructive,
			IdempotentHint:  false,
			OpenWorldHint:   &openWorld,
		},
	}, server.collect)

	streamableHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server {
			return protocolServer
		},
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
			Logger:       logger,
		},
	)
	limitedHandler := http.MaxBytesHandler(streamableHandler, maxRequestBytes)
	server.handler = protectHTTP(limitedHandler, maxRequestBytes)
	return server, nil
}

// Handler は、HTTP受信条件を含むMCPハンドラーを返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - http.Handler: /mcpへ登録するStreamable HTTPハンドラー。
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ----------------------------------------

// dataList は、外部通信せず固定データセット一覧を返します。
//
// 引数:
//   - ctx context.Context: MCPツール要求のコンテキスト。
//   - request *mcp.CallToolRequest: SDKが検証済みのツール要求。
//   - input DataListInput: 入力項目を持たない値。
//
// 返り値:
//   - *mcp.CallToolResult: SDKへ内容生成を委ねるためのnil。
//   - any: REST APIと同じdomain.DataList。SDKの数値再変換を避けるため動的出力として返す。
//   - error: datalistでは常にnil。
func (s *Server) dataList(
	ctx context.Context,
	request *mcp.CallToolRequest,
	input DataListInput,
) (*mcp.CallToolResult, any, error) {
	_ = ctx
	_ = request
	_ = input
	return nil, s.service.DataList(), nil
}

// collect は、共通サービスを使って要求時収集を実行します。
//
// 引数:
//   - ctx context.Context: 上流取得へ伝える期限とキャンセル。
//   - request *mcp.CallToolRequest: SDKが検証済みのツール要求。
//   - input domain.CollectRequest: REST APIと同じ収集入力。
//
// 返り値:
//   - *mcp.CallToolResult: SDKへ内容生成を委ねるためのnil。
//   - any: REST APIと同じdomain.CollectResponse。JSON整数精度を保持する動的出力。
//   - error: 入力検証またはprovider収集のエラー。
func (s *Server) collect(
	ctx context.Context,
	request *mcp.CallToolRequest,
	input domain.CollectRequest,
) (*mcp.CallToolResult, any, error) {
	_ = request
	result, err := s.service.Collect(ctx, input)
	if err != nil {
		var serviceErr *domain.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.Cause != nil {
			s.logger.Error(
				"MCPの収集処理に失敗しました",
				"code", serviceErr.Kind,
				"error", serviceErr.Cause,
			)
		}
		return nil, nil, err
	}
	return nil, result, nil
}

// ----------------------------------------

// protectHTTP は、MCP Streamable HTTPの受信条件を制限します。
//
// 引数:
//   - next http.Handler: 検証成功後に呼び出すMCPハンドラー。
//   - maxRequestBytes int64: Content-Lengthの最大値。
//
// 返り値:
//   - http.Handler: メソッド、MIME、圧縮、本文サイズを検証するハンドラー。
func protectHTTP(next http.Handler, maxRequestBytes int64) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "POSTだけを利用できます", http.StatusMethodNotAllowed)
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(writer, "Content-Typeはapplication/jsonを指定してください", http.StatusUnsupportedMediaType)
			return
		}
		contentEncoding := strings.TrimSpace(request.Header.Get("Content-Encoding"))
		if contentEncoding != "" && !strings.EqualFold(contentEncoding, "identity") {
			http.Error(writer, "圧縮された要求は受け付けていません", http.StatusUnsupportedMediaType)
			return
		}
		if request.ContentLength > maxRequestBytes {
			http.Error(writer, "要求本文が上限を超えています", http.StatusRequestEntityTooLarge)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
