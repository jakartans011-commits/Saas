package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// AGENT E2E TESTS
// ============================================================

func TestAgent_CreateAndGet(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-agent-1", "Tenant Agent 1",
		"owner-agent@test.test", "password")

	token := env.login(t, "owner-agent@test.test", "password")

	// Create agent
	var createOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-agent-1/agents",
		bearer(token),
		map[string]interface{}{
			"name":         "Test Agent",
			"description":  "A test agent",
			"capabilities": []string{"read", "write"},
			"metadata": map[string]string{
				"version": "1.0.0",
			},
		},
		&createOut,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	agentData, ok := createOut["agent"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected agent object, got %T", createOut["agent"])
	}
	agentID, ok := agentData["id"].(string)
	if !ok || agentID == "" {
		t.Fatalf("expected agent id, got %v", agentData["id"])
	}

	// Get agent
	var getOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-agent-1/agents/"+agentID,
		bearer(token),
		nil,
		&getOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	gotAgent, ok := getOut["agent"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected agent object, got %T", getOut["agent"])
	}
	if gotAgent["name"] != "Test Agent" {
		t.Fatalf("expected name 'Test Agent', got %v", gotAgent["name"])
	}
	if gotAgent["tenant_id"] != "tenant-agent-1" {
		t.Fatalf("expected tenant_id 'tenant-agent-1', got %v", gotAgent["tenant_id"])
	}
}

func TestAgent_List(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-agent-2", "Tenant Agent 2",
		"owner-agent2@test.test", "password")

	token := env.login(t, "owner-agent2@test.test", "password")

	// Create 3 agents
	for i := 0; i < 3; i++ {
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-agent-2/agents",
			bearer(token),
			map[string]interface{}{
				"name":        "Agent " + string(rune('A'+i)),
				"description": "Test agent " + string(rune('A'+i)),
			},
			nil,
		)
		if status != fiber.StatusCreated {
			t.Fatalf("failed to create agent %d: status %d", i, status)
		}
	}

	time.Sleep(50 * time.Millisecond)

	// List agents
	var listOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-agent-2/agents",
		bearer(token),
		nil,
		&listOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	agents, ok := listOut["agents"].([]interface{})
	if !ok {
		t.Fatalf("expected agents array, got %T", listOut["agents"])
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
}

func TestAgent_UpdateStatus(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-agent-3", "Tenant Agent 3",
		"owner-agent3@test.test", "password")

	token := env.login(t, "owner-agent3@test.test", "password")

	// Create agent
	var createOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-agent-3/agents",
		bearer(token),
		map[string]interface{}{
			"name": "Test Agent Status",
		},
		&createOut,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	agentData, _ := createOut["agent"].(map[string]interface{})
	agentID, _ := agentData["id"].(string)

	// Update status to inactive
	var updateOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodPut, "/api/tenants/tenant-agent-3/agents/"+agentID+"/status",
		bearer(token),
		map[string]string{
			"status": "inactive",
		},
		&updateOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	updated, ok := updateOut["agent"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected agent object, got %T", updateOut["agent"])
	}
	if updated["status"] != "inactive" {
		t.Fatalf("expected status 'inactive', got %v", updated["status"])
	}
}

func TestAgent_Delete(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-agent-4", "Tenant Agent 4",
		"owner-agent4@test.test", "password")

	token := env.login(t, "owner-agent4@test.test", "password")

	// Create agent
	var createOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-agent-4/agents",
		bearer(token),
		map[string]interface{}{
			"name": "Agent to Delete",
		},
		&createOut,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	agentData, _ := createOut["agent"].(map[string]interface{})
	agentID, _ := agentData["id"].(string)

	// Delete agent
	status = env.doJSON(t, fiber.MethodDelete, "/api/tenants/tenant-agent-4/agents/"+agentID,
		bearer(token),
		nil,
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	// Get agent should fail
	var getOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-agent-4/agents/"+agentID,
		bearer(token),
		nil,
		&getOut,
	)
	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestAgent_CrossTenantDenied(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-agent-a", "Tenant A",
		"owner-a@test.test", "password-a")
	env.bootstrapTenantOwner(t, "tenant-agent-b", "Tenant B",
		"owner-b@test.test", "password-b")

	tokenA := env.login(t, "owner-a@test.test", "password-a")

	// Tenant A tries to create agent in Tenant B
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-agent-b/agents",
		bearer(tokenA),
		map[string]interface{}{
			"name": "Intruder Agent",
		},
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}

	// Tenant A tries to list agents in Tenant B
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-agent-b/agents",
		bearer(tokenA),
		nil,
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

