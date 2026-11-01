// project/handlers_test.go
package project

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func TestCreateProjectHandler(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("success_without_plan_resolver", func(t *testing.T) {
		app := fiber.New()
		app.Post("/tenants/:id/projects", CreateProjectHandler(store, nil))

		rows := pgxmock.NewRows([]string{"id", "created_at"}).AddRow("proj-1", time.Now())
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO projects")).
			WithArgs("tenant-1", "My Project", ProjectStatusActive, int64(DefaultMonthlyEventLimit), DefaultRetentionDays).
			WillReturnRows(rows)

		req := newJSONRequest("POST", "/tenants/tenant-1/projects", `{"name":"My Project"}`)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("applies_resolved_plan", func(t *testing.T) {
		resolvePlan := func(tenantID string) (int64, int) { return 250000, 180 }
		app := fiber.New()
		app.Post("/tenants/:id/projects", CreateProjectHandler(store, resolvePlan))

		rows := pgxmock.NewRows([]string{"id", "created_at"}).AddRow("proj-2", time.Now())
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO projects")).
			WithArgs("tenant-1", "Pro Project", ProjectStatusActive, int64(250000), 180).
			WillReturnRows(rows)

		req := newJSONRequest("POST", "/tenants/tenant-1/projects", `{"name":"Pro Project"}`)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("empty_name_returns_400", func(t *testing.T) {
		app := fiber.New()
		app.Post("/tenants/:id/projects", CreateProjectHandler(store, nil))

		req := newJSONRequest("POST", "/tenants/tenant-1/projects", `{"name":""}`)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid_body_returns_400", func(t *testing.T) {
		app := fiber.New()
		app.Post("/tenants/:id/projects", CreateProjectHandler(store, nil))

		req := newJSONRequest("POST", "/tenants/tenant-1/projects", `not-json`)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestListProjectsHandler(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	app := fiber.New()
	app.Get("/tenants/:id/projects", ListProjectsHandler(store))

	t.Run("success", func(t *testing.T) {
		now := time.Now()
		rows := pgxmock.NewRows([]string{
			"id", "tenant_id", "name", "status",
			"monthly_event_limit", "retention_days", "created_at", "deleted_at",
		}).AddRow("proj-1", "tenant-1", "P1", ProjectStatusActive, int64(10000), 90, now, nil)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
			WithArgs("tenant-1").
			WillReturnRows(rows)

		req := newJSONRequest("GET", "/tenants/tenant-1/projects", "")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestGetProjectHandler(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	app := fiber.New()
	app.Get("/tenants/:id/projects/:project_id", GetProjectHandler(store))

	t.Run("found", func(t *testing.T) {
		now := time.Now()
		rows := pgxmock.NewRows([]string{
			"id", "tenant_id", "name", "status",
			"monthly_event_limit", "retention_days", "created_at", "deleted_at",
		}).AddRow("proj-1", "tenant-1", "P1", ProjectStatusActive, int64(10000), 90, now, nil)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
			WithArgs("proj-1", "tenant-1").
			WillReturnRows(rows)

		req := newJSONRequest("GET", "/tenants/tenant-1/projects/proj-1", "")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
			WithArgs("proj-missing", "tenant-1").
			WillReturnError(pgx.ErrNoRows)

		req := newJSONRequest("GET", "/tenants/tenant-1/projects/proj-missing", "")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestDeleteProjectHandler(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	app := fiber.New()
	app.Delete("/tenants/:id/projects/:project_id", DeleteProjectHandler(store))

	t.Run("success", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("proj-1", "tenant-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		req := newJSONRequest("DELETE", "/tenants/tenant-1/projects/proj-1", "")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("proj-missing", "tenant-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		req := newJSONRequest("DELETE", "/tenants/tenant-1/projects/proj-missing", "")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// ---- helpers ----

func newJSONRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
