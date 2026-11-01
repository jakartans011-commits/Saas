package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"trailseal/usage"
)

// ============================================================
// MOCK EVENT STORE
// ============================================================

type mockEventStore struct {
	mu        sync.Mutex
	saved     []*usage.UsageEvent
	saveCalls int
	saveErr   error
}

func (m *mockEventStore) Save(event *usage.UsageEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, event)
	return nil
}

func (m *mockEventStore) ListForTenant(tenantID string, limit int) ([]*usage.UsageEvent, error) {
	return nil, nil
}

func (m *mockEventStore) CountBillableSinceForTenant(tenantID string, since time.Time) (int, error) {
	return 0, nil
}

// ============================================================
// TEST INFRASTRUCTURE
// ============================================================

// newUsageTestApp membangun app dengan chain RequireAuth -> UsageMetering,
// meniru urutan produksi. Reuse testSigningKey/validClaims/buildToken/
// newActiveUserStore/newMockRevocationStore dari auth_middleware_test.go
// (package sama), supaya cara claims masuk ke context konsisten dengan
// implementasi RequireAuth yang sebenarnya (bukan mock context manual).
func newUsageTestApp(store usage.EventStore, tenantID, userID string) *fiber.App {
	userStore := newActiveUserStore(userID, tenantID)
	revStore := newMockRevocationStore()

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, userStore, revStore))
	app.Use(UsageMetering(store))
	return app
}

