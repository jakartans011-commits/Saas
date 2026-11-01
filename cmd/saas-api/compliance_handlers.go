package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	"trailseal/compliance"
	"trailseal/middleware"
	"trailseal/project"
	"trailseal/tenant"
)

// ============================================================
// COMPLIANCE HANDLERS — GDPR Deletion
// ============================================================

// gracePeriodDaysFor mengembalikan jumlah hari grace period sesuai plan
// tier, untuk ditampilkan di pesan response. Sumber kebenaran periode aktual
// tetap compliance.PurgeTenant (dipanggil dengan config=nil), ini hanya
// untuk teks pesan supaya tidak hardcode "30 days" untuk semua tier.
func gracePeriodDaysFor(planTier string) int {
	if planTier == "business" {
		return int(compliance.BusinessGracePeriod / (24 * time.Hour))
	}
	return int(compliance.DefaultGracePeriod / (24 * time.Hour))
}

// SoftDeleteTenantHandler — POST /api/tenants/:id/data/delete
func SoftDeleteTenantHandler(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		// Check if tenant exists (not soft-deleted)
		t, err := tenantStore.Get(tenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found or already soft-deleted",
			})
		}

		if err := compliance.SoftDeleteTenant(
			tenantStore,
			userStore,
			agentStore,
			tenantID,
		); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		days := gracePeriodDaysFor(t.PlanTier)

		return c.JSON(fiber.Map{
			"status":     "soft_deleted",
			"tenant_id":  tenantID,
			"deleted_at": time.Now().Format(time.RFC3339),
			"message": fmt.Sprintf(
				"Tenant soft-deleted. Data will be permanently deleted after %d days.",
				days,
			),
		})
	}
}

// RestoreTenantHandler — POST /api/tenants/:id/data/restore
func RestoreTenantHandler(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		if err := compliance.RestoreTenant(
			tenantStore,
			userStore,
			agentStore,
			tenantID,
		); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"status":    "restored",
			"tenant_id": tenantID,
			"message":   "Tenant restored successfully.",
		})
	}
}

// PurgeTenantHandler — POST /api/tenants/:id/data/purge
func PurgeTenantHandler(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
	projectStore *project.PostgresStore,
	apiKeyStore *apikey.PostgresStore,
	auditStore audit.Store,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		// FIX: config=nil supaya PurgeTenant pakai gracePeriodFor(t.PlanTier)
		// (30 hari Free/Pro, 60 hari Business). Sebelumnya selalu kirim
		// DefaultDeletionConfig() non-nil, jadi cabang tier-aware di
		// compliance.PurgeTenant tidak pernah tereksekusi — semua tenant,
		// termasuk Business, kena 30 hari.
		//
		// FIX (GDPR gap): sekarang juga hard-delete projects/API keys/audit
		// events tenant tsb — sebelumnya orphan permanen di store masing-
		// masing walau pesan sudah bilang "permanently deleted".
		if err := compliance.PurgeTenant(
			c.Context(),
			tenantStore,
			userStore,
			agentStore,
			projectStore,
			apiKeyStore,
			auditStore,
			tenantID,
			nil,
		); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"status":    "purged",
			"tenant_id": tenantID,
			"message":   "Tenant permanently deleted.",
		})
	}
}

// TenantDeletionStatusHandler — GET /api/tenants/:id/data/status
// Dipakai frontend untuk tahu apakah tenant sedang soft-deleted (tampilkan
// tombol Restore) atau aktif (tampilkan tombol Delete), tanpa terhalang
// gerbang session/layout biasa yang menolak user soft-deleted (404).
// Middleware harus RequireAuthAllowDeletedUser (sama seperti restore/purge),
// BUKAN requireAuth biasa — kalau tidak, Owner yang baru soft-delete
// tenantnya sendiri tidak akan pernah bisa cek status ini untuk restore.
func TenantDeletionStatusHandler(
	tenantStore *tenant.BoltTenantStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		t, err := tenantStore.GetIncludingDeleted(tenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}

		resp := fiber.Map{
			"tenant_id":         tenantID,
			"deleted":           t.DeletedAt != nil,
			"plan_tier":         t.PlanTier,
			"grace_period_days": gracePeriodDaysFor(t.PlanTier),
			"deleted_at":        nil,
			"purge_eligible_at": nil,
		}

		if t.DeletedAt != nil {
			resp["deleted_at"] = t.DeletedAt.Format(time.RFC3339)
			days := gracePeriodDaysFor(t.PlanTier)
			purgeAt := t.DeletedAt.Add(time.Duration(days) * 24 * time.Hour)
			resp["purge_eligible_at"] = purgeAt.Format(time.RFC3339)
		}

		return c.JSON(resp)
	}
}

// ============================================================
// ADMIN COMPLIANCE HANDLERS
// ============================================================

// AdminSoftDeleteTenantHandler — POST /api/admin/tenants/:id/data/delete
func AdminSoftDeleteTenantHandler(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		t, err := tenantStore.Get(tenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}

		if err := compliance.SoftDeleteTenant(
			tenantStore,
			userStore,
			agentStore,
			tenantID,
		); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		days := gracePeriodDaysFor(t.PlanTier)

		return c.JSON(fiber.Map{
			"status":     "soft_deleted",
			"tenant_id":  tenantID,
			"deleted_at": time.Now().Format(time.RFC3339),
			"message": fmt.Sprintf(
				"Tenant soft-deleted by admin. Data will be permanently deleted after %d days.",
				days,
			),
		})
	}
}

// AdminPurgeTenantHandler — POST /api/admin/tenants/:id/data/purge
func AdminPurgeTenantHandler(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
	projectStore *project.PostgresStore,
	apiKeyStore *apikey.PostgresStore,
	auditStore audit.Store,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		// FIX: config=nil, sama seperti PurgeTenantHandler di atas —
		// hormati tier-aware grace period (30/60 hari), jangan selalu 30.
		//
		// FIX (GDPR gap): sekarang juga hard-delete projects/API keys/audit
		// events tenant tsb.
		if err := compliance.PurgeTenant(
			c.Context(),
			tenantStore,
			userStore,
			agentStore,
			projectStore,
			apiKeyStore,
			auditStore,
			tenantID,
			nil,
		); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"status":    "purged",
			"tenant_id": tenantID,
			"message":   "Tenant permanently deleted by admin.",
		})
	}
}
