package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// COMPLIANCE E2E TESTS — GDPR Deletion
// ============================================================

func TestCompliance_SoftDeleteTenant(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-gdpr-delete", "Tenant GDPR Delete",
		"owner-gdpr@test.test", "password")

	token := env.login(t, "owner-gdpr@test.test", "password")

	// Soft delete tenant
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gdpr-delete/data/delete",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["status"] != "soft_deleted" {
		t.Fatalf("expected status 'soft_deleted', got %v", out["status"])
	}

	// Try to access tenant data — should be inaccessible
	var usageOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-gdpr-delete/usage",
		bearer(token),
		nil,
		&usageOut,
	)
	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404 for soft-deleted tenant, got %d", status)
	}
}

func TestCompliance_RestoreTenant(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-gdpr-restore", "Tenant GDPR Restore",
		"owner-restore@test.test", "password")

	token := env.login(t, "owner-restore@test.test", "password")

	// Soft delete
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gdpr-restore/data/delete",
		bearer(token),
		nil,
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("soft delete failed: status %d", status)
	}

	// Restore
	var out map[string]interface{}
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gdpr-restore/data/restore",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["status"] != "restored" {
		t.Fatalf("expected status 'restored', got %v", out["status"])
	}

	// Verify tenant is accessible again
	var usageOut map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-gdpr-restore/usage",
		bearer(token),
		nil,
		&usageOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 after restore, got %d", status)
	}
}

func TestCompliance_CrossTenantCannotDelete(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-gdpr-a", "Tenant A",
		"owner-a@test.test", "password-a")
	env.bootstrapTenantOwner(t, "tenant-gdpr-b", "Tenant B",
		"owner-b@test.test", "password-b")

	tokenA := env.login(t, "owner-a@test.test", "password-a")

	// Tenant A tries to soft delete Tenant B
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gdpr-b/data/delete",
		bearer(tokenA),
		nil,
		nil,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant delete, got %d", status)
	}
}

func TestCompliance_AdminSoftDelete(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-gdpr-admin", "Tenant GDPR Admin",
		"owner-admin@test.test", "password")

	// Admin soft delete
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodPost, "/api/admin/tenants/tenant-gdpr-admin/data/delete",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["status"] != "soft_deleted" {
		t.Fatalf("expected status 'soft_deleted', got %v", out["status"])
	}
}

func TestCompliance_PurgeTenant(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-gdpr-purge", "Tenant GDPR Purge",
		"owner-purge@test.test", "password")

	token := env.login(t, "owner-purge@test.test", "password")

	// Soft delete
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gdpr-purge/data/delete",
		bearer(token),
		nil,
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("soft delete failed: status %d", status)
	}

	// Purge should fail due to grace period
	var out map[string]interface{}
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gdpr-purge/data/purge",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 due to grace period, got %d", status)
	}

	// Error message should mention grace period
	errorMsg, ok := out["error"].(string)
	if !ok {
		t.Fatalf("expected error string, got %v", out["error"])
	}
	if errorMsg != "grace period not yet passed: 720h0m0s remaining" &&
		errorMsg != "tenant must be soft-deleted before purge" {
		t.Logf("Expected grace period error, got: %s", errorMsg)
	}
}
