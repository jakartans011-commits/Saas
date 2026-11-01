package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"trailseal/agent"
)

// ============================================================
// MCP PROTOCOL CONTRACT TESTS
//
// Test ini sengaja berada di package mcp agar dapat menguji
// resolver dan execution boundary secara langsung.
//
// Production behavior TIDAK diubah.
// ============================================================

// ------------------------------------------------------------
// HELPERS
// ------------------------------------------------------------

func testTool(
	id string,
	name string,
	toolType agent.ToolType,
) *agent.Tool {
	return &agent.Tool{
		ID:   id,
		Name: name,
		Type: toolType,
	}
}

// ============================================================
// PROTOCOL TESTS
// ============================================================

func TestMCPRequest_JSONDecodesToolCall(t *testing.T) {
	body := []byte(`{
		"tool_call": {
			"tool_name": "echo",
			"params": {
				"message": "hello"
			}
		}
	}`)

	var req MCPRequest

	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf(
			"failed to decode MCPRequest: %v",
			err,
		)
	}

	if req.ToolCall == nil {
		t.Fatal("expected ToolCall to be decoded")
	}

	if req.ToolCall.ToolName != "echo" {
		t.Fatalf(
			"expected tool_name echo, got %q",
			req.ToolCall.ToolName,
		)
	}

	if got := req.ToolCall.Params["message"]; got != "hello" {
		t.Fatalf(
			"expected message hello, got %#v",
			got,
		)
	}

	if err := req.Validate(); err != nil {
		t.Fatalf(
			"decoded MCPRequest should validate: %v",
			err,
		)
	}
}

func TestMCPRequest_ToolCallDoesNotRequireQuery(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "echo",
			Params: map[string]interface{}{
				"message": "hello",
			},
		},
	}

	if req.Query != "" {
		t.Fatal("expected query to be empty")
	}

	if err := req.Validate(); err != nil {
		t.Fatalf(
			"ToolCall should be valid without query: %v",
			err,
		)
	}
}
func TestMCPToolCall_RejectsOversizedToolName(t *testing.T) {
	toolName := strings.Repeat(
		"x",
		MCPMaxToolNameBytes+1,
	)

	call := &MCPToolCall{
		ToolName: toolName,
	}

	err := call.Validate()

	if err == nil {
		t.Fatal("expected oversized tool name to be rejected")
	}

	if !IsMCPErrorCode(
		err,
		MCPErrorInvalidRequest,
	) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPToolCall_RejectsOversizedArguments(t *testing.T) {
	call := &MCPToolCall{
		ToolName: "echo",
		Params: map[string]interface{}{
			"message": strings.Repeat(
				"x",
				MCPMaxArgumentStringBytes+1,
			),
		},
	}

	err := call.Validate()

	if err == nil {
		t.Fatal("expected oversized argument to be rejected")
	}

	if !IsMCPErrorCode(
		err,
		MCPErrorInvalidRequest,
	) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}
}
func TestMCPResolveExplicitTool_ByName(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"tool-health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
	}

	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "health-check",
		},
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("resolveTool() returned nil tool")
	}

	if got.Name != "health-check" {
		t.Fatalf(
			"expected health-check, got %q",
			got.Name,
		)
	}
}

func TestMCPToolCall_Validate(t *testing.T) {
	tests := []struct {
		name    string
		call    *MCPToolCall
		wantErr string
	}{
		{
			name: "valid tool call",
			call: &MCPToolCall{
				ToolName: "echo",
				Params: map[string]interface{}{
					"message": "hello",
				},
			},
		},
		{
			name:    "nil tool call",
			call:    nil,
			wantErr: "tool_call is required",
		},
		{
			name: "missing tool name",
			call: &MCPToolCall{
				Params: map[string]interface{}{
					"message": "hello",
				},
			},
			wantErr: "tool_name is required",
		},
		{
			name: "blank tool name",
			call: &MCPToolCall{
				ToolName: "   ",
			},
			wantErr: "tool_name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf(
						"expected no error, got %v",
						err,
					)
				}

				return
			}

			if err == nil {
				t.Fatalf(
					"expected error %q, got nil",
					tt.wantErr,
				)
			}

			if err.Error() != tt.wantErr {
				t.Fatalf(
					"expected error %q, got %q",
					tt.wantErr,
					err.Error(),
				)
			}
		})
	}
}

