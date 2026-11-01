package mcp

import (
	"github.com/google/uuid"
)

// ============================================================
// MCP TRACING / CORRELATION ID
// ============================================================
//
// Setiap MCP request mendapat satu correlation ID yang:
//
//   - diletakkan di MCPResponse.Metadata["request_id"], sehingga
//     caller bisa mengaitkan response dengan log server-side.
//   - dipropagasikan ke MCPExecutionContext.RequestID, sehingga
//     execution layer (executor, future planner/resolver logic)
//     bisa menyertakan request_id yang sama pada log/trace tanpa
//     perlu membuat ID baru atau membaca ulang dari luar.
//
// ID ini murni untuk observability. Ia bukan bagian dari identity
// (TenantID/UserID) dan tidak pernah dipakai untuk authorization.

// newRequestID membuat correlation ID baru untuk satu MCP request.
func newRequestID() string {
	return uuid.New().String()
}
