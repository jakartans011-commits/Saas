package auth

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestUserDB(t *testing.T) *BoltUserStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "user_extra_test.db")
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

func TestGetByIDIncludingDeleted_ReturnsActiveUser(t *testing.T) {
	store := openTestUserDB(t)
	user := &User{ID: "u1", TenantID: "tenant-a", Email: "a@example.com"}
	if err := store.Put(user); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := store.GetByIDIncludingDeleted("u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "u1" {
		t.Fatalf("expected id u1, got %s", got.ID)
	}
}

func TestGetByIDIncludingDeleted_ReturnsSoftDeletedUser(t *testing.T) {
	store := openTestUserDB(t)
	now := time.Now()
	user := &User{ID: "u2", TenantID: "tenant-a", Email: "b@example.com", DeletedAt: &now}
	if err := store.Put(user); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Regular GetByID must hide it.
	if _, err := store.GetByID("u2"); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound from GetByID for soft-deleted user, got %v", err)
	}

	// GetByIDIncludingDeleted must still return it.
	got, err := store.GetByIDIncludingDeleted("u2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "u2" {
		t.Fatalf("expected id u2, got %s", got.ID)
	}
	if got.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be preserved")
	}
}

func TestGetByIDIncludingDeleted_UnknownUser_ReturnsNotFound(t *testing.T) {
	store := openTestUserDB(t)
	_, err := store.GetByIDIncludingDeleted("nonexistent")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestListAllForTenant_ReturnsActiveAndDeleted(t *testing.T) {
	store := openTestUserDB(t)
	now := time.Now()

	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a"})
	_ = store.Put(&User{ID: "u2", TenantID: "tenant-a", DeletedAt: &now})
	_ = store.Put(&User{ID: "u3", TenantID: "tenant-b"})

	users, err := store.ListAllForTenant("tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users for tenant-a (active+deleted), got %d", len(users))
	}
}

func TestListAllForTenant_EmptyTenant_ReturnsEmptySlice(t *testing.T) {
	store := openTestUserDB(t)
	users, err := store.ListAllForTenant("tenant-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected empty slice, got %d users", len(users))
	}
}

func TestListDeletedForTenant_ReturnsOnlyDeleted(t *testing.T) {
	store := openTestUserDB(t)
	now := time.Now()

	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a"})
	_ = store.Put(&User{ID: "u2", TenantID: "tenant-a", DeletedAt: &now})
	_ = store.Put(&User{ID: "u3", TenantID: "tenant-a", DeletedAt: &now})

	deleted, err := store.ListDeletedForTenant("tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted users, got %d", len(deleted))
	}
	for _, u := range deleted {
		if u.DeletedAt == nil {
			t.Fatalf("expected all returned users to be soft-deleted, got %s with nil DeletedAt", u.ID)
		}
	}
}

func TestListDeletedForTenant_IsolatesOtherTenants(t *testing.T) {
	store := openTestUserDB(t)
	now := time.Now()

	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", DeletedAt: &now})
	_ = store.Put(&User{ID: "u2", TenantID: "tenant-b", DeletedAt: &now})

	deleted, err := store.ListDeletedForTenant("tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 1 || deleted[0].ID != "u1" {
		t.Fatalf("expected only tenant-a's deleted user, got %+v", deleted)
	}
}

func TestListDeletedForTenant_NoneDeleted_ReturnsEmpty(t *testing.T) {
	store := openTestUserDB(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a"})

	deleted, err := store.ListDeletedForTenant("tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected 0 deleted users, got %d", len(deleted))
	}
}
