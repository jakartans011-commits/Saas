// middleware/tenant_scope.go
package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// ============================================================
// REQUIRE TENANT SCOPE — tenant boundary EKSPLISIT, terpisah dari
// RequireCapability.
//
// Kenapa ini perlu (dikonfirmasi lewat audit middleware/auth_middleware.go):
// RequireCapability mensubstitusi template "{tenant_id}" SELALU dari
// claims.TenantID, tidak pernah membaca c.Params(...). Jadi tanpa
// middleware ini, JWT tenant-A dengan capability valid bisa mengenai
// endpoint /api/tenants/tenant-B/... tanpa pernah ditolak.
//
// Security invariant:
//   pathTenantID == claims.TenantID  → ALLOW (lanjut ke handler berikutnya)
//   pathTenantID != claims.TenantID  → DENY (403), TIDAK query DB,
//   TIDAK mengubah claims, TIDAK mengganti tenant ID dari URL menjadi
//   tenant ID caller.
//
// HARUS dipasang SETELAH RequireAuth (butuh claims sudah ada di
// fiber.Locals lewat ClaimsFromFiberCtx) dan SEBELUM RequireCapability.
// ============================================================

func RequireTenantScope(paramName string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			// Sama seperti RequireCapability: kalau claims tidak ada di
			// context, ini bug pemasangan middleware (RequireAuth belum
			// dipasang duluan di chain), bukan masalah request itu sendiri.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		pathTenantID := c.Params(paramName)

		// Perbandingan EXACT, case-sensitive. Tenant ID di sistem ini
		// (lihat tenant/tenant.go, tenant/provisioning.go) tidak pernah
		// dinormalisasi ke lower/upper-case di titik manapun, jadi tidak
		// ada alasan melakukan case-insensitive comparison di sini —
		// itu justru berpotensi membuka celah baru (tenant-A vs Tenant-A
		// dianggap sama padahal berbeda row di store).
		if pathTenantID != claims.TenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant_mismatch",
			})
		}

		return c.Next()
	}
}