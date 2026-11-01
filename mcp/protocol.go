package mcp

import (
	"errors"
	"fmt"
	"strings"
	
	"trailseal/audit"
)

// ============================================================
// MCP PROTOCOL
// ============================================================

// MCPErrorCode adalah taxonomy error MCP yang stabil.
//
// HTTP layer boleh memetakan code ini ke status HTTP tanpa harus
// melakukan parsing string error.
type MCPErrorCode string

const (
	MCPErrorInvalidRequest   MCPErrorCode = "invalid_request"
	MCPErrorToolNotFound     MCPErrorCode = "tool_not_found"
	MCPErrorUnsupportedTool  MCPErrorCode = "unsupported_tool_type"
	MCPErrorInternal         MCPErrorCode = "internal_error"
	MCPErrorQuotaExceeded    MCPErrorCode = "quota_exceeded"
	MCPErrorResourceNotFound MCPErrorCode = "resource_not_found"
	MCPErrorPromptNotFound   MCPErrorCode = "prompt_not_found"
	// MCPErrorForbidden — dipakai prompts/create: caller bukan Owner,
	// atau tenant bukan tier Business. Bukan "not found", bukan
	// "invalid params" — kedua kondisi itu sudah dipakai untuk arti
	// lain di taxonomy ini, jadi butuh code sendiri (lihat
	// jsonRPCResponseFromMCPError di server.go untuk pemetaan JSON-RPC).
	MCPErrorForbidden MCPErrorCode = "forbidden"
)

// ============================================================
// INPUT LIMITS
// ============================================================
//
// Batas ini berlaku pada MCP protocol boundary.
//
// Tujuannya:
//   - mencegah request/query berukuran tidak wajar
//   - membatasi tool argument abuse
//   - mencegah map/string explosion sebelum execution
//   - menjaga behavior tenant isolation tetap terpisah dari
//     parameter tool
//
// Nilai ini sengaja cukup longgar untuk penggunaan SaaS normal,
// tetapi tetap memberikan explicit security boundary.
//

const (
	// Maximum query size in bytes.
	MCPMaxQueryBytes = 8 * 1024

	// Maximum explicit tool name size in bytes.
	MCPMaxToolNameBytes = 256

	// Maximum number of tool arguments.
	MCPMaxArgumentCount = 64

	// Maximum argument key size in bytes.
	MCPMaxArgumentKeyBytes = 256

	// Maximum string argument size in bytes.
	MCPMaxArgumentStringBytes = 16 * 1024

	// Maximum nesting depth for argument objects/arrays.
	MCPMaxArgumentDepth = 8

	// Maximum number of context entries.
	MCPMaxContextEntries = 32

	// Maximum context key size in bytes.
	MCPMaxContextKeyBytes = 256

	// Maximum context value size in bytes.
	MCPMaxContextValueBytes = 4 * 1024
)

// ============================================================
// MCP ERROR
// ============================================================

// MCPError adalah typed error untuk boundary MCP.
//
// Message tetap dipertahankan agar behavior response yang sudah
// ada tidak berubah, sementara Code memberikan taxonomy yang
// stabil untuk HTTP layer dan future protocol clients.
type MCPError struct {
	Code    MCPErrorCode
	Message string
	Err     error
}

func (e *MCPError) Error() string {
	if e == nil {
		return ""
	}

	if e.Message != "" {
		return e.Message
	}

	if e.Err != nil {
		return e.Err.Error()
	}

	return string(e.Code)
}

