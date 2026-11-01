package saasagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================
// Client.MCPToolCall
// ============================================================

// TestClient_MCPToolCall_SendsToolCallBody verifies that MCPToolCall
// builds a request whose JSON body carries the tool_call field with
// the given tool name and params — and does NOT set the top-level
// Query field — mirroring server-side mcp.MCPToolCall priority over
// Query.
func TestClient_MCPToolCall_SendsToolCallBody(t *testing.T) {
	var captured map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if r.URL.Path != "/api/tenants/tenant-1/mcp/query" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{
			Response:        "ok",
			ToolsUsed:       []string{"health-check"},
			ExecutionTimeMs: 1,
			Metadata:        map[string]string{"request_id": "abc-123"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	resp, err := client.MCPToolCall("tenant-1", "health-check", map[string]interface{}{
		"foo": "bar",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// --- request shape assertions ---

	if _, present := captured["query"]; present {
		t.Errorf("expected no top-level query field in tool-call mode, got %v", captured["query"])
	}

	toolCallRaw, ok := captured["tool_call"]
	if !ok {
		t.Fatalf("expected tool_call field in request body, got %v", captured)
	}

	toolCall, ok := toolCallRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_call to be an object, got %T", toolCallRaw)
	}

	if toolCall["tool_name"] != "health-check" {
		t.Errorf("expected tool_name %q, got %v", "health-check", toolCall["tool_name"])
	}

	params, ok := toolCall["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected params to be an object, got %T", toolCall["params"])
	}

	if params["foo"] != "bar" {
		t.Errorf("expected params.foo %q, got %v", "bar", params["foo"])
	}

	// --- response decoding assertions ---

	if resp.Response != "ok" {
		t.Errorf("expected response %q, got %q", "ok", resp.Response)
	}

	if len(resp.ToolsUsed) != 1 || resp.ToolsUsed[0] != "health-check" {
		t.Errorf("expected tools_used [health-check], got %v", resp.ToolsUsed)
	}

	if resp.Metadata["request_id"] != "abc-123" {
		t.Errorf("expected metadata.request_id %q, got %q", "abc-123", resp.Metadata["request_id"])
	}
}

// TestClient_MCPToolCall_OmitsParamsWhenNil verifies params is
// omitted (not sent as null/empty object) when nil is passed,
// matching the omitempty tag on MCPToolCall.Params.
func TestClient_MCPToolCall_OmitsParamsWhenNil(t *testing.T) {
	var captured map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	if _, err := client.MCPToolCall("tenant-1", "health-check", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolCall, ok := captured["tool_call"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_call to be an object, got %v", captured["tool_call"])
	}

	if _, present := toolCall["params"]; present {
		t.Errorf("expected params to be omitted when nil, got %v", toolCall["params"])
	}
}

// TestClient_MCPToolCall_PropagatesServerError verifies that a
// non-200 response from the tool-call endpoint surfaces as an
// *APIError through the normal parseError path.
func TestClient_MCPToolCall_PropagatesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "tool not found"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	_, err := client.MCPToolCall("tenant-1", "missing-tool", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}

	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", apiErr.StatusCode)
	}

	if apiErr.Code != "tool_not_found" {
		t.Errorf("expected code %q, got %q", "tool_not_found", apiErr.Code)
	}

	if apiErr.Message != "tool not found" {
		t.Errorf("expected message %q, got %q", "tool not found", apiErr.Message)
	}
}

// ============================================================
// parseError / APIError
// ============================================================

func TestClient_ParseError_MapsStatusToCode(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		wantCode   string
	}{
		{"bad request", http.StatusBadRequest, "invalid_request"},
		{"unauthorized", http.StatusUnauthorized, "unauthenticated"},
		{"forbidden", http.StatusForbidden, "forbidden"},
		{"not found", http.StatusNotFound, "tool_not_found"},
		{"internal error", http.StatusInternalServerError, "internal_error"},
		{"unmapped status", http.StatusTeapot, "unknown_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
			}))
			defer server.Close()

			client := NewClient(server.URL, "")

			_, err := client.GetUsage("tenant-1", 0)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T", err)
			}

			if apiErr.Code != tc.wantCode {
				t.Errorf("status %d: expected code %q, got %q", tc.statusCode, tc.wantCode, apiErr.Code)
			}

			if apiErr.StatusCode != tc.statusCode {
				t.Errorf("expected StatusCode %d, got %d", tc.statusCode, apiErr.StatusCode)
			}

			if apiErr.Message != "boom" {
				t.Errorf("expected message %q, got %q", "boom", apiErr.Message)
			}
		})
	}
}

// TestClient_ParseError_FallsBackToRawBody verifies that when the
// error body is not the expected {"error": "..."} shape, the raw
// body is used as the message instead of being dropped.
func TestClient_ParseError_FallsBackToRawBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("plain text failure, not json"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	_, err := client.GetUsage("tenant-1", 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}

	if apiErr.Message != "plain text failure, not json" {
		t.Errorf("expected raw body as message, got %q", apiErr.Message)
	}

	if apiErr.Code != "internal_error" {
		t.Errorf("expected code %q, got %q", "internal_error", apiErr.Code)
	}
}

func TestAPIError_Error_FormatsAllFields(t *testing.T) {
	err := &APIError{
		Code:       "tool_not_found",
		Message:    "no such tool",
		StatusCode: 404,
	}

	got := err.Error()
	want := "API error [tool_not_found]: no such tool (status 404)"

	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestAPIError_Error_NilReceiverIsSafe(t *testing.T) {
	var err *APIError

	if got := err.Error(); got != "" {
		t.Errorf("expected empty string for nil *APIError, got %q", got)
	}
}