// ============================================================
// TOOL TESTS
// ============================================================

func TestTool_CreateAndGet(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-tool-1", "Tenant Tool 1",
		"owner-tool@test.test", "password")

	token := env.login(t, "owner-tool@test.test", "password")

	// Create tool
	var createOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-1/tools",
		bearer(token),
		map[string]interface{}{
			"name":        "Health Check Tool",
			"description": "Checks system health",
			"type":        "health_check",
			"config": map[string]string{
				"endpoint": "http://localhost:8080/health",
			},
		},
		&createOut,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	toolData, ok := createOut["tool"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool object, got %T", createOut["tool"])
	}
	toolID, ok := toolData["id"].(string)
	if !ok || toolID == "" {
		t.Fatalf("expected tool id, got %v", toolData["id"])
	}

	// Get tool
	var getOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-tool-1/tools/"+toolID,
		bearer(token),
		nil,
		&getOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	gotTool, ok := getOut["tool"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool object, got %T", getOut["tool"])
	}
	if gotTool["name"] != "Health Check Tool" {
		t.Fatalf("expected name 'Health Check Tool', got %v", gotTool["name"])
	}
}

func TestTool_List(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-tool-2", "Tenant Tool 2",
		"owner-tool2@test.test", "password")

	token := env.login(t, "owner-tool2@test.test", "password")

	// Create 3 tools
	for i := 0; i < 3; i++ {
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-2/tools",
			bearer(token),
			map[string]interface{}{
				"name": "Tool " + string(rune('A'+i)),
				"type": "echo",
			},
			nil,
		)
		if status != fiber.StatusCreated {
			t.Fatalf("failed to create tool %d: status %d", i, status)
		}
	}

	time.Sleep(50 * time.Millisecond)

	var listOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-tool-2/tools",
		bearer(token),
		nil,
		&listOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	tools, ok := listOut["tools"].([]interface{})
	if !ok {
		t.Fatalf("expected tools array, got %T", listOut["tools"])
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
}

func TestTool_Execute(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-tool-exec", "Tenant Tool Exec",
		"owner-exec@test.test", "password")

	token := env.login(t, "owner-exec@test.test", "password")

	// Create tool
	var createOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-exec/tools",
		bearer(token),
		map[string]interface{}{
			"name":        "Echo Tool",
			"description": "Echoes back params",
			"type":        "echo",
			"config": map[string]string{
				"prefix": "ECHO: ",
			},
		},
		&createOut,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	toolData, _ := createOut["tool"].(map[string]interface{})
	toolID, _ := toolData["id"].(string)

	// Execute tool
	var execOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-exec/tools/"+toolID+"/execute",
		bearer(token),
		map[string]interface{}{
			"params": map[string]interface{}{
				"message": "Hello, Agent!",
				"count":   42,
			},
		},
		&execOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	success, ok := execOut["success"].(bool)
	if !ok || !success {
		t.Fatalf("expected success=true, got %v", execOut["success"])
	}

	result, ok := execOut["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %T", execOut["result"])
	}
	if _, ok := result["echo"]; !ok {
		t.Fatalf("expected echo in result, got %v", result)
	}
}

