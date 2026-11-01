package mcp

import (
	"errors"
	"testing"

	"trailseal/agent"
)

func TestMCPRequestValidate_QueryMode(t *testing.T) {
	req := &MCPRequest{
		Query: "health check",
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestMCPRequestValidate_ToolCallMode(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "health-check",
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestMCPRequestValidate_NilRequest(t *testing.T) {
	var req *MCPRequest

	err := req.Validate()

	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsMCPErrorCode(err, MCPErrorInvalidRequest) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPRequestValidate_QueryAndToolCallOptionalCombination(t *testing.T) {
	req := &MCPRequest{
		Query: "this query should not be required",
		ToolCall: &MCPToolCall{
			ToolName: "health-check",
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestMCPRequestValidate_NoQueryAndNoToolCall(t *testing.T) {
	req := &MCPRequest{}

	err := req.Validate()

	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsMCPErrorCode(err, MCPErrorInvalidRequest) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}

	if err.Error() != "query is required" {
		t.Fatalf(
			"unexpected error message: %v",
			err,
		)
	}
}

func TestMCPRequestValidate_ToolCallWithoutName(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{},
	}

	err := req.Validate()

	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsMCPErrorCode(err, MCPErrorInvalidRequest) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}

	if err.Error() != "tool_name is required" {
		t.Fatalf(
			"unexpected error message: %v",
			err,
		)
	}
}

func TestMCPToolCallValidate_WhitespaceName(t *testing.T) {
	call := &MCPToolCall{
		ToolName: "   ",
	}

	err := call.Validate()

	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsMCPErrorCode(err, MCPErrorInvalidRequest) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPErrorTaxonomy_ToolNotFound(t *testing.T) {
	tools := []*agent.Tool{
		{
			ID:   "tool-1",
			Name: "health-check",
			Type: agent.ToolTypeHealthCheck,
		},
	}

	_, err := resolveExplicitTool(
		"does-not-exist",
		tools,
	)

	if err == nil {
		t.Fatal("expected tool-not-found error")
	}

	if !IsMCPErrorCode(err, MCPErrorToolNotFound) {
		t.Fatalf(
			"expected tool_not_found, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPErrorTaxonomy_QueryToolNotFound(t *testing.T) {
	tools := []*agent.Tool{
		{
			ID:   "tool-1",
			Name: "health-check",
			Type: agent.ToolTypeHealthCheck,
		},
	}

	_, err := resolveQueryTool(
		"something completely unrelated",
		tools,
	)

	if err == nil {
		t.Fatal("expected tool-not-found error")
	}

	if !IsMCPErrorCode(err, MCPErrorToolNotFound) {
		t.Fatalf(
			"expected tool_not_found, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPErrorTaxonomy_UnsupportedTool(t *testing.T) {
	tool := &agent.Tool{
		ID:   "unsupported",
		Name: "unsupported",
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
		t.Fatal("expected unsupported-tool error")
	}

	if !IsMCPErrorCode(err, MCPErrorUnsupportedTool) {
		t.Fatalf(
			"expected unsupported_tool_type, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPErrorTaxonomy_EchoValidation(t *testing.T) {
	tool := &agent.Tool{
		ID:   "echo-1",
		Name: "echo",
		Type: agent.ToolTypeEcho,
	}

	_, err := executeTool(
		tool,
		map[string]interface{}{},
		MCPExecutionContext{
			TenantID: "tenant-a",
			UserID:   "user-a",
		},
	)

	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsMCPErrorCode(err, MCPErrorInvalidRequest) {
		t.Fatalf(
			"expected invalid_request, got %v",
			MCPErrorCodeOf(err),
		)
	}
}

func TestMCPErrorTaxonomy_WrappedError(t *testing.T) {
	base := errors.New("storage failure")

	err := InternalMCPError(base)

	if !IsMCPErrorCode(err, MCPErrorInternal) {
		t.Fatalf(
			"expected internal_error, got %v",
			MCPErrorCodeOf(err),
		)
	}

	if !errors.Is(err, base) {
		t.Fatal("expected wrapped error to remain discoverable")
	}
}

func TestMCPErrorCodeOf_NonMCPErrorIsInternal(t *testing.T) {
	err := errors.New("unexpected dependency error")

	if got := MCPErrorCodeOf(err); got != MCPErrorInternal {
		t.Fatalf(
			"expected internal_error, got %v",
			got,
		)
	}
}
