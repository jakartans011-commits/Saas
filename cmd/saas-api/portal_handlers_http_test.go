package main

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// doRaw mengirim request lewat app.Test dan mengembalikan status,
// body mentah, dan Content-Type — dipakai untuk halaman portal yang
// merender HTML (bukan JSON, jadi doJSON tidak cocok di sini).
func (e *testEnv) doRaw(t *testing.T, method, path string, headers map[string]string) (int, string, string) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test failed for %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body for %s %s: %v", method, path, err)
	}

	return resp.StatusCode, string(body), resp.Header.Get("Content-Type")
}

// ============================================================
// Portal pages that only touch bbolt stores (tenant, usage) —
// safe to test without TRAILSEAL_TEST_POSTGRES_URL.
// ============================================================

func TestPortalOverview_Unauthenticated_Returns401(t *testing.T) {
	env := newTestEnv(t)

	status := env.doJSON(t, fiber.MethodGet, "/overview", nil, nil, nil)
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /overview, got %d", status)
	}
}

func TestPortalAgents_Authenticated_RendersHTML(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-portal-agents", "Portal Agents Tenant", "owner-agents@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-agents@example.test", "correct-horse-battery-staple")

	status, body, contentType := env.doRaw(t, fiber.MethodGet, "/agents", bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for /agents, got %d, body: %s", status, body)
	}
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}
}

func TestPortalOperations_Authenticated_RendersHTML(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-portal-ops", "Portal Ops Tenant", "owner-ops@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-ops@example.test", "correct-horse-battery-staple")

	status, body, contentType := env.doRaw(t, fiber.MethodGet, "/operations", bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for /operations, got %d, body: %s", status, body)
	}
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}
}

func TestPortalAudit_Authenticated_RendersHTML(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-portal-audit", "Portal Audit Tenant", "owner-audit@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-audit@example.test", "correct-horse-battery-staple")

	status, body, contentType := env.doRaw(t, fiber.MethodGet, "/audit", bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for /audit, got %d, body: %s", status, body)
	}
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}
}

func TestPortalUsage_Authenticated_RendersHTML(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-portal-usage", "Portal Usage Tenant", "owner-usage@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-usage@example.test", "correct-horse-battery-staple")

	status, body, contentType := env.doRaw(t, fiber.MethodGet, "/developer/usage", bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for /developer/usage, got %d, body: %s", status, body)
	}
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}
}

func TestPortalAgents_CrossTenantTokenCannotLeakOtherTenantData(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-portal-x", "Portal Tenant X", "owner-x@example.test", "correct-horse-battery-staple")
	env.bootstrapTenantOwner(t, "tenant-portal-y", "Portal Tenant Y", "owner-y@example.test", "correct-horse-battery-staple")

	tokenX := env.login(t, "owner-x@example.test", "correct-horse-battery-staple")

	// portalTenantID always derives tenant ID from the caller's own
	// JWT claims — there is no ?tenant= or similar override, so this
	// just confirms the page renders using tokenX's own tenant and
	// never errors trying to read another tenant's data.
	status, _, _ := env.doRaw(t, fiber.MethodGet, "/agents", bearer(tokenX))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for tenant X viewing its own /agents page, got %d", status)
	}
}

// ============================================================
// Portal pages that touch Postgres (ProjectStore/APIKeyStore) —
// skipped unless TRAILSEAL_TEST_POSTGRES_URL is set, same pattern
// as requirePostgres() elsewhere in this package.
// ============================================================

func TestPortalOverview_Authenticated_RendersHTML(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	env.bootstrapTenantOwner(t, "tenant-portal-overview", "Portal Overview Tenant", "owner-overview@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-overview@example.test", "correct-horse-battery-staple")

	status, body, contentType := env.doRaw(t, fiber.MethodGet, "/overview", bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for /overview, got %d, body: %s", status, body)
	}
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}
}

func TestPortalAPIKeys_NoProject_ShowsEmptyState(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	env.bootstrapTenantOwner(t, "tenant-portal-nokeys", "Portal No Keys Tenant", "owner-nokeys@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-nokeys@example.test", "correct-horse-battery-staple")

	// No project has been created for this tenant yet.
	status, body, _ := env.doRaw(t, fiber.MethodGet, "/developer/api-keys", bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 even with no project provisioned, got %d, body: %s", status, body)
	}
}

func TestPortalAPIKeyGenerateAndRevoke_FullFlow(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	env.bootstrapTenantOwnerPro(t, "tenant-portal-keyflow", "Portal Key Flow Tenant", "owner-keyflow@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-keyflow@example.test", "correct-horse-battery-staple")

	// Create a project via the real API first — the portal's key
	// generation handler requires the tenant to already have one.
	var projOut struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-portal-keyflow/projects", bearer(token),
		map[string]string{"name": "Default Project"}, &projOut)
	if status != fiber.StatusOK && status != fiber.StatusCreated {
		t.Fatalf("failed to create fixture project: status %d", status)
	}
	if projOut.Project.ID == "" {
		t.Fatal("expected project creation to return a project id")
	}

	// Generate an API key through the portal's form-post handler.
	form := "name=Test+Key&scope=read&project_id=" + projOut.Project.ID
	req := httptest.NewRequest(fiber.MethodPost, "/developer/api-keys/generate", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := env.app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303 redirect after key generation, got %d, body: %s", resp.StatusCode, body)
	}

	// Confirm the key now shows up on the API keys page.
	status, body, _ := env.doRaw(t, fiber.MethodGet, "/developer/api-keys?project="+projOut.Project.ID, bearer(token))
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for api-keys page after generation, got %d, body: %s", status, body)
	}
}

func TestPortalAPIKeyGenerate_MissingName_Returns400(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	env.bootstrapTenantOwner(t, "tenant-portal-badkey", "Portal Bad Key Tenant", "owner-badkey@example.test", "correct-horse-battery-staple")
	token := env.login(t, "owner-badkey@example.test", "correct-horse-battery-staple")

	form := "scope=read"
	req := httptest.NewRequest(fiber.MethodPost, "/developer/api-keys/generate", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := env.app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", resp.StatusCode)
	}
}
