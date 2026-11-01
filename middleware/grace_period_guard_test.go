package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"trailseal/tenant"
)

func buildAuthedRequest(t *testing.T, method, path, tenantID string) *http.Request {
	t.Helper()
	claims := validClaims("user-1", tenantID, "member")
	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestRequirePaymentCurrent_GracePeriodExpired_Blocks(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	past := time.Now().Add(-1 * time.Hour)
	_ = tenantStore.Put(&tenant.Tenant{
		ID: "tenant-1", Status: tenant.TenantStatusActive,
		SubscriptionStatus: "past_due", GracePeriodDeadline: &past,
	})

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("user-1", "tenant-1"), newMockRevocationStore()))
	app.Use(RequirePaymentCurrent(tenantStore))
	app.Get("/premium", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := buildAuthedRequest(t, "GET", "/premium", "tenant-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusPaymentRequired {
		t.Errorf("expected 402, got %d", resp.StatusCode)
	}
}

func TestRequirePaymentCurrent_StillInGracePeriod_Allows(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	future := time.Now().Add(48 * time.Hour)
	_ = tenantStore.Put(&tenant.Tenant{
		ID: "tenant-1", Status: tenant.TenantStatusActive,
		SubscriptionStatus: "past_due", GracePeriodDeadline: &future,
	})

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("user-1", "tenant-1"), newMockRevocationStore()))
	app.Use(RequirePaymentCurrent(tenantStore))
	app.Get("/premium", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := buildAuthedRequest(t, "GET", "/premium", "tenant-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 (still within grace period), got %d", resp.StatusCode)
	}
}

func TestRequirePaymentCurrent_ActiveSubscription_Allows(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	_ = tenantStore.Put(&tenant.Tenant{
		ID: "tenant-1", Status: tenant.TenantStatusActive,
		SubscriptionStatus: "active",
	})

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("user-1", "tenant-1"), newMockRevocationStore()))
	app.Use(RequirePaymentCurrent(tenantStore))
	app.Get("/premium", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := buildAuthedRequest(t, "GET", "/premium", "tenant-1")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for active subscription, got %d", resp.StatusCode)
	}
}
