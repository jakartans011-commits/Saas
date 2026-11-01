package mcp

import (
	"encoding/json"
	"testing"
)

// ============================================================
// JSON-RPC 2.0 CONTRACT TESTS
// ============================================================

func TestJSONRPCNewResult(t *testing.T) {
	response := newJSONRPCResult(
		"request-1",
		map[string]interface{}{
			"status": "ok",
		},
	)

	if response == nil {
		t.Fatal("expected response")
	}

	if response.JSONRPC != JSONRPCVersion {
		t.Fatalf(
			"expected jsonrpc %q, got %q",
			JSONRPCVersion,
			response.JSONRPC,
		)
	}

	if response.ID != "request-1" {
		t.Fatalf(
			"expected id request-1, got %#v",
			response.ID,
		)
	}

	if response.Error != nil {
		t.Fatalf(
			"expected no error, got %+v",
			response.Error,
		)
	}

	if response.Result == nil {
		t.Fatal("expected result")
	}
}

func TestJSONRPCNewError(t *testing.T) {
	response := newJSONRPCError(
		42,
		JSONRPCInvalidParams,
		"invalid params",
		nil,
	)

	if response == nil {
		t.Fatal("expected response")
	}

	if response.JSONRPC != JSONRPCVersion {
		t.Fatalf(
			"expected jsonrpc %q, got %q",
			JSONRPCVersion,
			response.JSONRPC,
		)
	}

	if response.ID != 42 {
		t.Fatalf(
			"expected id 42, got %#v",
			response.ID,
		)
	}

	if response.Result != nil {
		t.Fatalf(
			"expected nil result, got %#v",
			response.Result,
		)
	}

	if response.Error == nil {
		t.Fatal("expected JSON-RPC error")
	}

	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf(
			"expected code %d, got %d",
			JSONRPCInvalidParams,
			response.Error.Code,
		)
	}

	if response.Error.Message != "invalid params" {
		t.Fatalf(
			"unexpected error message: %q",
			response.Error.Message,
		)
	}
}

// ============================================================
// REQUEST VALIDATION
// ============================================================

func TestValidateJSONRPCRequest_Valid(t *testing.T) {
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      "1",
		Method:  "initialize",
	}

	if err := validateJSONRPCRequest(req); err != nil {
		t.Fatalf(
			"validateJSONRPCRequest() unexpected error: %v",
			err,
		)
	}
}

func TestValidateJSONRPCRequest_Nil(t *testing.T) {
	err := validateJSONRPCRequest(nil)

	if err == nil {
		t.Fatal("expected validation error")
	}

	if err.Error() != "request is required" {
		t.Fatalf(
			"expected request is required, got %q",
			err.Error(),
		)
	}
}

func TestValidateJSONRPCRequest_InvalidVersion(t *testing.T) {
	req := &JSONRPCRequest{
		JSONRPC: "1.0",
		ID:      "1",
		Method:  "initialize",
	}

	err := validateJSONRPCRequest(req)

	if err == nil {
		t.Fatal("expected invalid version error")
	}

	if err.Error() != "invalid jsonrpc version" {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}
}

func TestValidateJSONRPCRequest_EmptyMethod(t *testing.T) {
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      "1",
	}

	err := validateJSONRPCRequest(req)

	if err == nil {
		t.Fatal("expected method validation error")
	}

	if err.Error() != "method is required" {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}
}

// ============================================================
// PARAM DECODING
// ============================================================

func TestDecodeJSONRPCParams_Object(t *testing.T) {
	raw := json.RawMessage(
		`{"name":"echo","arguments":{"message":"hello"}}`,
	)

	var target MCPToolsCallParams

	if err := decodeJSONRPCParams(raw, &target); err != nil {
		t.Fatalf(
			"decodeJSONRPCParams() unexpected error: %v",
			err,
		)
	}

	if target.Name != "echo" {
		t.Fatalf(
			"expected echo, got %q",
			target.Name,
		)
	}

	if target.Arguments["message"] != "hello" {
		t.Fatalf(
			"expected hello, got %#v",
			target.Arguments["message"],
		)
	}
}

func TestDecodeJSONRPCParams_Empty(t *testing.T) {
	var target MCPToolsCallParams

	if err := decodeJSONRPCParams(nil, &target); err != nil {
		t.Fatalf(
			"expected empty params to be valid, got %v",
			err,
		)
	}

	if target.Name != "" {
		t.Fatalf(
			"expected zero-value target, got %q",
			target.Name,
		)
	}
}

func TestDecodeJSONRPCParams_Null(t *testing.T) {
	var target MCPToolsCallParams

	if err := decodeJSONRPCParams(
		json.RawMessage(`null`),
		&target,
	); err != nil {
		t.Fatalf(
			"expected null params to be valid, got %v",
			err,
		)
	}

	if target.Name != "" {
		t.Fatalf(
			"expected zero-value target, got %q",
			target.Name,
		)
	}
}

