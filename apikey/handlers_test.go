package apikey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/project"
)

// ============================================================
// TEST INFRASTRUCTURE
// Reuse pool helper dari store_postgres_test.go (package sama).
// Handler butuh *project.PostgresStore konkret (bukan interface),
// jadi test ini pakai koneksi Postgres asli, bukan mock.
// ============================================================

func newHandlerTestApp(pool *pgxpool.Pool) (*fiber.App, *PostgresStore, *project.PostgresStore) {
	apiStore := NewPostgresStore(pool)
	projectStore := project.NewPostgresStore(pool)

	app := fiber.New()
	app.Post("/api/tenants/:id/projects/:project_id/keys", CreateAPIKeyHandler(apiStore, projectStore))
	app.Get("/api/tenants/:id/projects/:project_id/keys", ListAPIKeysHandler(apiStore))
	app.Post("/api/tenants/:id/projects/:project_id/keys/:key_id/revoke", RevokeAPIKeyHandler(apiStore))

	return app, apiStore, projectStore
}

// ============================================================
// CreateAPIKeyHandler
// ============================================================

func TestCreateAPIKeyHandler_Success(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "handler create project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	app, _, _ := newHandlerTestApp(pool)

	body := `{"name":"prod key","scope":"ingest"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-1/projects/"+projectID+"/keys",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var out struct {
		Key    string `json:"key"`
		APIKey struct {
			ID string `json:"id"`
		} `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.HasPrefix(out.Key, "tsk_") {
		t.Errorf("expected plaintext key with tsk_ prefix in response, got %q", out.Key)
	}
	if out.APIKey.ID == "" {
		t.Error("expected api_key.id to be populated")
	}
}

// CreateAPIKeyHandler_WrongTenant_ProjectNotFound adalah pengecekan
// isolasi tenant paling krusial di handler ini: tenant B tidak boleh
// bisa membuat key untuk project milik tenant A walau tahu project_id
// yang benar (persis skenario yang disebut di komentar handlers.go).
func TestCreateAPIKeyHandler_WrongTenant_ProjectNotFound(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-owner", "victim project for handler")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	app, _, _ := newHandlerTestApp(pool)

	body := `{"name":"attacker key","scope":"admin"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-attacker/projects/"+projectID+"/keys",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when project does not belong to tenant, got %d", resp.StatusCode)
	}
}

func TestCreateAPIKeyHandler_UnknownProject_Returns404(t *testing.T) {
	pool := apikeyTestPool(t)

	app, _, _ := newHandlerTestApp(pool)

	body := `{"name":"key","scope":"read"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-1/projects/00000000-0000-0000-0000-000000000000/keys",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent project, got %d", resp.StatusCode)
	}
}

func TestCreateAPIKeyHandler_InvalidScope_Returns400(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "invalid scope project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	app, _, _ := newHandlerTestApp(pool)

	body := `{"name":"key","scope":"superuser"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-1/projects/"+projectID+"/keys",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid scope, got %d", resp.StatusCode)
	}
}

func TestCreateAPIKeyHandler_InvalidBody_Returns400(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "invalid body project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	app, _, _ := newHandlerTestApp(pool)

	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-1/projects/"+projectID+"/keys",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d", resp.StatusCode)
	}
}

// ============================================================
// ListAPIKeysHandler
// ============================================================

func TestListAPIKeysHandler_ReturnsKeysForProject(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "list handler project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	apiStore := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "listed key", ScopeRead)
	if err := apiStore.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to seed key: %v", err)
	}

	app, _, _ := newHandlerTestApp(pool)

	req := httptest.NewRequest(http.MethodGet,
		"/api/tenants/tenant-1/projects/"+projectID+"/keys", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if out.Count != 1 {
		t.Errorf("expected count 1, got %d", out.Count)
	}
}

// ListAPIKeysHandler tidak melakukan validasi project-ownership sendiri
// (beda dengan CreateAPIKeyHandler) — ini mengandalkan ListByProject
// yang menyaring by tenant_id+project_id di level query. Test ini
// membuktikan isolasi itu tetap berlaku dari sisi handler.
func TestListAPIKeysHandler_WrongTenant_ReturnsEmptyNotOtherTenantsKeys(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-owner", "list isolation project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	apiStore := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-owner", projectID, "owner's key", ScopeRead)
	if err := apiStore.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to seed key: %v", err)
	}

	app, _, _ := newHandlerTestApp(pool)

	req := httptest.NewRequest(http.MethodGet,
		"/api/tenants/tenant-attacker/projects/"+projectID+"/keys", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Count != 0 {
		t.Errorf("expected 0 keys for wrong tenant, got %d — cross-tenant leak", out.Count)
	}
}

// ============================================================
// RevokeAPIKeyHandler
// ============================================================

func TestRevokeAPIKeyHandler_Success(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "revoke handler project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	apiStore := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "to revoke via handler", ScopeAdmin)
	if err := apiStore.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to seed key: %v", err)
	}

	app, _, _ := newHandlerTestApp(pool)

	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-1/projects/"+projectID+"/keys/"+generated.Record.ID+"/revoke", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRevokeAPIKeyHandler_UnknownKey_Returns404(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "revoke 404 project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	app, _, _ := newHandlerTestApp(pool)

	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-1/projects/"+projectID+"/keys/00000000-0000-0000-0000-000000000000/revoke", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// RevokeAPIKeyHandler_WrongTenant_Returns404NotSuccess adalah proteksi
// paling penting di handler ini: tenant lain tidak boleh bisa
// me-revoke key tenant lain lewat endpoint dashboard walau tahu key_id.
func TestRevokeAPIKeyHandler_WrongTenant_Returns404(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-victim", "revoke handler victim project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	apiStore := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-victim", projectID, "victim's key via handler", ScopeAdmin)
	if err := apiStore.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to seed key: %v", err)
	}

	app, _, _ := newHandlerTestApp(pool)

	req := httptest.NewRequest(http.MethodPost,
		"/api/tenants/tenant-attacker/projects/"+projectID+"/keys/"+generated.Record.ID+"/revoke", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant revoke attempt, got %d", resp.StatusCode)
	}

	// Pastikan key masih aktif.
	found, findErr := apiStore.FindByPlaintext(ctx, generated.Plaintext)
	if findErr != nil {
		t.Fatalf("expected key to still be active: %v", findErr)
	}
	if found.RevokedAt != nil {
		t.Error("key must not be revoked by a cross-tenant request via handler")
	}
}
