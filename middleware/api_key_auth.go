package middleware

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
)

// API KEY AUTH: jalur otentikasi kedua, terpisah dari RequireAuth (JWT).
// Dipakai untuk endpoint /v1/events/* dan MCP yang diakses programmatic
// dari server customer, bukan dari browser dashboard.
//
// Header: Authorization: Bearer tsk_<random>

type apiKeyContextKeyType struct{}

var apiKeyContextKey = apiKeyContextKeyType{}

// APIKeyStore: interface minimal supaya middleware ini tidak bergantung
// langsung ke *apikey.PostgresStore (mudah di-mock di unit test).
type APIKeyStore interface {
	FindByPlaintext(ctx context.Context, plaintext string) (*apikey.APIKey, error)
	UpdateLastUsed(ctx context.Context, keyID string) error
}

// APIKeyGate adalah pemeriksaan tambahan setelah key valid dan belum
// di-revoke (misalnya batas key aktif per plan). allowed=false menolak
// request dengan 403 api_key_suspended_plan; err != nil menolak dengan 503.
type APIKeyGate func(ctx context.Context, key *apikey.APIKey) (allowed bool, err error)

// RequireAPIKey memvalidasi Bearer token sebagai API key (bukan JWT).
// Kalau valid, key record disisipkan ke fiber locals dan bisa diambil lewat
// APIKeyFromFiberCtx di handler. gates opsional dijalankan berurutan.
func RequireAPIKey(store APIKeyStore, gates ...APIKeyGate) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing_api_key",
			})
		}

		plaintext := strings.TrimPrefix(header, "Bearer ")
		if plaintext == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing_api_key",
			})
		}

		key, err := store.FindByPlaintext(c.Context(), plaintext)
		if err != nil {
			// Sengaja tidak membedakan "not found" vs "revoked" ke client,
			// mencegah enumerasi key valid.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_api_key",
			})
		}

		if key.RevokedAt != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid_api_key",
			})
		}

		for _, gate := range gates {
			if gate == nil {
				continue
			}
			allowed, gerr := gate(c.Context(), key)
			if gerr != nil {
				return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
					"error": "api_key_check_failed",
				})
			}
			if !allowed {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "api_key_suspended_plan",
					"message": "This API key is suspended because your plan allows fewer active keys. Upgrade your plan to restore it.",
				})
			}
		}

		c.Locals(apiKeyContextKey, key)

		// Fire-and-forget, tidak menahan response kalau update
		// last_used_at gagal/lambat.
		go func() {
			_ = store.UpdateLastUsed(context.Background(), key.ID)
		}()

		return c.Next()
	}
}

// APIKeyFromFiberCtx mengambil API key record yang sudah divalidasi
// RequireAPIKey. Handler wajib memanggil ini, jangan percaya project_id
// dari URL param: key.ProjectID adalah sumber kebenaran otorisasi.
func APIKeyFromFiberCtx(c *fiber.Ctx) (*apikey.APIKey, bool) {
	key, ok := c.Locals(apiKeyContextKey).(*apikey.APIKey)
	return key, ok
}

// RequireScope memastikan API key yang sudah diautentikasi punya scope
// yang cukup. Dipasang setelah RequireAPIKey di chain middleware.
func RequireScope(need func(apikey.Scope) bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}
		if !need(key.Scope) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "insufficient_scope",
			})
		}
		return c.Next()
	}
}