func TestTool_HealthCheck(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-tool-health", "Tenant Tool Health",
		"owner-health@test.test", "password")

	token := env.login(t, "owner-health@test.test", "password")

	// Create health check tool
	var createOut map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-health/tools",
		bearer(token),
		map[string]interface{}{
			"name": "Health Check",
			"type": "health_check",
		},
		&createOut,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	toolData, _ := createOut["tool"].(map[string]interface{})
	toolID, _ := toolData["id"].(string)

	// Execute health check
	var execOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-health/tools/"+toolID+"/execute",
		bearer(token),
		map[string]interface{}{
			"params": map[string]interface{}{},
		},
		&execOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	success, ok := execOut["success"].(bool)
	if !ok || !success {
		t.Fatalf("expected success=true, got %v", execOut["success"])
	}

	result, ok := execOut["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %T", execOut["result"])
	}
	if result["status"] != "healthy" {
		t.Fatalf("expected status='healthy', got %v", result["status"])
	}
}

func TestTool_CrossTenantDenied(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-tool-a", "Tenant A",
		"owner-a@test.test", "password-a")
	env.bootstrapTenantOwner(t, "tenant-tool-b", "Tenant B",
		"owner-b@test.test", "password-b")

	tokenA := env.login(t, "owner-a@test.test", "password-a")

	// Tenant A tries to create tool in Tenant B
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-tool-b/tools",
		bearer(tokenA),
		map[string]interface{}{
			"name": "Intruder Tool",
			"type": "echo",
		},
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

// ============================================================
// MCP TESTS
// ============================================================

func TestMCP_Query(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-mcp-1", "Tenant MCP 1",
		"owner-mcp@test.test", "password")

	token := env.login(t, "owner-mcp@test.test", "password")

	// Create some tools first
	toolTypes := []string{"echo", "health_check", "metadata_lookup"}
	for _, tt := range toolTypes {
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-mcp-1/tools",
			bearer(token),
			map[string]interface{}{
				"name": "Tool " + tt,
				"type": tt,
			},
			nil,
		)
		if status != fiber.StatusCreated {
			t.Fatalf("failed to create tool %s: status %d", tt, status)
		}
	}

	time.Sleep(50 * time.Millisecond)

	// Send MCP query
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-mcp-1/mcp/query",
		bearer(token),
		map[string]interface{}{
			"query": "check system health status",
		},
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	response, ok := out["response"].(string)
	if !ok || response == "" {
		t.Fatalf("expected response string, got %v", out["response"])
	}

	toolsUsed, ok := out["tools_used"].([]interface{})
	if !ok {
		t.Fatalf("expected tools_used array, got %T", out["tools_used"])
	}
	if len(toolsUsed) == 0 {
		t.Fatalf("expected at least one tool used, got 0")
	}
}

func TestMCP_QueryWithEcho(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-mcp-2", "Tenant MCP 2",
		"owner-mcp2@test.test", "password")

	token := env.login(t, "owner-mcp2@test.test", "password")

	// Create echo tool
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-mcp-2/tools",
		bearer(token),
		map[string]interface{}{
			"name": "Echo Tool",
			"type": "echo",
		},
		nil,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("failed to create echo tool: status %d", status)
	}

	time.Sleep(50 * time.Millisecond)

	// Send MCP query with echo
	var out map[string]interface{}
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-mcp-2/mcp/query",
		bearer(token),
		map[string]interface{}{
			"query": "echo Hello World",
		},
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	response, ok := out["response"].(string)
	if !ok || response == "" {
		t.Fatalf("expected response string, got %v", out["response"])
	}
	if response != "Echo: echo Hello World" {
		t.Fatalf("expected 'Echo: echo Hello World', got '%s'", response)
	}
}

