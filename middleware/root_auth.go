// middleware/root_auth.go
package middleware

import (
	"crypto/subtle"
	"errors"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// REQUIRE ROOT — autentikasi PLATFORM-LEVEL, terpisah total dari
// tenant JWT / rbac / capability. Melindungi operasi provisioning
// dan deprovisioning tenant (Section: Invariant #5, #6).
//
// Credential datang dari SAAS_ROOT_API_KEY (env var), dikirim
// caller lewat header X-Root-API-Key. TIDAK PERNAH:
//   - hardcoded default value
//   - fallback ke JWT tenant
//   - di-log (baik nilai maupun bagian darinya)
//   - dikembalikan di response
//
// Perbandingan pakai crypto/subtle.ConstantTimeCompare untuk
// menghindari timing attack terhadap panjang/isi key.
// ============================================================

// ErrRootKeyNotConfigured dikembalikan oleh NewRequireRoot kalau
// SAAS_ROOT_API_KEY kosong. Caller (cmd/saas-api/main.go) WAJIB
// memperlakukan ini sebagai fatal startup error untuk mode yang
// membutuhkan root administration — server tidak boleh start
// diam-diam tanpa proteksi root aktif.
var ErrRootKeyNotConfigured = errors.New(
	"SAAS_ROOT_API_KEY is not set; refusing to start root-protected routes",
)

// NewRequireRoot membangun middleware RequireRoot dengan root key
// yang SUDAH divalidasi non-kosong oleh caller. rootKey diambil dari
// env var oleh main.go (bukan oleh middleware ini) supaya failure
// mode (env var hilang) bisa gagal di startup, bukan diam-diam
// menolak semua request saat runtime.
//
// Kalau rootKey kosong di titik ini, itu bug pemanggilan — caller
// seharusnya sudah fatal sebelum sampai sini. Untuk defense-in-depth,
// tetap ditolak (bukan panic, bukan default value).
func NewRequireRoot(rootKey string) fiber.Handler {
	rootKeyBytes := []byte(rootKey)

	return func(c *fiber.Ctx) error {
		if len(rootKeyBytes) == 0 {
			// Tidak pernah seharusnya tercapai kalau main.go sudah fatal
			// saat startup ketika env var kosong — tapi jangan pernah
			// "fail open" (lolos begitu saja) kalau ini terjadi.
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "root_auth_unconfigured",
			})
		}

		provided := c.Get("X-Root-API-Key")
		if provided == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing_root_credential",
			})
		}

		// ConstantTimeCompare butuh panjang sama; kalau beda panjang,
		// hasilnya otomatis 0 (tidak match) TANPA membocorkan info
		// panjang lewat early-return sebelum compare.
		match := subtle.ConstantTimeCompare([]byte(provided), rootKeyBytes) == 1

		if !match {
			// SENGAJA tidak menyebut detail (bukan "key too short",
			// bukan "first byte correct") — satu pesan generik saja.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_root_credential",
			})
		}

		return c.Next()
	}
}
