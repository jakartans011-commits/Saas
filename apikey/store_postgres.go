package apikey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================
// POSTGRES STORE
// ============================================================

// DBQuerier — sama seperti project.DBQuerier: interface minimal yang
// dipakai PostgresStore, supaya bisa ditest tanpa Postgres asli
// (*pgxpool.Pool tetap otomatis memenuhi ini di production).
type DBQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
}

type PostgresStore struct {
	pool DBQuerier
}

func NewPostgresStore(pool DBQuerier) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, k *APIKey) error {
	if k.KeyHash == "" {
		return errors.New("key_hash_required")
	}
	if !k.Scope.Valid() {
		return ErrInvalidScope
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (project_id, tenant_id, name, key_hash, key_prefix, scope)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, k.ProjectID, k.TenantID, k.Name, k.KeyHash, k.KeyPrefix, k.Scope)

	if err := row.Scan(&k.ID, &k.CreatedAt); err != nil {
		return fmt.Errorf("failed to create api key: %w", err)
	}
	return nil
}

// FindByPlaintext — lookup dengan HASH key (indexed, O(1) lookup),
// BUKAN scan seluruh tabel + bandingkan plaintext seperti versi
// bbolt sebelumnya. Menolak key yang sudah revoked.
func (s *PostgresStore) FindByPlaintext(ctx context.Context, plaintext string) (*APIKey, error) {
	hash := HashKey(plaintext)

	var k APIKey
	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, tenant_id, name, key_hash, key_prefix, scope, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE key_hash = $1
	`, hash)

	if err := row.Scan(&k.ID, &k.ProjectID, &k.TenantID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.Scope, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to find api key: %w", err)
	}

	if k.RevokedAt != nil {
		return nil, ErrRevoked
	}

	return &k, nil
}

func (s *PostgresStore) ListByProject(ctx context.Context, tenantID, projectID string) ([]*APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, tenant_id, name, key_hash, key_prefix, scope, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE project_id = $1 AND tenant_id = $2
		ORDER BY created_at DESC
	`, projectID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list api keys: %w", err)
	}
	defer rows.Close()

	var out []*APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.TenantID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.Scope, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, fmt.Errorf("failed to scan api key row: %w", err)
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Revoke(ctx context.Context, tenantID, projectID, keyID string) error {
	tag, err := s.pool.Exec(ctx, `
        UPDATE api_keys
        SET revoked_at = now()
        WHERE id = $1
          AND tenant_id = $2
          AND project_id = $3
          AND revoked_at IS NULL
`,
		keyID,
		tenantID,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastUsed — dipanggil fire-and-forget dari middleware setelah
// request berhasil divalidasi, TIDAK diblokir untuk response.
func (s *PostgresStore) UpdateLastUsed(ctx context.Context, keyID string) error {
	now := time.Now()
	_, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET last_used_at = $2 WHERE id = $1
	`, keyID, now)
	return err
}

// DeleteAllForTenant — dipanggil dari compliance.PurgeTenant saat GDPR
// purge. Menghapus semua API key metadata milik tenant (plaintext key
// tidak pernah disimpan, jadi tidak ada yang perlu di-scrub selain baris
// ini).
func (s *PostgresStore) DeleteAllForTenant(ctx context.Context, tenantID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("delete all api keys for tenant %s: %w", tenantID, err)
	}
	return nil
}

// ErrKeyLimitReached dikembalikan CreateWithLimit bila jumlah key aktif tenant
// sudah mencapai batas plan.
var ErrKeyLimitReached = errors.New("api_key_limit_reached")

// CountActiveByTenant menghitung key yang belum di-revoke milik satu tenant
// (lintas project, K2).
func (s *PostgresStore) CountActiveByTenant(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM api_keys
		WHERE tenant_id = $1 AND revoked_at IS NULL
	`, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("failed to count active api keys: %w", err)
	}
	return n, nil
}

// CreateWithLimit = Create + batas key aktif per tenant. limit <= 0 berarti
// tanpa batas (perilaku lama). Hitung-lalu-insert tidak atomik, jadi setelah
// insert dihitung ulang; bila melebihi batas (dua request serentak), key baru
// di-revoke dan request ditolak. Dalam kasus serentak, keduanya bisa ditolak,
// dan pengguna cukup mengulang.
func (s *PostgresStore) CreateWithLimit(ctx context.Context, k *APIKey, limit int) error {
	if limit <= 0 {
		return s.Create(ctx, k)
	}

	n, err := s.CountActiveByTenant(ctx, k.TenantID)
	if err != nil {
		return err
	}
	if n >= limit {
		return ErrKeyLimitReached
	}

	if err := s.Create(ctx, k); err != nil {
		return err
	}

	n, err = s.CountActiveByTenant(ctx, k.TenantID)
	if err != nil {
		_ = s.Revoke(ctx, k.TenantID, k.ProjectID, k.ID)
		return err
	}
	if n > limit {
		_ = s.Revoke(ctx, k.TenantID, k.ProjectID, k.ID)
		return ErrKeyLimitReached
	}
	return nil
}
