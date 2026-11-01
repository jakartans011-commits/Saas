package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================
// POSTGRES STORE
// ============================================================

type PostgresStore struct {
	pool DBQuerier
}

func NewPostgresStore(pool DBQuerier) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// DBQuerier — interface minimal yang dipakai PostgresStore.
// *pgxpool.Pool otomatis memenuhi ini, begitu juga pgxmock.PgxPoolIface,
// sehingga PostgresStore bisa ditest tanpa Postgres asli.
type DBQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
}

// ============================================================
// CREATE
//
// Project dibuat dengan entitlement plan yang sudah di-resolve
// oleh handler.
//
// Paid tenant:
//   MonthlyEventLimit = plan limit
//   RetentionDays     = plan retention
//
// Free / unknown:
//   fallback ke default project.
// ============================================================

func (s *PostgresStore) Create(ctx context.Context, p *Project) error {
	if p.Name == "" {
		return ErrNameRequired
	}

	if p.TenantID == "" {
		return errors.New("tenant_id_required")
	}

	if p.Status == "" {
		p.Status = ProjectStatusActive
	}

	if p.MonthlyEventLimit <= 0 {
		p.MonthlyEventLimit = DefaultMonthlyEventLimit
	}

	if p.RetentionDays <= 0 {
		p.RetentionDays = DefaultRetentionDays
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO projects (
			tenant_id,
			name,
			status,
			monthly_event_limit,
			retention_days
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`,
		p.TenantID,
		p.Name,
		p.Status,
		p.MonthlyEventLimit,
		p.RetentionDays,
	)

	if err := row.Scan(
		&p.ID,
		&p.CreatedAt,
	); err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	return nil
}

// ============================================================
// SET RETENTION DAYS
//
// Storage primitive untuk mengubah retention satu project.
// Tidak digunakan lagi oleh CreateProjectHandler karena project
// sekarang dibuat langsung dengan entitlement final.
// ============================================================

func (s *PostgresStore) SetRetentionDays(
	ctx context.Context,
	projectID string,
	retentionDays int,
) error {
	if projectID == "" {
		return errors.New("project_id_required")
	}

	if retentionDays <= 0 {
		return errors.New("retention_days_invalid")
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE projects
		SET retention_days = $2
		WHERE id = $1
		  AND deleted_at IS NULL
	`,
		projectID,
		retentionDays,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to set retention_days: %w",
			err,
		)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ============================================================
// GET
//
// Tenant isolation enforced directly at storage layer.
// ============================================================

func (s *PostgresStore) Get(
	ctx context.Context,
	tenantID,
	projectID string,
) (*Project, error) {
	var p Project

	row := s.pool.QueryRow(ctx, `
		SELECT
			id,
			tenant_id,
			name,
			status,
			monthly_event_limit,
			retention_days,
			created_at,
			deleted_at
		FROM projects
		WHERE id = $1
		  AND tenant_id = $2
		  AND deleted_at IS NULL
	`,
		projectID,
		tenantID,
	)

	if err := row.Scan(
		&p.ID,
		&p.TenantID,
		&p.Name,
		&p.Status,
		&p.MonthlyEventLimit,
		&p.RetentionDays,
		&p.CreatedAt,
		&p.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf(
			"failed to get project: %w",
			err,
		)
	}

	return &p, nil
}

// ============================================================
// LIST BY TENANT
// ============================================================

func (s *PostgresStore) ListByTenant(
	ctx context.Context,
	tenantID string,
) ([]*Project, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id_required")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			id,
			tenant_id,
			name,
			status,
			monthly_event_limit,
			retention_days,
			created_at,
			deleted_at
		FROM projects
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
	`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to list projects: %w",
			err,
		)
	}

	defer rows.Close()

	var out []*Project

	for rows.Next() {
		var p Project

		if err := rows.Scan(
			&p.ID,
			&p.TenantID,
			&p.Name,
			&p.Status,
			&p.MonthlyEventLimit,
			&p.RetentionDays,
			&p.CreatedAt,
			&p.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"failed to scan project row: %w",
				err,
			)
		}

		out = append(out, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"failed to iterate project rows: %w",
			err,
		)
	}

	return out, nil
}

// ============================================================
// SOFT DELETE
// ============================================================

func (s *PostgresStore) SoftDelete(
	ctx context.Context,
	tenantID,
	projectID string,
) error {
	if tenantID == "" {
		return errors.New("tenant_id_required")
	}

	if projectID == "" {
		return errors.New("project_id_required")
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE projects
		SET
			status = 'deleted',
			deleted_at = now()
		WHERE id = $1
		  AND tenant_id = $2
		  AND deleted_at IS NULL
	`,
		projectID,
		tenantID,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to delete project: %w",
			err,
		)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ============================================================
// PATCH ApplyPlanToTenant (2026-09) — tenant-pooled quota fix.
//
// SEBELUM: hanya update projects.monthly_event_limit per baris,
// enforcement lama (Append() di events package) baca dari sini.
//
// SESUDAH: enforcement pindah ke tabel tenant_quotas (lihat
// migration 000007_tenant_quota). Fungsi ini WAJIB ikut update
// tenant_quotas, kalau tidak, upgrade/downgrade plan customer
// TIDAK akan mengubah limit yang benar-benar di-enforce.
//
// projects.monthly_event_limit TETAP diupdate juga (baris asli
// tidak dihapus) — supaya kolom itu tidak jadi data basi/nyasar
// kalau ada pemanggil lain yang masih membacanya untuk keperluan
// non-enforcement (mis. tampilan "limit per project" di UI lama).
// ============================================================

func (s *PostgresStore) ApplyPlanToTenant(
	ctx context.Context,
	tenantID string,
	monthlyEventLimit int64,
	retentionDays int,
) error {
	if tenantID == "" {
		return errors.New("tenant_id_required")
	}

	if monthlyEventLimit <= 0 {
		return errors.New("monthly_event_limit_invalid")
	}

	if retentionDays <= 0 {
		return errors.New("retention_days_invalid")
	}

	// BARIS BARU — sumber kebenaran quota enforcement.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id, monthly_event_limit)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET
			monthly_event_limit = $2,
			updated_at = now()
	`,
		tenantID,
		monthlyEventLimit,
	)
	if err != nil {
		return fmt.Errorf("failed to apply plan to tenant quota: %w", err)
	}

	// BARIS ASLI — dipertahankan, sudah tidak dipakai untuk enforcement
	// tapi jangan dihapus supaya tidak ada perubahan destruktif tambahan.
	_, err = s.pool.Exec(ctx, `
		UPDATE projects
		SET
			monthly_event_limit = $2,
			retention_days = $3
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
	`,
		tenantID,
		monthlyEventLimit,
		retentionDays,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to apply plan to tenant projects: %w",
			err,
		)
	}

	return nil
}

// DeleteAllForTenant — dipanggil dari compliance.PurgeTenant saat GDPR
// purge. Hard delete semua project milik tenant (bukan SoftDelete).
func (s *PostgresStore) DeleteAllForTenant(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return errors.New("tenant_id_required")
	}

	_, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("delete all projects for tenant %s: %w", tenantID, err)
	}
	return nil
}
