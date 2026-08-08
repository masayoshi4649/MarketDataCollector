package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

type fakeService struct {
	dataList domain.DataList
	result   domain.CollectResponse
	err      error
	requests []domain.CollectRequest
}

/*
DataList は、テスト用の固定provider一覧を返します。

機能:
  - MCPのdatalistツールへ設定済み一覧を提供する

引数:
  - なし

返り値:
  - domain.DataList: テスト用の固定一覧
*/
func (f *fakeService) DataList() domain.DataList {
	return f.dataList
}

/*
Collect は、MCP入力を記録してテスト用の固定結果を返します。

機能:
  - MCPとRESTで共有するDTOがそのまま渡されることを確認可能にする

引数:
  - ctx context.Context: MCP要求のコンテキスト
  - request domain.CollectRequest: MCPから復号された共通要求

返り値:
  - domain.CollectResponse: テスト用の固定結果
  - error: テストで設定したエラー
*/
func (f *fakeService) Collect(
	ctx context.Context,
	request domain.CollectRequest,
) (domain.CollectResponse, error) {
	_ = ctx
	f.requests = append(f.requests, request)
	return f.result, f.err
}

// ----------------------------------------

/*
TestServerExposesSharedDataListAndCollectTools は、標準MCPの結合を検証します。

機能:
  - 公式MCPクライアントでStreamable HTTPへ接続する
  - datalistとcollectだけが公開されることを確認する
  - RESTと同じ共通DTOが構造化結果とサービス入力に使われることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServerExposesSharedDataListAndCollectTools(t *testing.T) {
	service := &fakeService{
		dataList: domain.DataList{Version: domain.APIVersion, Providers: []domain.ProviderDescriptor{{
			Name: "test", DisplayName: "テスト",
			Datasets: []domain.DatasetDescriptor{{
				Name: "prices", Description: "テスト価格", Parameters: []domain.ParameterDescriptor{},
			}},
		}}},
		result: domain.CollectResponse{
			Version: domain.APIVersion, Provider: "test", Dataset: "prices",
			Data: map[string]any{"price": float64(123), "large_integer": json.Number("9007199254740993")},
		},
	}
	server := newTestServer(t, service)
	session := connectTestClient(t, server.Handler())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	toolsByName := make(map[string]*mcp.Tool, len(tools.Tools))
	for _, tool := range tools.Tools {
		toolsByName[tool.Name] = tool
	}
	if len(tools.Tools) != 2 {
		t.Fatalf("tool件数 = %d, 2を期待", len(tools.Tools))
	}
	if _, exists := toolsByName["datalist"]; !exists {
		t.Fatal("datalistツールがありません")
	}
	if _, exists := toolsByName["collect"]; !exists {
		t.Fatal("collectツールがありません")
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint ||
			tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Errorf("tool注釈 = %+v, 読み取り専用・非破壊を期待", tool.Annotations)
		}
	}
	assertToolOutputSchema(t, toolsByName["datalist"], "version", "providers")
	assertToolOutputSchema(
		t,
		toolsByName["collect"],
		"version",
		"provider",
		"dataset",
		"collected_at",
		"metadata",
		"data",
	)

	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "datalist", Arguments: map[string]any{}})
	if err != nil || listResult.IsError {
		t.Fatalf("CallTool(datalist) = (%+v, %v)", listResult, err)
	}
	var dataList domain.DataList
	decodeStructuredResult(t, listResult, &dataList)
	if len(dataList.Providers) != 1 {
		t.Fatalf("datalist provider件数 = %d, 1を期待", len(dataList.Providers))
	}
	if dataList.Providers[0].Name != "test" {
		t.Errorf("datalist結果 = %+v, test providerを期待", dataList)
	}
	if len(dataList.Providers[0].Datasets) != 1 {
		t.Fatalf("datalist dataset件数 = %d, 1を期待", len(dataList.Providers[0].Datasets))
	}
	if dataList.Providers[0].Datasets[0].Parameters == nil {
		t.Errorf("datalist parameters = %#v, output schemaどおり空配列を期待", dataList.Providers[0].Datasets[0].Parameters)
	}

	collectResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "collect",
		Arguments: map[string]any{
			"provider": "test", "dataset": "prices",
			"parameters": map[string]any{"symbol": "A"},
		},
	})
	if err != nil || collectResult.IsError {
		t.Fatalf("CallTool(collect) = (%+v, %v)", collectResult, err)
	}
	var response domain.CollectResponse
	decodeStructuredResult(t, collectResult, &response)
	if response.Provider != "test" || response.Dataset != "prices" {
		t.Errorf("collect結果 = %+v, 共通識別子を期待", response)
	}
	textContent, ok := collectResult.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(textContent.Text, `"large_integer":9007199254740993`) {
		t.Errorf("MCP JSON text = %#v, 2^53超整数の精度保持を期待", collectResult.Content)
	}
	if len(service.requests) != 1 || service.requests[0].Parameters["symbol"] != "A" {
		t.Errorf("共通サービス要求 = %+v, symbol=Aを期待", service.requests)
	}
}

// ----------------------------------------

/*
TestOutputSchemaForUsesDomainTypes は、Go型から生成するoutput schemaを検証します。

機能:
  - DataListの入れ子配列と説明がdomain型から生成されることを確認する
  - CollectResponseの日時、動的metadata、動的dataが正しく表現されることを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestOutputSchemaForUsesDomainTypes(t *testing.T) {
	dataListSchema, err := outputSchemaFor[domain.DataList]("データ一覧")
	if err != nil {
		t.Fatalf("DataList output schema生成 error = %v", err)
	}
	providersSchema := dataListSchema.Properties["providers"]
	if providersSchema == nil || providersSchema.Type != "array" || providersSchema.Items == nil {
		t.Fatalf("providers schema = %+v, 要素定義付きarrayを期待", providersSchema)
	}
	datasetsSchema := providersSchema.Items.Properties["datasets"]
	if datasetsSchema == nil || datasetsSchema.Type != "array" || datasetsSchema.Items == nil {
		t.Fatalf("datasets schema = %+v, 要素定義付きarrayを期待", datasetsSchema)
	}
	parametersSchema := datasetsSchema.Items.Properties["parameters"]
	if parametersSchema == nil || parametersSchema.Type != "array" || parametersSchema.Items == nil {
		t.Fatalf("parameters schema = %+v, 要素定義付きarrayを期待", parametersSchema)
	}
	if parametersSchema.Description == "" {
		t.Error("parameters schemaにdomain型の説明を期待")
	}

	collectSchema, err := outputSchemaFor[domain.CollectResponse]("収集結果")
	if err != nil {
		t.Fatalf("CollectResponse output schema生成 error = %v", err)
	}
	collectedAtSchema := collectSchema.Properties["collected_at"]
	if collectedAtSchema == nil || collectedAtSchema.Type != "string" || collectedAtSchema.Format != "date-time" {
		t.Errorf("collected_at schema = %+v, date-time文字列を期待", collectedAtSchema)
	}
	metadataSchema := collectSchema.Properties["metadata"]
	if metadataSchema == nil || metadataSchema.Type != "object" || metadataSchema.AdditionalProperties == nil {
		t.Errorf("metadata schema = %+v, 任意項目を持つobjectを期待", metadataSchema)
	}
	dataSchema := collectSchema.Properties["data"]
	if dataSchema == nil || dataSchema.Type != "" || len(dataSchema.Types) != 0 {
		t.Errorf("data schema = %+v, 型制限なしを期待", dataSchema)
	}
}

// ----------------------------------------

/*
TestNewStructuredToolResultPreservesRawJSON は、MCP成功結果のJSON表現を検証します。

機能:
  - structured contentとtext contentが同じ生JSONになることを確認する
  - 2^53超整数と高精度小数がfloat64へ変換されないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestNewStructuredToolResultPreservesRawJSON(t *testing.T) {
	result, err := newStructuredToolResult(map[string]any{
		"large_integer":   json.Number("9007199254740993"),
		"precise_decimal": json.Number("0.12345678901234567890123456789"),
	})
	if err != nil {
		t.Fatalf("newStructuredToolResult() error = %v", err)
	}
	rawContent, ok := result.StructuredContent.(json.RawMessage)
	if !ok {
		t.Fatalf("StructuredContent = %#v, json.RawMessageを期待", result.StructuredContent)
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content = %#v, TextContentを期待", result.Content)
	}
	if string(rawContent) != textContent.Text {
		t.Errorf("structured content = %s, text content = %s, 同じJSONを期待", rawContent, textContent.Text)
	}
	for _, expected := range []string{"9007199254740993", "0.12345678901234567890123456789"} {
		if !strings.Contains(textContent.Text, expected) {
			t.Errorf("MCP JSON text = %s, %sの精度保持を期待", textContent.Text, expected)
		}
	}
}

// ----------------------------------------

/*
TestServerProtectsHTTPBoundary は、MCPのHTTP受信条件を検証します。

機能:
  - POST以外、JSON以外、圧縮要求、Content-Length超過を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServerProtectsHTTPBoundary(t *testing.T) {
	server := newTestServer(t, &fakeService{})
	testCases := []struct {
		name        string
		method      string
		body        string
		contentType string
		encoding    string
		wantStatus  int
	}{
		{name: "GET", method: http.MethodGet, wantStatus: http.StatusMethodNotAllowed},
		{name: "JSON以外", method: http.MethodPost, body: `{}`, contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "圧縮", method: http.MethodPost, body: `{}`, contentType: "application/json", encoding: "gzip", wantStatus: http.StatusUnsupportedMediaType},
		{name: "過大", method: http.MethodPost, body: strings.Repeat("x", 1025), contentType: "application/json", wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/mcp", strings.NewReader(testCase.body))
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			if testCase.encoding != "" {
				request.Header.Set("Content-Encoding", testCase.encoding)
			}
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != testCase.wantStatus {
				t.Errorf("状態コード = %d, 期待値は%d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
		})
	}
}

/*
newTestServer は、ログを破棄するMCPテストサーバーを生成します。

機能:
  - 1 KiBの本文上限と偽サービスでMCPサーバーを初期化する

引数:
  - t *testing.T: テスト状態を管理する値
  - service Service: テストで利用する共通サービス

返り値:
  - *Server: 初期化済みMCPサーバー
*/
func newTestServer(t *testing.T, service Service) *Server {
	t.Helper()
	server, err := New(service, 1024, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server
}

/*
connectTestClient は、公式MCPクライアントをテストサーバーへ接続します。

機能:
  - httptest.ServerでMCPハンドラーを公開する
  - SSEと再試行を無効にしたStreamable HTTP接続を初期化する

引数:
  - t *testing.T: テスト状態を管理する値
  - handler http.Handler: 公開するMCPハンドラー

返り値:
  - *mcp.ClientSession: 初期化済みの公式MCPクライアントセッション
*/
func connectTestClient(t *testing.T, handler http.Handler) *mcp.ClientSession {
	t.Helper()
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpServer.Client(), MaxRetries: -1, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("Client.Connect() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("ClientSession.Close() error = %v", err)
		}
	})
	return session
}

