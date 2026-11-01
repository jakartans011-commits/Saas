package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// AUDIT RETENTION E2E TESTS
//
// UPDATED 2026-09-13: migrated from the removed admin-only endpoints
// (/api/admin/audit/retention, /api/admin/audit/cleanup) to the
// tenant-scoped endpoints introduced in Phase 9
// (/api/tenants/:id/audit/retention, .../retention/cleanup). The old
// endpoints were removed from the route table when retention became
// tenant-scoped (see ROADMAP.md Phase 9) — this file was left testing
// the deleted routes until now, causing every test below to fail
// (404 / non-JSON body), unrelated to the actual tenant-scoped
// endpoints which were already live-tested manually and covered by
// the 7 tests in audit/retention_test.go.
// ============================================================

func TestAuditRetention_GetPolicy(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-retention-get", "Tenant Retention Get",
		"owner-retention-get@test.test", "password")
	token := env.login(t, "owner-retention-get@test.test", "password")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-retention-get/audit/retention",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["retention_days"] == nil {
		t.Fatalf("expected retention_days in response")
	}
	if out["anonymization_days"] == nil {
		t.Fatalf("expected anonymization_days in response")
	}
	if out["plan_retention_days"] == nil {
		t.Fatalf("expected plan_retention_days in response")
	}
	if out["tenant_id"] != "tenant-retention-get" {
		t.Fatalf("expected tenant_id 'tenant-retention-get', got %v", out["tenant_id"])
	}
}

func TestAuditRetention_UpdatePolicy(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-retention-update", "Tenant Retention Update",
		"owner-retention-update@test.test", "password")
	token := env.login(t, "owner-retention-update@test.test", "password")

	// 60/15 chosen to stay under the Free-tier ceiling (plan_retention_days
	// falls back to 90 for a tenant that hasn't checked out — see
	// API-CONTRACTS.md "Audit Retention"). The old test used 180, which
	// would now be rejected as retention_days_exceeds_plan.
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodPut, "/api/tenants/tenant-retention-update/audit/retention",
		bearer(token),
		map[string]int{
			"retention_days":     10,
			"anonymization_days": 5,
		},
		&out,
	)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["status"] != "updated" {
		t.Fatalf("expected status 'updated', got %v", out["status"])
	}
	if out["retention_days"] != float64(10) {
		t.Fatalf("expected retention_days 10, got %v", out["retention_days"])
	}
	if out["anonymization_days"] != float64(5) {
		t.Fatalf("expected anonymization_days 15, got %v", out["anonymization_days"])
	}
}

func TestAuditRetention_UpdatePolicyInvalid(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-retention-invalid", "Tenant Retention Invalid",
		"owner-retention-invalid@test.test", "password")
	token := env.login(t, "owner-retention-invalid@test.test", "password")

	// Invalid: anonymization_days > retention_days (both still under the
	// plan ceiling, so this isolates the anonymization_days_exceeds_
	// retention_days check specifically).
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodPut, "/api/tenants/tenant-retention-invalid/audit/retention",
		bearer(token),
		map[string]int{
			"retention_days":     30,
			"anonymization_days": 60,
		},
		&out,
	)
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestAuditRetention_TriggerCleanup(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-retention-cleanup", "Tenant Retention Cleanup",
		"owner-retention-cleanup@test.test", "password")
	token := env.login(t, "owner-retention-cleanup@test.test", "password")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-retention-cleanup/audit/retention/cleanup",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["status"] != "cleanup_completed" {
		t.Fatalf("expected status 'cleanup_completed', got %v", out["status"])
	}
	if out["tenant_id"] != "tenant-retention-cleanup" {
		t.Fatalf("expected tenant_id 'tenant-retention-cleanup', got %v", out["tenant_id"])
	}
}

// TestAuditRetention_AdminOnly — RENAMED SEMANTICS 2026-09-13: capability
// for this endpoint is Owner-only PER TENANT now (tenant.{id}.audit.
// retention.read|write), not root-only like the old admin endpoint. Kept
// the function name to avoid churn elsewhere, but the test now checks
// tenant isolation (an Owner from tenant A cannot read tenant B's policy)
// and that an unauthenticated request is rejected — the closest
// equivalent to the old "only privileged callers allowed" intent.
func TestAuditRetention_AdminOnly(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-retention-admin-a", "Tenant Retention Admin A",
		"owner-retention-admin-a@test.test", "password")
	env.bootstrapTenantOwner(t, "tenant-retention-admin-b", "Tenant Retention Admin B",
		"owner-retention-admin-b@test.test", "password")

	tokenA := env.login(t, "owner-retention-admin-a@test.test", "password")

	// Tenant A's owner tries to read Tenant B's retention policy — must
	// be denied (cross-tenant, same pattern as other RequireTenantScope
	// checks in this suite, e.g. TestCapability_Escalation_CrossTenantCapability).
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-retention-admin-b/audit/retention",
		bearer(tokenA),
		nil,
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant retention read, got %d", status)
	}

	// No token at all — must be rejected.
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-retention-admin-a/audit/retention",
		nil,
		nil,
		nil,
	)
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated retention read, got %d", status)
	}
}

func TestAuditRetention_ExportAudit(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-audit-export", "Tenant Audit Export",
		"owner-export@test.test", "password")

	token := env.login(t, "owner-export@test.test", "password")

	// Make some requests to generate audit events
	for i := 0; i < 3; i++ {
		status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-audit-export/usage",
			bearer(token),
			nil,
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("failed to generate audit event %d: status %d", i, status)
		}
	}

	// Export audit log
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-audit-export/audit/export",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["tenant_id"] != "tenant-audit-export" {
		t.Fatalf("expected tenant_id 'tenant-audit-export', got %v", out["tenant_id"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) == 0 {
		t.Fatalf("expected at least one event, got 0")
	}
}
