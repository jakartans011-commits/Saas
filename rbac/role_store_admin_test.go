// rbac/role_store_admin_test.go
package rbac

import "testing"

func TestInMemoryRoleStore_CountForTenant(t *testing.T) {
	store := NewInMemoryRoleStore()
	_ = store.PutRole(&Role{ID: "r1", TenantID: "tenant-a"})
	_ = store.PutRole(&Role{ID: "r2", TenantID: "tenant-a"})
	_ = store.PutRole(&Role{ID: "r3", TenantID: "tenant-b"})

	if got := store.CountForTenant("tenant-a"); got != 2 {
		t.Errorf("expected 2 roles for tenant-a, got %d", got)
	}
	if got := store.CountForTenant("tenant-b"); got != 1 {
		t.Errorf("expected 1 role for tenant-b, got %d", got)
	}
	if got := store.CountForTenant("tenant-c"); got != 0 {
		t.Errorf("expected 0 roles for tenant-c, got %d", got)
	}
}

func TestInMemoryRoleStore_DeleteAllForTenant(t *testing.T) {
	store := NewInMemoryRoleStore()
	_ = store.PutRole(&Role{ID: "r1", TenantID: "tenant-a"})
	_ = store.PutRole(&Role{ID: "r2", TenantID: "tenant-a"})
	_ = store.PutRole(&Role{ID: "r3", TenantID: "tenant-b"})

	deleted := store.DeleteAllForTenant("tenant-a")
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}
	if got := store.CountForTenant("tenant-a"); got != 0 {
		t.Errorf("expected 0 remaining for tenant-a, got %d", got)
	}
	if got := store.CountForTenant("tenant-b"); got != 1 {
		t.Errorf("expected tenant-b untouched, got %d", got)
	}
}

func TestInMemoryRoleStore_DeleteAllForTenant_NoneFound(t *testing.T) {
	store := NewInMemoryRoleStore()
	if deleted := store.DeleteAllForTenant("nonexistent"); deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}