func TestMCP_ToolCallByName(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-mcp-tool-name", "Tenant MCP Tool Name",
		"owner-mcp-tool-name@test.test", "password")

	token := env.login(t, "owner-mcp-tool-name@test.test", "password")

	status := env.doJSON(t, fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-name/tools",
		bearer(token),
		map[string]interface{}{
			"name": "Echo Tool",
			"type": "echo",
		},
		nil,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("failed to create echo tool: status %d", status)
	}

	time.Sleep(50 * time.Millisecond)

	var out map[string]interface{}

	status = env.doJSON(t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-name/mcp/query",
		bearer(token),
		map[string]interface{}{
			"tool_call": map[string]interface{}{
				"tool_name": "Echo Tool",
				"params": map[string]interface{}{
					"message": "Hello Explicit MCP",
				},
			},
		},
		&out,
	)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	response, ok := out["response"].(string)
	if !ok {
		t.Fatalf("expected response string, got %T", out["response"])
	}

	if response != "Echo: Hello Explicit MCP" {
		t.Fatalf(
			"expected 'Echo: Hello Explicit MCP', got '%s'",
			response,
		)
	}

	toolsUsed, ok := out["tools_used"].([]interface{})
	if !ok {
		t.Fatalf("expected tools_used array, got %T", out["tools_used"])
	}

	if len(toolsUsed) != 1 {
		t.Fatalf("expected exactly one tool used, got %d", len(toolsUsed))
	}

	if toolsUsed[0] != "Echo Tool" {
		t.Fatalf("expected Echo Tool in tools_used, got %v", toolsUsed[0])
	}
}

func TestMCP_ToolCallByID(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-mcp-tool-id", "Tenant MCP Tool ID",
		"owner-mcp-tool-id@test.test", "password")

	token := env.login(t, "owner-mcp-tool-id@test.test", "password")

	status := env.doJSON(t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-id/tools",
		bearer(token),
		map[string]interface{}{
			"name": "Echo Tool",
			"type": "echo",
		},
		nil,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("failed to create echo tool: status %d", status)
	}

	time.Sleep(50 * time.Millisecond)

	// Ambil tool ID dari endpoint tools.
	var toolsOut map[string]interface{}

	status = env.doJSON(t,
		fiber.MethodGet,
		"/api/tenants/tenant-mcp-tool-id/tools",
		bearer(token),
		nil,
		&toolsOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 when listing tools, got %d", status)
	}

	tools, ok := toolsOut["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools array with at least one tool, got %v", toolsOut["tools"])
	}

	firstTool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first tool object, got %T", tools[0])
	}

	toolID, ok := firstTool["id"].(string)
	if !ok || toolID == "" {
		t.Fatalf("expected tool id, got %v", firstTool["id"])
	}

	var out map[string]interface{}

	status = env.doJSON(t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-id/mcp/query",
		bearer(token),
		map[string]interface{}{
			"tool_call": map[string]interface{}{
				"tool_name": toolID,
				"params": map[string]interface{}{
					"message": "Hello By Tool ID",
				},
			},
		},
		&out,
	)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	response, ok := out["response"].(string)
	if !ok {
		t.Fatalf("expected response string, got %T", out["response"])
	}

	if response != "Echo: Hello By Tool ID" {
		t.Fatalf(
			"expected 'Echo: Hello By Tool ID', got '%s'",
			response,
		)
	}
}

func TestMCP_ToolCallParamsOverrideRequestParams(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-mcp-tool-params", "Tenant MCP Tool Params",
		"owner-mcp-tool-params@test.test", "password")

	token := env.login(t, "owner-mcp-tool-params@test.test", "password")

	status := env.doJSON(t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-params/tools",
		bearer(token),
		map[string]interface{}{
			"name": "Echo Tool",
			"type": "echo",
		},
		nil,
	)
	if status != fiber.StatusCreated {
		t.Fatalf("failed to create echo tool: status %d", status)
	}

	time.Sleep(50 * time.Millisecond)

	var out map[string]interface{}

	status = env.doJSON(t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-params/mcp/query",
		bearer(token),
		map[string]interface{}{
			"params": map[string]interface{}{
				"message": "Request Params",
			},
			"tool_call": map[string]interface{}{
				"tool_name": "Echo Tool",
				"params": map[string]interface{}{
					"message": "ToolCall Params",
				},
			},
		},
		&out,
	)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	response, ok := out["response"].(string)
	if !ok {
		t.Fatalf("expected response string, got %T", out["response"])
	}

	if response != "Echo: ToolCall Params" {
		t.Fatalf(
			"expected ToolCall params to win, got '%s'",
			response,
		)
	}
}

