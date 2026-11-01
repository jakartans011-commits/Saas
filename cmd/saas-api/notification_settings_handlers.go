// cmd/saas-api/notification_settings_handlers.go
package main

import (
	"regexp"

	"github.com/gofiber/fiber/v2"

	"trailseal/tenant"
)

// Sederhana, cukup untuk validasi input form — bukan RFC 5322 penuh.
var simpleEmailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// GetNotificationSettingsHandler — GET /api/tenants/:id/notification-settings
func GetNotificationSettingsHandler(tenantStore *tenant.BoltTenantStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
		}

		t, err := tenantStore.Get(tenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant not found"})
		}

		return c.JSON(fiber.Map{
			"tenant_id":          tenantID,
			"notification_email": t.NotificationEmail, // kosong string kalau belum diset — frontend fallback ke "menggunakan email Owner"
		})
	}
}

// UpdateNotificationSettingsHandler — PUT /api/tenants/:id/notification-settings
func UpdateNotificationSettingsHandler(tenantStore *tenant.BoltTenantStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id_required"})
		}

		var req struct {
			NotificationEmail string `json:"notification_email"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_body"})
		}

		// Kosong = valid, artinya "pakai default (email Owner)".
		if req.NotificationEmail != "" && !simpleEmailRe.MatchString(req.NotificationEmail) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_email_format"})
		}

		t, err := tenantStore.Get(tenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant not found"})
		}

		t.NotificationEmail = req.NotificationEmail
		if err := tenantStore.Put(t); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save"})
		}

		return c.JSON(fiber.Map{
			"status":             "updated",
			"tenant_id":          tenantID,
			"notification_email": t.NotificationEmail,
		})
	}
}
