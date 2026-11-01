package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"trailseal/agent"
)

// ============================================================
// Structured execution result — non-Echo tool types
// ============================================================
//
// TestServer_ToolsCall_TenantScopedExecution (server_test.go) already
// locks the Content[]/IsError shape for Echo. These two extend that
// same tools/call -> MCPToolsCallResult contract to the other two
// allowlisted tool types, so "structured execution result" isn't
// only proven for one of the three.

func TestServer_ToolsCall_HealthCheckReturnsStructuredContent(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-hc",
		"health-a",
		agent.ToolTypeHealthCheck,
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
				"name":"health-a",
				"arguments":{}
			}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)

	if response.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", response.Error)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("failed to decode tools/call result: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected successful execution, got error result: %+v", result)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected tool content")
	}

	text := result.Content[0].Text

	if !strings.Contains(text, "healthy") {
		t.Errorf("expected content to contain %q, got %q", "healthy", text)
	}

	if !strings.Contains(text, "status") {
		t.Errorf("expected content to contain %q, got %q", "status", text)
	}
}

func TestServer_ToolsCall_MetadataLookupReturnsStructuredContent(t *testing.T) {
	gateway, db := newTestGateway(t)
	defer db.Close()

	saveTestTool(
		t,
		db,
		"tenant-a",
		"tool-meta",
		"meta-a",
		agent.ToolTypeMetadataLookup,
		map[string]string{"region": "us-east-1"},
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
				"name":"meta-a",
				"arguments":{}
			}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)

	if response.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", response.Error)
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	var result MCPToolsCallResult

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("failed to decode tools/call result: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected successful execution, got error result: %+v", result)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected tool content")
	}

	text := result.Content[0].Text

	if !strings.Contains(text, "region") || !strings.Contains(text, "us-east-1") {
		t.Errorf("expected content to reflect configured metadata, got %q", text)
	}
}
