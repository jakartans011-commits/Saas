package main

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
	"trailseal/middleware"
	"trailseal/tenant"
)

// ============================================================
// TENANT AUDIT ENDPOINT
// GET /api/tenants/:id/audit
//
// Security: RequireAuth → RequireTenantScope → RequireCapability("tenant.{tenant_id}.audit.read")
// ============================================================

func TenantAuditHandler(auditStore audit.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant id is required",
			})
		}

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "access denied: tenant mismatch",
			})
		}

		limit := 50
		if limitStr := c.Query("limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil {
				switch {
				case parsed <= 0:
					limit = 50
				case parsed > 200:
					limit = 200
				default:
					limit = parsed
				}
			}
		}

		events, err := auditStore.ListForTenant(tenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve audit data",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"limit":     limit,
			"events":    events,
			"count":     len(events),
		})
	}
}

// ExportAuditHandler — GET /api/tenants/:id/audit/export
// Export audit log as JSON (GDPR right to access)
func ExportAuditHandler(auditStore audit.Store, capResolver ...func(tenantID string) int) fiber.Handler {
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

		// Parse limit (default 1000 for export)
		limit := 1000
		if limitStr := c.Query("limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil {
				if parsed > 0 && parsed <= 10000 {
					limit = parsed
				}
			}
		}

		if len(capResolver) > 0 && capResolver[0] != nil {
			if exportCap := capResolver[0](tenantID); exportCap > 0 {
				if limit > exportCap {
					limit = exportCap
				}
				c.Set("X-Export-Capped", "true")
				c.Set("X-Export-Plan-Cap", strconv.Itoa(exportCap))
			}
		}

		events, err := auditStore.ListForTenant(tenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to export audit data",
			})
		}

		// Build export response
		exportData := fiber.Map{
			"tenant_id":    tenantID,
			"exported_at":  time.Now().Format(time.RFC3339),
			"total_events": len(events),
			"events":       events,
		}

		// Set response headers for download
		c.Set("Content-Type", "application/json")
		c.Set("Content-Disposition", "attachment; filename=audit_export_"+tenantID+"_"+time.Now().Format("20060102")+".json")

		return c.JSON(exportData)
	}
}

// ============================================================
// ADMIN AUDIT ENDPOINT
// GET /api/admin/tenants/:id/audit
//
// Security: RequireRoot SAJA
// ============================================================

func AdminAuditHandler(
	auditStore audit.Store,
	tenantStore *tenant.BoltTenantStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant id is required",
			})
		}

		if _, err := tenantStore.Get(tenantID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}

		limit := 50
		if limitStr := c.Query("limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil {
				switch {
				case parsed <= 0:
					limit = 50
				case parsed > 200:
					limit = 200
				default:
					limit = parsed
				}
			}
		}

		events, err := auditStore.ListForTenant(tenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve audit data",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"limit":     limit,
			"events":    events,
			"count":     len(events),
		})
	}
}

// AdminPlatformAuditHandler menampilkan event audit tanpa tenant
// (tenant khusus _platform). Hanya untuk root admin.
func AdminPlatformAuditHandler(auditStore audit.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		limit := 50
		if limitStr := c.Query("limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil {
				switch {
				case parsed <= 0:
					limit = 50
				case parsed > 200:
					limit = 200
				default:
					limit = parsed
				}
			}
		}

		events, err := auditStore.ListForTenant(audit.PlatformTenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve audit data",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id": audit.PlatformTenantID,
			"limit":     limit,
			"events":    events,
			"count":     len(events),
		})
	}
}
