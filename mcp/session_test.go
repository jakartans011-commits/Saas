package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func doRPC(t *testing.T, s *Server, tenant, user, session, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1/rpc", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-ID", user)
	if session != "" {
		req.Header.Set(SessionHeader, session)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

const (
	initBody    = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	unknownBody = `{"jsonrpc":"2.0","id":2,"method":"nonexistent/method"}`
)

func TestSession_InitializeIssuesSessionID(t *testing.T) {
	s := NewServer(nil)
	rec := doRPC(t, s, "tenant-a", "user-1", "", initBody)
	if rec.Header().Get(SessionHeader) == "" {
		t.Fatalf("expected %s header on initialize, status=%d body=%s",
			SessionHeader, rec.Code, rec.Body.String())
	}
}

func TestSession_ValidSessionPasses(t *testing.T) {
	s := NewServer(nil)
	id := doRPC(t, s, "tenant-a", "user-1", "", initBody).Header().Get(SessionHeader)
	rec := doRPC(t, s, "tenant-a", "user-1", id, unknownBody)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("valid session must not 404")
	}
}

func TestSession_UnknownSessionIs404(t *testing.T) {
	s := NewServer(nil)
	rec := doRPC(t, s, "tenant-a", "user-1", "bogus", unknownBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestSession_CrossTenantIs404(t *testing.T) {
	s := NewServer(nil)
	id := doRPC(t, s, "tenant-a", "user-1", "", initBody).Header().Get(SessionHeader)
	rec := doRPC(t, s, "tenant-b", "user-1", id, unknownBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant session must 404, got %d", rec.Code)
	}
}

func TestSession_LenientAllowsNoHeader(t *testing.T) {
	s := NewServer(nil)
	rec := doRPC(t, s, "tenant-a", "user-1", "", unknownBody)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("lenient mode must allow requests without session header")
	}
}

func TestSession_StrictRequiresHeader(t *testing.T) {
	s := NewServer(nil)
	s.SetStrictSessions(true)
	rec := doRPC(t, s, "tenant-a", "user-1", "", unknownBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("strict mode without header must 404, got %d", rec.Code)
	}
}

func TestSession_ExpiredIs404(t *testing.T) {
	st := NewSessionStore(time.Minute)
	base := time.Now()
	st.now = func() time.Time { return base }
	id := st.Create("t", "u")
	st.now = func() time.Time { return base.Add(2 * time.Minute) }
	if st.Validate(id, "t", "u") {
		t.Fatalf("expired session must be invalid")
	}
}

func doDelete(t *testing.T, s *Server, tenant, user, session string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/mcp/v1/rpc", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-ID", user)
	if session != "" {
		req.Header.Set(SessionHeader, session)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestSession_DeleteTerminatesSession(t *testing.T) {
	s := NewServer(nil)
	id := doRPC(t, s, "tenant-a", "user-1", "", initBody).Header().Get(SessionHeader)

	if rec := doDelete(t, s, "tenant-a", "user-1", id); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE want 204, got %d", rec.Code)
	}
	if rec := doRPC(t, s, "tenant-a", "user-1", id, unknownBody); rec.Code != http.StatusNotFound {
		t.Fatalf("request after DELETE want 404, got %d", rec.Code)
	}
}

func TestSession_DeleteUnknownIs404(t *testing.T) {
	s := NewServer(nil)
	if rec := doDelete(t, s, "tenant-a", "user-1", "bogus"); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestSession_DeleteWithoutHeaderIs400(t *testing.T) {
	s := NewServer(nil)
	if rec := doDelete(t, s, "tenant-a", "user-1", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestSession_DeleteCrossTenantIs404AndKeepsSession(t *testing.T) {
	s := NewServer(nil)
	id := doRPC(t, s, "tenant-a", "user-1", "", initBody).Header().Get(SessionHeader)

	if rec := doDelete(t, s, "tenant-b", "user-1", id); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant DELETE want 404, got %d", rec.Code)
	}
	if rec := doRPC(t, s, "tenant-a", "user-1", id, unknownBody); rec.Code == http.StatusNotFound {
		t.Fatalf("session owner's session must survive foreign DELETE")
	}
}

func TestServeHTTP_GetIs405WithAllow(t *testing.T) {
	s := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/mcp/v1/rpc", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST, DELETE" {
		t.Fatalf("want 405 + Allow 'POST, DELETE', got %d %q", rec.Code, rec.Header().Get("Allow"))
	}
}