package auth

import (
	"testing"
	"time"
)

func TestInMemoryAccessTokenRevocationStore(t *testing.T) {
	store := NewInMemoryAccessTokenRevocationStore()

	expiresAt := time.Now().Add(15 * time.Minute)

	if err := store.Revoke("jti-1", expiresAt); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	revoked, err := store.IsRevoked("jti-1")
	if err != nil {
		t.Fatalf("IsRevoked failed: %v", err)
	}

	if !revoked {
		t.Fatal("expected jti-1 to be revoked")
	}

	revoked, err = store.IsRevoked("jti-2")
	if err != nil {
		t.Fatalf("IsRevoked unknown JTI failed: %v", err)
	}

	if revoked {
		t.Fatal("unexpected revocation for unknown JTI")
	}
}

func TestInMemoryAccessTokenRevocationStore_ExpiredEntry(t *testing.T) {
	store := NewInMemoryAccessTokenRevocationStore()

	expiredAt := time.Now().Add(-time.Minute)

	if err := store.Revoke("expired-jti", expiredAt); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	revoked, err := store.IsRevoked("expired-jti")
	if err != nil {
		t.Fatalf("IsRevoked failed: %v", err)
	}

	if revoked {
		t.Fatal("expired JTI must not remain revoked")
	}
}
