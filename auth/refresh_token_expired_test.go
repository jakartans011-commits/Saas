// auth/refresh_token_expired_test.go
package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// TestRefreshToken_Expired_Rejected
//
// Menguji token refresh yang telah melewati batas waktu
// kedaluwarsa (TTL) agar secara eksplisit ditolak oleh sistem.
//
// Fungsi yang diuji di sini — RotateRefreshTokenForAnyUser — adalah
// fungsi yang SAMA PERSIS dipanggil oleh handler HTTP
// "POST /api/auth/refresh" di saas-api/app.go. Handler itu memetakan
// SETIAP error dari fungsi ini ke:
//
//	c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
//
// Karena package auth tidak boleh (dan tidak perlu) tahu soal Fiber/
// HTTP — itu tanggung jawab saas-api, bukan auth — test ini menguji
// kontrak di titik yang paling dekat dengan boundary itu: memastikan
// token expired MENGHASILKAN ERROR (bukan sukses diam-diam), dan
// error itu membawa taxonomy "refresh_token_expired" yang dipakai
// utuh sebagai body JSON respons 401.
//
// Dengan kata lain: non-nil error di sini == 401 Unauthorized di API.
// ============================================================

func TestRefreshToken_Expired_Rejected(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"refresh_ttl_expired_rejected_test.db",
	)

	db := openTestRefreshBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltRefreshTokenStore(db)
	if err != nil {
		t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
	}

	const userID = "user-ttl-expired"

	token, err := GenerateRefreshToken(userID, store)
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}

	// Paksa token melewati TTL-nya, mensimulasikan refresh token
	// lama yang baru dipakai setelah batas waktu kedaluwarsa lewat.
	record, err := store.FindByHash(hashToken(token))
	if err != nil {
		t.Fatalf("FindByHash failed: %v", err)
	}

	record.ExpiresAt = time.Now().Add(-1 * time.Second)

	if err := store.Save(record); err != nil {
		t.Fatalf("Save expired record failed: %v", err)
	}

	t.Run("rotation_is_explicitly_rejected", func(t *testing.T) {
		newToken, gotUserID, err := RotateRefreshTokenForAnyUser(
			token,
			store,
		)

		if err == nil {
			t.Fatal(
				"expected expired refresh token to be rejected, " +
					"got nil error",
			)
		}

		if newToken != "" {
			t.Errorf(
				"expected no replacement refresh token to be "+
					"issued for an expired token, got %q",
				newToken,
			)
		}

		if gotUserID != "" {
			t.Errorf(
				"expected no user id to be returned for a "+
					"rejected refresh token, got %q",
				gotUserID,
			)
		}

		if !strings.Contains(err.Error(), "refresh_token_expired") {
			t.Fatalf(
				"expected error to carry the refresh_token_expired "+
					"taxonomy code, since saas-api/app.go's "+
					"POST /api/auth/refresh handler forwards "+
					"err.Error() verbatim as the 401 response "+
					"body; got: %v",
				err,
			)
		}

		t.Logf(
			"OK: expired refresh token rejected with %q — "+
				"POST /api/auth/refresh maps this to 401 "+
				"Unauthorized",
			err,
		)
	})

	t.Run("expired_token_is_not_silently_consumed", func(t *testing.T) {
		// Rotate() gagal SEBELUM transaksi commit, jadi record
		// yang sudah expired seharusnya tetap ada dan tetap
		// belum ter-revoke — bukan diam-diam "dihabiskan" seolah
		// berhasil dipakai sekali.
		stillThere, err := store.FindByHash(hashToken(token))
		if err != nil {
			t.Fatalf(
				"expected expired record to still be "+
					"retrievable (rejection, not deletion), "+
					"got error: %v",
				err,
			)
		}

		if stillThere.RevokedAt != nil {
			t.Error(
				"expired token must not be marked revoked by a " +
					"rejected rotation attempt",
			)
		}
	})

	t.Run("repeated_attempts_stay_rejected", func(t *testing.T) {
		// Tidak boleh ada state parsial yang membuat percobaan
		// kedua justru lolos.
		_, _, err := RotateRefreshTokenForAnyUser(token, store)
		if err == nil {
			t.Fatal(
				"expected a repeated rotation attempt on an " +
					"already-expired token to also be rejected",
			)
		}

		if !strings.Contains(err.Error(), "refresh_token_expired") {
			t.Fatalf(
				"expected repeated attempt to still surface "+
					"refresh_token_expired, got: %v",
				err,
			)
		}
	})
}
