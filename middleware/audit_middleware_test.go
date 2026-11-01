package middleware

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	bolt "go.etcd.io/bbolt"

	"trailseal/audit"
	"trailseal/auth"
)

// ============================================================
// TEST HELPERS
// ============================================================

func openTestAuditDB(t *testing.T) (*bolt.DB, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_test.db")
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return db, func() { db.Close() }
}

// ============================================================
// TEST AUDIT LOGGER - SUCCESS
// ============================================================

func TestAuditLogger_Success(t *testing.T) {
	db, cleanup := openTestAuditDB(t)
	defer cleanup()

	auditStore, err := audit.NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("failed to create audit store: %v", err)
	}

	app := fiber.New()

	// Mock auth middleware
	app.Use(func(c *fiber.Ctx) error {
		claims := &auth.Claims{
			UserID:   "user-1",
			TenantID: "tenant-1",
			RoleID:   "owner",
		}
		c.Locals(localsClaimsKey, claims)
		return c.Next()
	})

	app.Use(AuditLogger(auditStore))
	app.Get("/api/tenants/tenant-1/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/api/tenants/tenant-1/test", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verifikasi audit log
	events, err := auditStore.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}

	if len(events) == 0 {
		t.Error("expected audit event to be recorded")
	}

	// Verifikasi isi audit event
	found := false
	for _, e := range events {
		if e.Action == "GET /api/tenants/tenant-1/test" {
			found = true
			if e.Outcome != audit.OutcomeSuccess {
				t.Errorf("expected outcome success, got %s", e.Outcome)
			}
			if e.TenantID != "tenant-1" {
				t.Errorf("expected tenant_id tenant-1, got %s", e.TenantID)
			}
			if e.UserID != "user-1" {
				t.Errorf("expected user_id user-1, got %s", e.UserID)
			}
			break
		}
	}

	if !found {
		t.Error("expected audit event with action GET /api/tenants/tenant-1/test")
	}
}

// ============================================================
// TEST AUDIT LOGGER - SKIP HEALTH
// ============================================================

func TestAuditLogger_SkipHealth(t *testing.T) {
	db, cleanup := openTestAuditDB(t)
	defer cleanup()

	auditStore, err := audit.NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("failed to create audit store: %v", err)
	}

	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		claims := &auth.Claims{
			UserID:   "user-1",
			TenantID: "tenant-1",
			RoleID:   "owner",
		}
		c.Locals(localsClaimsKey, claims)
		return c.Next()
	})

	app.Use(AuditLogger(auditStore))
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/health", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verifikasi audit log - health endpoint tidak dicatat
	events, err := auditStore.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}

	for _, e := range events {
		if e.Action == "GET /health" {
			t.Error("health endpoint should not be recorded in audit log")
		}
	}
}

// ============================================================
// TEST AUDIT LOGGER - SKIP AUTH
// ============================================================

func TestAuditLogger_SkipAuth(t *testing.T) {
	db, cleanup := openTestAuditDB(t)
	defer cleanup()

	auditStore, err := audit.NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("failed to create audit store: %v", err)
	}

	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		claims := &auth.Claims{
			UserID:   "user-1",
			TenantID: "tenant-1",
			RoleID:   "owner",
		}
		c.Locals(localsClaimsKey, claims)
		return c.Next()
	})

	app.Use(AuditLogger(auditStore))
	app.Post("/api/auth/login", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("POST", "/api/auth/login", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verifikasi audit log - auth endpoint tidak dicatat
	events, err := auditStore.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}

	for _, e := range events {
		if e.Action == "POST /api/auth/login" {
			t.Error("auth endpoint should not be recorded in audit log")
		}
	}
}

// ============================================================
// TEST AUDIT LOGGER - DENIED REQUEST
// ============================================================

func TestAuditLogger_DeniedRequest(t *testing.T) {
	db, cleanup := openTestAuditDB(t)
	defer cleanup()

	auditStore, err := audit.NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("failed to create audit store: %v", err)
	}

	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		claims := &auth.Claims{
			UserID:   "user-1",
			TenantID: "tenant-1",
			RoleID:   "owner",
		}
		c.Locals(localsClaimsKey, claims)
		return c.Next()
	})

	app.Use(AuditLogger(auditStore))
	app.Get("/api/tenants/tenant-1/denied", func(c *fiber.Ctx) error {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	})

	req := httptest.NewRequest("GET", "/api/tenants/tenant-1/denied", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 403 {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}

	// Verifikasi audit log - denied request tetap tercatat
	events, err := auditStore.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}

	found := false
	for _, e := range events {
		if e.Action == "GET /api/tenants/tenant-1/denied" {
			found = true
			if e.Outcome != audit.OutcomeDenied {
				t.Errorf("expected outcome denied, got %s", e.Outcome)
			}
			break
		}
	}

	if !found {
		t.Error("denied request should be recorded in audit log")
	}
}

// ============================================================
// TEST AUDIT LOGGER - SELF AUDIT EXCLUSION
// ============================================================

func TestAuditLogger_SelfAuditExclusion(t *testing.T) {
	db, cleanup := openTestAuditDB(t)
	defer cleanup()

	auditStore, err := audit.NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("failed to create audit store: %v", err)
	}

	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		claims := &auth.Claims{
			UserID:   "user-1",
			TenantID: "tenant-1",
			RoleID:   "owner",
		}
		c.Locals(localsClaimsKey, claims)
		return c.Next()
	})

	app.Use(AuditLogger(auditStore))
	app.Get("/api/tenants/tenant-1/audit", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"events": []string{}})
	})

	req := httptest.NewRequest("GET", "/api/tenants/tenant-1/audit", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verifikasi audit log - self audit tidak dicatat
	events, err := auditStore.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}

	for _, e := range events {
		if e.Action == "GET /api/tenants/tenant-1/audit" {
			t.Error("self-audit endpoint should not be recorded in audit log")
		}
	}
}
