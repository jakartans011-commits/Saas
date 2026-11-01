// auth/user_store_bbolt_test.go
package auth

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func openTestBoltDB(t *testing.T, path string) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test bolt db: %v", err)
	}
	return db
}

func TestBoltUserStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "users_test.db")

	t.Run("put_and_get_roundtrip", func(t *testing.T) {
		db := openTestBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltUserStore(db)
		if err != nil {
			t.Fatalf("NewBoltUserStore failed: %v", err)
		}

		user := &User{
			ID: "u1", TenantID: "tenant-a", Email: "u1@a.test",
			RoleID: "owner", Status: UserStatusActive,
		}
		if err := store.Put(user); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		got, err := store.GetByID("u1")
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if got.Email != "u1@a.test" || got.TenantID != "tenant-a" {
			t.Fatalf("roundtrip mismatch: got %+v", got)
		}
		t.Log("OK: put/get roundtrip matches")
	})

	t.Run("get_nonexistent_returns_error", func(t *testing.T) {
		db := openTestBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltUserStore(db)
		if err != nil {
			t.Fatalf("NewBoltUserStore failed: %v", err)
		}

		_, err = store.GetByID("does-not-exist")
		if err == nil {
			t.Fatal("expected error for nonexistent user, got nil")
		}
		t.Logf("OK: got expected error: %v", err)
	})

	t.Run("delete_all_for_tenant_isolates_correctly", func(t *testing.T) {
		db := openTestBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltUserStore(db)
		if err != nil {
			t.Fatalf("NewBoltUserStore failed: %v", err)
		}

		_ = store.Put(&User{ID: "ta-1", TenantID: "tenant-x", Status: UserStatusActive})
		_ = store.Put(&User{ID: "ta-2", TenantID: "tenant-x", Status: UserStatusActive})
		_ = store.Put(&User{ID: "tb-1", TenantID: "tenant-y", Status: UserStatusActive})

		if got := store.CountForTenant("tenant-x"); got != 2 {
			t.Fatalf("expected 2 users in tenant-x before delete, got %d", got)
		}

		deleted := store.DeleteAllForTenant("tenant-x")
		if deleted != 2 {
			t.Fatalf("expected 2 deleted, got %d", deleted)
		}

		if got := store.CountForTenant("tenant-x"); got != 0 {
			t.Fatalf("GHOST DETECTED: tenant-x should have 0 users, got %d", got)
		}
		if got := store.CountForTenant("tenant-y"); got != 1 {
			t.Fatalf("tenant-y should be UNAFFECTED, expected 1, got %d", got)
		}
		t.Log("OK: delete isolated correctly, tenant-y untouched")
	})

	t.Run("data_survives_close_and_reopen", func(t *testing.T) {
		persistPath := filepath.Join(t.TempDir(), "persist_test.db")

		// --- Sesi 1: tulis data, lalu TUTUP db ---
		db1 := openTestBoltDB(t, persistPath)
		store1, err := NewBoltUserStore(db1)
		if err != nil {
			t.Fatalf("NewBoltUserStore failed: %v", err)
		}
		if err := store1.Put(&User{
			ID: "persist-1", TenantID: "tenant-p", Email: "p@p.test",
			Status: UserStatusActive,
		}); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if err := db1.Close(); err != nil {
			t.Fatalf("failed to close db: %v", err)
		}

		// --- Sesi 2: buka LAGI file yang SAMA, data harus masih ada ---
		db2 := openTestBoltDB(t, persistPath)
		defer db2.Close()
		store2, err := NewBoltUserStore(db2)
		if err != nil {
			t.Fatalf("NewBoltUserStore (reopen) failed: %v", err)
		}

		got, err := store2.GetByID("persist-1")
		if err != nil {
			t.Fatalf("PERSISTENCE FAILED: data not found after reopen: %v", err)
		}
		if got.Email != "p@p.test" {
			t.Fatalf("persisted data corrupted: got %+v", got)
		}
		t.Log("OK: data survives close+reopen — this is the real difference from in-memory store")
	})
}
