package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
)

type gateFakeStore struct{}

func (gateFakeStore) FindByPlaintext(_ context.Context, _ string) (*apikey.APIKey, error) {
	return &apikey.APIKey{ID: "k1", TenantID: "t1"}, nil
}
func (gateFakeStore) UpdateLastUsed(_ context.Context, _ string) error { return nil }

func gateStatus(t *testing.T, gates ...APIKeyGate) int {
	t.Helper()
	app := fiber.New()
	app.Get("/x", RequireAPIKey(gateFakeStore{}, gates...), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer tsk_test")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestRequireAPIKey_NoGate(t *testing.T) {
	if got := gateStatus(t); got != 200 {
		t.Fatalf("want 200, got %d", got)
	}
}

func TestRequireAPIKey_GateAllows(t *testing.T) {
	g := func(context.Context, *apikey.APIKey) (bool, error) { return true, nil }
	if got := gateStatus(t, g); got != 200 {
		t.Fatalf("want 200, got %d", got)
	}
}

func TestRequireAPIKey_GateSuspends(t *testing.T) {
	g := func(context.Context, *apikey.APIKey) (bool, error) { return false, nil }
	if got := gateStatus(t, g); got != 403 {
		t.Fatalf("want 403, got %d", got)
	}
}

func TestRequireAPIKey_GateError(t *testing.T) {
	g := func(context.Context, *apikey.APIKey) (bool, error) { return false, errors.New("db down") }
	if got := gateStatus(t, g); got != 503 {
		t.Fatalf("want 503, got %d", got)
	}
}
