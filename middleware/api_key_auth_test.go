package middleware

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
)

// ============================================================
// MOCK API KEY STORE
// ============================================================

type mockAPIKeyStore struct {
	findFn   func(ctx context.Context, plaintext string) (*apikey.APIKey, error)
	updateFn func(ctx context.Context, keyID string) error
}

func (m *mockAPIKeyStore) FindByPlaintext(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
	if m.findFn != nil {
		return m.findFn(ctx, plaintext)
	}
	return nil, apikey.ErrNotFound
}

func (m *mockAPIKeyStore) UpdateLastUsed(ctx context.Context, keyID string) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, keyID)
	}
	return nil
}

// ============================================================
// TEST API KEY AUTH - SUCCESS
// ============================================================

func TestRequireAPIKey_Success(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			return &apikey.APIKey{
				ID:        "key-1",
				ProjectID: "proj-1",
				TenantID:  "tenant-1",
				Name:      "test-key",
				KeyHash:   "hash",
				KeyPrefix: "tsk_12345678",
				Scope:     apikey.ScopeIngest,
				RevokedAt: nil,
			}, nil
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		key, ok := APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "no key"})
		}
		return c.JSON(fiber.Map{"key_id": key.ID})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST API KEY AUTH - MISSING HEADER
// ============================================================

func TestRequireAPIKey_MissingHeader(t *testing.T) {
	app := fiber.New()
	mockStore := &mockAPIKeyStore{}

	app.Use(RequireAPIKey(mockStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST API KEY AUTH - INVALID KEY
// ============================================================

func TestRequireAPIKey_InvalidKey(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			return nil, apikey.ErrNotFound
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST API KEY AUTH - REVOKED KEY
// ============================================================

func TestRequireAPIKey_RevokedKey(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			revokedAt := time.Now()
			return &apikey.APIKey{
				ID:        "key-1",
				ProjectID: "proj-1",
				TenantID:  "tenant-1",
				Name:      "revoked-key",
				KeyHash:   "hash",
				KeyPrefix: "tsk_12345678",
				Scope:     apikey.ScopeIngest,
				RevokedAt: &revokedAt,
			}, nil
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer revoked-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST API KEY AUTH - EMPTY TOKEN
// ============================================================

func TestRequireAPIKey_EmptyToken(t *testing.T) {
	app := fiber.New()
	mockStore := &mockAPIKeyStore{}

	app.Use(RequireAPIKey(mockStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST REQUIRE SCOPE - SUCCESS (Ingest)
// ============================================================

func TestRequireScope_Ingest_Success(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			return &apikey.APIKey{
				ID:        "key-1",
				ProjectID: "proj-1",
				TenantID:  "tenant-1",
				Name:      "ingest-key",
				KeyHash:   "hash",
				KeyPrefix: "tsk_12345678",
				Scope:     apikey.ScopeIngest,
				RevokedAt: nil,
			}, nil
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Use(RequireScope(func(s apikey.Scope) bool { return s.CanIngest() }))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST REQUIRE SCOPE - ADMIN CAN INGEST
// ============================================================

func TestRequireScope_Admin_CanIngest(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			return &apikey.APIKey{
				ID:        "key-1",
				ProjectID: "proj-1",
				TenantID:  "tenant-1",
				Name:      "admin-key",
				KeyHash:   "hash",
				KeyPrefix: "tsk_12345678",
				Scope:     apikey.ScopeAdmin,
				RevokedAt: nil,
			}, nil
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Use(RequireScope(func(s apikey.Scope) bool { return s.CanIngest() }))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer admin-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST REQUIRE SCOPE - READ CANNOT INGEST
// ============================================================

func TestRequireScope_Read_CannotIngest(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			return &apikey.APIKey{
				ID:        "key-1",
				ProjectID: "proj-1",
				TenantID:  "tenant-1",
				Name:      "read-key",
				KeyHash:   "hash",
				KeyPrefix: "tsk_12345678",
				Scope:     apikey.ScopeRead,
				RevokedAt: nil,
			}, nil
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Use(RequireScope(func(s apikey.Scope) bool { return s.CanIngest() }))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer read-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 403 {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST API KEY FROM FIBER CTX - SUCCESS
// ============================================================

func TestAPIKeyFromFiberCtx_Success(t *testing.T) {
	app := fiber.New()

	mockStore := &mockAPIKeyStore{
		findFn: func(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
			return &apikey.APIKey{
				ID:        "key-1",
				ProjectID: "proj-1",
				TenantID:  "tenant-1",
				Name:      "test-key",
				KeyHash:   "hash",
				KeyPrefix: "tsk_12345678",
				Scope:     apikey.ScopeIngest,
				RevokedAt: nil,
			}, nil
		},
	}

	app.Use(RequireAPIKey(mockStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		key, ok := APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "no key"})
		}
		if key.ID != "key-1" {
			return c.Status(500).JSON(fiber.Map{"error": "wrong key"})
		}
		if key.ProjectID != "proj-1" {
			return c.Status(500).JSON(fiber.Map{"error": "wrong project"})
		}
		if key.TenantID != "tenant-1" {
			return c.Status(500).JSON(fiber.Map{"error": "wrong tenant"})
		}
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