func authedRequest(t *testing.T, method, path, tenantID, userID string) *http.Request {
	t.Helper()
	token := buildToken(t, validClaims(userID, tenantID, "role-member"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// ============================================================
// STEP 0: NO CLAIMS
// ============================================================

func TestUsageMetering_NoClaims_Returns401AndDoesNotMeter(t *testing.T) {
	store := &mockEventStore{}

	app := fiber.New()
	app.Use(UsageMetering(store)) // tanpa RequireAuth -> tidak ada claims
	app.Get("/api/tenants/tenant-1/tools", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/api/tenants/tenant-1/tools", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	if store.saveCalls != 0 {
		t.Errorf("expected 0 save calls without claims, got %d", store.saveCalls)
	}
}

// ============================================================
// STEP 1: SKIP NON-TENANT ENDPOINTS
//
// PERHATIAN: karena cek claims (STEP 0) dijalankan SEBELUM cek skip-path
// (STEP 1), endpoint ini TETAP butuh token valid untuk lolos ke titik
// skip. Tanpa token, /health pun akan kena 401 duluan, bukan diteruskan.
// Test ini membuktikan perilaku KODE AKTUAL saat ini.
// ============================================================

func TestUsageMetering_HealthEndpoint_NotMetered(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := authedRequest(t, "GET", "/health", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if store.saveCalls != 0 {
		t.Errorf("expected /health to be skipped (0 saves), got %d", store.saveCalls)
	}
}

func TestUsageMetering_AuthEndpoint_NotMetered(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Post("/api/auth/login", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := authedRequest(t, "POST", "/api/auth/login", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if store.saveCalls != 0 {
		t.Errorf("expected /api/auth/* to be skipped, got %d saves", store.saveCalls)
	}
}

func TestUsageMetering_AdminEndpoint_NotMetered(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/admin/tenants", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := authedRequest(t, "GET", "/api/admin/tenants", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if store.saveCalls != 0 {
		t.Errorf("expected /api/admin/* to be skipped, got %d saves", store.saveCalls)
	}
}

func TestUsageMetering_NonTenantPath_NotMetered(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	// Path valid tapi TIDAK di bawah /api/tenants/
	app.Get("/api/projects", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := authedRequest(t, "GET", "/api/projects", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if store.saveCalls != 0 {
		t.Errorf("expected non-/api/tenants/ path to be skipped, got %d saves", store.saveCalls)
	}
}

// ============================================================
// STEP 3: SELF-METERING EXCLUSION
// ============================================================

func TestUsageMetering_SelfUsageEndpoint_GET_NotMetered(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/tenants/tenant-1/usage", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := authedRequest(t, "GET", "/api/tenants/tenant-1/usage", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if store.saveCalls != 0 {
		t.Errorf("expected GET .../usage self-exclusion, got %d saves", store.saveCalls)
	}
}

// Exclusion HARUS spesifik ke method GET — method lain ke path yang sama
// (misal POST, kalaupun tidak realistis untuk endpoint usage) tetap dicatat.
func TestUsageMetering_UsagePath_POST_IsStillMetered(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Post("/api/tenants/tenant-1/usage", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := authedRequest(t, "POST", "/api/tenants/tenant-1/usage", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if store.saveCalls != 1 {
		t.Errorf("expected POST .../usage to be metered, got %d saves", store.saveCalls)
	}
}

// ============================================================
// STEP 5: HANYA STATUS < 400 YANG DICATAT
// ============================================================

func TestUsageMetering_SuccessfulRequest_RecordsEvent(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/tenants/tenant-1/tools", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := authedRequest(t, "GET", "/api/tenants/tenant-1/tools", "tenant-1", "user-1")
	req.Header.Set("User-Agent", "test-agent/1.0")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if store.saveCalls != 1 {
		t.Fatalf("expected exactly 1 save call, got %d", store.saveCalls)
	}

	event := store.saved[0]
	if event.TenantID != "tenant-1" {
		t.Errorf("expected TenantID tenant-1, got %s", event.TenantID)
	}
	if event.EventType != usage.EventTypeAPIRequest {
		t.Errorf("expected EventTypeAPIRequest, got %s", event.EventType)
	}
	if event.Metadata["method"] != "GET" {
		t.Errorf("expected method GET, got %s", event.Metadata["method"])
	}
	if event.Metadata["path"] != "/api/tenants/tenant-1/tools" {
		t.Errorf("unexpected path metadata: %s", event.Metadata["path"])
	}
	if event.Metadata["status"] != "200" {
		t.Errorf("expected status 200, got %s", event.Metadata["status"])
	}
	if event.Metadata["user_agent"] != "test-agent/1.0" {
		t.Errorf("expected user_agent to be captured, got %q", event.Metadata["user_agent"])
	}
}

func TestUsageMetering_ClientErrorStatus_NotRecorded(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/tenants/tenant-1/tools", func(c *fiber.Ctx) error {
		return c.Status(404).JSON(fiber.Map{"error": "not_found"})
	})

	req := authedRequest(t, "GET", "/api/tenants/tenant-1/tools", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected status 404 to still be returned to client, got %d", resp.StatusCode)
	}
	if store.saveCalls != 0 {
		t.Errorf("expected 0 saves for status >= 400, got %d", store.saveCalls)
	}
}

func TestUsageMetering_ServerErrorStatus_NotRecorded(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/tenants/tenant-1/tools", func(c *fiber.Ctx) error {
		return c.Status(500).JSON(fiber.Map{"error": "internal"})
	})

	req := authedRequest(t, "GET", "/api/tenants/tenant-1/tools", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Errorf("expected 500 to be returned, got %d", resp.StatusCode)
	}
	if store.saveCalls != 0 {
		t.Errorf("expected 0 saves for status >= 400, got %d", store.saveCalls)
	}
}

// ============================================================
// METERING FAILURE HARUS TIDAK MEMBATALKAN REQUEST BISNIS
// ============================================================

func TestUsageMetering_StoreSaveError_DoesNotFailRequest(t *testing.T) {
	store := &mockEventStore{saveErr: errors.New("bolt write failed")}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/tenants/tenant-1/tools", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := authedRequest(t, "GET", "/api/tenants/tenant-1/tools", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("metering failure must not fail the business request, got status %d", resp.StatusCode)
	}
	if store.saveCalls != 1 {
		t.Errorf("expected Save to still be attempted once, got %d calls", store.saveCalls)
	}
}

// ============================================================
// HANDLER ERROR (bukan status code) TETAP DITERUSKAN KE CALLER FIBER
// ============================================================

func TestUsageMetering_HandlerReturnsError_ErrorIsPropagated(t *testing.T) {
	store := &mockEventStore{}
	app := newUsageTestApp(store, "tenant-1", "user-1")
	app.Get("/api/tenants/tenant-1/tools", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusTeapot, "custom error")
	})

	req := authedRequest(t, "GET", "/api/tenants/tenant-1/tools", "tenant-1", "user-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusTeapot {
		t.Errorf("expected error status to propagate (418), got %d", resp.StatusCode)
	}
	if store.saveCalls != 0 {
		t.Errorf("expected 0 saves for error status >= 400, got %d", store.saveCalls)
	}
}
