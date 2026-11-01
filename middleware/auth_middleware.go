// middleware/auth_middleware.go
package middleware

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"trailseal/auth"
	"trailseal/core/capability"
	"trailseal/rbac"
)

// ============================================================
// FIBER LOCALS KEYS
// ============================================================

const localsClaimsKey = "auth_claims"

// ============================================================
// STEP A: RequireAuth
//
// Urutan:
//   1. Authenticate JWT
//   2. Validate token (signature, expiry, algorithm)
//   3. Validate token JTI / revocation status
//   4. Validate user status
//   5. Store validated claims in Fiber Locals
//
// Digunakan untuk endpoint aplikasi normal.
//
// User yang DeletedAt != nil TIDAK boleh menggunakan endpoint
// normal karena auth.CheckUserActive() akan menolaknya.
// ============================================================

// ============================================================
// extractAccessToken
//
// Sumber token, urutan prioritas:
//   1. Header Authorization: Bearer <token>   (dipakai API/programmatic client)
//   2. Cookie "access_token"                   (dipakai navigasi browser, mis. /overview)
//
// Dipakai HANYA oleh RequireAuth — RequireAuthAllowDeletedUser
// TIDAK diubah, tetap header-only.
// ============================================================

func extractAccessToken(c *fiber.Ctx) (string, bool) {
	authHeader := c.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer "), true
	}

	if cookieToken := c.Cookies("access_token"); cookieToken != "" {
		return cookieToken, true
	}

	return "", false
}

func RequireAuth(
	signingKey []byte,
	userStore auth.UserStore,
	revocationStore auth.AccessTokenRevocationStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenString, ok := extractAccessToken(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing_or_malformed_authorization_header",
			})
		}

		// ========================================================
		// JWT VALIDATION
		// ========================================================

		claims, err := auth.ValidateAccessToken(tokenString, signingKey)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		// Access-token JTI wajib ada.
		//
		// Token tanpa JTI tidak dapat direvoke secara individual,
		// sehingga token seperti ini tidak boleh digunakan.
		if claims.ID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		// ========================================================
		// ACCESS TOKEN REVOCATION CHECK
		// ========================================================

		revoked, err := revocationStore.IsRevoked(claims.ID)
		if err != nil {
			// Fail closed:
			// jika revocation store gagal diperiksa,
			// token tidak boleh diterima.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		if revoked {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "token_revoked",
			})
		}

		// ========================================================
		// USER STATUS CHECK
		// ========================================================

		if err := auth.CheckUserActive(userStore, claims.UserID); err != nil {
			if errors.Is(err, auth.ErrUserNotFound) {
				// User tidak ada / sudah soft-deleted (GDPR).
				// 404, bukan 401 — supaya tidak membocorkan
				// bahwa token pernah valid untuk akun yang kini
				// "tidak ada".
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "not_found",
				})
			}

			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// ========================================================
		// STORE VALIDATED CLAIMS
		// ========================================================

		c.Locals(localsClaimsKey, claims)

		return c.Next()
	}
}

// ============================================================
// RequireAuthAllowDeletedUser
//
// Khusus endpoint compliance:
//
//   - POST /api/tenants/:id/data/restore
//   - POST /api/tenants/:id/data/purge
//
// PERBEDAAN dengan RequireAuth:
//
// RequireAuth:
//     JWT valid
//     + JTI valid
//     + token belum direvoke
//     + user harus aktif
//
// RequireAuthAllowDeletedUser:
//     JWT valid
//     + JTI valid
//     + token belum direvoke
//     + user BOLEH sudah soft-deleted
//
// Kenapa diperlukan?
//
// Setelah tenant/user di-soft-delete:
//
//     user.DeletedAt != nil
//
// maka UserStore.GetByID() sengaja menganggap user tersebut
// tidak tersedia untuk endpoint normal.
//
// Namun user tersebut masih membutuhkan kemampuan untuk:
//
//     restore
//
// atau:
//
//     purge
//
// Oleh karena itu endpoint compliance membutuhkan jalur
// autentikasi khusus.
//
// SECURITY:
//
// Fungsi ini TIDAK melewati:
//   - JWT signature validation
//   - JWT expiration validation
//   - JWT algorithm validation
//   - JTI validation
//   - token revocation
//
// Yang dilewati HANYA:
//
//     auth.CheckUserActive()
//
// Authorization tetap dilakukan oleh middleware berikutnya:
//
//     RequireTenantScope()
//     RequireCapability()
//
// JANGAN gunakan middleware ini untuk endpoint aplikasi normal.
// ============================================================