func (e *MCPError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func newMCPError(
	code MCPErrorCode,
	message string,
	err error,
) error {
	return &MCPError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// ============================================================
// MCP REQUEST
// ============================================================

// MCPRequest adalah request yang diterima oleh MCP Gateway.
//
// Dua mode didukung:
//
// 1. Query mode:
//
//	{
//	  "query": "check health",
//	  "params": {...},
//	  "context": {...}
//	}
//
// 2. Explicit tool-call mode:
//
//	{
//	  "tool_call": {
//	    "tool_name": "health-check",
//	    "params": {...}
//	  },
//	  "context": {...}
//	}
//
// Jika ToolCall tersedia, explicit tool selection memiliki
// prioritas dibanding Query.
type MCPRequest struct {
	Query    string                 `json:"query,omitempty"`
	Context  map[string]string      `json:"context,omitempty"`
	Params   map[string]interface{} `json:"params,omitempty"`
	ToolCall *MCPToolCall           `json:"tool_call,omitempty"`
}

// Validate melakukan structural dan security validation terhadap
// request.
//
// Aturan protocol:
//
//   - request wajib ada
//   - minimal Query atau ToolCall harus tersedia
//   - Query boleh kosong jika ToolCall tersedia
//   - ToolCall.ToolName wajib ada jika ToolCall digunakan
//   - query memiliki maximum size
//   - context memiliki bounded size
//   - params memiliki bounded structure
//
// TenantID/UserID tidak divalidasi dari request ini karena keduanya
// bukan bagian dari MCPRequest yang authoritative.
func (r *MCPRequest) Validate() error {
	if r == nil {
		return newMCPError(
			MCPErrorInvalidRequest,
			"request is required",
			nil,
		)
	}

	if r.Query != "" {
		if len([]byte(r.Query)) > MCPMaxQueryBytes {
			return newMCPError(
				MCPErrorInvalidRequest,
				"query exceeds maximum size",
				nil,
			)
		}
	}

	if err := validateMCPContext(r.Context); err != nil {
		return err
	}

	if err := validateMCPParams(r.Params); err != nil {
		return err
	}

	if r.ToolCall == nil {
		if strings.TrimSpace(r.Query) == "" {
			return newMCPError(
				MCPErrorInvalidRequest,
				"query is required",
				nil,
			)
		}

		return nil
	}

	return r.ToolCall.Validate()
}

// ============================================================
// MCP RESPONSE
// ============================================================

// MCPResponse adalah response dari MCP Gateway.
type MCPResponse struct {
	Response        string            `json:"response"`
	ToolsUsed       []string          `json:"tools_used,omitempty"`
	ExecutionTimeMs int64             `json:"execution_time_ms"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// ============================================================
// MCP TOOL CALL
// ============================================================

// MCPToolCall merepresentasikan explicit tool call.
//
// ToolName dapat berupa:
//   - tool ID
//   - tool Name
//
// Pencarian tetap dibatasi terhadap tools milik tenant yang sedang
// authenticated.
type MCPToolCall struct {
	ToolName string                 `json:"tool_name"`
	Params   map[string]interface{} `json:"params,omitempty"`
}

// Validate melakukan structural dan security validation terhadap
// ToolCall.
func (t *MCPToolCall) Validate() error {
	if t == nil {
		return newMCPError(
			MCPErrorInvalidRequest,
			"tool_call is required",
			nil,
		)
	}

	if strings.TrimSpace(t.ToolName) == "" {
		return newMCPError(
			MCPErrorInvalidRequest,
			"tool_name is required",
			nil,
		)
	}

	if len([]byte(t.ToolName)) > MCPMaxToolNameBytes {
		return newMCPError(
			MCPErrorInvalidRequest,
			"tool_name exceeds maximum size",
			nil,
		)
	}

	if err := validateMCPParams(t.Params); err != nil {
		return err
	}

	return nil
}

// ============================================================
// MCP EXECUTION CONTEXT
// ============================================================

// MCPExecutionContext adalah identity/context yang diberikan
// kepada execution layer.
//
// TenantID dan UserID berasal dari authenticated request,
// bukan dari MCPRequest.Context.
//
// RequestID adalah correlation ID yang dibuat oleh Gateway per
// request (lihat tracing.go), diberikan agar execution layer bisa
// menyertakan ID yang sama pada log/trace tanpa membuat ID baru.
type MCPExecutionContext struct {
	TenantID  string
	UserID    string
	Context   map[string]string
	RequestID string
	AuditStore audit.Store
}

// ============================================================
// ERROR HELPERS
// ============================================================

// MCPErrorCodeOf mengambil taxonomy code dari error.
//
// Error yang bukan MCPError dianggap internal error.
func MCPErrorCodeOf(err error) MCPErrorCode {
	if err == nil {
		return ""
	}

	var mcpErr *MCPError

	if errors.As(err, &mcpErr) && mcpErr != nil {
		return mcpErr.Code
	}

	return MCPErrorInternal
}

// IsMCPErrorCode memeriksa taxonomy error secara aman.
func IsMCPErrorCode(err error, code MCPErrorCode) bool {
	return MCPErrorCodeOf(err) == code
}

// InternalMCPError membungkus error internal agar detail storage/
// infrastructure tidak perlu bocor ke HTTP response.
func InternalMCPError(err error) error {
	if err == nil {
		return newMCPError(
			MCPErrorInternal,
			"mcp_internal_error",
			nil,
		)
	}

	return newMCPError(
		MCPErrorInternal,
		"mcp_internal_error",
		err,
	)
}

// ensureMCPError mempertahankan MCPError yang sudah typed.
// Error biasa dianggap internal.
func ensureMCPError(err error) error {
	if err == nil {
		return nil
	}

	var mcpErr *MCPError

	if errors.As(err, &mcpErr) && mcpErr != nil {
		return err
	}

	return InternalMCPError(err)
}

// ============================================================
// INTERNAL VALIDATION HELPERS
// ============================================================

func invalidRequestError(message string) error {
	return newMCPError(
		MCPErrorInvalidRequest,
		fmt.Sprint(message),
		nil,
	)
}

// validateMCPContext membatasi ukuran context yang datang dari
// caller. Context bukan authoritative identity.
func validateMCPContext(
	context map[string]string,
) error {
	if len(context) > MCPMaxContextEntries {
		return newMCPError(
			MCPErrorInvalidRequest,
			"context exceeds maximum number of entries",
			nil,
		)
	}

	for key, value := range context {
		if len([]byte(key)) > MCPMaxContextKeyBytes {
			return newMCPError(
				MCPErrorInvalidRequest,
				"context key exceeds maximum size",
				nil,
			)
		}

		if len([]byte(value)) > MCPMaxContextValueBytes {
			return newMCPError(
				MCPErrorInvalidRequest,
				"context value exceeds maximum size",
				nil,
			)
		}
	}

	return nil
}

// validateMCPParams membatasi parameter tool.
//
// Parameter tetap generic karena setiap tool boleh memiliki schema
// berbeda. Validation di sini hanya melakukan protocol-level
// resource bounding, bukan business validation.
func validateMCPParams(
	params map[string]interface{},
) error {
	if len(params) > MCPMaxArgumentCount {
		return newMCPError(
			MCPErrorInvalidRequest,
			"arguments exceed maximum number of entries",
			nil,
		)
	}

	for key, value := range params {
		if len([]byte(key)) > MCPMaxArgumentKeyBytes {
			return newMCPError(
				MCPErrorInvalidRequest,
				"argument key exceeds maximum size",
				nil,
			)
		}

		if err := validateMCPValue(
			value,
			0,
		); err != nil {
			return err
		}
	}

	return nil
}

// validateMCPValue melakukan bounded recursive validation terhadap
// JSON-like values:
//
//   - string
//   - map[string]interface{}
//   - []interface{}
//   - primitive values
//
// Tipe lain dibiarkan karena decoder/runtime dapat menggunakan tipe
// custom, tetapi nested map/array tetap dibatasi bila ditemukan.
func validateMCPValue(
	value interface{},
	depth int,
) error {
	if depth > MCPMaxArgumentDepth {
		return newMCPError(
			MCPErrorInvalidRequest,
			"argument nesting exceeds maximum depth",
			nil,
		)
	}

	switch v := value.(type) {
	case string:
		if len([]byte(v)) > MCPMaxArgumentStringBytes {
			return newMCPError(
				MCPErrorInvalidRequest,
				"argument string exceeds maximum size",
				nil,
			)
		}

	case map[string]interface{}:
		if len(v) > MCPMaxArgumentCount {
			return newMCPError(
				MCPErrorInvalidRequest,
				"nested arguments exceed maximum number of entries",
				nil,
			)
		}

		for key, child := range v {
			if len([]byte(key)) > MCPMaxArgumentKeyBytes {
				return newMCPError(
					MCPErrorInvalidRequest,
					"nested argument key exceeds maximum size",
					nil,
				)
			}

			if err := validateMCPValue(
				child,
				depth+1,
			); err != nil {
				return err
			}
		}

	case []interface{}:
		if len(v) > MCPMaxArgumentCount {
			return newMCPError(
				MCPErrorInvalidRequest,
				"nested arguments exceed maximum number of entries",
				nil,
			)
		}

		for _, child := range v {
			if err := validateMCPValue(
				child,
				depth+1,
			); err != nil {
				return err
			}
		}
	}

	return nil
}