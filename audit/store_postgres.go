package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresAuditStore adalah implementasi audit.Store di atas Postgres,
// menggantikan BoltAuditStore (file lokal per-instance) supaya jejak
// audit konsisten walau saas-api dijalankan lebih dari satu instance,
// dan supaya backup/replication ikut strategi Postgres yang sudah ada
// untuk tenant/user/apikey lain.
//
// PENTING: perilaku CleanupForTenant/DeleteAllForTenant/anonymize di sini
// SENGAJA disamakan persis dengan BoltAuditStore (lihat store_bbolt.go)
// supaya semantic retention/anonymization yang sudah dijanjikan ke
// customer (lihat komentar di retention.go soal Â§1 pricing) tidak
// berubah gara-gara migrasi storage.
type PostgresAuditStore struct {
	pool *pgxpool.Pool
}

// NewPostgresAuditStore membuat PostgresAuditStore baru. Migration
// (lihat migrations/000008_audit_events.up.sql) WAJIB sudah dijalankan
// sebelum store ini dipakai.
func NewPostgresAuditStore(pool *pgxpool.Pool) (*PostgresAuditStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgxpool is required")
	}
	return &PostgresAuditStore{pool: pool}, nil
}

// Save menyimpan satu audit event. Sesuai interface Store.
func (s *PostgresAuditStore) Save(event *AuditEvent) error {
	if event == nil {
		return fmt.Errorf("event_required")
	}
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	metadataJSON, err := marshalMetadata(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO audit_events (id, tenant_id, user_id, action, resource, outcome, "timestamp", metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, event.ID, event.TenantID, event.UserID, event.Action, event.Resource, event.Outcome, event.Timestamp, metadataJSON)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// ListForTenant mengembalikan event terbaru dulu untuk satu tenant,
// dibatasi limit. Sesuai interface Store.
func (s *PostgresAuditStore) ListForTenant(tenantID string, limit int) ([]*AuditEvent, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id_required")
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT id, tenant_id, user_id, action, resource, outcome, "timestamp", metadata
		FROM audit_events
		WHERE tenant_id = $1
		ORDER BY "timestamp" DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// CleanupForTenant menghapus event yang lebih tua dari policy.RetentionDays,
// dan meng-anonymize event yang lebih tua dari policy.AnonymizationDays
// tapi masih di dalam RetentionDays — untuk SATU tenant saja.
//
// PERILAKU SENGAJA DISAMAKAN dengan BoltAuditStore.CleanupForTenant:
// delete dulu (row yang sudah lewat delete cutoff, apapun status
// anonymize-nya, langsung hilang), baru anonymize sisanya yang qualifies
// dan belum pernah dianonymize.
func (s *PostgresAuditStore) CleanupForTenant(tenantID string, policy *RetentionPolicy) (deleted int, anonymized int, err error) {
	if tenantID == "" {
		return 0, 0, fmt.Errorf("tenant_id_required")
	}
	if policy == nil {
		return 0, 0, fmt.Errorf("policy_required")
	}

	now := time.Now()
	deleteCutoff := now.AddDate(0, 0, -policy.RetentionDays)
	anonymizeCutoff := now.AddDate(0, 0, -policy.AnonymizationDays)

	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op kalau sudah di-commit

	// 1) DELETE — event yang sudah lewat retention penuh.
	deleteTag, err := tx.Exec(ctx, `
		DELETE FROM audit_events
		WHERE tenant_id = $1 AND "timestamp" < $2
	`, tenantID, deleteCutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("delete expired audit events: %w", err)
	}
	deleted = int(deleteTag.RowsAffected())

	// 2) ANONYMIZE — event yang lewat anonymization cutoff, masih di
	// dalam retention, dan BELUM pernah dianonymize (marker metadata).
	// user_id di-set jadi "[anonymized]" dan metadata di-scrub, sisakan
	// hanya method/status — identik dengan anonymizeEvent() di Bolt.
	anonTag, err := tx.Exec(ctx, `
		UPDATE audit_events
		SET user_id = '[anonymized]',
		    metadata = jsonb_strip_nulls(jsonb_build_object(
		        'anonymized', 'true',
		        'method', metadata->>'method',
		        'status', metadata->>'status'
		    ))
		WHERE tenant_id = $1
		  AND "timestamp" < $2
		  AND "timestamp" >= $3
		  AND COALESCE(metadata->>'anonymized', 'false') != 'true'
	`, tenantID, anonymizeCutoff, deleteCutoff)
	if err != nil {
		return deleted, 0, fmt.Errorf("anonymize audit events: %w", err)
	}
	anonymized = int(anonTag.RowsAffected())

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit cleanup tx: %w", err)
	}
	return deleted, anonymized, nil
}

// DeleteAllForTenant menghapus SELURUH audit event tenant tanpa syarat
// retensi — dipanggil dari compliance.PurgeTenant saat GDPR purge.
// Sesuai interface Store.
func (s *PostgresAuditStore) DeleteAllForTenant(tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant_id_required")
	}
	_, err := s.pool.Exec(context.Background(), `
		DELETE FROM audit_events WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return fmt.Errorf("delete all audit events for tenant: %w", err)
	}
	return nil
}

// ============================================================
// Helpers
// ============================================================

func marshalMetadata(metadata map[string]string) ([]byte, error) {
	if metadata == nil {
		return nil, nil
	}
	return json.Marshal(metadata)
}

// rowScanner mengizinkan scanAuditEvent dipakai untuk pgx.Rows maupun
// pgx.Row (mis. kalau nanti ditambah GetByID).
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAuditEvent(row rowScanner) (*AuditEvent, error) {
	var event AuditEvent
	var metadataRaw []byte

	if err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.UserID,
		&event.Action,
		&event.Resource,
		&event.Outcome,
		&event.Timestamp,
		&metadataRaw,
	); err != nil {
		return nil, fmt.Errorf("scan audit event: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &event.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return &event, nil
}

// Pastikan *PostgresAuditStore memenuhi interface Store di compile-time.
var _ Store = (*PostgresAuditStore)(nil)
