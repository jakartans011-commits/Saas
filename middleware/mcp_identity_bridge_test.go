package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
)

// TestInjectMCPIdentityFromAPIKey_StripsRoleAndTierHeaders memverifikasi
// fix privilege-escalation (2026-09): client API key yang menyisipkan
// X-Role-ID/X-Plan-Tier di request mentahnya sendiri TIDAK BOLEH lolos
// ke mcp.Server — kedua header itu harus di-strip oleh middleware ini,
// bukan diteruskan apa adanya. Tanpa fix ini, pemegang API key scope
// "read" bisa selipkan X-Role-ID: owner + X-Plan-Tier: business dan
// lolos gate Owner-only+Business-only di mcp.Server.handlePromptsCreate.
func TestInjectMCPIdentityFromAPIKey_StripsRoleAndTierHeaders(t *testing.T) {
	app := fiber.New()

	// Simulasikan posisi RequireAPIKey di chain app.go asli: taruh
	// *apikey.APIKey di Locals SEBELUM InjectMCPIdentityFromAPIKey
	// jalan, pakai key context yang sama persis (apiKeyContextKey,
	// unexported, tapi test ini satu package jadi bisa akses).
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(apiKeyContextKey, &apikey.APIKey{
			ID:       "key-123",
			TenantID: "tenant-a",
		})
		return c.Next()
	})

	app.Use(InjectMCPIdentityFromAPIKey())

	var gotRoleID, gotPlanTier, gotTenantID, gotUserID string
	app.Get("/probe", func(c *fiber.Ctx) error {
		gotRoleID = c.Get("X-Role-ID")
		gotPlanTier = c.Get("X-Plan-Tier")
		gotTenantID = c.Get("X-Tenant-ID")
		gotUserID = c.Get("X-User-ID")
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/probe", nil)
	// Client (pemegang API key) mencoba menyelipkan identitas elevated
	// di request mentahnya sendiri — HARUS di-strip oleh middleware.
	req.Header.Set("X-Role-ID", "owner")
	req.Header.Set("X-Plan-Tier", "business")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if gotRoleID != "" {
		t.Fatalf("X-Role-ID should be stripped for API key callers, got %q", gotRoleID)
	}
	if gotPlanTier != "" {
		t.Fatalf("X-Plan-Tier should be stripped for API key callers, got %q", gotPlanTier)
	}
	if gotTenantID != "tenant-a" {
		t.Fatalf("X-Tenant-ID should come from key.TenantID, got %q", gotTenantID)
	}
	if gotUserID != "apikey:key-123" {
		t.Fatalf("X-User-ID should be synthesized from key.ID, got %q", gotUserID)
	}
}