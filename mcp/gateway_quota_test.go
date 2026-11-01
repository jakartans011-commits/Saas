package mcp

import (
	"errors"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
	"trailseal/agent"
)

// ============================================================
// mockQuotaEnforcer — implements tenant.QuotaEnforcer
// ============================================================

type mockQuotaEnforcer struct {
	allowed   bool
	remaining int64
	err       error

	calls  int
	lastID string
	lastN  int64
}

func (m *mockQuotaEnforcer) TryConsumeQuota(tenantID string, n int64) (bool, int64, error) {
	m.calls++
	m.lastID = tenantID
	m.lastN = n
	return m.allowed, m.remaining, m.err
}

// ============================================================
// helpers
// ============================================================

func openGatewayQuotaDB(t *testing.T) (*bolt.DB, func()) {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "gw_quota.db"), 0600, nil)
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	return db, func() { _ = db.Close() }
}

func newGatewayWithQuotaDB(t *testing.T, db *bolt.DB) *Gateway {
	t.Helper()
	agentStore, err := agent.NewBoltAgentStore(db)
	if err != nil {
		t.Fatalf("NewBoltAgentStore: %v", err)
	}
	return NewGateway(agentStore, nil)
}

func minimalHealthRequest() *MCPRequest {
	return &MCPRequest{Query: "health"}
}

// ============================================================
// SetQuotaEnforcer — wiring
// ============================================================

func TestGateway_SetQuotaEnforcer_EnforcerIsCalledOnQuery(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	gw.SetQuotaEnforcer(enforcer)

	_, _ = gw.Query("tenant-1", "user-1", minimalHealthRequest())

	if enforcer.calls == 0 {
		t.Fatal("expected TryConsumeQuota to be called, got 0 calls")
	}
}

func TestGateway_SetQuotaEnforcer_PassesCorrectTenantID(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	gw.SetQuotaEnforcer(enforcer)

	_, _ = gw.Query("acme-corp", "user-1", minimalHealthRequest())

	if enforcer.lastID != "acme-corp" {
		t.Errorf("expected tenantID=%q, got %q", "acme-corp", enforcer.lastID)
	}
}

func TestGateway_SetQuotaEnforcer_ConsumesOneUnitPerQuery(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	gw.SetQuotaEnforcer(enforcer)

	_, _ = gw.Query("t1", "u1", minimalHealthRequest())

	if enforcer.lastN != 1 {
		t.Errorf("expected n=1 per query, got n=%d", enforcer.lastN)
	}
}

func TestGateway_SetQuotaEnforcer_CalledOncePerQuery(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	gw.SetQuotaEnforcer(enforcer)

	for i := 0; i < 3; i++ {
		_, _ = gw.Query("t1", "u1", minimalHealthRequest())
	}

	if enforcer.calls != 3 {
		t.Errorf("expected 3 calls for 3 queries, got %d", enforcer.calls)
	}
}

// ============================================================
// Quota exceeded — MCPErrorQuotaExceeded
// ============================================================

func TestGateway_QuotaExceeded_ReturnsQuotaError(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	gw.SetQuotaEnforcer(&mockQuotaEnforcer{allowed: false, remaining: 0})

	_, err := gw.Query("tenant-1", "user-1", minimalHealthRequest())
	if err == nil {
		t.Fatal("expected error when quota exceeded, got nil")
	}

	mcpErr, ok := err.(*MCPError)
	if !ok {
		t.Fatalf("expected *MCPError, got %T: %v", err, err)
	}
	if mcpErr.Code != MCPErrorQuotaExceeded {
		t.Errorf("expected code=%q (QuotaExceeded), got %q", MCPErrorQuotaExceeded, mcpErr.Code)
	}
}