func TestMCPResolveExplicitTool_ByID(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"tool-health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
	}

	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "tool-health-1",
		},
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("resolveTool() returned nil tool")
	}

	if got.ID != "tool-health-1" {
		t.Fatalf(
			"expected tool-health-1, got %q",
			got.ID,
		)
	}
}

func TestMCPRequest_Validate_ExplicitToolCall(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "echo",
			Params: map[string]interface{}{
				"message": "hello",
			},
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf(
			"expected valid MCPToolCall request, got %v",
			err,
		)
	}
}

func TestMCPRequest_Validate_ToolCallOverridesQuery(t *testing.T) {
	req := &MCPRequest{
		Query: "health status",
		ToolCall: &MCPToolCall{
			ToolName: "echo",
			Params: map[string]interface{}{
				"message": "explicit tool call",
			},
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf(
			"expected valid request, got %v",
			err,
		)
	}
}

func TestMCPResolveExplicitTool_CaseInsensitive(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"tool-health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
	}

	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "HEALTH-CHECK",
		},
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("resolveTool() returned nil tool")
	}

	if got.Name != "health-check" {
		t.Fatalf(
			"expected health-check, got %q",
			got.Name,
		)
	}
}

func TestMCPResolveExplicitTool_NotFound(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"tool-health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
	}

	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "secret-tool",
		},
	}

	_, err := resolveTool(req, tools)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(
		strings.ToLower(err.Error()),
		"tool not found",
	) {
		t.Fatalf(
			"expected tool not found error, got: %v",
			err,
		)
	}
}

// ============================================================
// TENANT SECURITY BOUNDARY
// ============================================================
//
// Resolver hanya boleh melihat tools yang diberikan caller.
//
// Test ini membuktikan bahwa tool di luar supplied tenant list
// tidak mungkin ditemukan.
//
// Tidak ada global lookup.
// ============================================================

func TestMCPResolver_TenantScopedToolListOnly(t *testing.T) {
	tenantATools := []*agent.Tool{
		testTool(
			"tenant-a-health",
			"health-check-a",
			agent.ToolTypeHealthCheck,
		),
	}

	// Tool milik tenant B sengaja TIDAK diberikan ke resolver.
	tenantBTool := testTool(
		"tenant-b-secret",
		"secret-tool",
		agent.ToolTypeEcho,
	)

	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: tenantBTool.Name,
		},
	}

	got, err := resolveTool(req, tenantATools)

	if err == nil {
		t.Fatal(
			"expected tenant boundary to reject tool outside supplied list",
		)
	}

	if got != nil {
		t.Fatalf(
			"expected nil tool, got %+v",
			got,
		)
	}
}

// ============================================================
// QUERY RESOLUTION
// ============================================================

func TestMCPResolveQuery_HealthAlias(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
	}

	req := &MCPRequest{
		Query: "please check system health",
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("expected health tool")
	}

	if got.Type != agent.ToolTypeHealthCheck {
		t.Fatalf(
			"expected health-check type, got %v",
			got.Type,
		)
	}
}

func TestMCPResolveQuery_MetadataAlias(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"metadata-1",
			"metadata",
			agent.ToolTypeMetadataLookup,
		),
	}

	req := &MCPRequest{
		Query: "show metadata",
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("expected metadata tool")
	}

	if got.Type != agent.ToolTypeMetadataLookup {
		t.Fatalf(
			"expected metadata type, got %v",
			got.Type,
		)
	}
}

func TestMCPResolveQuery_EchoAlias(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"echo-1",
			"echo",
			agent.ToolTypeEcho,
		),
	}

	req := &MCPRequest{
		Query: "echo hello",
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("expected echo tool")
	}

	if got.Type != agent.ToolTypeEcho {
		t.Fatalf(
			"expected echo type, got %v",
			got.Type,
		)
	}
}

// ============================================================
// REQUEST VALIDATION
// ============================================================

func TestMCPResolve_NilRequest(t *testing.T) {
	_, err := resolveTool(nil, nil)

	if err == nil {
		t.Fatal("expected error for nil request")
	}

	if err.Error() != "request is required" {
		t.Fatalf(
			"expected request is required, got %q",
			err.Error(),
		)
	}
}

func TestMCPResolve_EmptyQuery(t *testing.T) {
	req := &MCPRequest{}

	_, err := resolveTool(req, nil)

	if err == nil {
		t.Fatal("expected error for empty query")
	}

	if err.Error() != "query is required" {
		t.Fatalf(
			"expected query is required, got %q",
			err.Error(),
		)
	}
}

