package mcp

import (
	"fmt"

	"trailseal/agent"
)

// ============================================================
// MCP DISCOVERY
// ============================================================

// discoverTools mengambil tools berdasarkan authenticated tenant.
//
// CRITICAL:
// Tidak ada global Registry lookup.
// Semua discovery berasal dari AgentStore.ListTools(tenantID).
func (g *Gateway) discoverTools(
	tenantID string,
) (MCPToolsListResult, error) {
	if g == nil || g.agentStore == nil {
		return MCPToolsListResult{}, InternalMCPError(
			fmt.Errorf("mcp gateway is not initialized"),
		)
	}

	if tenantID == "" {
		return MCPToolsListResult{}, newMCPError(
			MCPErrorInvalidRequest,
			"tenant_id is required",
			nil,
		)
	}

	tools, err := g.agentStore.ListTools(tenantID)
	if err != nil {
		return MCPToolsListResult{}, InternalMCPError(
			fmt.Errorf("failed to list tools: %w", err),
		)
	}

	return buildMCPToolsList(tools), nil
}

// discoverToolDefinitions adalah helper internal untuk testing
// dan future protocol extensions.
func discoverToolDefinitions(
	tools []*agent.Tool,
) []MCPToolDefinition {
	result := buildMCPToolsList(tools)

	return result.Tools
}
