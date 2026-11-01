package auth

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestRevocationDB(t *testing.T) *bolt.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "revocation_test.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNewBoltAccessTokenRevocationStore_NilDB(t *testing.T) {
	store, err := NewBoltAccessTokenRevocationStore(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
	if store != nil {
		t.Fatal("expected nil store on error")
	}
}

func TestNewBoltAccessTokenRevocationStore_CreatesBucket(t *testing.T) {
	db := openTestRevocationDB(t)
	store, err := NewBoltAccessTokenRevocationStore(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestRevoke_EmptyJTI_Rejected(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	err := store.Revoke("", time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected error for empty jti")
	}
}

func TestRevoke_ZeroExpiry_Rejected(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	err := store.Revoke("jti-1", time.Time{})
	if err == nil {
		t.Fatal("expected error for zero expiry")
	}
}

func TestRevoke_AlreadyExpired_SilentlyIgnored(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	err := store.Revoke("jti-expired", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	revoked, err := store.IsRevoked("jti-expired")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Fatal("expected already-expired jti to not be stored as revoked")
	}
}

func TestRevokeAndIsRevoked_RoundTrip(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	err := store.Revoke("jti-active", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	revoked, err := store.IsRevoked("jti-active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("expected jti to be revoked")
	}
}

func TestIsRevoked_EmptyJTI_Rejected(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	_, err := store.IsRevoked("")
	if err == nil {
		t.Fatal("expected error for empty jti")
	}
}

func TestIsRevoked_UnknownJTI_ReturnsFalse(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	revoked, err := store.IsRevoked("jti-never-seen")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Fatal("expected unknown jti to not be revoked")
	}
}

func TestIsRevoked_LazyCleanupOfExpiredRecord(t *testing.T) {
	db := openTestRevocationDB(t)
	store, _ := NewBoltAccessTokenRevocationStore(db)

	// Revoke with a short expiry in the future, then check after it lapses.
	err := store.Revoke("jti-soon-expired", time.Now().Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	revoked, err := store.IsRevoked("jti-soon-expired")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Fatal("expected expired revocation record to be lazily cleaned up and report not-revoked")
	}

	// Second call should still be false (record actually deleted).
	revoked, err = store.IsRevoked("jti-soon-expired")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Fatal("expected cleaned-up record to remain not-revoked on second check")
	}
}

func TestRevoke_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocation_persist.db")

	db1, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}
	store1, err := NewBoltAccessTokenRevocationStore(db1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store1.Revoke("jti-persist", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}

	db2, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to reopen bolt db: %v", err)
	}
	defer db2.Close()

	store2, err := NewBoltAccessTokenRevocationStore(db2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	revoked, err := store2.IsRevoked("jti-persist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("expected revocation to survive close+reopen")
	}
}
