package mcp

import (
	"encoding/json"
	"fmt"
)

// ============================================================
// JSON-RPC 2.0
// ============================================================

const (
	JSONRPCVersion = "2.0"

	JSONRPCParseError     = -32700
	JSONRPCInvalidRequest = -32600
	JSONRPCMethodNotFound = -32601
	JSONRPCInvalidParams  = -32602
	JSONRPCInternalError  = -32603
)

// JSONRPCRequest adalah JSON-RPC 2.0 request.
//
// ID menggunakan interface{} agar kompatibel dengan string maupun
// numeric JSON-RPC IDs.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse adalah JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError adalah protocol-level JSON-RPC error.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// newJSONRPCResult membuat response sukses.
func newJSONRPCResult(
	id interface{},
	result interface{},
) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

// newJSONRPCError membuat response error.
func newJSONRPCError(
	id interface{},
	code int,
	message string,
	data interface{},
) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// validateJSONRPCRequest melakukan validation protocol dasar.
func validateJSONRPCRequest(
	req *JSONRPCRequest,
) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}

	if req.JSONRPC != JSONRPCVersion {
		return fmt.Errorf("invalid jsonrpc version")
	}

	if req.Method == "" {
		return fmt.Errorf("method is required")
	}

	return nil
}

// decodeJSONRPCParams decode params menjadi target.
//
// Params boleh tidak ada. Dalam kasus tersebut target tetap zero-value.
func decodeJSONRPCParams(
	raw json.RawMessage,
	target interface{},
) error {
	if len(raw) == 0 {
		return nil
	}

	if string(raw) == "null" {
		return nil
	}

	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	return nil
}
