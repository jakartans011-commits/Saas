package auth

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// --- InMemoryUserStore.FindByEmail ---

func TestInMemoryFindByEmail_Success(t *testing.T) {
	store := NewInMemoryUserStore()
	user := &User{ID: "u1", TenantID: "tenant-a", Email: "a@example.com"}
	if err := store.Put(user); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := store.FindByEmail("a@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "u1" {
		t.Fatalf("expected id u1, got %s", got.ID)
	}
}

func TestInMemoryFindByEmail_NotFound(t *testing.T) {
	store := NewInMemoryUserStore()
	_, err := store.FindByEmail("nobody@example.com")
	if err == nil {
		t.Fatal("expected error for unknown email")
	}
}

func TestInMemoryFindByEmail_CaseSensitive(t *testing.T) {
	store := NewInMemoryUserStore()
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", Email: "a@example.com"})

	// Documenting current behavior: lookup is exact-match, no
	// case-folding. If this becomes case-insensitive later, update
	// this test to match the new contract deliberately.
	_, err := store.FindByEmail("A@EXAMPLE.COM")
	if err == nil {
		t.Fatal("expected exact-match FindByEmail to reject differently-cased email")
	}
}

// --- BoltUserStore.FindByEmail ---

func openTestUserDBForEmail(t *testing.T) *BoltUserStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "user_email_test.db")
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

func TestBoltFindByEmail_Success(t *testing.T) {
	store := openTestUserDBForEmail(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", Email: "a@example.com"})
	_ = store.Put(&User{ID: "u2", TenantID: "tenant-a", Email: "b@example.com"})

	got, err := store.FindByEmail("b@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "u2" {
		t.Fatalf("expected id u2, got %s", got.ID)
	}
}

func TestBoltFindByEmail_NotFound(t *testing.T) {
	store := openTestUserDBForEmail(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", Email: "a@example.com"})

	_, err := store.FindByEmail("nobody@example.com")
	if err == nil {
		t.Fatal("expected error for unknown email")
	}
}

func TestBoltFindByEmail_EmptyStore_ReturnsNotFound(t *testing.T) {
	store := openTestUserDBForEmail(t)
	_, err := store.FindByEmail("a@example.com")
	if err == nil {
		t.Fatal("expected error when store is empty")
	}
}

func TestBoltFindByEmail_ReturnsIndependentCopy(t *testing.T) {
	store := openTestUserDBForEmail(t)
	_ = store.Put(&User{ID: "u1", TenantID: "tenant-a", Email: "a@example.com"})

	got, err := store.FindByEmail("a@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got.Email = "mutated@example.com"

	got2, err := store.FindByEmail("a@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got2.Email != "a@example.com" {
		t.Fatalf("expected stored record to be unaffected by mutation of returned copy, got %s", got2.Email)
	}
}
