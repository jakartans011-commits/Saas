// middleware/tenant_scope_test.go
package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/auth"
)

// newTestAppWithTenantScope membangun app Fiber minimal: inject claims
// palsu (mensimulasikan RequireAuth yang sudah lolos), lalu pasang
// RequireTenantScope(paramName), lalu handler spy yang menandai dirinya
// terpanggil. Test memverifikasi status code DAN flag handlerCalled —
// bukan cuma status code saja (STEP 8).
func newTestAppWithTenantScope(claims *auth.Claims, paramName string, handlerCalled *bool) *fiber.App {
	app := fiber.New()
	app.Post(
		"/api/tenants/:"+paramName+"/users",
		func(c *fiber.Ctx) error {
			// Simulasi RequireAuth: taruh claims yang sudah "tervalidasi"
			// ke locals dengan key yang SAMA dengan yang dipakai
			// RequireAuth asli (localsClaimsKey, package-private, jadi
			// test ini valid hanya karena berada di package middleware).
			c.Locals(localsClaimsKey, claims)
			return c.Next()
		},
		RequireTenantScope(paramName),
		func(c *fiber.Ctx) error {
			*handlerCalled = true
			return c.SendStatus(fiber.StatusOK)
		},
	)
	return app
}

// TEST 1: JWT tenant-A mengakses /api/tenants/tenant-A/... -> ALLOW.
func TestRequireTenantScope_SameTenant_Allowed(t *testing.T) {
	handlerCalled := false
	claims := &auth.Claims{TenantID: "tenant-A"}
	app := newTestAppWithTenantScope(claims, "id", &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/tenants/tenant-A/users", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called for matching tenant, but it was not")
	}
}

// TEST 2: JWT tenant-A mengakses /api/tenants/tenant-B/... -> DENY 403,
// error tenant_mismatch, handler TIDAK dipanggil.
func TestRequireTenantScope_CrossTenant_A_to_B_Denied(t *testing.T) {
	handlerCalled := false
	claims := &auth.Claims{TenantID: "tenant-A"}
	app := newTestAppWithTenantScope(claims, "id", &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/tenants/tenant-B/users", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called despite tenant mismatch (tenant-A -> tenant-B)")
	}
}

// TEST 3: JWT tenant-B mengakses /api/tenants/tenant-A/... -> DENY 403.
// Membuktikan boundary simetris, bukan cuma satu arah.
func TestRequireTenantScope_CrossTenant_B_to_A_Denied(t *testing.T) {
	handlerCalled := false
	claims := &auth.Claims{TenantID: "tenant-B"}
	app := newTestAppWithTenantScope(claims, "id", &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/tenants/tenant-A/users", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called despite tenant mismatch (tenant-B -> tenant-A)")
	}
}