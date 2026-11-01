package auth

import (
	"testing"
	"time"
)

// seedRevokedToken menyimpan record refresh token yang SUDAH dicabut pada
// waktu tertentu, sehingga umur pencabutan bisa diatur tanpa menunggu.
func seedRevokedToken(t *testing.T, store *BoltRefreshTokenStore, id, plaintext string, revokedAt time.Time) {
	t.Helper()
	if err := store.Save(&RefreshTokenRecord{
		ID:        id,
		UserID:    "user-1",
		TokenHash: hashToken(plaintext),
		ExpiresAt: time.Now().Add(time.Hour),
		RevokedAt: &revokedAt,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// Penggunaan ulang yang sudah lama = rantai bocor. Seluruh sesi pemilik
// harus mati, termasuk token sah yang masih dipegang pelanggan.
func TestRevokeAllSessionsIfTokenReused_StaleReuseKillsWholeChain(t *testing.T) {
	store := newTestRefreshStore(t)

	liveToken, err := GenerateRefreshToken("user-1", store)
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	seedRevokedToken(t, store, "rt-leaked", "token-bocor", time.Now().Add(-time.Hour))

	if !RevokeAllSessionsIfTokenReused("token-bocor", store, ErrRefreshTokenAlreadyUsed) {
		t.Fatal("penggunaan ulang lama seharusnya memicu pencabutan sesi")
	}

	if _, _, err := RotateRefreshTokenForAnyUser(liveToken, store); err == nil {
		t.Fatal("token sah masih hidup setelah rantai bocor terdeteksi")
	}
}

// Pengiriman ganda dalam hitungan detik adalah klien yang gagap, bukan
// serangan. Sesi pelanggan tidak boleh ikut dicabut.
func TestRevokeAllSessionsIfTokenReused_FreshReuseKeepsSession(t *testing.T) {
	store := newTestRefreshStore(t)

	liveToken, err := GenerateRefreshToken("user-1", store)
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	seedRevokedToken(t, store, "rt-retry", "token-retry", time.Now())

	if RevokeAllSessionsIfTokenReused("token-retry", store, ErrRefreshTokenAlreadyUsed) {
		t.Fatal("pengiriman ganda yang wajar tidak boleh mencabut sesi")
	}

	if _, _, err := RotateRefreshTokenForAnyUser(liveToken, store); err != nil {
		t.Fatalf("token sah seharusnya selamat: %v", err)
	}
}

// Error lain tidak boleh memicu pencabutan apa pun.
func TestRevokeAllSessionsIfTokenReused_IgnoresOtherErrors(t *testing.T) {
	store := newTestRefreshStore(t)

	if RevokeAllSessionsIfTokenReused("token-asing", store, ErrRefreshTokenNotFound) {
		t.Error("token tidak dikenal tidak boleh memicu pencabutan sesi")
	}
	if RevokeAllSessionsIfTokenReused("token-asing", store, nil) {
		t.Error("error nil tidak boleh memicu pencabutan sesi")
	}
}
