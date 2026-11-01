package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrQuotaRowMissing: tenant belum punya baris di tenant_quotas, biasanya
// karena plan belum pernah diterapkan. Pemanggil boleh jatuh ke nilai
// bawaan plan alih-alih menampilkan error ke pelanggan.
var ErrQuotaRowMissing = errors.New("tenant_quota_row_missing")

// TenantQuotaUsage mengembalikan pemakaian dan batas event bulanan tenant
// untuk periode berjalan, DARI SUMBER YANG SAMA yang dipakai ingest untuk
// menolak dengan quota_exceeded.
//
// Ini ada karena dashboard sebelumnya menampilkan counter bbolt
// (Tenant.CurrentEventCount), yang hanya dinaikkan oleh jalur MCP.
// Pelanggan yang mengirim event lewat /v1/events melihat pemakaian nol
// sampai tiba-tiba ditolak 429 — angka di layar tidak pernah berhubungan
// dengan batas yang sebenarnya menahan mereka.
func (s *PostgresStore) TenantQuotaUsage(
	ctx context.Context,
	tenantID string,
) (used int64, limit int64, err error) {
	if s == nil || s.pool == nil {
		// Build tanpa Postgres (mis. test lokal): tidak ada counter untuk
		// dibaca. Pemanggil jatuh ke nilai bawaan, bukan gagal.
		return 0, 0, ErrQuotaRowMissing
	}
	if tenantID == "" {
		return 0, 0, fmt.Errorf("tenant id is required")
	}

	err = s.pool.QueryRow(ctx, `
		SELECT
			q.monthly_event_limit,
			COALESCE(c.event_count, 0)
		FROM tenant_quotas q
		LEFT JOIN tenant_usage_counters c
			ON c.tenant_id = q.tenant_id AND c.period = $2
		WHERE q.tenant_id = $1
	`, tenantID, currentPeriod()).Scan(&limit, &used)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrQuotaRowMissing
	}
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read tenant quota usage: %w", err)
	}
	return used, limit, nil
}
