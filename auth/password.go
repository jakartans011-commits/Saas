// auth/password.go
package auth

import (
	"os"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// ============================================================
// PASSWORD HASHING — bcrypt, TIDAK PERNAH plaintext/MD5/SHA256
// polos (sesuai catatan Section 25 dari prompt Fase 1 sebelumnya).
// ============================================================

// bcrypt cost 12 — lebih tinggi dari default (10) untuk keamanan
// ekstra, masih dalam batas wajar performa untuk login (~250ms).
// Jangan turunkan di bawah 10 tanpa alasan kuat.
//
// PRODUCTION: nilai ini SELALU 12, tidak berubah, kecuali env var
// SHADOWHARVEST_TEST_BCRYPT_COST di-set eksplisit (lihat init()).
// JANGAN PERNAH set env var ini di lingkungan production/deployment.
const defaultBcryptCost = 12

var bcryptCost = defaultBcryptCost

func init() {
	// Hanya untuk mempercepat test suite di bawah -race, di mana
	// bcrypt cost 12 + instrumentasi race detector membuat suite
	// jadi sangat lambat. Tidak berpengaruh ke production karena
	// env var ini tidak pernah di-set di deployment sungguhan.
	raw := os.Getenv("SHADOWHARVEST_TEST_BCRYPT_COST")
	if raw == "" {
		return
	}

	cost, err := strconv.Atoi(raw)
	if err != nil {
		return
	}

	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		// Nilai di luar batas valid bcrypt diabaikan, tetap
		// pakai default 12 — fail-safe ke arah lebih aman.
		return
	}

	bcryptCost = cost
}

func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword mengembalikan nil kalau plaintext cocok dengan hash,
// error kalau tidak cocok (termasuk hash yang malformed).
func VerifyPassword(hash, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
}
