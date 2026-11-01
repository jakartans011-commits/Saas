package project

import (
	"errors"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// PLAN RESOLVER
// ============================================================

// PlanResolver diisi oleh app.go.
//
// Package project tidak mengetahui billing package secara langsung,
// sehingga tidak terjadi circular import.
//
// Return (0, 0) berarti gunakan default:
// tenant belum checkout / masih Free / plan tidak dikenali.
type PlanResolver func(tenantID string) (monthlyEventLimit int64, retentionDays int)

// ============================================================
// HANDLERS — JWT-authed (dashboard side), BUKAN API key.
//
// Ini yang dipakai owner/admin tenant untuk membuat Project baru.
// ============================================================

func CreateProjectHandler(
	store *PostgresStore,
	resolvePlan PlanResolver,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		var req struct {
			Name string `json:"name"`
		}

		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		p, err := NewProject(tenantID, req.Name)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// ========================================================
		// RESOLVE PLAN SAAT PROJECT DIBUAT
		//
		// Seluruh entitlement plan harus dimasukkan ke object
		// Project SEBELUM INSERT ke database.
		//
		// Contoh Pro:
		//   MonthlyEventLimit = 250000
		//   RetentionDays     = 180
		//
		// Dengan cara ini:
		//   - database menerima nilai final
		//   - response API juga menggunakan nilai final yang sama
		//   - tidak perlu INSERT lalu UPDATE retention_days
		// ========================================================

		if resolvePlan != nil {
			limit, days := resolvePlan(tenantID)

			if limit > 0 {
				p.MonthlyEventLimit = limit
			}

			if days > 0 {
				p.RetentionDays = days
			}
		}

		// ========================================================
		// CREATE SEKALI DENGAN NILAI PLAN YANG SUDAH FINAL
		// ========================================================

		if err := store.Create(c.Context(), p); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "project_creation_failed",
			})
		}

		// p sekarang merepresentasikan nilai yang sama dengan
		// yang disimpan ke database.
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"project": p,
		})
	}
}

func ListProjectsHandler(store *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		projects, err := store.ListByTenant(c.Context(), tenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "list_projects_failed",
			})
		}

		return c.JSON(fiber.Map{
			"projects": projects,
			"count":    len(projects),
		})
	}
}

func GetProjectHandler(store *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		projectID := c.Params("project_id")

		p, err := store.Get(c.Context(), tenantID, projectID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "project_not_found",
				})
			}

			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "get_project_failed",
			})
		}

		return c.JSON(fiber.Map{
			"project": p,
		})
	}
}

func DeleteProjectHandler(store *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		projectID := c.Params("project_id")

		if err := store.SoftDelete(
			c.Context(),
			tenantID,
			projectID,
		); err != nil {
			if errors.Is(err, ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "project_not_found",
				})
			}

			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "delete_project_failed",
			})
		}

		return c.JSON(fiber.Map{
			"status": "deleted",
		})
	}
}
