// Package mcpserver は、共通収集サービスをStreamable HTTP MCPとして公開します。
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	dataListToolName       = "datalist"
	collectToolName        = "collect"
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

// New は、データソース選択の初期指示、datalist、collectを登録したStreamable HTTP MCPを生成します。
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
	dataList := service.DataList()
	dataSourceSummary := buildDataSourceSummary(dataList)
	dataListOutputSchema, err := outputSchemaFor[domain.DataList](
		"利用可能な市場データprovider、dataset、入力項目の一覧です。",
	)
	if err != nil {
		return nil, fmt.Errorf("datalistのoutput schemaを生成できません: %w", err)
	}
	collectOutputSchema, err := outputSchemaFor[domain.CollectResponse](
		"指定したproviderから収集した市場データです。",
	)
	if err != nil {
		return nil, fmt.Errorf("collectのoutput schemaを生成できません: %w", err)
	}

	server := &Server{service: service, logger: logger}
	protocolServer := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{
			Instructions: buildServerInstructions(dataSourceSummary),
			Logger:       logger,
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
		Name: dataListToolName,
		Description: "この会話で現在の一覧を未確認の場合、市場データを収集する前に最初に呼び出し、設定上有効な全providerを比較するための一覧です。" +
			"provider、dataset、入力項目を返し、外部通信は行いません。RESTのGET /api/datalistと同じ仕様です。",
		OutputSchema: dataListOutputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "市場データ一覧",
			ReadOnlyHint:    readOnly,
			DestructiveHint: &notDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   &notDestructive,
		},
	}, server.dataList)
	mcp.AddTool(protocolServer, &mcp.Tool{
		Name: collectToolName,
		Description: "providerとdatasetを指定し、要求時に市場情報を収集して返します。" +
			"この会話で現在の一覧を未確認の場合は先にdatalistを呼び、設定上有効な全providerを比較してから、掲載された識別子と入力項目を指定してください。" +
			"ユーザーがproviderを指定していない場合、一般知識、掲載順、dataset件数を優先度として特定providerを既定選択しないでください。" +
			"RESTのPOST /api/collectと同じ入力・出力です。\n\n設定上有効なデータソース概要:\n" + dataSourceSummary,
		OutputSchema: collectOutputSchema,
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

// buildDataSourceSummary は、設定上有効なproviderの短い一覧を生成します。
//
// 主な特徴:
//   - provider識別子、表示名、用途概要だけを列挙する
//   - datasetと入力項目の詳細はdatalistへ集約して重複させない
//   - 表示用文字列の改行と連続空白を単一の空白へ正規化する
//
// 引数:
//   - dataList domain.DataList: 外部通信なしで取得した現在有効なprovider一覧。
//
// 返り値:
//   - string: MCPの初期指示とtool説明へ掲載するprovider概要。
func buildDataSourceSummary(dataList domain.DataList) string {
	if len(dataList.Providers) == 0 {
		return "- ありません。サーバーのprovider設定を確認してください。"
	}
	var builder strings.Builder
	for _, descriptor := range dataList.Providers {
		_, _ = fmt.Fprintf(
			&builder,
			"- %s（provider: %s）: %s\n",
			strings.Join(strings.Fields(descriptor.DisplayName), " "),
			descriptor.Name,
			strings.Join(strings.Fields(descriptor.Description), " "),
		)
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

// ----------------------------------------

// buildServerInstructions は、MCP接続時にモデルへ渡すデータソース選択手順を生成します。
//
// 主な特徴:
//   - 会話内で未確認の場合は最初のcollectより前にdatalistを確認するよう案内する
//   - 一覧順やdataset件数を優先度にせず、用途に応じて全providerを比較するよう案内する
//   - 選択したprovider、dataset、理由を利用者へ明示するよう案内する
//
// 引数:
//   - dataSourceSummary string: buildDataSourceSummaryで生成した設定上有効なprovider概要。
//
// 返り値:
//   - string: MCPのinitialize応答へ設定する日本語の利用手順。
func buildServerInstructions(dataSourceSummary string) string {
	var builder strings.Builder
	builder.WriteString("このサーバーで市場データを扱う場合は、次の手順を守ってください。\n")
	builder.WriteString("1. この会話で現在の一覧をまだ確認していない場合、最初のcollectより前にdatalistを呼び、設定上有効な全provider、dataset、入力項目を確認してください。\n")
	builder.WriteString("2. ユーザーがproviderを明示していない場合、一般知識、一覧の掲載順、dataset件数を優先度として特定providerを既定選択しないでください。\n")
	builder.WriteString("3. datalistに掲載された全providerを、対象地域、資産、データ種別、入力条件に照らして比較し、目的に最も合うproviderとdatasetを選んでください。同程度に適合する候補がある場合は候補を示し、判断に必要な条件が不足する場合だけユーザーへ確認してください。\n")
	builder.WriteString("4. collectにはdatalistに掲載された識別子と入力項目だけを使用し、存在しない値を推測しないでください。選択したprovider、dataset、選択理由を利用者へ明示してください。\n")
	builder.WriteString("\n設定上有効なデータソース概要（掲載順は優先度を表しません）:\n")
	builder.WriteString(dataSourceSummary)
	return builder.String()
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
//   - *mcp.CallToolResult: REST APIと同じJSONを含むMCP結果。
//   - any: SDKによるSchema再変換を避けるため常にnil。
//   - error: JSON化に失敗した場合のエラー。
func (s *Server) dataList(
	ctx context.Context,
	request *mcp.CallToolRequest,
	input DataListInput,
) (*mcp.CallToolResult, any, error) {
	_ = ctx
	_ = request
	_ = input
	result, err := newStructuredToolResult(s.service.DataList())
	return result, nil, err
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
	toolResult, err := newStructuredToolResult(result)
	return toolResult, nil, err
}

// ----------------------------------------

// newStructuredToolResult は、値を精度を変えずにMCPの構造化結果へ変換します。
//
// 主な特徴:
//   - encoding/jsonで一度だけJSON化する
//   - 同じ生JSONをstructured contentとtext contentへ設定する
//   - SDKのoutput schema検証に伴う動的数値のfloat64再変換を避ける
//
// 引数:
//   - output any: MCPツールから返すJSON化可能な値。
//
// 返り値:
//   - *mcp.CallToolResult: 生JSONを構造化結果とテキスト結果へ設定した値。
//   - error: JSON化に失敗した場合のエラー。
func newStructuredToolResult(output any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("MCPツール出力をJSONへ変換できません: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
		StructuredContent: json.RawMessage(data),
	}, nil
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
