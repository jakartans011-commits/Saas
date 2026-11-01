package main

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// AUDIT LOGGING E2E TESTS
// ============================================================

func TestAuditLogging_TenantCanReadOwnAudit(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-audit-a", "Tenant Audit A",
		"owner-a@audit.test", "password-a")

	tokenA := env.login(t, "owner-a@audit.test", "password-a")

	// Buat beberapa request untuk generate audit events
	for i := 0; i < 3; i++ {
		email := "user" + string(rune('a'+i)) + "@audit.test"
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-audit-a/users",
			bearer(tokenA),
			map[string]string{
				"email":    email,
				"password": "password",
				"role_id":  "owner",
			},
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("failed to create user %s: status %d", email, status)
		}
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-audit-a/audit",
		bearer(tokenA),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["tenant_id"] != "tenant-audit-a" {
		t.Fatalf("expected tenant_id=tenant-audit-a, got %v", out["tenant_id"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) < 3 {
		t.Fatalf("expected at least 3 audit events, got %d", len(events))
	}
}

func TestAuditLogging_TenantCannotReadOtherTenantAudit(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-audit-a", "Tenant Audit A",
		"owner-a2@audit.test", "password-a2")
	env.bootstrapTenantOwner(t, "tenant-audit-b", "Tenant Audit B",
		"owner-b@audit.test", "password-b")

	tokenA := env.login(t, "owner-a2@audit.test", "password-a2")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-audit-b/audit",
		bearer(tokenA),
		nil,
		&out,
	)

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	if out["error"] != "tenant_mismatch" {
		t.Fatalf("expected error tenant_mismatch, got %v", out["error"])
	}
}

func TestAuditLogging_RootCanReadAnyTenantAudit(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-audit-root", "Tenant Audit Root",
		"owner-root@audit.test", "password-root")

	tokenRoot := env.login(t, "owner-root@audit.test", "password-root")

	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-audit-root/users",
		bearer(tokenRoot),
		map[string]string{
			"email":    "user-root@audit.test",
			"password": "password",
			"role_id":  "owner",
		},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("failed to create user: status %d", status)
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/admin/tenants/tenant-audit-root/audit",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["tenant_id"] != "tenant-audit-root" {
		t.Fatalf("expected tenant_id=tenant-audit-root, got %v", out["tenant_id"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) < 1 {
		t.Fatalf("expected at least 1 audit event, got %d", len(events))
	}
}

func TestAuditLogging_RootTenantNotFound(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/admin/tenants/tenant-not-exist/audit",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		nil,
		&out,
	)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
	if out["error"] != "tenant not found" {
		t.Fatalf("expected error 'tenant not found', got %v", out["error"])
	}
}

func TestAuditLogging_RecordsDeniedRequests(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-audit-deny", "Tenant Audit Deny",
		"owner-deny@audit.test", "password-deny")
	env.bootstrapTenantOwner(t, "tenant-audit-deny-b", "Tenant Audit Deny B",
		"owner-deny-b@audit.test", "password-deny-b")

	tokenA := env.login(t, "owner-deny@audit.test", "password-deny")

	// Cross-tenant request → harus denied
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-audit-deny-b/users",
		bearer(tokenA),
		map[string]string{
			"email":    "intruder@audit.test",
			"password": "password",
			"role_id":  "owner",
		},
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}

	time.Sleep(100 * time.Millisecond)

	// Tenant A harus bisa melihat audit event dari request yang gagal ini
	var out map[string]interface{}
	status2 := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-audit-deny/audit",
		bearer(tokenA),
		nil,
		&out,
	)
	if status2 != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status2)
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}

	// Cari event dengan outcome denied
	foundDenied := false
	for _, e := range events {
		eventMap, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if outcome, ok := eventMap["outcome"].(string); ok && outcome == "denied" {
			foundDenied = true
			break
		}
	}

	if !foundDenied {
		t.Fatal("expected to find denied audit event, but none found")
	}
}

func TestAuditLogging_SelfAuditExclusion(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-self-audit", "Tenant Self Audit",
		"owner-self@audit.test", "password-self")

	token := env.login(t, "owner-self@audit.test", "password-self")

	// Buat satu event (create user)
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-self-audit/users",
		bearer(token),
		map[string]string{
			"email":    "user-self@audit.test",
			"password": "password",
			"role_id":  "owner",
		},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("failed to create user: status %d", status)
	}

	time.Sleep(100 * time.Millisecond)

	var out1 map[string]interface{}
	status1 := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-self-audit/audit",
		bearer(token),
		nil,
		&out1,
	)
	if status1 != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status1)
	}

	events1, ok := out1["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out1["events"])
	}
	count1 := len(events1)

	time.Sleep(100 * time.Millisecond)

	var out2 map[string]interface{}
	status2 := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-self-audit/audit",
		bearer(token),
		nil,
		&out2,
	)
	if status2 != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status2)
	}

	events2, ok := out2["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out2["events"])
	}
	count2 := len(events2)

	// Jumlah audit event harus SAMA (self-audit exclusion)
	if count1 != count2 {
		t.Fatalf("self-audit exclusion failed: count1=%d, count2=%d (should be equal)", count1, count2)
	}
}

func TestAuditLogging_LimitDefault(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-audit-limit", "Tenant Audit Limit",
		"owner-limit@audit.test", "password-limit")

	token := env.login(t, "owner-limit@audit.test", "password-limit")

	// Buat 10 events
	for i := 0; i < 10; i++ {
		email := "user" + string(rune('a'+i)) + "@audit-limit.test"
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-audit-limit/users",
			bearer(token),
			map[string]string{
				"email":    email,
				"password": "password",
				"role_id":  "owner",
			},
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("failed to create user %s: status %d", email, status)
		}
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-audit-limit/audit",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["limit"] != float64(50) {
		t.Fatalf("expected limit=50, got %v", out["limit"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) < 10 {
		t.Fatalf("expected at least 10 audit events, got %d", len(events))
	}
}
