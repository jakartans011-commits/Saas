package mcp

import (
	"fmt"
	"strings"
	"time"

	"trailseal/agent"
)

// ============================================================
// MCP TOOL EXECUTOR
// ============================================================

// executeTool menjalankan tool yang sebelumnya sudah dipilih
// oleh resolver.
//
// Security boundary:
//
//	Gateway
//	  ↓
//	ListTools(tenantID)
//	  ↓
//	resolveTool(...)
//	  ↓
//	executeTool(...)
//
// Executor tidak melakukan global tool lookup.
func executeTool(
	tool *agent.Tool,
	params map[string]interface{},
	execCtx MCPExecutionContext,
) (interface{}, error) {

	if tool == nil {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"tool is required",
			nil,
		)
	}

	// Identity/context sengaja diterima sebagai parameter execution
	// sehingga execution layer tidak perlu membaca Fiber context,
	// JWT, atau HTTP state secara langsung.
	//
	// Identity tidak pernah diambil dari params.
	_ = execCtx

	switch tool.Type {
	case agent.ToolTypeHealthCheck:
		return executeHealthCheck(tool, params)

	case agent.ToolTypeMetadataLookup:
		return executeMetadataLookup(tool, params)

	case agent.ToolTypeEcho:
		return executeEcho(tool, params)

	case agent.ToolTypeAuditLookup:
		return executeAuditLookup(tool, params, execCtx)

	default:
		return nil, newMCPError(
			MCPErrorUnsupportedTool,
			fmt.Sprintf("unsupported tool type: %s", tool.Type),
			nil,
		)
	}
}

// executeHealthCheck adalah allowlisted health-check execution.
func executeHealthCheck(
	tool *agent.Tool,
	params map[string]interface{},
) (interface{}, error) {

	_ = tool
	_ = params

	return map[string]interface{}{
		"status":    "healthy",
		"message":   "All systems operational",
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}

// executeMetadataLookup mengembalikan metadata dari konfigurasi
// tool yang memang sudah disimpan pada agent.Tool.
func executeMetadataLookup(
	tool *agent.Tool,
	params map[string]interface{},
) (interface{}, error) {

	_ = params

	metadata := make(map[string]string)

	if tool.Config != nil {
		for key, value := range tool.Config {
			metadata[key] = value
		}
	}

	metadata["lookup_time"] = time.Now().Format(time.RFC3339)

	return metadata, nil
}

// executeEcho menjalankan echo terhadap value yang diberikan.
//
// Supported parameter names:
//   - message
//   - text
//   - query
//
// Gateway query mode akan meneruskan query MCP sebagai "message".
func executeEcho(
	tool *agent.Tool,
	params map[string]interface{},
) (interface{}, error) {

	_ = tool

	if len(params) == 0 {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"echo message is required",
			nil,
		)
	}

	var message string

	for _, key := range []string{
		"message",
		"text",
		"query",
	} {
		value, ok := params[key]

		if !ok || value == nil {
			continue
		}

		message = strings.TrimSpace(fmt.Sprint(value))

		if message != "" {
			break
		}
	}

	if message == "" {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"echo message is required",
			nil,
		)
	}

	return "Echo: " + message, nil
}

// formatExecutionResult mengubah hasil execution menjadi response
// string untuk MCPResponse.
func formatExecutionResult(result interface{}) string {
	return fmt.Sprint(result)
}

// executeAuditLookup - Query audit log untuk context injection
func executeAuditLookup(
	tool *agent.Tool,
	params map[string]interface{},
	execCtx MCPExecutionContext,
) (interface{}, error) {
	// Ambil limit dari params (default 10)
	limit := 10
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	if limit > 50 {
		limit = 50
	}

	if execCtx.AuditStore == nil {
		return nil, fmt.Errorf("audit lookup unavailable: auditStore not configured")
	}

	records, err := execCtx.AuditStore.ListForTenant(execCtx.TenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("audit lookup failed: %w", err)
	}

	events := make([]map[string]interface{}, 0, len(records))
	for _, e := range records {
		events = append(events, map[string]interface{}{
			"action":    e.Action,
			"outcome":   string(e.Outcome),
			"timestamp": e.Timestamp.Format(time.RFC3339),
			"resource":  e.Resource,
		})
	}

	return map[string]interface{}{
		"events":      events,
		"total_count": len(events),
		"query": map[string]interface{}{
			"limit":  limit,
			"tenant": execCtx.TenantID,
		},
	}, nil
}