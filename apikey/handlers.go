package apikey

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"trailseal/project"
)

// ============================================================
// HANDLERS — JWT-authed (dashboard side).
//
// Setiap handler di sini WAJIB verifikasi project milik tenant
// yang sedang login (lewat project.Get, yang query-nya sudah
// menyaring by tenant_id) SEBELUM membuat/mengubah API key —
// mencegah user tenant A membuat key untuk project tenant B
// meski project_id-nya ditebak dengan benar.
// ============================================================
// KeyLimitResolver mengembalikan batas key aktif per tenant. <= 0 = tanpa batas.
type KeyLimitResolver func(tenantID string) int

func CreateAPIKeyHandler(apiStore *PostgresStore, projectStore *project.PostgresStore, limitResolver ...KeyLimitResolver) fiber.Handler {
	var resolveLimit KeyLimitResolver
	if len(limitResolver) > 0 {
		resolveLimit = limitResolver[0]
	}

	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		projectID := c.Params("project_id")

		if _, err := projectStore.Get(c.Context(), tenantID, projectID); err != nil {
			if errors.Is(err, project.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "project_not_found",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "project_lookup_failed",
			})
		}

		var req struct {
			Name  string `json:"name"`
			Scope string `json:"scope"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		generated, err := GenerateAPIKey(tenantID, projectID, req.Name, Scope(req.Scope))
		if err != nil {
			if errors.Is(err, ErrInvalidScope) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_scope",
					"valid_scopes": []string{
						string(ScopeIngest), string(ScopeRead), string(ScopeAdmin),
					},
				})
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		limit := 0
		if resolveLimit != nil {
			limit = resolveLimit(tenantID)
		}
		if err := apiStore.CreateWithLimit(c.Context(), generated.Record, limit); err != nil {
			if errors.Is(err, ErrKeyLimitReached) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "api_key_limit_reached",
					"limit": limit,
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "api_key_creation_failed",
			})
		}

		// Plaintext HANYA muncul di response ini, SATU KALI. Tidak
		// pernah bisa diambil lagi setelah ini (bahkan lewat endpoint
		// admin sekalipun) — kalau hilang, satu-satunya jalan adalah
		// revoke lalu buat key baru.
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"api_key": generated.Record,
			"key":     generated.Plaintext,
			"warning": "This is the only time the plaintext key will be shown. Store it securely now.",
		})
	}
}

func ListAPIKeysHandler(apiStore *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		projectID := c.Params("project_id")

		keys, err := apiStore.ListByProject(c.Context(), tenantID, projectID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "list_api_keys_failed",
			})
		}

		return c.JSON(fiber.Map{
			"api_keys": keys,
			"count":    len(keys),
		})
	}
}

func RevokeAPIKeyHandler(apiStore *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		keyID := c.Params("key_id")
		projectID := c.Params("project_id")

		if err := apiStore.Revoke(c.Context(), tenantID, projectID, keyID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "api_key_not_found",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "revoke_failed",
			})
		}

		return c.JSON(fiber.Map{
			"status": "revoked",
		})
	}
}
