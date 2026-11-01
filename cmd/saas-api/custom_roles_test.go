package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/rbac"
)

const customRolesTestPassword = "correct-horse-battery-staple"

func setPlanTierForCustomRolesTest(t *testing.T, env *testEnv, tenantID, tier string) {
	t.Helper()
	tn, err := env.tenantStore.Get(tenantID)
	if err != nil {
		t.Fatalf("get tenant %s: %v", tenantID, err)
	}
	tn.PlanTier = tier
	if err := env.tenantStore.Put(tn); err != nil {
		t.Fatalf("set plan %s for tenant %s: %v", tier, tenantID, err)
	}
}

func customRoleField(out map[string]any, key string) any {
	r, _ := out["role"].(map[string]any)
	return r[key]
}

func TestCustomRoles_ProOwner_CannotCreate(t *testing.T) {
	const tid = "tenant-cr-pro"
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, tid, "CR Pro", "owner-cr-pro@example.test", customRolesTestPassword)
	token := env.login(t, "owner-cr-pro@example.test", customRolesTestPassword)

	var out map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/custom-roles", bearer(token),
		map[string]any{"name": "Auditor", "capabilities": []string{"audit.read"}}, &out)
	if status != fiber.StatusForbidden || out["error"] != "plan_upgrade_required" || out["required_plan"] != "business" {
		t.Fatalf("expected 403 plan_upgrade_required/business, got %d %v", status, out)
	}
}

func TestCustomRoles_Business_FullFlow(t *testing.T) {
	const tid = "tenant-cr-biz"
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, tid, "CR Biz", "owner-cr-biz@example.test", customRolesTestPassword)
	setPlanTierForCustomRolesTest(t, env, tid, "business")
	token := env.login(t, "owner-cr-biz@example.test", customRolesTestPassword)
	base := "/api/tenants/" + tid + "/custom-roles"

	post := func(body map[string]any) (int, map[string]any) {
		var out map[string]any
		status := env.doJSON(t, fiber.MethodPost, base, bearer(token), body, &out)
		return status, out
	}

	// Pembuatan valid
	status, out := post(map[string]any{"name": "Auditor", "capabilities": []string{"audit.read", "audit.export"}})
	if status != fiber.StatusOK || customRoleField(out, "id") != "custom-auditor" {
		t.Fatalf("create: expected 200 custom-auditor, got %d %v", status, out)
	}

	// Validasi
	if status, out = post(map[string]any{"name": "Biller", "capabilities": []string{"billing.write"}}); status != fiber.StatusBadRequest || out["error"] != "unknown_capability" {
		t.Fatalf("billing.write must be rejected, got %d %v", status, out)
	}
	if status, out = post(map[string]any{"name": "Admin", "capabilities": []string{"audit.read"}}); status != fiber.StatusBadRequest || out["error"] != "reserved_role_name" {
		t.Fatalf("reserved name must be rejected, got %d %v", status, out)
	}
	if status, out = post(map[string]any{"name": "x", "capabilities": []string{"audit.read"}}); status != fiber.StatusBadRequest || out["error"] != "invalid_role_name" {
		t.Fatalf("short name must be rejected, got %d %v", status, out)
	}
	if status, out = post(map[string]any{"name": "Empty", "capabilities": []string{}}); status != fiber.StatusBadRequest || out["error"] != "capabilities_required" {
		t.Fatalf("empty capabilities must be rejected, got %d %v", status, out)
	}
	if status, out = post(map[string]any{"name": "Auditor", "capabilities": []string{"audit.read"}}); status != fiber.StatusConflict || out["error"] != "role_already_exists" {
		t.Fatalf("duplicate must be 409, got %d %v", status, out)
	}

	// List
	var list map[string]any
	if status = env.doJSON(t, fiber.MethodGet, base, bearer(token), nil, &list); status != fiber.StatusOK || list["count"] != float64(1) || list["plan_allows"] != true {
		t.Fatalf("list: expected count=1 plan_allows=true, got %d %v", status, list)
	}

	// Update
	var upd map[string]any
	status = env.doJSON(t, fiber.MethodPut, base+"/custom-auditor", bearer(token),
		map[string]any{"capabilities": []string{"audit.read"}}, &upd)
	if caps, _ := customRoleField(upd, "capabilities").([]any); status != fiber.StatusOK || len(caps) != 1 {
		t.Fatalf("update: expected 200 with 1 capability, got %d %v", status, upd)
	}

	// Owner memberikan role kustom ke user baru
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(token),
		map[string]string{"email": "auditor@example.test", "password": "auditor-password-1", "role_id": "custom-auditor"}, nil)
	if status != fiber.StatusOK {
		t.Fatalf("owner assigning custom role: expected 200, got %d", status)
	}

	// Role yang dipegang user tidak boleh dihapus
	var del map[string]any
	if status = env.doJSON(t, fiber.MethodDelete, base+"/custom-auditor", bearer(token), nil, &del); status != fiber.StatusConflict || del["error"] != "role_in_use" {
		t.Fatalf("delete in-use: expected 409 role_in_use, got %d %v", status, del)
	}

	// Role tanpa pemegang bisa dihapus
	if status, out = post(map[string]any{"name": "Temp Role", "capabilities": []string{"usage.read"}}); status != fiber.StatusOK || customRoleField(out, "id") != "custom-temp-role" {
		t.Fatalf("create temp role: got %d %v", status, out)
	}
	if status = env.doJSON(t, fiber.MethodDelete, base+"/custom-temp-role", bearer(token), nil, &del); status != fiber.StatusOK {
		t.Fatalf("delete unused role: expected 200, got %d", status)
	}

	// Pemegang role kustom tidak bisa membuat user (users.create tidak ada di katalog)
	auditorToken := env.login(t, "auditor@example.test", "auditor-password-1")
	var denied map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(auditorToken),
		map[string]string{"email": "x@example.test", "password": "whatever-password", "role_id": "owner"}, &denied)
	if status != fiber.StatusForbidden || denied["error"] == "role_resolution_failed" {
		t.Fatalf("custom role holder: expected 403 capability denial (role resolved), got %d %v", status, denied)
	}
}

