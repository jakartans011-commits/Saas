// rbac/role_store_bbolt_test.go
package rbac

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func openTestRoleBoltDB(t *testing.T, path string) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test bolt db: %v", err)
	}
	return db
}

func TestBoltRoleStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "roles_test.db")

	t.Run("put_and_get_roundtrip", func(t *testing.T) {
		db := openTestRoleBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRoleStore(db)
		if err != nil {
			t.Fatalf("NewBoltRoleStore failed: %v", err)
		}

		role := &Role{
			ID: "owner", TenantID: "tenant-a", Name: "owner",
			CapabilityGrants: []string{"tenant.tenant-a.*"},
		}
		if err := store.PutRole(role); err != nil {
			t.Fatalf("PutRole failed: %v", err)
		}

		got, err := store.GetRole("tenant-a", "owner")
		if err != nil {
			t.Fatalf("GetRole failed: %v", err)
		}
		if len(got.CapabilityGrants) != 1 || got.CapabilityGrants[0] != "tenant.tenant-a.*" {
			t.Fatalf("roundtrip mismatch: got %+v", got)
		}
		t.Log("OK: put/get roundtrip matches")
	})

	// ============================================================
	// PALING PENTING: role ID SAMA di tenant berbeda TIDAK BOLEH
	// saling bocor — ini bug class yang sama seperti Section 17
	// (role_id collision) yang sudah diuji di level RoleResolver
	// Fase 1, sekarang diuji ULANG khusus di level storage bbolt.
	// ============================================================

	t.Run("same_role_id_different_tenant_no_leak", func(t *testing.T) {
		db := openTestRoleBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRoleStore(db)
		if err != nil {
			t.Fatalf("NewBoltRoleStore failed: %v", err)
		}

		if err := store.PutRole(&Role{
			ID: "admin", TenantID: "tenant-x", Name: "admin",
			CapabilityGrants: []string{"tenant.tenant-x.*"},
		}); err != nil {
			t.Fatalf("PutRole tenant-x failed: %v", err)
		}
		if err := store.PutRole(&Role{
			ID: "admin", TenantID: "tenant-y", Name: "admin",
			CapabilityGrants: []string{"tenant.tenant-y.*"},
		}); err != nil {
			t.Fatalf("PutRole tenant-y failed: %v", err)
		}

		roleX, err := store.GetRole("tenant-x", "admin")
		if err != nil {
			t.Fatalf("GetRole tenant-x failed: %v", err)
		}
		if roleX.CapabilityGrants[0] != "tenant.tenant-x.*" {
			t.Fatalf("LEAK DETECTED: tenant-x admin got grant %v", roleX.CapabilityGrants)
		}

		roleY, err := store.GetRole("tenant-y", "admin")
		if err != nil {
			t.Fatalf("GetRole tenant-y failed: %v", err)
		}
		if roleY.CapabilityGrants[0] != "tenant.tenant-y.*" {
			t.Fatalf("LEAK DETECTED: tenant-y admin got grant %v", roleY.CapabilityGrants)
		}
		t.Log("OK: identical role ID in two tenants resolved independently, no leak")
	})

	t.Run("get_role_wrong_tenant_returns_not_found_not_other_tenants_role", func(t *testing.T) {
		db := openTestRoleBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRoleStore(db)
		if err != nil {
			t.Fatalf("NewBoltRoleStore failed: %v", err)
		}
		if err := store.PutRole(&Role{
			ID: "member", TenantID: "tenant-z", Name: "member",
			CapabilityGrants: []string{"tenant.tenant-z.module.recon.read"},
		}); err != nil {
			t.Fatalf("PutRole failed: %v", err)
		}

		// Minta role "member" tapi di tenant yang TIDAK PERNAH punya role ini.
		_, err = store.GetRole("tenant-nonexistent", "member")
		if err == nil {
			t.Fatal("SECURITY FAILURE: role from tenant-z leaked to tenant-nonexistent lookup")
		}
		t.Logf("OK: correctly not found for wrong tenant: %v", err)
	})

	t.Run("delete_all_for_tenant_isolates_correctly", func(t *testing.T) {
		db := openTestRoleBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRoleStore(db)
		if err != nil {
			t.Fatalf("NewBoltRoleStore failed: %v", err)
		}

		_ = store.PutRole(&Role{ID: "r1", TenantID: "tenant-del", CapabilityGrants: []string{"tenant.tenant-del.a"}})
		_ = store.PutRole(&Role{ID: "r2", TenantID: "tenant-del", CapabilityGrants: []string{"tenant.tenant-del.b"}})
		_ = store.PutRole(&Role{ID: "r1", TenantID: "tenant-keep", CapabilityGrants: []string{"tenant.tenant-keep.a"}})

		deleted := store.DeleteAllForTenant("tenant-del")
		if deleted != 2 {
			t.Fatalf("expected 2 deleted, got %d", deleted)
		}
		if got := store.CountForTenant("tenant-del"); got != 0 {
			t.Fatalf("GHOST DETECTED: %d role(s) remain for tenant-del", got)
		}
		if got := store.CountForTenant("tenant-keep"); got != 1 {
			t.Fatalf("tenant-keep should be UNAFFECTED, expected 1, got %d", got)
		}
		t.Log("OK: delete isolated correctly, tenant-keep untouched")
	})

	t.Run("data_survives_close_and_reopen", func(t *testing.T) {
		persistPath := filepath.Join(t.TempDir(), "roles_persist_test.db")

		db1 := openTestRoleBoltDB(t, persistPath)
		store1, err := NewBoltRoleStore(db1)
		if err != nil {
			t.Fatalf("NewBoltRoleStore failed: %v", err)
		}
		if err := store1.PutRole(&Role{
			ID: "owner", TenantID: "tenant-persist",
			CapabilityGrants: []string{"tenant.tenant-persist.*"},
		}); err != nil {
			t.Fatalf("PutRole failed: %v", err)
		}
		if err := db1.Close(); err != nil {
			t.Fatalf("failed to close db: %v", err)
		}

		db2 := openTestRoleBoltDB(t, persistPath)
		defer db2.Close()
		store2, err := NewBoltRoleStore(db2)
		if err != nil {
			t.Fatalf("NewBoltRoleStore (reopen) failed: %v", err)
		}

		got, err := store2.GetRole("tenant-persist", "owner")
		if err != nil {
			t.Fatalf("PERSISTENCE FAILED: role not found after reopen: %v", err)
		}
		if got.CapabilityGrants[0] != "tenant.tenant-persist.*" {
			t.Fatalf("persisted role corrupted: got %+v", got)
		}
		t.Log("OK: role data survives close+reopen")
	})
}