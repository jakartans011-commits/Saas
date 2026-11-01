// project/store_postgres_test.go
package project

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func TestPostgresStore_Create(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("success_with_defaults", func(t *testing.T) {
		p := &Project{TenantID: "tenant-1", Name: "My Project"}

		rows := pgxmock.NewRows([]string{"id", "created_at"}).
			AddRow("proj-1", time.Now())

		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO projects")).
			WithArgs("tenant-1", "My Project", ProjectStatusActive, int64(DefaultMonthlyEventLimit), DefaultRetentionDays).
			WillReturnRows(rows)

		if err := store.Create(context.Background(), p); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if p.ID != "proj-1" {
			t.Fatalf("expected ID proj-1, got %s", p.ID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("empty_name_returns_error", func(t *testing.T) {
		p := &Project{TenantID: "tenant-1"}
		if err := store.Create(context.Background(), p); err != ErrNameRequired {
			t.Fatalf("expected ErrNameRequired, got %v", err)
		}
	})

	t.Run("empty_tenant_id_returns_error", func(t *testing.T) {
		p := &Project{Name: "X"}
		if err := store.Create(context.Background(), p); err == nil {
			t.Fatal("expected error for empty tenant_id")
		}
	})
}

func TestPostgresStore_SetRetentionDays(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("success", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("proj-1", 30).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		if err := store.SetRetentionDays(context.Background(), "proj-1", 30); err != nil {
			t.Fatalf("SetRetentionDays failed: %v", err)
		}
	})

	t.Run("not_found_when_zero_rows_affected", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("proj-missing", 30).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		if err := store.SetRetentionDays(context.Background(), "proj-missing", 30); err != ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("invalid_inputs_return_error", func(t *testing.T) {
		if err := store.SetRetentionDays(context.Background(), "", 30); err == nil {
			t.Fatal("expected error for empty project_id")
		}
		if err := store.SetRetentionDays(context.Background(), "proj-1", 0); err == nil {
			t.Fatal("expected error for invalid retention_days")
		}
	})
}

func TestPostgresStore_Get(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("found", func(t *testing.T) {
		now := time.Now()
		rows := pgxmock.NewRows([]string{
			"id", "tenant_id", "name", "status",
			"monthly_event_limit", "retention_days", "created_at", "deleted_at",
		}).AddRow("proj-1", "tenant-1", "My Project", ProjectStatusActive, int64(10000), 90, now, nil)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
			WithArgs("proj-1", "tenant-1").
			WillReturnRows(rows)

		p, err := store.Get(context.Background(), "tenant-1", "proj-1")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if p.ID != "proj-1" {
			t.Fatalf("expected proj-1, got %s", p.ID)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
			WithArgs("proj-missing", "tenant-1").
			WillReturnError(pgx.ErrNoRows)

		_, err := store.Get(context.Background(), "tenant-1", "proj-missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestPostgresStore_ListByTenant(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("success", func(t *testing.T) {
		now := time.Now()
		rows := pgxmock.NewRows([]string{
			"id", "tenant_id", "name", "status",
			"monthly_event_limit", "retention_days", "created_at", "deleted_at",
		}).
			AddRow("proj-1", "tenant-1", "P1", ProjectStatusActive, int64(10000), 90, now, nil).
			AddRow("proj-2", "tenant-1", "P2", ProjectStatusActive, int64(10000), 90, now, nil)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
			WithArgs("tenant-1").
			WillReturnRows(rows)

		out, err := store.ListByTenant(context.Background(), "tenant-1")
		if err != nil {
			t.Fatalf("ListByTenant failed: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("expected 2 projects, got %d", len(out))
		}
	})

	t.Run("empty_tenant_id_returns_error", func(t *testing.T) {
		if _, err := store.ListByTenant(context.Background(), ""); err == nil {
			t.Fatal("expected error for empty tenant_id")
		}
	})
}

func TestPostgresStore_SoftDelete(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("success", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("proj-1", "tenant-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		if err := store.SoftDelete(context.Background(), "tenant-1", "proj-1"); err != nil {
			t.Fatalf("SoftDelete failed: %v", err)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("proj-missing", "tenant-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		if err := store.SoftDelete(context.Background(), "tenant-1", "proj-missing"); err != ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("empty_ids_return_error", func(t *testing.T) {
		if err := store.SoftDelete(context.Background(), "", "proj-1"); err == nil {
			t.Fatal("expected error for empty tenant_id")
		}
		if err := store.SoftDelete(context.Background(), "tenant-1", ""); err == nil {
			t.Fatal("expected error for empty project_id")
		}
	})
}

func TestPostgresStore_ApplyPlanToTenant(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)

	t.Run("success", func(t *testing.T) {
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO tenant_quotas")).
			WithArgs("tenant-1", int64(250000)).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		mock.ExpectExec(regexp.QuoteMeta("UPDATE projects")).
			WithArgs("tenant-1", int64(250000), 180).
			WillReturnResult(pgxmock.NewResult("UPDATE", 3))

		if err := store.ApplyPlanToTenant(context.Background(), "tenant-1", 250000, 180); err != nil {
			t.Fatalf("ApplyPlanToTenant failed: %v", err)
		}
	})

	t.Run("invalid_inputs_return_error", func(t *testing.T) {
		if err := store.ApplyPlanToTenant(context.Background(), "", 100, 30); err == nil {
			t.Fatal("expected error for empty tenant_id")
		}
		if err := store.ApplyPlanToTenant(context.Background(), "tenant-1", 0, 30); err == nil {
			t.Fatal("expected error for invalid monthly_event_limit")
		}
		if err := store.ApplyPlanToTenant(context.Background(), "tenant-1", 100, 0); err == nil {
			t.Fatal("expected error for invalid retention_days")
		}
	})
}
