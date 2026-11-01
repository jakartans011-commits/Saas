package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/rbac"
)

func TestTeamGate_FreeOwner_CannotCreateUsers(t *testing.T) {
	const pw = "correct-horse-battery-staple"
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-gate-free", "Gate Free", "owner-gate-free@example.test", pw)
	token := env.login(t, "owner-gate-free@example.test", pw)

	before := env.userStore.CountForTenant("tenant-gate-free")

	var out map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-gate-free/users", bearer(token),
		map[string]string{"email": "second@example.test", "password": "another-password", "role_id": "owner"},
		&out,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 on Free plan, got %d", status)
	}
	if out["error"] != "plan_upgrade_required" || out["required_plan"] != "pro" {
		t.Fatalf("expected plan_upgrade_required/pro, got: %v", out)
	}
	if after := env.userStore.CountForTenant("tenant-gate-free"); after != before {
		t.Fatalf("user count changed on Free plan: before=%d after=%d", before, after)
	}
}

func TestTeamGate_ProAdmin_CannotCreateOwner(t *testing.T) {
	const (
		pw  = "correct-horse-battery-staple"
		tid = "tenant-gate-pro"
	)
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, tid, "Gate Pro", "owner-gate-pro@example.test", pw)

	if err := env.roleStore.PutRole(&rbac.Role{
		ID:               "admin",
		TenantID:         tid,
		Name:             "admin",
		CapabilityGrants: []string{"tenant." + tid + ".users.create"},
	}); err != nil {
		t.Fatalf("put admin role: %v", err)
	}

	ownerToken := env.login(t, "owner-gate-pro@example.test", pw)

	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(ownerToken),
		map[string]string{"email": "admin-gate@example.test", "password": "admin-password-1", "role_id": "admin"},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("owner creating admin on Pro: expected 200, got %d", status)
	}

	adminToken := env.login(t, "admin-gate@example.test", "admin-password-1")
	before := env.userStore.CountForTenant(tid)

	var out map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(adminToken),
		map[string]string{"email": "escalated@example.test", "password": "whatever-password", "role_id": "owner"},
		&out,
	)
	if status != fiber.StatusForbidden || out["error"] != "owner_role_requires_owner" {
		t.Fatalf("expected 403 owner_role_requires_owner, got %d %v", status, out)
	}
	if after := env.userStore.CountForTenant(tid); after != before {
		t.Fatalf("admin created an owner: before=%d after=%d", before, after)
	}

	// Kontrol positif: gate hanya menolak role owner; admin boleh membuat admin.
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(adminToken),
		map[string]string{"email": "admin2-gate@example.test", "password": "admin-password-2", "role_id": "admin"},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("admin creating admin: expected 200, got %d", status)
	}
}
