package middleware

import (
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"trailseal/usage"
)

// UsageMetering mencatat API request usage untuk tenant-scoped endpoints.
// HARUS dipasang SETELAH RequireAuth, RequireTenantScope, dan RequireCapability
// dalam chain middleware.
//
// Tenant ID diambil dari fiber.Locals (claims yang sudah tervalidasi oleh
// RequireAuth/RequireTenantScope), BUKAN dari c.Params("id").
//
// Endpoint yang TIDAK dicatat:
//   - /health
//   - /api/auth/*
//   - /api/admin/*
//   - GET /api/tenants/:id/usage (self-metering exclusion)
//
// Hanya request yang benar-benar berhasil (tidak ada error DAN status < 400)
// yang dicatat.
func UsageMetering(eventStore usage.EventStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Ambil claims dari context (sudah divalidasi oleh RequireAuth)
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		path := c.Path()
		method := c.Method()

		// ============================================================
		// STEP 1: Skip NON-TENANT endpoints
		// ============================================================
		// /health, /api/auth/*, /api/admin/*
		if path == "/health" ||
			strings.HasPrefix(path, "/api/auth/") ||
			strings.HasPrefix(path, "/api/admin/") {
			return c.Next()
		}

		// ============================================================
		// STEP 2: Hanya catat tenant-scoped endpoints di bawah /api/tenants/
		// ============================================================
		// "/api/tenants/" panjangnya 13 karakter
		if !strings.HasPrefix(path, "/api/tenants/") {
			return c.Next()
		}

		// ============================================================
		// STEP 3: SELF-METERING EXCLUSION
		// GET /api/tenants/:id/usage tidak dicatat
		// ============================================================
		if method == fiber.MethodGet && path == "/api/tenants/"+claims.TenantID+"/usage" {
			return c.Next()
		}

		// ============================================================
		// STEP 4: Lanjutkan ke handler terlebih dahulu
		// ============================================================
		err := c.Next()

		// ============================================================
		// STEP 5: Hanya catat request yang benar-benar berhasil.
		//
		// PENTING: err != nil harus dicek TERPISAH dari status code.
		// Fiber baru mengonversi error handler (mis. fiber.NewError)
		// menjadi HTTP status di error handler pusat, yang jalan
		// SETELAH middleware ini return — jadi c.Response().StatusCode()
		// di titik ini bisa masih default (200) meskipun response akhir
		// ke client adalah 4xx/5xx. Tanpa cek err != nil, request yang
		// gagal lewat pola "return err" akan salah tercatat sebagai sukses.
		// ============================================================
		status := c.Response().StatusCode()
		if err != nil || status >= 400 {
			return err
		}

		// ============================================================
		// STEP 6: Bangun metadata untuk event (non-sensitif)
		// ============================================================
		metadata := map[string]string{
			"method":     method,
			"path":       path,
			"status":     strconv.Itoa(status),
			"ip":         c.IP(),
			"user_agent": string(c.Request().Header.Peek("User-Agent")),
		}

		// ============================================================
		// STEP 7: Catat event menggunakan helper RecordEvent
		// ============================================================
		if saveErr := usage.RecordEvent(
			eventStore,
			claims.TenantID,
			usage.EventTypeAPIRequest,
			metadata,
		); saveErr != nil {
			log.Printf("WARN: usage metering failed for tenant %s: %v", claims.TenantID, saveErr)
		}

		return err
	}
}