func TestMCP_QueryRequiresQueryOrToolCall(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(
		t,
		"tenant-mcp-invalid-request",
		"Tenant MCP Invalid Request",
		"owner-mcp-invalid@test.test",
		"password",
	)

	token := env.login(t, "owner-mcp-invalid@test.test", "password")

	var out map[string]interface{}

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-invalid-request/mcp/query",
		bearer(token),
		map[string]interface{}{},
		&out,
	)

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", status, out)
	}
}

func TestMCP_ToolCallNotFound(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(
		t,
		"tenant-mcp-tool-not-found",
		"Tenant MCP Tool Not Found",
		"owner-mcp-not-found@test.test",
		"password",
	)

	token := env.login(t, "owner-mcp-not-found@test.test", "password")

	var out map[string]interface{}

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-tool-not-found/mcp/query",
		bearer(token),
		map[string]interface{}{
			"tool_call": map[string]interface{}{
				"tool_name": "Does Not Exist",
			},
		},
		&out,
	)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d: %v", status, out)
	}
}

func TestMCP_CaseInsensitiveToolCall(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(
		t,
		"tenant-mcp-case",
		"Tenant MCP Case",
		"owner-mcp-case@test.test",
		"password",
	)

	token := env.login(t, "owner-mcp-case@test.test", "password")

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-case/tools",
		bearer(token),
		map[string]interface{}{
			"name": "Echo Tool",
			"type": "echo",
		},
		nil,
	)

	if status != fiber.StatusCreated {
		t.Fatalf("failed to create echo tool: status %d", status)
	}

	time.Sleep(50 * time.Millisecond)

	var out map[string]interface{}

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-case/mcp/query",
		bearer(token),
		map[string]interface{}{
			"tool_call": map[string]interface{}{
				"tool_name": "eChO tOoL",
				"params": map[string]interface{}{
					"message": "Case Insensitive",
				},
			},
		},
		&out,
	)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, out)
	}

	response, ok := out["response"].(string)
	if !ok {
		t.Fatalf("expected response string, got %T", out["response"])
	}

	if response != "Echo: Case Insensitive" {
		t.Fatalf(
			"expected 'Echo: Case Insensitive', got '%s'",
			response,
		)
	}
}

