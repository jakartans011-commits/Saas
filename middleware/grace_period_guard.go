package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/tenant"
)

// RequirePaymentCurrent adalah pengecekan CEPAT (read-only, tanpa
// write) — dipasang SETELAH RequireAuth di route yang butuh fitur
// premium. Ini TIDAK melakukan downgrade; downgrade aktual (ubah
// PlanTier ke free) adalah tanggung jawab cron sweep di billing
// package. Middleware ini murni mencegah akses selama window antara
// deadline lewat dan cron berikutnya jalan.
func RequirePaymentCurrent(tenantStore tenant.TenantStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		t, err := tenantStore.Get(claims.TenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant_not_found"})
		}

		if t.SubscriptionStatus == "past_due" &&
			t.GracePeriodDeadline != nil &&
			time.Now().After(*t.GracePeriodDeadline) {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
				"error":                  "payment_required",
				"grace_period_ended":     t.GracePeriodDeadline,
				"payment_failure_reason": t.PaymentFailureReason,
			})
		}

		return c.Next()
	}
}