func TestMCPResolve_EmptyToolName(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{},
	}

	_, err := resolveTool(req, nil)

	if err == nil {
		t.Fatal("expected error for empty tool name")
	}

	if err.Error() != "tool_name is required" {
		t.Fatalf(
			"expected tool_name is required, got %q",
			err.Error(),
		)
	}
}

// ============================================================
// TOOL CALL PRIORITY
// ============================================================
//
// Explicit ToolCall harus memiliki prioritas dibanding Query.
// ============================================================

func TestMCPResolve_ToolCallOverridesQuery(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
		testTool(
			"echo-1",
			"echo",
			agent.ToolTypeEcho,
		),
	}

	req := &MCPRequest{
		Query: "health status",
		ToolCall: &MCPToolCall{
			ToolName: "echo",
		},
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("expected tool")
	}

	if got.Type != agent.ToolTypeEcho {
		t.Fatalf(
			"expected explicit ToolCall to win, got %v",
			got.Type,
		)
	}
}

// ============================================================
// PARAMETER BEHAVIOR
// ============================================================

func TestMCPExecutor_QueryParameterBecomesMessage(t *testing.T) {
	tool := testTool(
		"echo-1",
		"echo",
		agent.ToolTypeEcho,
	)

	params := map[string]interface{}{
		"message": "hello from query",
	}

	result, err := executeTool(
		tool,
		params,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	got := result.(string)

	if got != "Echo: hello from query" {
		t.Fatalf(
			"unexpected echo result: %q",
			got,
		)
	}
}

func TestMCPExecutor_ToolCallParams(t *testing.T) {
	tool := testTool(
		"echo-1",
		"echo",
		agent.ToolTypeEcho,
	)

	params := map[string]interface{}{
		"message": "explicit params",
	}

	result, err := executeTool(
		tool,
		params,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	got := result.(string)

	if got != "Echo: explicit params" {
		t.Fatalf(
			"unexpected result: %q",
			got,
		)
	}
}

func TestMCPExecutor_ContextDoesNotChangeExecutionIdentity(t *testing.T) {
	tool := testTool(
		"health-1",
		"health-check",
		agent.ToolTypeHealthCheck,
	)

	execCtx := MCPExecutionContext{
		TenantID: "authoritative-tenant",
		UserID:   "authoritative-user",
		Context: map[string]string{
			"tenant_id": "attacker-tenant",
			"user_id":   "attacker-user",
		},
	}

	result, err := executeTool(
		tool,
		nil,
		execCtx,
	)

	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected health result")
	}

	if execCtx.TenantID != "authoritative-tenant" {
		t.Fatalf(
			"execution tenant identity was modified: %q",
			execCtx.TenantID,
		)
	}

	if execCtx.UserID != "authoritative-user" {
		t.Fatalf(
			"execution user identity was modified: %q",
			execCtx.UserID,
		)
	}
}

// ============================================================
// CONTEXT CLONING
// ============================================================

func TestMCPCloneContext_Isolated(t *testing.T) {
	input := map[string]string{
		"request_id": "abc",
		"source":     "mcp",
	}

	output := cloneContext(input)

	if output == nil {
		t.Fatal("expected cloned context")
	}

	output["request_id"] = "modified"

	if input["request_id"] != "abc" {
		t.Fatal(
			"cloneContext() did not isolate original map",
		)
	}

	if output["source"] != "mcp" {
		t.Fatal(
			"cloneContext() lost existing value",
		)
	}
}

func TestMCPCloneContext_Nil(t *testing.T) {
	output := cloneContext(nil)

	if output != nil {
		t.Fatalf(
			"expected nil cloned context, got %#v",
			output,
		)
	}
}

// ============================================================
// EXECUTION SECURITY / ALLOWLIST
// ============================================================

func TestMCPExecutor_NilTool(t *testing.T) {
	_, err := executeTool(
		nil,
		nil,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err == nil {
		t.Fatal("expected error for nil tool")
	}

	if err.Error() != "tool is required" {
		t.Fatalf(
			"expected tool is required, got %q",
			err.Error(),
		)
	}
}

func TestMCPExecutor_UnsupportedToolType(t *testing.T) {
	tool := &agent.Tool{
		ID:   "unsupported-1",
		Name: "unsupported",
		Type: agent.ToolType("unsupported"),
	}

	_, err := executeTool(
		tool,
		nil,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err == nil {
		t.Fatal("expected unsupported tool type error")
	}

	if !strings.Contains(
		strings.ToLower(err.Error()),
		"unsupported tool type",
	) {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}
}

// ============================================================
// ECHO VALIDATION
// ============================================================

func TestMCPExecutor_EchoRequiresMessage(t *testing.T) {
	tool := testTool(
		"echo-1",
		"echo",
		agent.ToolTypeEcho,
	)

	_, err := executeTool(
		tool,
		nil,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err == nil {
		t.Fatal("expected echo validation error")
	}

	if err.Error() != "echo message is required" {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}
}

func TestMCPExecutor_EchoSupportsText(t *testing.T) {
	tool := testTool(
		"echo-1",
		"echo",
		agent.ToolTypeEcho,
	)

	result, err := executeTool(
		tool,
		map[string]interface{}{
			"text": "hello",
		},
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	if result != "Echo: hello" {
		t.Fatalf(
			"unexpected result: %v",
			result,
		)
	}
}

func TestMCPExecutor_EchoSupportsQuery(t *testing.T) {
	tool := testTool(
		"echo-1",
		"echo",
		agent.ToolTypeEcho,
	)

	result, err := executeTool(
		tool,
		map[string]interface{}{
			"query": "hello query",
		},
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	if result != "Echo: hello query" {
		t.Fatalf(
			"unexpected result: %v",
			result,
		)
	}
}

// ============================================================
// METADATA EXECUTION
// ============================================================

func TestMCPExecutor_MetadataLookup(t *testing.T) {
	tool := testTool(
		"metadata-1",
		"metadata",
		agent.ToolTypeMetadataLookup,
	)

	tool.Config = map[string]string{
		"version": "v1",
		"owner":   "platform",
	}

	result, err := executeTool(
		tool,
		nil,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)
	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	metadata, ok := result.(map[string]string)
	if !ok {
		t.Fatalf(
			"expected map[string]string, got %T",
			result,
		)
	}

	if metadata["version"] != "v1" {
		t.Fatalf(
			"expected version v1, got %q",
			metadata["version"],
		)
	}

	if metadata["owner"] != "platform" {
		t.Fatalf(
			"expected owner platform, got %q",
			metadata["owner"],
		)
	}

	if metadata["lookup_time"] == "" {
		t.Fatal("expected lookup_time")
	}
}

// ============================================================
// HEALTH CHECK
// ============================================================

func TestMCPExecutor_HealthCheck(t *testing.T) {
	tool := testTool(
		"health-1",
		"health-check",
		agent.ToolTypeHealthCheck,
	)

	result, err := executeTool(
		tool,
		nil,
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err != nil {
		t.Fatalf("executeTool() unexpected error: %v", err)
	}

	health, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf(
			"expected map[string]interface{}, got %T",
			result,
		)
	}

	if health["status"] != "healthy" {
		t.Fatalf(
			"expected healthy status, got %v",
			health["status"],
		)
	}

	if health["message"] != "All systems operational" {
		t.Fatalf(
			"unexpected health message: %v",
			health["message"],
		)
	}

	if health["timestamp"] == "" {
		t.Fatal("expected timestamp")
	}
}

// ============================================================
// RESULT FORMATTING
// ============================================================

func TestMCPFormatExecutionResult(t *testing.T) {
	result := map[string]string{
		"status": "healthy",
	}

	formatted := formatExecutionResult(result)

	if formatted == "" {
		t.Fatal("expected formatted execution result")
	}

	if !strings.Contains(formatted, "healthy") {
		t.Fatalf(
			"expected formatted result to contain healthy, got %q",
			formatted,
		)
	}
}

// ============================================================
// QUERY TOOL MATCHING PRIORITY
// ============================================================

func TestMCPResolveQuery_ExactNameBeforeAlias(t *testing.T) {
	tools := []*agent.Tool{
		testTool(
			"echo-1",
			"health",
			agent.ToolTypeEcho,
		),
		testTool(
			"health-1",
			"health-check",
			agent.ToolTypeHealthCheck,
		),
	}

	req := &MCPRequest{
		Query: "health",
	}

	got, err := resolveTool(req, tools)
	if err != nil {
		t.Fatalf("resolveTool() unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("expected tool")
	}

	if got.Name != "health" {
		t.Fatalf(
			"expected exact name match to win, got %q",
			got.Name,
		)
	}
}