func TestMCP_TenantToolIsolation(t *testing.T) {
	env := newTestEnv(t)

	// ------------------------------------------------------------
	// 1. Create two tenants + owners.
	// ------------------------------------------------------------

	env.bootstrapTenantOwner(
		t,
		"tenant-isolation-a",
		"Tenant Isolation A",
		"isolation-a@example.test",
		"Isolation-A-Password!",
	)

	env.bootstrapTenantOwner(
		t,
		"tenant-isolation-b",
		"Tenant Isolation B",
		"isolation-b@example.test",
		"Isolation-B-Password!",
	)

	// ------------------------------------------------------------
	// 2. Login Tenant A.
	// ------------------------------------------------------------

	var authA map[string]any

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/auth/login",
		nil,
		map[string]any{
			"email":    "isolation-a@example.test",
			"password": "Isolation-A-Password!",
		},
		&authA,
	)

	if status != fiber.StatusOK {
		t.Fatalf("Tenant A login failed: status=%d body=%v", status, authA)
	}

	tokenA, ok := authA["access_token"].(string)
	if !ok || tokenA == "" {
		t.Fatalf("Tenant A login did not return access_token: %v", authA)
	}

	// ------------------------------------------------------------
	// 3. Login Tenant B.
	// ------------------------------------------------------------

	var authB map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/auth/login",
		nil,
		map[string]any{
			"email":    "isolation-b@example.test",
			"password": "Isolation-B-Password!",
		},
		&authB,
	)

	if status != fiber.StatusOK {
		t.Fatalf("Tenant B login failed: status=%d body=%v", status, authB)
	}

	tokenB, ok := authB["access_token"].(string)
	if !ok || tokenB == "" {
		t.Fatalf("Tenant B login did not return access_token: %v", authB)
	}

	// ------------------------------------------------------------
	// 4. Create echo tool owned by Tenant A.
	// ------------------------------------------------------------

	var toolA map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-isolation-a/tools",
		map[string]string{
			"Authorization": "Bearer " + tokenA,
		},
		map[string]any{
			"name":        "echo",
			"description": "Tenant A echo tool",
			"type":        "echo",
			"config":      map[string]string{},
		},
		&toolA,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"creating Tenant A tool failed: status=%d body=%v",
			status,
			toolA,
		)
	}

	// ------------------------------------------------------------
	// 5. Create private tool owned by Tenant B.
	// ------------------------------------------------------------

	var toolB map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-isolation-b/tools",
		map[string]string{
			"Authorization": "Bearer " + tokenB,
		},
		map[string]any{
			"name":        "secret-b-tool",
			"description": "Tenant B private tool",
			"type":        "echo",
			"config":      map[string]string{},
		},
		&toolB,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"creating Tenant B tool failed: status=%d body=%v",
			status,
			toolB,
		)
	}

	toolObj, ok := toolB["tool"].(map[string]any)
	if !ok {
		t.Fatalf(
			"Tenant B tool response missing tool object: %v",
			toolB,
		)
	}

	toolBID, ok := toolObj["id"].(string)
	if !ok || toolBID == "" {
		t.Fatalf(
			"Tenant B tool did not return id: %v",
			toolB,
		)
	}

	// ------------------------------------------------------------
	// 6. POSITIVE CONTROL:
	//    Tenant A can execute its own tool.
	// ------------------------------------------------------------

	var ownA map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-isolation-a/mcp/query",
		map[string]string{
			"Authorization": "Bearer " + tokenA,
		},
		map[string]any{
			"tool_call": map[string]any{
				"tool_name": "echo",
				"params": map[string]any{
					"message": "tenant-a still valid",
				},
			},
		},
		&ownA,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"Tenant A should execute own tool: status=%d body=%v",
			status,
			ownA,
		)
	}

	if got, ok := ownA["response"].(string); !ok ||
		got != "Echo: tenant-a still valid" {
		t.Fatalf(
			"unexpected Tenant A response: %v",
			ownA,
		)
	}

	// ------------------------------------------------------------
	// 7. SECURITY:
	//    Tenant A cannot resolve Tenant B tool by NAME.
	// ------------------------------------------------------------

	var crossByName map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-isolation-a/mcp/query",
		map[string]string{
			"Authorization": "Bearer " + tokenA,
		},
		map[string]any{
			"tool_call": map[string]any{
				"tool_name": "secret-b-tool",
				"params": map[string]any{
					"message": "cross tenant attack by name",
				},
			},
		},
		&crossByName,
	)

	if status != fiber.StatusNotFound {
		t.Fatalf(
			"expected Tenant B tool to be invisible by name: status=%d body=%v",
			status,
			crossByName,
		)
	}

	if got, ok := crossByName["error"].(string); !ok ||
		got != "tool not found: secret-b-tool" {
		t.Fatalf(
			"unexpected cross-tenant name error: %v",
			crossByName,
		)
	}

	// ------------------------------------------------------------
	// 8. SECURITY:
	//    Tenant A cannot resolve Tenant B tool by ID.
	// ------------------------------------------------------------

	var crossByID map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-isolation-a/mcp/query",
		map[string]string{
			"Authorization": "Bearer " + tokenA,
		},
		map[string]any{
			"tool_call": map[string]any{
				"tool_name": toolBID,
				"params": map[string]any{
					"message": "cross tenant attack by ID",
				},
			},
		},
		&crossByID,
	)

	if status != fiber.StatusNotFound {
		t.Fatalf(
			"expected Tenant B tool to be invisible by ID: status=%d body=%v",
			status,
			crossByID,
		)
	}

	if got, ok := crossByID["error"].(string); !ok ||
		got != "tool not found: "+toolBID {
		t.Fatalf(
			"unexpected cross-tenant ID error: %v",
			crossByID,
		)
	}

	// ------------------------------------------------------------
	// 9. POSITIVE CONTROL:
	//    Tenant B can execute its own private tool.
	// ------------------------------------------------------------

	var ownB map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-isolation-b/mcp/query",
		map[string]string{
			"Authorization": "Bearer " + tokenB,
		},
		map[string]any{
			"tool_call": map[string]any{
				"tool_name": "secret-b-tool",
				"params": map[string]any{
					"message": "tenant-b private execution",
				},
			},
		},
		&ownB,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"Tenant B should execute own tool: status=%d body=%v",
			status,
			ownB,
		)
	}

	if got, ok := ownB["response"].(string); !ok ||
		got != "Echo: tenant-b private execution" {
		t.Fatalf(
			"unexpected Tenant B response: %v",
			ownB,
		)
	}
}

