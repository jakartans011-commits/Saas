package mcp

import (
	"testing"

	"trailseal/agent"
)

// ============================================================
// Gateway.discoverTools — tool discovery response
// ============================================================
//
// These exercise discoverTools directly against a real tenant-scoped
// BoltAgentStore (via the newTestGateway/saveTestTool helpers already
// used by server_test.go), so the discovery response is verified
// end-to-end through the actual store, not just through the pure
// buildMCPToolsList formatting function covered in tools_test.go.

func TestDiscoverTools_ReturnsTenantScopedDefinitions(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-1",
		"echo-a",
		agent.ToolTypeEcho,
		nil,
	)

	saveTestTool(
		t,
		db,
		"tenant-b",
		"tool-2",
		"echo-b",
		agent.ToolTypeEcho,
		nil,
	)

	result, err := gateway.discoverTools("tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected exactly 1 tool for tenant-a, got %d", len(result.Tools))
	}

	if result.Tools[0].Name != "echo-a" {
		t.Errorf("expected tool name %q, got %q", "echo-a", result.Tools[0].Name)
	}

	expected := "Echo back any message provided by the user. " +
		"This tool is useful for testing connectivity, validating the MCP gateway, " +
		"and simple response generation. Returns the exact message with prefix 'Echo: '. " +
		"Supports messages up to 16KB in size."
	if result.Tools[0].Description != expected {
		t.Errorf("expected explicit echo description in discovery response, got %q", result.Tools[0].Description)
	}

	if result.Tools[0].InputSchema == nil {
		t.Error("expected non-nil InputSchema in discovery response")
	}
}

func TestDiscoverTools_EmptyTenantIDReturnsInvalidRequestError(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	_, err := gateway.discoverTools("")
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}

	if !IsMCPErrorCode(err, MCPErrorInvalidRequest) {
		t.Errorf("expected MCPErrorInvalidRequest, got %v", MCPErrorCodeOf(err))
	}
}

func TestDiscoverTools_NilGatewayReturnsInternalError(t *testing.T) {
	var gateway *Gateway

	_, err := gateway.discoverTools("tenant-a")
	if err == nil {
		t.Fatal("expected error for nil gateway, got nil")
	}

	if !IsMCPErrorCode(err, MCPErrorInternal) {
		t.Errorf("expected MCPErrorInternal, got %v", MCPErrorCodeOf(err))
	}
}

func TestDiscoverTools_NoToolsForTenantReturnsEmptyList(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	result, err := gateway.discoverTools("tenant-with-no-tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Tools == nil {
		t.Fatal("expected non-nil empty Tools slice, got nil")
	}

	if len(result.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(result.Tools))
	}
}

// ============================================================
// discoverToolDefinitions
// ============================================================

func TestDiscoverToolDefinitions_SkipsNilTools(t *testing.T) {
	tools := []*agent.Tool{
		nil,
		testTool("id-1", "echo-a", agent.ToolTypeEcho),
		nil,
	}

	defs := discoverToolDefinitions(tools)

	if len(defs) != 1 {
		t.Fatalf("expected 1 definition after skipping nils, got %d", len(defs))
	}

	if defs[0].Name != "echo-a" {
		t.Errorf("expected name %q, got %q", "echo-a", defs[0].Name)
	}
}
