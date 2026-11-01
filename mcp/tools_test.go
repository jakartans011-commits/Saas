package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"trailseal/agent"
)

// ============================================================
// buildMCPToolDefinition — structured tool metadata
// ============================================================

func TestBuildMCPToolDefinition_NilToolReturnsEmptyDefaultDefinition(t *testing.T) {
	def := buildMCPToolDefinition(nil)

	if def.Name != "" {
		t.Errorf("expected empty Name for nil tool, got %q", def.Name)
	}

	if def.Description != "" {
		t.Errorf("expected empty Description for nil tool, got %q", def.Description)
	}

	if def.InputSchema["type"] != "object" {
		t.Errorf("expected default InputSchema type object, got %v", def.InputSchema)
	}
}

func TestBuildMCPToolDefinition_EchoSchema(t *testing.T) {
	tool := testTool("id-echo", "echo-a", agent.ToolTypeEcho)

	def := buildMCPToolDefinition(tool)

	if def.Name != "echo-a" {
		t.Errorf("expected Name %q, got %q", "echo-a", def.Name)
	}

	expected := "Echo back any message provided by the user. " +
		"This tool is useful for testing connectivity, validating the MCP gateway, " +
		"and simple response generation. Returns the exact message with prefix 'Echo: '. " +
		"Supports messages up to 16KB in size."
	if def.Description != expected {
		t.Errorf("expected echo description, got %q", def.Description)
	}

	properties, ok := def.InputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %v", def.InputSchema["properties"])
	}

	for _, key := range []string{"message", "text", "query"} {
		if _, present := properties[key]; !present {
			t.Errorf("expected echo schema to declare property %q", key)
		}
	}
}

func TestBuildMCPToolDefinition_HealthCheckSchema(t *testing.T) {
	tool := testTool("id-hc", "health-a", agent.ToolTypeHealthCheck)

	def := buildMCPToolDefinition(tool)

	expected := "Check the health status of all systems. " +
		"Returns current system status including database connectivity, " +
		"service availability, and response times. No parameters required."
	if def.Description != expected {
		t.Errorf("expected health-check description, got %q", def.Description)
	}

	if def.InputSchema["type"] != "object" {
		t.Errorf("expected InputSchema type object, got %v", def.InputSchema)
	}

	properties, ok := def.InputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %v", def.InputSchema["properties"])
	}

	if len(properties) != 0 {
		t.Errorf("expected health-check to declare no input properties, got %v", properties)
	}
}

func TestBuildMCPToolDefinition_MetadataLookupSchema(t *testing.T) {
	tool := testTool("id-meta", "meta-a", agent.ToolTypeMetadataLookup)

	def := buildMCPToolDefinition(tool)

	expected := "Look up metadata and configuration details for this tool. " +
		"Returns tool metadata including creation time, configuration parameters, " +
		"and operational status. No parameters required."
	if def.Description != expected {
		t.Errorf("expected metadata-lookup description, got %q", def.Description)
	}

	properties, ok := def.InputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %v", def.InputSchema["properties"])
	}

	if len(properties) != 0 {
		t.Errorf("expected metadata-lookup to declare no input properties, got %v", properties)
	}
}

func TestBuildMCPToolDefinition_UnknownTypeUsesDefaultSchema(t *testing.T) {
	tool := testTool("id-x", "custom-a", agent.ToolType("something-not-allowlisted"))

	def := buildMCPToolDefinition(tool)

	expected := "Execute the 'custom-a' tool with provided parameters. " +
		"This tool is of type 'something-not-allowlisted' and accepts the specified input schema."
	if def.Description != expected {
		t.Errorf("expected default description, got %q", def.Description)
	}

	if def.InputSchema["type"] != "object" {
		t.Errorf("expected InputSchema type object, got %v", def.InputSchema)
	}
}

// TestBuildMCPToolDefinition_DoesNotExposeInternalID verifies the MCP
// name uses tool.Name, never tool.ID — the internal identifier must
// not leak into the discovery response.
func TestBuildMCPToolDefinition_DoesNotExposeInternalID(t *testing.T) {
	tool := testTool("super-secret-internal-id", "public-tool-name", agent.ToolTypeEcho)

	def := buildMCPToolDefinition(tool)

	if def.Name != "public-tool-name" {
		t.Errorf("expected Name to be the public tool name, got %q", def.Name)
	}

	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	if strings.Contains(string(data), "super-secret-internal-id") {
		t.Errorf("expected internal tool ID not to appear in serialized definition, got %s", string(data))
	}
}

// ============================================================
// buildMCPToolsList
// ============================================================

func TestBuildMCPToolsList_SkipsNilTools(t *testing.T) {
	tools := []*agent.Tool{
		nil,
		testTool("id-1", "echo-a", agent.ToolTypeEcho),
		nil,
		testTool("id-2", "echo-b", agent.ToolTypeEcho),
	}

	result := buildMCPToolsList(tools)

	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools after skipping nils, got %d", len(result.Tools))
	}

	if result.Tools[0].Name != "echo-a" || result.Tools[1].Name != "echo-b" {
		t.Errorf("expected [echo-a, echo-b] in order, got %v", result.Tools)
	}
}

// TestBuildMCPToolsList_EmptyInputProducesEmptyJSONArrayNotNull
// guards the wire contract: MCPToolsListResult.Tools has no
// `omitempty`, so an empty tool list must serialize as `"tools":[]`,
// not `"tools":null` — a JSON-RPC/MCP client iterating the array
// should never need a nil check.
func TestBuildMCPToolsList_EmptyInputProducesEmptyJSONArrayNotNull(t *testing.T) {
	result := buildMCPToolsList(nil)

	if result.Tools == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	if !strings.Contains(string(data), `"tools":[]`) {
		t.Errorf("expected serialized tools to be an empty array, got %s", string(data))
	}
}