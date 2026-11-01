// rbac/role_test.go
package rbac

import "testing"

func TestInMemoryRoleStore_PutAndGetRole(t *testing.T) {
	store := NewInMemoryRoleStore()
	role := &Role{ID: "r1", TenantID: "tenant-a", Name: "admin", CapabilityGrants: []string{"tenant.tenant-a.*"}}

	if err := store.PutRole(role); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}

	got, err := store.GetRole("tenant-a", "r1")
	if err != nil {
		t.Fatalf("GetRole failed: %v", err)
	}
	if got.Name != "admin" {
		t.Errorf("expected name admin, got %s", got.Name)
	}
}

func TestInMemoryRoleStore_PutRole_MissingTenantID(t *testing.T) {
	store := NewInMemoryRoleStore()
	role := &Role{ID: "r1", Name: "admin"}

	if err := store.PutRole(role); err == nil {
		t.Fatal("expected error for missing tenant_id")
	}
}

func TestInMemoryRoleStore_GetRole_NotFound(t *testing.T) {
	store := NewInMemoryRoleStore()
	if _, err := store.GetRole("tenant-a", "does-not-exist"); err == nil {
		t.Fatal("expected error for missing role")
	}
}

func TestInMemoryRoleStore_GetRole_TenantIsolation(t *testing.T) {
	store := NewInMemoryRoleStore()
	role := &Role{ID: "r1", TenantID: "tenant-a", Name: "admin"}
	if err := store.PutRole(role); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}

	// Same role ID, different tenant — must NOT resolve to tenant-a's role.
	if _, err := store.GetRole("tenant-b", "r1"); err == nil {
		t.Fatal("expected error: role from tenant-a must not leak to tenant-b")
	}
}

func TestRoleResolver_ResolveCapabilities(t *testing.T) {
	store := NewInMemoryRoleStore()
	role := &Role{
		ID:               "r1",
		TenantID:         "tenant-a",
		Name:             "admin",
		CapabilityGrants: []string{"tenant.tenant-a.*", "tenant.tenant-a.billing.write"},
	}
	if err := store.PutRole(role); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}

	resolver := NewRoleResolver(store)
	grants, err := resolver.ResolveCapabilities("tenant-a", "r1")
	if err != nil {
		t.Fatalf("ResolveCapabilities failed: %v", err)
	}
	if len(grants) != 2 {
		t.Errorf("expected 2 grants, got %d", len(grants))
	}
}

func TestRoleResolver_ResolveCapabilities_RoleNotFound(t *testing.T) {
	store := NewInMemoryRoleStore()
	resolver := NewRoleResolver(store)

	if _, err := resolver.ResolveCapabilities("tenant-a", "does-not-exist"); err == nil {
		t.Fatal("expected error for missing role")
	}
}

// fakeMismatchStore lets us reach ResolveCapabilities' defense-in-depth
// branch (role.TenantID != tenantID) — a scenario InMemoryRoleStore's own
// key-scoping can never trigger on its own, but a future RoleStore
// implementation (bbolt/SQL) with a query bug could.
type fakeMismatchStore struct {
	role *Role
}

func (f *fakeMismatchStore) GetRole(tenantID, roleID string) (*Role, error) {
	return f.role, nil
}
func (f *fakeMismatchStore) PutRole(role *Role) error { return nil }

func TestRoleResolver_ResolveCapabilities_TenantMismatchDefenseInDepth(t *testing.T) {
	store := &fakeMismatchStore{role: &Role{ID: "r1", TenantID: "tenant-b", CapabilityGrants: []string{"x"}}}
	resolver := NewRoleResolver(store)

	if _, err := resolver.ResolveCapabilities("tenant-a", "r1"); err == nil {
		t.Fatal("expected tenant_mismatch error")
	}
}
