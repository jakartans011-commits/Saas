package project

import (
	"context"
	"errors"
	"fmt"
)

// PurgeAllForTenant menghapus project tenant BESERTA seluruh baris yang
// mereferensikannya.
//
// DeleteAllForTenant yang lama hanya menghapus baris projects. Lima tabel
// memegang foreign key ke projects(id) tanpa ON DELETE CASCADE
// (project_chain_state, events, idempotency_keys, project_usage_counters,
// event_retention_checkpoints), jadi DELETE itu SELALU ditolak Postgres untuk
// tenant yang pernah mengirim satu event pun — dan GDPR purge ikut gagal.
//
// SENGAJA TANPA TRANSAKSI. PostgresStore memakai interface DBQuerier yang
// hanya punya QueryRow/Exec/Query, bukan Begin; menambah Begin ke interface
// itu akan mematahkan setiap mock yang mengimplementasikannya. Tanpa transaksi
// pun aman di sini: semua pernyataan adalah DELETE idempoten dan urutannya
// dari daun ke akar, jadi kegagalan di tengah meninggalkan project yang BELUM
// terhapus — bukan keadaan setengah jadi yang tampak selesai. PurgeTenant
// mengembalikan error, tenant tetap soft-deleted, dan sweep harian berikutnya
// mengulang sampai tuntas.
func (s *PostgresStore) PurgeAllForTenant(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return errors.New("tenant_id_required")
	}

	const sel = `SELECT id FROM projects WHERE tenant_id = $1`

	// tenant_usage_counters punya FK ke tenant_quotas, jadi ia lebih dulu.
	// audit_verify_checkpoints.project_id bertipe TEXT (migrasi 000010)
	// sementara projects.id bertipe UUID — karena itu perlu cast ::text.
	stmts := []string{
		`DELETE FROM idempotency_keys            WHERE project_id IN (` + sel + `)`,
		`DELETE FROM events                      WHERE project_id IN (` + sel + `)`,
		`DELETE FROM project_chain_state         WHERE project_id IN (` + sel + `)`,
		`DELETE FROM project_usage_counters      WHERE project_id IN (` + sel + `)`,
		`DELETE FROM event_retention_checkpoints WHERE project_id IN (` + sel + `)`,
		`DELETE FROM audit_verify_checkpoints    WHERE project_id IN (SELECT id::text FROM projects WHERE tenant_id = $1)`,
		`DELETE FROM projects                    WHERE tenant_id = $1`,
		`DELETE FROM tenant_usage_counters       WHERE tenant_id = $1`,
		`DELETE FROM tenant_quotas               WHERE tenant_id = $1`,
		`DELETE FROM tenant_audit_verify_cache   WHERE tenant_id = $1`,
	}

	for i, q := range stmts {
		if _, err := s.pool.Exec(ctx, q, tenantID); err != nil {
			return fmt.Errorf("purge tenant %s (langkah %d dari %d): %w", tenantID, i+1, len(stmts), err)
		}
	}

	return nil
}