func TestDecodeJSONRPCParams_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{"name":`)

	var target MCPToolsCallParams

	err := decodeJSONRPCParams(raw, &target)

	if err == nil {
		t.Fatal("expected invalid params error")
	}

	if len(err.Error()) < len("invalid params:") {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}

	if err.Error()[:len("invalid params:")] != "invalid params:" {
		t.Fatalf(
			"expected invalid params prefix, got %q",
			err.Error(),
		)
	}
}

// ============================================================
// JSON SERIALIZATION CONTRACT
// ============================================================

func TestJSONRPCResponse_MarshalSuccess(t *testing.T) {
	response := newJSONRPCResult(
		"abc",
		map[string]interface{}{
			"status": "ok",
		},
	)

	data, err := json.Marshal(response)

	if err != nil {
		t.Fatalf(
			"json.Marshal() unexpected error: %v",
			err,
		)
	}

	var decoded map[string]interface{}

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf(
			"json.Unmarshal() unexpected error: %v",
			err,
		)
	}

	if decoded["jsonrpc"] != "2.0" {
		t.Fatalf(
			"expected jsonrpc 2.0, got %#v",
			decoded["jsonrpc"],
		)
	}

	if decoded["id"] != "abc" {
		t.Fatalf(
			"expected id abc, got %#v",
			decoded["id"],
		)
	}

	if _, exists := decoded["error"]; exists {
		t.Fatalf(
			"expected no error field, got %#v",
			decoded["error"],
		)
	}

	if decoded["result"] == nil {
		t.Fatal("expected result field")
	}
}

func TestJSONRPCResponse_MarshalError(t *testing.T) {
	response := newJSONRPCError(
		"abc",
		JSONRPCMethodNotFound,
		"method not found",
		nil,
	)

	data, err := json.Marshal(response)

	if err != nil {
		t.Fatalf(
			"json.Marshal() unexpected error: %v",
			err,
		)
	}

	var decoded map[string]interface{}

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf(
			"json.Unmarshal() unexpected error: %v",
			err,
		)
	}

	if decoded["jsonrpc"] != "2.0" {
		t.Fatalf(
			"expected jsonrpc 2.0, got %#v",
			decoded["jsonrpc"],
		)
	}

	if _, exists := decoded["result"]; exists {
		t.Fatalf(
			"expected no result field, got %#v",
			decoded["result"],
		)
	}

	errorObject, ok := decoded["error"].(map[string]interface{})

	if !ok {
		t.Fatalf(
			"expected error object, got %#v",
			decoded["error"],
		)
	}

	if errorObject["code"] != float64(JSONRPCMethodNotFound) {
		t.Fatalf(
			"unexpected error code: %#v",
			errorObject["code"],
		)
	}

	if errorObject["message"] != "method not found" {
		t.Fatalf(
			"unexpected error message: %#v",
			errorObject["message"],
		)
	}
}

// ============================================================
// JSON-RPC ERROR CONSTANTS
// ============================================================

func TestJSONRPCErrorConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{
			name: "parse error",
			got:  JSONRPCParseError,
			want: -32700,
		},
		{
			name: "invalid request",
			got:  JSONRPCInvalidRequest,
			want: -32600,
		},
		{
			name: "method not found",
			got:  JSONRPCMethodNotFound,
			want: -32601,
		},
		{
			name: "invalid params",
			got:  JSONRPCInvalidParams,
			want: -32602,
		},
		{
			name: "internal error",
			got:  JSONRPCInternalError,
			want: -32603,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf(
					"expected %d, got %d",
					tt.want,
					tt.got,
				)
			}
		})
	}
}

// ============================================================
// ID COMPATIBILITY
// ============================================================

func TestJSONRPCRequest_StringID(t *testing.T) {
	raw := []byte(`{
		"jsonrpc":"2.0",
		"id":"request-123",
		"method":"initialize"
	}`)

	var req JSONRPCRequest

	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf(
			"json.Unmarshal() unexpected error: %v",
			err,
		)
	}

	if req.ID != "request-123" {
		t.Fatalf(
			"expected string ID, got %#v",
			req.ID,
		)
	}
}

func TestJSONRPCRequest_NumericID(t *testing.T) {
	raw := []byte(`{
		"jsonrpc":"2.0",
		"id":123,
		"method":"initialize"
	}`)

	var req JSONRPCRequest

	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf(
			"json.Unmarshal() unexpected error: %v",
			err,
		)
	}

	if req.ID != float64(123) {
		t.Fatalf(
			"expected numeric ID 123, got %#v",
			req.ID,
		)
	}
}
