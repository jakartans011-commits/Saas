package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// inMemoryRefreshTokenStoreWithRevoke — InMemoryRefreshTokenStore
// TIDAK mengimplementasikan RefreshTokenStore secara penuh: method
// Revoke tidak pernah ditambahkan ke struct itu (lihat jwt.go).
// Production selalu memakai BoltRefreshTokenStore (yang punya Revoke
// lengkap), jadi celah ini tidak pernah kena di jalur nyata — tapi
// berarti InMemoryRefreshTokenStore tidak bisa dipakai langsung di
// fungsi yang menerima parameter bertipe RefreshTokenStore
// (GenerateRefreshToken, RotateRefreshTokenForAnyUser).
//
// Adapter kecil ini HANYA untuk kebutuhan test path in-memory —
// tidak mengubah apa pun di jwt.go.
type inMemoryRefreshTokenStoreWithRevoke struct {
	*InMemoryRefreshTokenStore
}

func (s *inMemoryRefreshTokenStoreWithRevoke) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if rec.ID == id {
			now := time.Now()
			rec.RevokedAt = &now
			return nil
		}
	}
	return errors.New("refresh_token_not_found")
}

func newInMemoryRefreshTokenStoreWithRevoke() *inMemoryRefreshTokenStoreWithRevoke {
	return &inMemoryRefreshTokenStoreWithRevoke{InMemoryRefreshTokenStore: NewInMemoryRefreshTokenStore()}
}

// --- InMemoryRefreshTokenStore ---

func TestNewInMemoryRefreshTokenStore_Empty(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	_, err := store.FindByHash("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown hash on fresh store")
	}
}

func TestInMemoryRefreshTokenStore_SaveAndFindByHash(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	record := &RefreshTokenRecord{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash-abc",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if err := store.Save(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := store.FindByHash("hash-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "rt-1" || got.UserID != "user-1" {
		t.Fatalf("unexpected record returned: %+v", got)
	}
}

func TestInMemoryRefreshTokenStore_FindByHash_NotFound(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	_, err := store.FindByHash("missing-hash")
	if err == nil {
		t.Fatal("expected error for missing hash")
	}
}

func TestInMemoryRefreshTokenStore_Rotate_Success(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	record := &RefreshTokenRecord{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash-abc",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	_ = store.Save(record)

	rotated, err := store.Rotate("hash-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rotated.RevokedAt == nil {
		t.Fatal("expected Rotate to mark record as revoked")
	}
}

func TestInMemoryRefreshTokenStore_Rotate_UnknownHash(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	_, err := store.Rotate("missing-hash")
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
}

func TestInMemoryRefreshTokenStore_Rotate_AlreadyRevoked_Rejected(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	record := &RefreshTokenRecord{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash-abc",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	_ = store.Save(record)

	if _, err := store.Rotate("hash-abc"); err != nil {
		t.Fatalf("unexpected error on first rotate: %v", err)
	}

	// Second rotate of the same (now-revoked) hash must fail — this
	// is the replay-after-rotation protection.
	if _, err := store.Rotate("hash-abc"); err == nil {
		t.Fatal("expected error rotating an already-revoked token")
	}
}

func TestInMemoryRefreshTokenStore_Rotate_Expired_Rejected(t *testing.T) {
	store := NewInMemoryRefreshTokenStore()
	record := &RefreshTokenRecord{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash-expired",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	_ = store.Save(record)

	_, err := store.Rotate("hash-expired")
	if err == nil {
		t.Fatal("expected error rotating an expired token")
	}
}

func TestGenerateRefreshToken_WithInMemoryStore(t *testing.T) {
	store := newInMemoryRefreshTokenStoreWithRevoke()
	plaintext, err := GenerateRefreshToken("user-1", store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plaintext == "" {
		t.Fatal("expected non-empty plaintext token")
	}

	hash := hashToken(plaintext)
	record, err := store.FindByHash(hash)
	if err != nil {
		t.Fatalf("expected token to be findable by hash: %v", err)
	}
	if record.UserID != "user-1" {
		t.Fatalf("expected UserID user-1, got %s", record.UserID)
	}
}

func TestRotateRefreshTokenForAnyUser_WithInMemoryStore(t *testing.T) {
	store := newInMemoryRefreshTokenStoreWithRevoke()
	plaintext, err := GenerateRefreshToken("user-1", store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newPlaintext, userID, err := RotateRefreshTokenForAnyUser(plaintext, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "user-1" {
		t.Fatalf("expected user-1, got %s", userID)
	}
	if newPlaintext == "" || newPlaintext == plaintext {
		t.Fatal("expected a new, different plaintext token")
	}

	// Old token must now be rejected (Rotate is single-use).
	if _, _, err := RotateRefreshTokenForAnyUser(plaintext, store); err == nil {
		t.Fatal("expected old refresh token to be rejected after rotation")
	}
}

// --- Context helpers ---

func TestWithClaimsAndClaimsFromContext_RoundTrip(t *testing.T) {
	claims := &Claims{UserID: "u1", TenantID: "tenant-a", RoleID: "owner"}
	ctx := WithClaims(context.Background(), claims)

	got, ok := ClaimsFromContext(ctx)
	if !ok {
		t.Fatal("expected claims to be found in context")
	}
	if got.UserID != "u1" || got.TenantID != "tenant-a" {
		t.Fatalf("unexpected claims returned: %+v", got)
	}
}

func TestClaimsFromContext_NotSet_ReturnsFalse(t *testing.T) {
	_, ok := ClaimsFromContext(context.Background())
	if ok {
		t.Fatal("expected ok=false when no claims are set in context")
	}
}

func TestClaimsFromContext_WrongType_ReturnsFalse(t *testing.T) {
	// Same key used for something else — must not panic or type-assert incorrectly.
	ctx := context.WithValue(context.Background(), claimsContextKey, "not-a-claims-struct")
	_, ok := ClaimsFromContext(ctx)
	if ok {
		t.Fatal("expected ok=false when context value has the wrong type")
	}
}
