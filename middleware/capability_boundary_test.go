// middleware/capability_boundary_test.go
package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/auth"
	"trailseal/core/capability"
	"trailseal/rbac"
)

// newTestAppWithFullChain membangun app dengan chain lengkap:
// (inject claims palsu, simulasi RequireAuth yang sudah lolos)
//   -> RequireTenantScope("id")
//   -> RequireCapability(engine, roleResolver, template)
//   -> handler spy
//
// Ini persis urutan yang dipasang di cmd/saas-api/main.go untuk
// POST /api/tenants/:id/users.
func newTestAppWithFullChain(
	claims *auth.Claims,
	roleResolver *rbac.RoleResolver,
	engine *capability.Engine,
	capabilityTemplate string,
	handlerCalled *bool,
) *fiber.App {
	app := fiber.New()
	app.Post(
		"/api/tenants/:id/users",
		func(c *fiber.Ctx) error {
			c.Locals(localsClaimsKey, claims)
			return c.Next()
		},
		RequireTenantScope("id"),
		RequireCapability(engine, roleResolver, capabilityTemplate),
		func(c *fiber.Ctx) error {
			*handlerCalled = true
			return c.SendStatus(fiber.StatusOK)
		},
	)
	return app
}

// TEST 4: JWT tenant-A mengakses /api/tenants/tenant-A/... (tenant
// boundary COCOK, lolos RequireTenantScope), TAPI role-nya tidak
// punya capability yang diminta -> 403 dari RequireCapability, BUKAN
// dari RequireTenantScope. Membuktikan dua layer ini independen:
// tenant boundary lolos tidak berarti otorisasi otomatis lolos juga.
func TestRequireCapability_TenantMatchesButCapabilityDenied(t *testing.T) {
	store := rbac.NewInMemoryRoleStore()
	// Role "limited" di tenant-A SENGAJA hanya diberi grant yang tidak
	// mencakup "tenant.tenant-A.users.create".
	if err := store.PutRole(&rbac.Role{
		ID:               "limited",
		TenantID:         "tenant-A",
		Name:             "limited",
		CapabilityGrants: []string{"tenant.tenant-A.reports.read"},
	}); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}
	roleResolver := rbac.NewRoleResolver(store)
	engine := capability.NewEngine()

	claims := &auth.Claims{TenantID: "tenant-A", RoleID: "limited"}
	handlerCalled := false
	app := newTestAppWithFullChain(
		claims, roleResolver, engine, "tenant.{tenant_id}.users.create", &handlerCalled,
	)

	req := httptest.NewRequest(fiber.MethodPost, "/api/tenants/tenant-A/users", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called despite missing capability")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	// Buktikan penolakan ini datang dari LAYER CAPABILITY, bukan
	// tertukar dengan tenant_mismatch dari RequireTenantScope — dua
	// kegagalan mode yang harus tetap bisa dibedakan.
	if !strings.Contains(string(body), "capability_denied") {
		t.Fatalf("expected error reason capability_denied, got body: %s", body)
	}
	if strings.Contains(string(body), "tenant_mismatch") {
		t.Fatalf("tenant matched but response wrongly reports tenant_mismatch: %s", body)
	}
}

// Kontrol positif: tenant boundary COCOK dan capability DIMILIKI ->
// request harus benar-benar mencapai handler. Tanpa test ini, TEST 4
// di atas bisa lolos "kebetulan" kalau chain-nya salah pasang dan
// selalu menolak apa pun.
func TestRequireCapability_TenantMatchesAndCapabilityGranted_HandlerCalled(t *testing.T) {
	store := rbac.NewInMemoryRoleStore()
	if err := store.PutRole(&rbac.Role{
		ID:               "owner",
		TenantID:         "tenant-A",
		Name:             "owner",
		CapabilityGrants: []string{"tenant.tenant-A.*"},
	}); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}
	roleResolver := rbac.NewRoleResolver(store)
	engine := capability.NewEngine()

	claims := &auth.Claims{TenantID: "tenant-A", RoleID: "owner"}
	handlerCalled := false
	app := newTestAppWithFullChain(
		claims, roleResolver, engine, "tenant.{tenant_id}.users.create", &handlerCalled,
	)

	req := httptest.NewRequest(fiber.MethodPost, "/api/tenants/tenant-A/users", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called when tenant matches and capability is granted")
	}
}

// Kontrol tambahan: tenant TIDAK cocok, capability owner penuh
// (wildcard "tenant.tenant-A.*") sekalipun -> tetap ditolak DENGAN
// tenant_mismatch, dan RequireCapability TIDAK PERNAH dievaluasi
// (dibuktikan lewat roleResolver yang sengaja diberi store kosong
// untuk tenant-B -- kalau RequireCapability sempat jalan, dia akan
// gagal dengan cara berbeda, bukan tenant_mismatch).
func TestRequireCapability_TenantMismatch_NeverReachesCapabilityLayer(t *testing.T) {
	store := rbac.NewInMemoryRoleStore()
	// SENGAJA tidak ada role apa pun terdaftar untuk tenant-B --
	// kalau RequireCapability sempat dievaluasi untuk tenant-B, dia
	// akan gagal di ResolveCapabilities dengan role_resolution_failed,
	// bukan tenant_mismatch. Jadi error yang benar-benar muncul
	// membuktikan middleware mana yang sebenarnya menghentikan request.
	roleResolver := rbac.NewRoleResolver(store)
	engine := capability.NewEngine()

	claims := &auth.Claims{TenantID: "tenant-A", RoleID: "owner"}
	handlerCalled := false
	app := newTestAppWithFullChain(
		claims, roleResolver, engine, "tenant.{tenant_id}.users.create", &handlerCalled,
	)

	req := httptest.NewRequest(fiber.MethodPost, "/api/tenants/tenant-B/users", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called despite tenant mismatch")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !strings.Contains(string(body), "tenant_mismatch") {
		t.Fatalf("expected tenant_mismatch from RequireTenantScope, got: %s", body)
	}
}
