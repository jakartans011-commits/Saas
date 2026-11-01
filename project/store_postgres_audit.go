package project

import (
	"context"
	"errors"
	"fmt"
)

// ============================================================
// ListByTenantIncludingDeleted — Phase 2, ROADMAP §7 keputusan D4
// (Opsi B). Dipakai KHUSUS oleh audit-verify (events.VerifyTenant):
// project yang sudah di-soft-delete TETAP menyertakan event yang
// masih bagian sah dari chain hash (SoftDelete hanya menandai
// projects.status/deleted_at, TIDAK menyentuh tabel events sama
// sekali — lihat project/store_postgres.go SoftDelete). Verifikasi
// integritas TIDAK BOLEH diam-diam melewati event itu hanya karena
// project-nya sudah "dihapus" secara produk.
//
// ADDITIVE — ListByTenant (yang lama, dipakai UI daftar project) TIDAK
// diubah sama sekali.
//
// Urutan: id ASC (stabil), BUKAN created_at DESC seperti ListByTenant
// — sesuai D4 ("berurutan stabil menurut ID").
// ============================================================
func (s *PostgresStore) ListByTenantIncludingDeleted(
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
		ORDER BY id ASC
	`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to list projects including deleted: %w",
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