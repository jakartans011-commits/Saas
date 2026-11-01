package auth

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// --- InMemoryUserStore.HasOwner ---

func TestInMemoryHasOwner_True(t *testing.T) {
	store := NewInMemoryUserStore()
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "owner"})

	if !store.HasOwner("tenant-a") {
		t.Fatal("expected HasOwner to return true")
	}
}

func TestInMemoryHasOwner_False_NoOwnerRole(t *testing.T) {
	store := NewInMemoryUserStore()
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "member"})

	if store.HasOwner("tenant-a") {
		t.Fatal("expected HasOwner to return false when no owner role exists")
	}
}

func TestInMemoryHasOwner_False_EmptyTenant(t *testing.T) {
	store := NewInMemoryUserStore()
	if store.HasOwner("tenant-nonexistent") {
		t.Fatal("expected HasOwner to return false for tenant with no users")
	}
}

func TestInMemoryHasOwner_IsolatesTenants(t *testing.T) {
	store := NewInMemoryUserStore()
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "owner"})

	if store.HasOwner("tenant-b") {
		t.Fatal("expected HasOwner to not leak owner status across tenants")
	}
}

// --- BoltUserStore.HasOwner ---

func openTestUserDBForOwner(t *testing.T) *BoltUserStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "user_owner_test.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewBoltUserStore(db)
	if err != nil {
		t.Fatalf("failed to init user store: %v", err)
	}
	return store
}

func TestBoltHasOwner_True(t *testing.T) {
	store := openTestUserDBForOwner(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "owner"})

	if !store.HasOwner("tenant-a") {
		t.Fatal("expected HasOwner to return true")
	}
}

func TestBoltHasOwner_False_NoOwnerRole(t *testing.T) {
	store := openTestUserDBForOwner(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "member"})

	if store.HasOwner("tenant-a") {
		t.Fatal("expected HasOwner to return false when no owner role exists")
	}
}

func TestBoltHasOwner_EmptyStore_ReturnsFalse(t *testing.T) {
	store := openTestUserDBForOwner(t)
	if store.HasOwner("tenant-a") {
		t.Fatal("expected HasOwner to return false on empty store")
	}
}

func TestBoltHasOwner_IsolatesTenants(t *testing.T) {
	store := openTestUserDBForOwner(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "owner"})
	_ = store.Put(&User{ID: "u2", TenantID: "tenant-b", RoleID: "member"})

	if !store.HasOwner("tenant-a") {
		t.Fatal("expected tenant-a to have an owner")
	}
	if store.HasOwner("tenant-b") {
		t.Fatal("expected tenant-b to not have an owner")
	}
}

func TestBoltHasOwner_MultipleUsersOnlyOneOwner(t *testing.T) {
	store := openTestUserDBForOwner(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", RoleID: "member"})
	_ = store.Put(&User{ID: "u2", TenantID: "tenant-a", RoleID: "member"})
	_ = store.Put(&User{ID: "u3", TenantID: "tenant-a", RoleID: "owner"})

	if !store.HasOwner("tenant-a") {
		t.Fatal("expected HasOwner to find the single owner among multiple users")
	}
}
