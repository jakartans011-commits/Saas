package middleware

import (
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
)

// AuditLogger mencatat audit event untuk setiap authenticated tenant-scoped request,
// termasuk request yang kemudian ditolak oleh middleware authorization.
// HARUS dipasang SETELAH RequireAuth (jalur JWT) ATAU RequireAPIKey (jalur
// API key, mis. /mcp/v1/rpc) tetapi SEBELUM middleware authorization lainnya.
//
// Mencatat SEMUA request (termasuk yang gagal/denied) untuk forensics.
// Berbeda dengan UsageMetering yang hanya mencatat success.
func AuditLogger(auditStore audit.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Ambil identitas dari context. Dua jalur auth berbeda bisa
		// memasang middleware ini: RequireAuth (JWT, dashboard) ATAU
		// RequireAPIKey (API key, MCP native /mcp/v1/rpc) — keduanya
		// menyimpan identitas di Locals key yang berbeda, jadi coba
		// JWT dulu, fallback ke API key sebelum menyerah.
		var tenantID, userID string
		if claims, ok := ClaimsFromFiberCtx(c); ok {
			tenantID = claims.TenantID
			userID = claims.UserID
		} else if key, ok := APIKeyFromFiberCtx(c); ok {
			// Sinkron dengan sintesis userID di InjectMCPIdentityFromAPIKey
			// ("apikey:"+key.ID) — API key adalah caller machine, bukan
			// human user, tapi audit trail tetap butuh identitas non-kosong.
			tenantID = key.TenantID
			userID = "apikey:" + key.ID
		} else {
			// Tidak ada RequireAuth maupun RequireAPIKey yang berhasil
			// dipasang lebih dulu di chain — tidak bisa lanjut, tidak
			// ada tenant ID untuk dicatat.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		path := c.Path()
		method := c.Method()

		// ============================================================
		// STEP 1: Skip non-audit endpoints
		// ============================================================
		// /health, /api/auth/*, /api/admin/*
		if path == "/health" ||
			(len(path) >= 10 && path[:10] == "/api/auth/") ||
			(len(path) >= 11 && path[:11] == "/api/admin/") {
			return c.Next()
		}

		// ============================================================
		// STEP 2: Hanya audit tenant-scoped endpoints
		// ============================================================
		if len(path) < 13 || path[:13] != "/api/tenants/" {
			return c.Next()
		}

		// ============================================================
		// STEP 3: Self-audit exclusion
		// GET /api/tenants/:id/audit tidak dicatat
		// ============================================================
		if method == fiber.MethodGet && path == "/api/tenants/"+tenantID+"/audit" {
			return c.Next()
		}

		// ============================================================
		// STEP 4: Jalankan handler TERLEBIH DAHULU
		// ============================================================
		err := c.Next()

		// ============================================================
		// STEP 5: Tentukan outcome berdasarkan status
		// ============================================================
		status := c.Response().StatusCode()

		var outcome audit.Outcome
		switch {
		case status >= 200 && status < 400:
			outcome = audit.OutcomeSuccess
		case status == fiber.StatusForbidden || status == fiber.StatusUnauthorized:
			outcome = audit.OutcomeDenied
		case status >= 400 && status < 500:
			outcome = audit.OutcomeFailure
		default:
			outcome = audit.OutcomeFailure
		}

		// ============================================================
		// STEP 6: Catat audit event (termasuk yang gagal/denied)
		// ============================================================
		metadata := map[string]string{
			"method":     method,
			"path":       path,
			"status":     strconv.Itoa(status),
			"ip":         c.IP(),
			"user_agent": string(c.Request().Header.Peek("User-Agent")),
		}

		action := method + " " + path
		resource := path

		if saveErr := audit.RecordEvent(
			auditStore,
			tenantID,
			userID,
			action,
			resource,
			outcome,
			metadata,
		); saveErr != nil {
			log.Printf("WARN: audit logging failed for tenant %s: %v", tenantID, saveErr)
		}

		return err
	}
}