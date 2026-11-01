package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"trailseal/agent"
)

// ============================================================
// TEST HELPERS
// ============================================================

// newTestGateway membuat Gateway dengan BoltDB temporary.
//
// CRITICAL:
// Jangan menggunakan:
//
//	bolt.Open("", ...)
//
// karena pada Windows path kosong menghasilkan:
//
//	open : The system cannot find the file specified.
//
// t.TempDir() memberikan directory temporary yang valid dan
// otomatis dibersihkan oleh testing framework.
func newTestGateway(t *testing.T) (*Gateway, *bolt.DB) {
	t.Helper()

	dbPath := filepath.Join(
		t.TempDir(),
		"test.db",
	)

	db, err := bolt.Open(
		dbPath,
		0600,
		nil,
	)
	if err != nil {
		t.Fatalf(
			"failed to open test bolt db: %v",
			err,
		)
	}

	store, err := agent.NewBoltAgentStore(db)
	if err != nil {
		_ = db.Close()

		t.Fatalf(
			"failed to create agent store: %v",
			err,
		)
	}

	gateway := NewGateway(store, nil)

	return gateway, db
}

// saveTestTool membuat tool test ke tenant tertentu.
func saveTestTool(
	t *testing.T,
	db *bolt.DB,
	tenantID string,
	toolID string,
	toolName string,
	toolType agent.ToolType,
	metadata map[string]string,
) {
	t.Helper()

	store, err := agent.NewBoltAgentStore(db)
	if err != nil {
		t.Fatalf(
			"failed to create agent store: %v",
			err,
		)
	}

	tool := &agent.Tool{
		ID:       toolID,
		TenantID: tenantID,
		Name:     toolName,
		Type:     toolType,
		Config:   metadata,
	}

	if err := store.SaveTool(tool); err != nil {
		t.Fatalf(
			"failed to save test tool: %v",
			err,
		)
	}
}

// executeServerRequest menjalankan request HTTP ke MCP Server.
func executeServerRequest(
	t *testing.T,
	server *Server,
	tenantID string,
	userID string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodPost,
		"/mcp",
		bytes.NewBufferString(body),
	)

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	req.Header.Set(
		"X-Tenant-ID",
		tenantID,
	)

	req.Header.Set(
		"X-User-ID",
		userID,
	)

	recorder := httptest.NewRecorder()

	server.ServeHTTP(
		recorder,
		req,
	)

	return recorder
}

// decodeJSONRPCResponse decode response JSON-RPC.
func decodeJSONRPCResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) JSONRPCResponse {
	t.Helper()

	var response JSONRPCResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf(
			"failed to decode JSON-RPC response: %v\nbody=%s",
			err,
			recorder.Body.String(),
		)
	}

	return response
}

// ============================================================
// SERVER TESTS
// ============================================================