func TestMCP_ConcurrentExecution(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(
		t,
		"tenant-mcp-concurrent",
		"Tenant MCP Concurrent",
		"mcp-concurrent-owner@example.test",
		"correct-horse-battery-staple",
	)

	token := env.login(
		t,
		"mcp-concurrent-owner@example.test",
		"correct-horse-battery-staple",
	)

	// Register one echo tool used by all concurrent requests.
	var toolOut map[string]any

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/tenant-mcp-concurrent/tools",
		map[string]string{
			"Authorization": "Bearer " + token,
		},
		map[string]any{
			"name":        "concurrent-echo",
			"description": "Concurrent MCP echo test",
			"type":        "echo",
			"config":      map[string]string{},
		},
		&toolOut,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"expected tool creation 201, got %d: %v",
			status,
			toolOut,
		)
	}

	const workers = 20

	type result struct {
		index    int
		status   int
		response map[string]any
	}

	results := make(chan result, workers)

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		i := i

		go func() {
			defer wg.Done()

			var out map[string]any

			status := env.doJSON(
				t,
				fiber.MethodPost,
				"/api/tenants/tenant-mcp-concurrent/mcp/query",
				map[string]string{
					"Authorization": "Bearer " + token,
				},
				map[string]any{
					"tool_call": map[string]any{
						"tool_name": "concurrent-echo",
						"params": map[string]any{
							"message": fmt.Sprintf(
								"concurrent-message-%02d",
								i,
							),
						},
					},
				},
				&out,
			)

			results <- result{
				index:    i,
				status:   status,
				response: out,
			}
		}()
	}

	wg.Wait()
	close(results)

	for r := range results {
		if r.status != fiber.StatusOK {
			t.Errorf(
				"request %d: expected 200, got %d: %v",
				r.index,
				r.status,
				r.response,
			)
			continue
		}

		expected := fmt.Sprintf(
			"Echo: concurrent-message-%02d",
			r.index,
		)

		actual, ok := r.response["response"].(string)
		if !ok {
			t.Errorf(
				"request %d: response field missing or not string: %v",
				r.index,
				r.response,
			)
			continue
		}

		if actual != expected {
			t.Errorf(
				"request %d: response mismatch: expected %q, got %q",
				r.index,
				expected,
				actual,
			)
		}

		toolsUsed, ok := r.response["tools_used"].([]any)
		if !ok {
			t.Errorf(
				"request %d: tools_used missing or wrong type: %v",
				r.index,
				r.response,
			)
			continue
		}

		if len(toolsUsed) != 1 || toolsUsed[0] != "concurrent-echo" {
			t.Errorf(
				"request %d: unexpected tools_used: %v",
				r.index,
				toolsUsed,
			)
		}
	}
}

func TestMCP_CrossTenantDenied(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-mcp-a", "Tenant A",
		"owner-a@test.test", "password-a")
	env.bootstrapTenantOwner(t, "tenant-mcp-b", "Tenant B",
		"owner-b@test.test", "password-b")

	tokenA := env.login(t, "owner-a@test.test", "password-a")

	// Tenant A tries to query MCP in Tenant B
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-mcp-b/mcp/query",
		bearer(tokenA),
		map[string]interface{}{
			"query": "test",
		},
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}