func TestCustomRoles_Admin_CannotAssignCustomRole(t *testing.T) {
	const tid = "tenant-cr-admin"
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, tid, "CR Admin", "owner-cr-admin@example.test", customRolesTestPassword)
	setPlanTierForCustomRolesTest(t, env, tid, "business")

	if err := env.roleStore.PutRole(&rbac.Role{
		ID: "admin", TenantID: tid, Name: "admin",
		CapabilityGrants: []string{"tenant." + tid + ".users.create"},
	}); err != nil {
		t.Fatalf("put admin role: %v", err)
	}
	ownerToken := env.login(t, "owner-cr-admin@example.test", customRolesTestPassword)

	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/custom-roles", bearer(ownerToken),
		map[string]any{"name": "Auditor", "capabilities": []string{"audit.read"}}, nil)
	if status != fiber.StatusOK {
		t.Fatalf("create role: expected 200, got %d", status)
	}
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(ownerToken),
		map[string]string{"email": "admin-cr@example.test", "password": "admin-password-1", "role_id": "admin"}, nil)
	if status != fiber.StatusOK {
		t.Fatalf("create admin user: expected 200, got %d", status)
	}

	adminToken := env.login(t, "admin-cr@example.test", "admin-password-1")
	var out map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/users", bearer(adminToken),
		map[string]string{"email": "escalated-cr@example.test", "password": "whatever-password", "role_id": "custom-auditor"}, &out)
	if status != fiber.StatusForbidden || out["error"] != "custom_role_requires_owner" {
		t.Fatalf("admin assigning custom role: expected 403 custom_role_requires_owner, got %d %v", status, out)
	}

	// Admin juga tidak boleh mengelola role kustom (roles.write hanya owner).
	status = env.doJSON(t, fiber.MethodPost, "/api/tenants/"+tid+"/custom-roles", bearer(adminToken),
		map[string]any{"name": "Sneaky", "capabilities": []string{"audit.read"}}, nil)
	if status != fiber.StatusForbidden {
		t.Fatalf("admin creating custom role: expected 403, got %d", status)
	}
}

// Penangguhan saat request: Business = role kustom, Pro = bertindak Member,
// Free = ditangguhkan, upgrade lagi = pulih. Tanpa mengubah data apa pun.
func TestCustomRoles_PlanAwareResolver_DowngradeAndRestore(t *testing.T) {
	const tid = "tenant-cr-resolver"
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, tid, "CR Resolver", "owner-cr-resolver@example.test", customRolesTestPassword)

	if err := env.roleStore.PutRole(&rbac.Role{
		ID: "custom-auditor", TenantID: tid, Name: "Auditor",
		CapabilityGrants: []string{"tenant." + tid + ".audit.read"},
	}); err != nil {
		t.Fatalf("put custom role: %v", err)
	}
	if err := env.roleStore.PutRole(&rbac.Role{
		ID: "member", TenantID: tid, Name: "member",
		CapabilityGrants: []string{"tenant." + tid + ".usage.read"},
	}); err != nil {
		t.Fatalf("put member role: %v", err)
	}

	resolver := rbac.NewRoleResolver(newPlanAwareRoleStore(env.roleStore, env.tenantStore))
	resolve := func() ([]string, error) { return resolver.ResolveCapabilities(tid, "custom-auditor") }

	setPlanTierForCustomRolesTest(t, env, tid, "business")
	if got, err := resolve(); err != nil || len(got) != 1 || got[0] != "tenant."+tid+".audit.read" {
		t.Fatalf("business: expected custom grants, got %v err=%v", got, err)
	}

	setPlanTierForCustomRolesTest(t, env, tid, "pro")
	if got, err := resolve(); err != nil || len(got) != 1 || got[0] != "tenant."+tid+".usage.read" {
		t.Fatalf("pro: expected member grants, got %v err=%v", got, err)
	}

	setPlanTierForCustomRolesTest(t, env, tid, "free")
	if got, err := resolve(); err == nil {
		t.Fatalf("free: expected suspension error, got grants %v", got)
	}

	setPlanTierForCustomRolesTest(t, env, tid, "business")
	if got, err := resolve(); err != nil || len(got) != 1 || got[0] != "tenant."+tid+".audit.read" {
		t.Fatalf("restored business: expected custom grants, got %v err=%v", got, err)
	}
}