func TestServer_Initialize(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"initialize"
		}`,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			recorder.Code,
		)
	}

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.JSONRPC != JSONRPCVersion {
		t.Fatalf(
			"expected jsonrpc %q, got %q",
			JSONRPCVersion,
			response.JSONRPC,
		)
	}

	if response.ID != float64(1) {
		t.Fatalf(
			"expected id 1, got %#v",
			response.ID,
		)
	}

	if response.Error != nil {
		t.Fatalf(
			"unexpected JSON-RPC error: %+v",
			response.Error,
		)
	}

	if response.Result == nil {
		t.Fatal("expected initialize result")
	}
}

func TestServer_ToolsList_TenantScoped(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	saveTestTool(
		t,
		db,
		"tenant-b",
		"tool-b",
		"echo-b",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/list"
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error != nil {
		t.Fatalf(
			"unexpected error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf(
			"failed to marshal result: %v",
			err,
		)
	}

	var result MCPToolsListResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode tools/list result: %v",
			err,
		)
	}

	if len(result.Tools) != 1 {
		t.Fatalf(
			"expected exactly 1 tool for tenant-a, got %d",
			len(result.Tools),
		)
	}

	if result.Tools[0].Name != "echo-a" {
		t.Fatalf(
			"expected echo-a, got %q",
			result.Tools[0].Name,
		)
	}
}

func TestServer_ToolsCall_TenantScopedExecution(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":{
				"name":"echo-a",
				"arguments":{
					"message":"hello tenant-a"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error != nil {
		t.Fatalf(
			"unexpected JSON-RPC error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf(
			"failed to marshal result: %v",
			err,
		)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode tools/call result: %v",
			err,
		)
	}

	if result.IsError {
		t.Fatalf(
			"expected successful tool execution, got error result: %+v",
			result,
		)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected tool content")
	}

	if result.Content[0].Text != "Echo: hello tenant-a" {
		t.Fatalf(
			"expected echo response %q, got %q",
			"Echo: hello tenant-a",
			result.Content[0].Text,
		)
	}
}

func TestServer_ToolsCall_UsesMCPToolCall(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":"tool-call-1",
			"method":"tools/call",
			"params":{
				"name":"echo-a",
				"arguments":{
					"message":"MCPToolCall works"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error != nil {
		t.Fatalf(
			"unexpected JSON-RPC error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf(
			"failed to marshal result: %v",
			err,
		)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode tools/call result: %v",
			err,
		)
	}

	if result.IsError {
		t.Fatalf(
			"expected successful execution, got error: %+v",
			result,
		)
	}

	if len(result.Content) != 1 {
		t.Fatalf(
			"expected exactly one content block, got %d",
			len(result.Content),
		)
	}

	if result.Content[0].Type != "text" {
		t.Fatalf(
			"expected content type text, got %q",
			result.Content[0].Type,
		)
	}

	if result.Content[0].Text != "Echo: MCPToolCall works" {
		t.Fatalf(
			"unexpected tool result: %q",
			result.Content[0].Text,
		)
	}
}

func TestServer_ToolsCall_MapsArgumentsIntoMCPToolCall(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":"tool-call-1",
			"method":"tools/call",
			"params":{
				"name":"echo-a",
				"arguments":{
					"message":"MCPToolCall works"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error != nil {
		t.Fatalf(
			"unexpected JSON-RPC error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf(
			"failed to marshal result: %v",
			err,
		)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode tools/call result: %v",
			err,
		)
	}

	if result.IsError {
		t.Fatalf(
			"expected successful execution, got %+v",
			result,
		)
	}

	if len(result.Content) != 1 {
		t.Fatalf(
			"expected exactly one content item, got %d",
			len(result.Content),
		)
	}

	if result.Content[0].Text != "Echo: MCPToolCall works" {
		t.Fatalf(
			"unexpected tool result: %q",
			result.Content[0].Text,
		)
	}
}

func TestServer_ToolsCall_ByToolID(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":{
				"name":"tool-a",
				"arguments":{
					"message":"called by id"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error != nil {
		t.Fatalf(
			"unexpected error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatal(err)
	}

	if result.IsError {
		t.Fatalf(
			"expected success, got %+v",
			result,
		)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}

	if result.Content[0].Text != "Echo: called by id" {
		t.Fatalf(
			"unexpected response: %q",
			result.Content[0].Text,
		)
	}
}

func TestServer_ToolsCall_CannotEscapeTenant(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	saveTestTool(
		t,
		db,
		"tenant-b",
		"tool-b",
		"echo-b",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":{
				"name":"echo-b",
				"arguments":{
					"message":"tenant escape"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	// Tool tenant-b TIDAK boleh ditemukan ketika authenticated
	// tenant adalah tenant-a.
	if response.Error == nil {
		t.Fatal(
			"expected JSON-RPC error when calling another tenant's tool",
		)
	}

	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf(
			"expected JSON-RPC invalid params, got %d",
			response.Error.Code,
		)
	}
}

func TestServer_ToolsList_DoesNotHonorTenantFromParams(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	saveTestTool(
		t,
		db,
		"tenant-b",
		"tool-b",
		"echo-b",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/list",
			"params":{
				"tenant_id":"tenant-b"
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	// FIX:
	// Sebelumnya kode di sini menggunakan:
	//
	//     result.Content[0].Text
	//
	// padahal "result" belum dideklarasikan dan response ini
	// adalah tools/list, bukan tools/call.
	if response.Error != nil {
		t.Fatalf(
			"unexpected JSON-RPC error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf(
			"failed to marshal result: %v",
			err,
		)
	}

	var result MCPToolsListResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode tools/list result: %v",
			err,
		)
	}

	if len(result.Tools) != 1 {
		t.Fatalf(
			"expected one tenant-a tool, got %d",
			len(result.Tools),
		)
	}

	if result.Tools[0].Name != "echo-a" {
		t.Fatalf(
			"tenant_id in params must not override authenticated tenant: got %q",
			result.Tools[0].Name,
		)
	}
}

func TestServer_ToolsCall_DoesNotHonorTenantFromArguments(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-a",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	saveTestTool(
		t,
		db,
		"tenant-b",
		"tool-b",
		"echo-b",
		agent.ToolTypeEcho,
		nil,
	)

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":{
				"name":"echo-a",
				"arguments":{
					"tenant_id":"tenant-b",
					"message":"must remain tenant-a"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error != nil {
		t.Fatalf(
			"unexpected JSON-RPC error: %+v",
			response.Error,
		)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf(
			"failed to marshal result: %v",
			err,
		)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(
		resultBytes,
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode result: %v",
			err,
		)
	}

	if result.IsError {
		t.Fatalf(
			"expected successful execution, got error result: %+v",
			result,
		)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}

	if result.Content[0].Text != "Echo: must remain tenant-a" {
		t.Fatalf(
			"unexpected response: %q",
			result.Content[0].Text,
		)
	}
}

func TestServer_MissingTenantHeader(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	req := httptest.NewRequest(
		http.MethodPost,
		"/mcp",
		bytes.NewBufferString(
			`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		),
	)

	req.Header.Set("X-User-ID", "user-a")

	recorder := httptest.NewRecorder()

	server.ServeHTTP(
		recorder,
		req,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected error")
	}

	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf(
			"expected invalid request, got %d",
			response.Error.Code,
		)
	}
}

