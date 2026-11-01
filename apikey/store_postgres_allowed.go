package apikey

import (
	"context"
	"fmt"
)

// IsAmongOldestActive melaporkan apakah keyID termasuk `limit` key aktif
// (belum di-revoke) paling lama milik tenant. limit <= 0 = tanpa batas,
// selalu true. Urutan: created_at ASC, id ASC (id sebagai pemutus seri).
func (s *PostgresStore) IsAmongOldestActive(ctx context.Context, tenantID, keyID string, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM (
				SELECT id FROM api_keys
				WHERE tenant_id = $1 AND revoked_at IS NULL
				ORDER BY created_at ASC, id ASC
				LIMIT $2
			) t
			WHERE t.id = $3
		)
	`, tenantID, limit, keyID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("failed to check api key plan allowance: %w", err)
	}
	return ok, nil
}