func TestGateway_QuotaExceeded_ErrorMessageIsDescriptive(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	gw.SetQuotaEnforcer(&mockQuotaEnforcer{allowed: false, remaining: 0})

	_, err := gw.Query("t1", "u1", minimalHealthRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGateway_QuotaExceeded_DoesNotCallAgentStore(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	gw.SetQuotaEnforcer(&mockQuotaEnforcer{allowed: false, remaining: 0})

	_, err := gw.Query("nonexistent-tenant", "user-1", minimalHealthRequest())
	if err == nil {
		t.Fatal("expected error")
	}

	mcpErr, ok := err.(*MCPError)
	if !ok {
		t.Fatalf("expected *MCPError, got %T", err)
	}
	if mcpErr.Code != MCPErrorQuotaExceeded {
		t.Errorf("got code=%q, want %q — agent store was queried when it shouldn't be",
			mcpErr.Code, MCPErrorQuotaExceeded)
	}
}

// ============================================================
// Quota enforcer returns error — InternalError
// ============================================================

func TestGateway_QuotaError_ReturnsInternalError(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	gw.SetQuotaEnforcer(&mockQuotaEnforcer{
		err: errors.New("db connection lost"),
	})

	_, err := gw.Query("t1", "u1", minimalHealthRequest())
	if err == nil {
		t.Fatal("expected error when enforcer fails")
	}

	mcpErr, ok := err.(*MCPError)
	if !ok {
		t.Fatalf("expected *MCPError, got %T: %v", err, err)
	}
	if mcpErr.Code != MCPErrorInternal {
		t.Errorf("expected code=%q (Internal), got %q", MCPErrorInternal, mcpErr.Code)
	}
}

func TestGateway_QuotaError_WrapsOriginalError(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	sentinel := errors.New("store unavailable")
	gw.SetQuotaEnforcer(&mockQuotaEnforcer{err: sentinel})

	_, err := gw.Query("t1", "u1", minimalHealthRequest())
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, sentinel) {
		t.Errorf("expected errors.Is(err, sentinel)=true; err=%v", err)
	}
}

// ============================================================
// Nil quota enforcer — backward compatibility
// ============================================================

func TestGateway_NilQuota_DoesNotBlockQuery(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	// SetQuotaEnforcer NOT called → quota == nil

	_, err := gw.Query("t1", "u1", minimalHealthRequest())

	if err != nil {
		mcpErr, ok := err.(*MCPError)
		if ok && mcpErr.Code == MCPErrorQuotaExceeded {
			t.Fatal("nil quota enforcer must NOT produce QuotaExceeded")
		}
	}
}

func TestGateway_NilQuota_SetToNilExplicitlyIsNoOp(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)
	gw.SetQuotaEnforcer(nil) // must not panic

	_, err := gw.Query("t1", "u1", minimalHealthRequest())

	if err != nil {
		mcpErr, ok := err.(*MCPError)
		if ok && mcpErr.Code == MCPErrorQuotaExceeded {
			t.Fatal("nil enforcer must not produce QuotaExceeded")
		}
	}
}

// ============================================================
// Hot swap enforcer
// ============================================================

func TestGateway_SetQuotaEnforcer_CanBeReplaced(t *testing.T) {
	db, cleanup := openGatewayQuotaDB(t)
	defer cleanup()

	gw := newGatewayWithQuotaDB(t, db)

	first := &mockQuotaEnforcer{allowed: true, remaining: 99}
	gw.SetQuotaEnforcer(first)
	_, _ = gw.Query("t1", "u1", minimalHealthRequest())

	second := &mockQuotaEnforcer{allowed: false, remaining: 0}
	gw.SetQuotaEnforcer(second)
	_, err := gw.Query("t1", "u1", minimalHealthRequest())

	if first.calls != 1 {
		t.Errorf("first enforcer: expected 1 call, got %d", first.calls)
	}
	if second.calls != 1 {
		t.Errorf("second enforcer: expected 1 call, got %d", second.calls)
	}
	if err == nil {
		t.Fatal("expected QuotaExceeded from second enforcer")
	}
	mcpErr, _ := err.(*MCPError)
	if mcpErr == nil || mcpErr.Code != MCPErrorQuotaExceeded {
		t.Errorf("expected QuotaExceeded, got %v", err)
	}
}