package mcp

import (
	"strings"

	"trailseal/agent"
)

// ============================================================
// MCP TOOL RESOLVER
// ============================================================

// resolveTool memilih tool dari daftar tools yang SUDAH dibatasi
// oleh tenant.
//
// Resolver tidak pernah mengakses Registry.
// Resolver tidak pernah mencari tool secara global.
//
// Semua input tools harus berasal dari:
//
//	agentStore.ListTools(tenantID)
//
// sehingga tenant boundary tetap berada di layer AgentStore/Gateway.
func resolveTool(req *MCPRequest, tools []*agent.Tool) (*agent.Tool, error) {
	if req == nil {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"request is required",
			nil,
		)
	}

	// --------------------------------------------------------
	// Explicit ToolCall memiliki prioritas tertinggi.
	// --------------------------------------------------------
	if req.ToolCall != nil {
		return resolveExplicitTool(req.ToolCall.ToolName, tools)
	}

	// --------------------------------------------------------
	// Query mode.
	// --------------------------------------------------------
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"query is required",
			nil,
		)
	}

	return resolveQueryTool(query, tools)
}

// resolveExplicitTool mencari tool berdasarkan ID atau Name.
//
// Matching bersifat case-insensitive.
//
// Yang sangat penting:
// fungsi ini hanya menerima "tools" hasil ListTools(tenantID).
// Tidak ada lookup global di sini.
func resolveExplicitTool(
	toolName string,
	tools []*agent.Tool,
) (*agent.Tool, error) {
	target := strings.TrimSpace(toolName)

	if target == "" {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"tool_name is required",
			nil,
		)
	}

	targetLower := strings.ToLower(target)

	for _, tool := range tools {
		if tool == nil {
			continue
		}

		if strings.ToLower(strings.TrimSpace(tool.ID)) == targetLower {
			return tool, nil
		}

		if strings.ToLower(strings.TrimSpace(tool.Name)) == targetLower {
			return tool, nil
		}
	}

	return nil, newMCPError(
		MCPErrorToolNotFound,
		"tool not found: "+toolName,
		nil,
	)
}

// resolveQueryTool memilih tool berdasarkan query.
//
// Urutan:
//  1. exact/name match
//  2. known aliases berdasarkan tool type
//
// Semua matching case-insensitive.
func resolveQueryTool(
	query string,
	tools []*agent.Tool,
) (*agent.Tool, error) {
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))

	if normalizedQuery == "" {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"query is required",
			nil,
		)
	}

	// --------------------------------------------------------
	// First pass: exact tool name / ID references.
	// --------------------------------------------------------
	for _, tool := range tools {
		if tool == nil {
			continue
		}

		name := strings.ToLower(strings.TrimSpace(tool.Name))
		id := strings.ToLower(strings.TrimSpace(tool.ID))

		if name != "" && strings.Contains(normalizedQuery, name) {
			return tool, nil
		}

		if id != "" && strings.Contains(normalizedQuery, id) {
			return tool, nil
		}
	}

	// --------------------------------------------------------
	// Second pass: known semantic aliases.
	//
	// Ini hanya resolver untuk tool types yang saat ini sudah
	// ada di agent package. Tidak ada domain module discovery.
	// --------------------------------------------------------
	for _, tool := range tools {
		if tool == nil {
			continue
		}

		switch tool.Type {
		case agent.ToolTypeHealthCheck:
			if containsKeyword(
				normalizedQuery,
				"health",
				"status",
				"ping",
				"healthy",
			) {
				return tool, nil
			}

		case agent.ToolTypeMetadataLookup:
			if containsKeyword(
				normalizedQuery,
				"metadata",
				"meta",
				"info",
				"detail",
			) {
				return tool, nil
			}

		case agent.ToolTypeEcho:
			if containsKeyword(
				normalizedQuery,
				"echo",
				"repeat",
				"say",
			) {
				return tool, nil
			}
		}
	}

	return nil, newMCPError(
		MCPErrorToolNotFound,
		"no matching tool found for query: "+query,
		nil,
	)
}

// containsKeyword melakukan case-insensitive keyword matching.
func containsKeyword(text string, keywords ...string) bool {
	text = strings.ToLower(text)

	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))

		if keyword == "" {
			continue
		}

		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}