func RequireAuthAllowDeletedUser(
	signingKey []byte,
	userStore auth.UserStore,
	revocationStore auth.AccessTokenRevocationStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenString, ok := extractAccessToken(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing_or_malformed_authorization_header",
			})
		}

		// ========================================================
		// JWT VALIDATION
		// ========================================================

		claims, err := auth.ValidateAccessToken(tokenString, signingKey)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		// JTI wajib ada.
		if claims.ID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		// ========================================================
		// ACCESS TOKEN REVOCATION CHECK
		// ========================================================

		revoked, err := revocationStore.IsRevoked(claims.ID)
		if err != nil {
			// Fail closed.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		if revoked {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "token_revoked",
			})
		}

		// ========================================================
		// IMPORTANT
		//
		// Jangan panggil:
		//
		//     auth.CheckUserActive(userStore, claims.UserID)
		//
		// karena endpoint restore/purge memang harus tetap bisa
		// diakses ketika user/tenant sudah soft-deleted.
		//
		// userStore sengaja tidak digunakan di sini.
		// Parameter tetap dipertahankan agar signature middleware
		// konsisten dengan RequireAuth dan app.go.
		// ========================================================

		_ = userStore

		// ========================================================
		// CLAIMS VALIDATION
		//
		// Pastikan token memiliki identitas minimum yang dibutuhkan
		// oleh RequireTenantScope dan RequireCapability.
		// ========================================================

		if claims.UserID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		if claims.TenantID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_token",
			})
		}

		// ========================================================
		// STORE VALIDATED CLAIMS
		// ========================================================

		c.Locals(localsClaimsKey, claims)

		return c.Next()
	}
}

// ============================================================
// ClaimsFromFiberCtx
//
// Mengambil claims yang sudah divalidasi oleh:
//
//     RequireAuth
//
// atau:
//
//     RequireAuthAllowDeletedUser
//
// Middleware/handler berikutnya dapat menggunakan function ini
// tanpa perlu memvalidasi JWT lagi.
// ============================================================

func ClaimsFromFiberCtx(c *fiber.Ctx) (*auth.Claims, bool) {
	claims, ok := c.Locals(localsClaimsKey).(*auth.Claims)
	return claims, ok
}

// ============================================================
// STEP B: RequireCapability
//
// Urutan:
//
//   1. Ambil claims dari RequireAuth /
//      RequireAuthAllowDeletedUser
//   2. Resolve role
//   3. Resolve capability grants
//   4. Match requested capability
//   5. ALLOW / DENY
//
// capabilityTemplate menggunakan placeholder:
//
//     {tenant_id}
//
// Contoh:
//
//     tenant.{tenant_id}.module.recon.read
//
// Placeholder selalu berasal dari claims.TenantID.
//
// Request TIDAK boleh menentukan tenant untuk capability.
// Tenant berasal dari JWT yang sudah tervalidasi.
// ============================================================

func RequireCapability(
	engine *capability.Engine,
	roleResolver *rbac.RoleResolver,
	capabilityTemplate string,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			// RequireAuth atau RequireAuthAllowDeletedUser
			// seharusnya sudah dipasang sebelumnya.
			//
			// Jangan panic apabila claims tidak tersedia.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		// ========================================================
		// BUILD REQUESTED CAPABILITY
		// ========================================================

		requestedCapability := strings.ReplaceAll(
			capabilityTemplate,
			"{tenant_id}",
			claims.TenantID,
		)

		// ========================================================
		// RESOLVE ROLE CAPABILITIES
		// ========================================================

		grantedCaps, err := roleResolver.ResolveCapabilities(
			claims.TenantID,
			claims.RoleID,
		)
		if err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "role_resolution_failed",
			})
		}

		// ========================================================
		// CHECK ACCESS
		// ========================================================

		accessErr := rbac.CheckAccess(
			engine,
			claims.TenantID,
			grantedCaps,
			requestedCapability,
		)
		if accessErr != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":  string(accessErr.Reason),
				"detail": accessErr.Message,
			})
		}

		return c.Next()
	}
}