func TestServer_MissingUserHeader(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	req := httptest.NewRequest(
		http.MethodPost,
		"/mcp",
		bytes.NewBufferString(
			`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		),
	)

	req.Header.Set("X-Tenant-ID", "tenant-a")

	recorder := httptest.NewRecorder()

	server.ServeHTTP(
		recorder,
		req,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected error")
	}

	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf(
			"expected invalid request, got %d",
			response.Error.Code,
		)
	}
}

func TestServer_ParseError(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{"jsonrpc":"2.0","id":1,"method":`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected parse error")
	}

	if response.Error.Code != JSONRPCParseError {
		t.Fatalf(
			"expected parse error %d, got %d",
			JSONRPCParseError,
			response.Error.Code,
		)
	}
}

func TestServer_InvalidJSONRPCVersion(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"1.0",
			"id":1,
			"method":"initialize"
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected invalid request error")
	}

	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf(
			"expected invalid request %d, got %d",
			JSONRPCInvalidRequest,
			response.Error.Code,
		)
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"does/not/exist"
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected method not found")
	}

	if response.Error.Code != JSONRPCMethodNotFound {
		t.Fatalf(
			"expected method not found %d, got %d",
			JSONRPCMethodNotFound,
			response.Error.Code,
		)
	}
}

func TestServer_ToolsCall_InvalidParamsJSON(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected parse error")
	}

	if response.Error.Code != JSONRPCParseError {
		t.Fatalf(
			"expected parse error %d, got %d",
			JSONRPCParseError,
			response.Error.Code,
		)
	}
}

func TestServer_ToolsCall_MissingName(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":{
				"arguments":{
					"message":"hello"
				}
			}
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.Error == nil {
		t.Fatal("expected invalid params error")
	}

	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf(
			"expected invalid params %d, got %d",
			JSONRPCInvalidParams,
			response.Error.Code,
		)
	}
}

func TestServer_PreservesStringID(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":"abc-123",
			"method":"initialize"
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.ID != "abc-123" {
		t.Fatalf(
			"expected string ID abc-123, got %#v",
			response.ID,
		)
	}
}

func TestServer_PreservesNumericID(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":42,
			"method":"initialize"
		}`,
	)

	response := decodeJSONRPCResponse(
		t,
		recorder,
	)

	if response.ID != float64(42) {
		t.Fatalf(
			"expected numeric ID 42, got %#v",
			response.ID,
		)
	}
}

func TestServer_ResponseContentType(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"initialize"
		}`,
	)

	contentType := recorder.Header().Get("Content-Type")

	if contentType != "application/json" {
		t.Fatalf(
			"expected application/json, got %q",
			contentType,
		)
	}
}

func TestServer_ResponseIsValidJSON(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	server := NewServer(gateway)

	recorder := executeServerRequest(
		t,
		server,
		"tenant-a",
		"user-a",
		`{
			"jsonrpc":"2.0",
			"id":1,
			"method":"initialize"
		}`,
	)

	var raw interface{}

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&raw,
	); err != nil {
		t.Fatalf(
			"server response is not valid JSON: %v\nbody=%s",
			err,
			recorder.Body.String(),
		)
	}
}
