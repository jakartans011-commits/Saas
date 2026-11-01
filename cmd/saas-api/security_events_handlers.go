package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
	"trailseal/middleware"
)

// ============================================================
// SECURITY EVENTS ENDPOINT
// GET /api/tenants/:id/security-events
//
// Subset dari audit.AuditEvent yang lolos audit.IsSecurityEvent.
// Response shape sama dengan TenantAuditHandler.
//
// Security: RequireAuth → RequireTenantScope → rateLimiter →
//           RequireCapability("tenant.{tenant_id}.security_events.read")
// ============================================================

// securityEventsScanWindow = berapa event audit TERBARU yang dipindai.
// Bolt dan Postgres sama-sama mengembalikan event terbaru dulu, jadi
// security event yang lebih lama dari jendela ini tidak ikut tampil.
const securityEventsScanWindow = 1000

func SecurityEventsHandler(auditStore audit.Store) fiber.Handler {
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

		all, err := auditStore.ListForTenant(tenantID, securityEventsScanWindow)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve security events",
			})
		}

		events := make([]*audit.AuditEvent, 0, limit)
		for _, e := range all {
			if !audit.IsSecurityEvent(e.Action) {
				continue
			}
			events = append(events, e)
			if len(events) >= limit {
				break
			}
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"limit":     limit,
			"events":    events,
			"count":     len(events),
		})
	}
}
