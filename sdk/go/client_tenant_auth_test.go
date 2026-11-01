package saasagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================
// Tenant propagation
// ============================================================
//
// TenantID is not part of the MCPRequest/MCPToolCall JSON body — it
// is authoritative routing information carried in the URL path, the
// same way the server's Gateway.Query treats tenantID as coming from
// "route + JWT validation", never from request body/context. These
// tests lock in that the SDK actually puts tenantID on the path for
// every tenant-scoped call, for every tenant value passed in.

func TestClient_MCPQuery_UsesTenantScopedPath(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	if _, err := client.MCPQuery("acme-corp", &MCPRequest{Query: "check health"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/api/tenants/acme-corp/mcp/query"
	if gotPath != want {
		t.Errorf("expected path %q, got %q", want, gotPath)
	}
}

func TestClient_MCPToolCall_UsesTenantScopedPath(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	if _, err := client.MCPToolCall("tenant-xyz", "health-check", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/api/tenants/tenant-xyz/mcp/query"
	if gotPath != want {
		t.Errorf("expected path %q, got %q", want, gotPath)
	}
}

// TestClient_MCPQuery_DifferentTenantsProduceDifferentPaths guards
// against a hardcoded/stale tenant segment slipping in — each call
// must reflect the tenantID it was actually given.
func TestClient_MCPQuery_DifferentTenantsProduceDifferentPaths(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
	for _, tenantID := range tenants {
		if _, err := client.MCPQuery(tenantID, &MCPRequest{Query: "ping"}); err != nil {
			t.Fatalf("tenant %q: unexpected error: %v", tenantID, err)
		}

		want := "/api/tenants/" + tenantID + "/mcp/query"
		if gotPath != want {
			t.Errorf("tenant %q: expected path %q, got %q", tenantID, want, gotPath)
		}
	}
}

// ============================================================
// Auth propagation
// ============================================================
//
// The Authorization header is attached once, centrally, in
// Client.do — these tests exercise that through a real endpoint
// (MCPQuery) rather than testing `do` directly, so a regression in
// how MCP calls are wired to `do` would also be caught.

func TestClient_MCPQuery_SetsBearerAuthorizationHeaderWhenAPIKeySet(t *testing.T) {
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token")

	if _, err := client.MCPQuery("acme-corp", &MCPRequest{Query: "check health"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Bearer secret-token"
	if gotAuth != want {
		t.Errorf("expected Authorization header %q, got %q", want, gotAuth)
	}
}

func TestClient_MCPQuery_OmitsAuthorizationHeaderWhenAPIKeyEmpty(t *testing.T) {
	var gotAuth []string
	var headerPresent bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, headerPresent = r.Header["Authorization"], len(r.Header["Authorization"]) > 0
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	if _, err := client.MCPQuery("acme-corp", &MCPRequest{Query: "check health"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if headerPresent {
		t.Errorf("expected no Authorization header when APIKey is empty, got %v", gotAuth)
	}
}

func TestClient_MCPQuery_SetsJSONContentType(t *testing.T) {
	var gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	if _, err := client.MCPQuery("acme-corp", &MCPRequest{Query: "check health"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "application/json"
	if gotContentType != want {
		t.Errorf("expected Content-Type %q, got %q", want, gotContentType)
	}
}

// TestClient_MCPToolCall_PropagatesTenantAndAuthTogether is the
// end-to-end propagation check: a single call must carry the correct
// tenant-scoped path AND the bearer token AND the tool_call body all
// at once, since a partial regression (e.g. auth wired but tenant
// path hardcoded) would not be caught by the single-concern tests
// above if they happened to run against a default/empty value.
func TestClient_MCPToolCall_PropagatesTenantAndAuthTogether(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MCPResponse{Response: "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "tenant-scoped-token")

	_, err := client.MCPToolCall("tenant-42", "echo", map[string]interface{}{"text": "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := "/api/tenants/tenant-42/mcp/query"; gotPath != want {
		t.Errorf("expected path %q, got %q", want, gotPath)
	}

	if want := "Bearer tenant-scoped-token"; gotAuth != want {
		t.Errorf("expected Authorization %q, got %q", want, gotAuth)
	}

	toolCall, ok := gotBody["tool_call"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_call in body, got %v", gotBody)
	}

	if toolCall["tool_name"] != "echo" {
		t.Errorf("expected tool_name %q, got %v", "echo", toolCall["tool_name"])
	}
}