/*
decodeStructuredResult は、MCP構造化結果を指定したGo値へ変換します。

機能:
  - StructuredContentをJSONとして再符号化して出力型へ復号する

引数:
  - t *testing.T: テスト状態を管理する値
  - result *mcp.CallToolResult: MCPツール呼び出し結果
  - output any: 復号先へのポインター

返り値:
  - なし
*/
func decodeStructuredResult(t *testing.T, result *mcp.CallToolResult, output any) {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("StructuredContentのJSON化 error = %v", err)
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatalf("StructuredContentの復号 error = %v, JSON=%s", err, data)
	}
}

/*
assertToolOutputSchema は、公開ツールのoutput schemaを検証します。

機能:
  - output schemaがobjectとして公開されることを確認する
  - モデルが結果を解釈するために必要なプロパティが定義されることを確認する

引数:
  - t *testing.T: テスト状態を管理する値
  - tool *mcp.Tool: 検証する公開ツール
  - propertyNames ...string: schemaに必要なプロパティ名

返り値:
  - なし
*/
func assertToolOutputSchema(t *testing.T, tool *mcp.Tool, propertyNames ...string) {
	t.Helper()
	if tool == nil {
		t.Fatal("output schemaを検証するツールがありません")
	}
	schema, ok := tool.OutputSchema.(map[string]any)
	if !ok {
		t.Fatalf("%s output schema = %#v, JSON objectを期待", tool.Name, tool.OutputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf("%s output schema type = %#v, objectを期待", tool.Name, schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s output schema properties = %#v, JSON objectを期待", tool.Name, schema["properties"])
	}
	for _, propertyName := range propertyNames {
		if _, exists := properties[propertyName]; !exists {
			t.Errorf("%s output schemaに%sがありません", tool.Name, propertyName)
		}
	}
}
