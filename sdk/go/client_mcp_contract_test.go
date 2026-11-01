package saasagent

import (
	"encoding/json"
	"testing"
)

// ============================================================
// SDK <-> server MCP contract
// ============================================================
//
// The SDK's MCPRequest/MCPToolCall/MCPResponse are hand-mirrored
// copies of mcp.MCPRequest/mcp.MCPToolCall/mcp.MCPResponse on the
// server (sdk/go has no dependency on the server module, by design).
// Nothing enforces that the two stay in sync — a tag typo or a
// renamed field on either side would silently break the wire
// protocol without failing to compile on either side.
//
// These tests don't reach across modules; instead they pin down the
// exact JSON shape the SDK currently produces/consumes. If someone
// changes a json tag here, the test fails immediately instead of
// failing silently against a live server. When the server's
// protocol.go changes, these expected strings are what to diff
// against by hand.
//
// Current server-side reference (mcp/protocol.go):
//   MCPRequest{ Query, Context, Params, ToolCall }
//     json: query,omitempty / context,omitempty / params,omitempty / tool_call,omitempty
//   MCPToolCall{ ToolName, Params }
//     json: tool_name / params,omitempty
//   MCPResponse{ Response, ToolsUsed, ExecutionTimeMs, Metadata, Error }
//     json: response / tools_used,omitempty / execution_time_ms / metadata,omitempty / error,omitempty

func TestMCPRequest_JSON_QueryMode_WireShape(t *testing.T) {
	req := &MCPRequest{Query: "check health"}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	want := `{"query":"check health"}`
	if string(data) != want {
		t.Errorf("expected %s, got %s", want, string(data))
	}
}

func TestMCPRequest_JSON_ToolCallMode_WireShape(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{ToolName: "health-check"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	want := `{"tool_call":{"tool_name":"health-check"}}`
	if string(data) != want {
		t.Errorf("expected %s, got %s", want, string(data))
	}
}

func TestMCPRequest_JSON_ToolCallWithParams_WireShape(t *testing.T) {
	req := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: "echo",
			Params:   map[string]interface{}{"text": "hi"},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	want := `{"tool_call":{"tool_name":"echo","params":{"text":"hi"}}}`
	if string(data) != want {
		t.Errorf("expected %s, got %s", want, string(data))
	}
}

func TestMCPRequest_JSON_WithContext_WireShape(t *testing.T) {
	req := &MCPRequest{
		Query:   "check health",
		Context: map[string]string{"session_id": "abc"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if decoded["query"] != "check health" {
		t.Errorf("expected query %q, got %v", "check health", decoded["query"])
	}

	ctx, ok := decoded["context"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected context object, got %v", decoded["context"])
	}

	if ctx["session_id"] != "abc" {
		t.Errorf("expected context.session_id %q, got %v", "abc", ctx["session_id"])
	}

	if _, present := decoded["tool_call"]; present {
		t.Errorf("expected tool_call to be omitted, got %v", decoded["tool_call"])
	}

	if _, present := decoded["params"]; present {
		t.Errorf("expected params to be omitted, got %v", decoded["params"])
	}
}

func TestMCPRequest_JSON_EmptyRequest_OmitsAllFields(t *testing.T) {
	req := &MCPRequest{}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	want := `{}`
	if string(data) != want {
		t.Errorf("expected %s, got %s", want, string(data))
	}
}

// TestMCPResponse_DecodesServerShapedPayload feeds the SDK's
// MCPResponse a payload shaped exactly like what mcp.MCPResponse
// produces on a successful call (including the request_id
// correlation metadata added by the gateway), and checks every field
// round-trips correctly.
func TestMCPResponse_DecodesServerShapedPayload(t *testing.T) {
	payload := `{
		"response": "healthy",
		"tools_used": ["health-check"],
		"execution_time_ms": 12,
		"metadata": {
			"tenant_id": "acme-corp",
			"user_id": "user-1",
			"tool_id": "tool-1",
			"tool_name": "health-check",
			"type": "health_check",
			"request_id": "9f4d9b0a-1c2e-4a6f-8b1a-000000000000"
		}
	}`

	var resp MCPResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if resp.Response != "healthy" {
		t.Errorf("expected response %q, got %q", "healthy", resp.Response)
	}

	if len(resp.ToolsUsed) != 1 || resp.ToolsUsed[0] != "health-check" {
		t.Errorf("expected tools_used [health-check], got %v", resp.ToolsUsed)
	}

	if resp.ExecutionTimeMs != 12 {
		t.Errorf("expected execution_time_ms 12, got %d", resp.ExecutionTimeMs)
	}

	if resp.Metadata["request_id"] != "9f4d9b0a-1c2e-4a6f-8b1a-000000000000" {
		t.Errorf("expected metadata.request_id to round-trip, got %q", resp.Metadata["request_id"])
	}

	if resp.Metadata["tenant_id"] != "acme-corp" {
		t.Errorf("expected metadata.tenant_id to round-trip, got %q", resp.Metadata["tenant_id"])
	}
}

// TestMCPResponse_DecodesMinimalPayload guards against a decode
// panic/error when the server omits optional fields (tools_used,
// metadata) — both are omitempty server-side, so a minimal success
// response is a real shape the SDK must tolerate.
func TestMCPResponse_DecodesMinimalPayload(t *testing.T) {
	payload := `{"response": "ok", "execution_time_ms": 0}`

	var resp MCPResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if resp.Response != "ok" {
		t.Errorf("expected response %q, got %q", "ok", resp.Response)
	}

	if resp.ToolsUsed != nil {
		t.Errorf("expected nil ToolsUsed, got %v", resp.ToolsUsed)
	}

	if resp.Metadata != nil {
		t.Errorf("expected nil Metadata, got %v", resp.Metadata)
	}
}

func TestMCPToolCall_JSON_WireShape(t *testing.T) {
	tc := &MCPToolCall{
		ToolName: "metadata-lookup",
		Params:   map[string]interface{}{"key": "region"},
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	want := `{"tool_name":"metadata-lookup","params":{"key":"region"}}`
	if string(data) != want {
		t.Errorf("expected %s, got %s", want, string(data))
	}
}

func TestMCPToolCall_JSON_OmitsParamsWhenNil(t *testing.T) {
	tc := &MCPToolCall{ToolName: "health-check"}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	want := `{"tool_name":"health-check"}`
	if string(data) != want {
		t.Errorf("expected %s, got %s", want, string(data))
	}
}