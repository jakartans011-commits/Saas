package events

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/notify"
)

// ============================================================
// POSTGRES STORE
// ============================================================

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

var (
	ErrNotFound         = errors.New("event_not_found")
	ErrIdempotentReplay = errors.New("idempotent_replay") // bukan error sungguhan, sinyal "sudah pernah, ini event yang lama"
	ErrQuotaExceeded    = errors.New("quota_exceeded")
)

// currentPeriod — format 'YYYY-MM', dipakai sebagai partisi logis
// counter kuota bulanan. UTC supaya konsisten lintas region server.
func currentPeriod() string {
	return time.Now().UTC().Format("2006-01")
}

func (s *PostgresStore) Append(ctx context.Context, projectID, tenantID string, req *IngestRequest, idempotencyKey string) (*Event, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Truncate ke microsecond — kolom occurred_at bertipe TIMESTAMPTZ
	// di Postgres cuma menyimpan presisi microsecond (6 digit), bukan
	// nanosecond (9 digit) seperti time.Now() di Go. Kalau tidak
	// dipotong di sini, hash yang dihitung SEKARANG (pakai presisi
	// nano) akan berbeda dari hash yang dihitung ulang saat verify
	// (setelah dibaca lagi dari DB, presisinya sudah terpotong
	// otomatis oleh Postgres) — akibatnya VerifyChain SELALU melaporkan
	// broken meski datanya tidak pernah diubah siapa pun.
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	if req.OccurredAt != nil {
		occurredAt = req.OccurredAt.UTC().Truncate(time.Microsecond)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op kalau sudah di-Commit

	// ... (sisa fungsi TIDAK berubah, biarkan seperti aslinya)

	// Idempotency check DI DALAM transaksi yang sama — supaya tidak
	// ada race antara cek dan insert.
	if idempotencyKey != "" {
		var existingEventID string
		err := tx.QueryRow(ctx, `
			SELECT event_id FROM idempotency_keys
			WHERE project_id = $1 AND idempotency_key = $2
		`, projectID, idempotencyKey).Scan(&existingEventID)

		if err == nil {
			// Sudah pernah diproses — ambil event yang sudah ada,
			// kembalikan itu, JANGAN insert baru.
			existing, getErr := s.getByIDTx(ctx, tx, existingEventID)
			if getErr != nil {
				return nil, getErr
			}
			_ = tx.Commit(ctx) // commit no-op read, aman
			return existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to check idempotency key: %w", err)
		}
	}

	// ============================================================
	// QUOTA CHECK — atomik dalam transaksi yang sama dengan insert
	// event, supaya tidak ada race antara "cek kuota" dan "insert
	// event" (kalau dipisah, dua request concurrent bisa sama-sama
	// lolos cek sebelum salah satunya increment).
	//
	// FIX (2026-09): quota di-enforce PER TENANT (tenant_quotas +
	// tenant_usage_counters), BUKAN per-project seperti sebelumnya.
	// Locked Pricing & BUSINESS_MODEL.md menjanjikan kuota pooled
	// lintas semua project dalam satu tenant — enforcement lama
	// (projects.monthly_event_limit + project_usage_counters) di-skip,
	// TIDAK dihapus dari skema (lihat migration 000007) untuk hindari
	// perubahan destruktif, tapi sudah tidak dipakai untuk enforcement.
	// ============================================================

	var monthlyLimit int64

	// Auto-create baris Free-default (5000) kalau tenant belum punya
	// tenant_quotas SAMA SEKALI — penting untuk tenant baru yang belum
	// pernah kena ApplyPlanToTenant (mis. signup Free tanpa webhook
	// Stripe), supaya Append() tidak gagal keras "no rows" untuk
	// kasus yang sebenarnya valid (tenant baru = Free, bukan error).
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id) VALUES ($1)
		ON CONFLICT (tenant_id) DO NOTHING
	`, tenantID); err != nil {
		return nil, fmt.Errorf("failed to init tenant quota: %w", err)
	}

	if err := tx.QueryRow(ctx, `
		SELECT monthly_event_limit FROM tenant_quotas WHERE tenant_id = $1
	`, tenantID).Scan(&monthlyLimit); err != nil {
		return nil, fmt.Errorf("failed to load tenant quota: %w", err)
	}

	period := currentPeriod()

	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_usage_counters (tenant_id, period, event_count)
		VALUES ($1, $2, 0)
		ON CONFLICT (tenant_id, period) DO NOTHING
	`, tenantID, period); err != nil {
		return nil, fmt.Errorf("failed to init tenant usage counter: %w", err)
	}

	var currentCount int64
	if err := tx.QueryRow(ctx, `
		SELECT event_count FROM tenant_usage_counters
		WHERE tenant_id = $1 AND period = $2
		FOR UPDATE
	`, tenantID, period).Scan(&currentCount); err != nil {
		return nil, fmt.Errorf("failed to lock tenant usage counter: %w", err)
	}

	if currentCount >= monthlyLimit {
		return nil, ErrQuotaExceeded
	}

	// Kunci baris chain state project ini. Kalau belum ada baris
	// (project baru), buat dulu dengan last_sequence=0.
	var lastSequence int64
	var lastHash *string

	err = tx.QueryRow(ctx, `
		SELECT last_sequence, last_hash FROM project_chain_state
		WHERE project_id = $1
		FOR UPDATE
	`, projectID).Scan(&lastSequence, &lastHash)

	if errors.Is(err, pgx.ErrNoRows) {
		_, insErr := tx.Exec(ctx, `
			INSERT INTO project_chain_state (project_id, last_sequence, last_hash)
			VALUES ($1, 0, NULL)
		`, projectID)
		if insErr != nil {
			return nil, fmt.Errorf("failed to init chain state: %w", insErr)
		}
		lastSequence = 0
		lastHash = nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to lock chain state: %w", err)
	}

	prevHash := ""
	if lastHash != nil {
		prevHash = *lastHash
	}

	nextSequence := lastSequence + 1
	hash := ComputeHash(projectID, nextSequence, occurredAt, req.EventType, req.ActorID, req.Payload, prevHash)

	e := &Event{
		ProjectID:  projectID,
		TenantID:   tenantID,
		Sequence:   nextSequence,
		EventType:  req.EventType,
		ActorID:    req.ActorID,
		Payload:    req.Payload,
		PrevHash:   prevHash,
		Hash:       hash,
		OccurredAt: occurredAt,
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO events (project_id, tenant_id, sequence, event_type, actor_id, payload, prev_hash, hash, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at
	`, e.ProjectID, e.TenantID, e.Sequence, e.EventType, e.ActorID, e.Payload, nullIfEmpty(e.PrevHash), e.Hash, e.OccurredAt)

	if err := row.Scan(&e.ID, &e.CreatedAt); err != nil {
		return nil, fmt.Errorf("failed to insert event: %w", err)
	}

	if idempotencyKey != "" {
		_, err := tx.Exec(ctx, `
			INSERT INTO idempotency_keys (project_id, idempotency_key, event_id)
			VALUES ($1, $2, $3)
		`, projectID, idempotencyKey, e.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to record idempotency key: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE project_chain_state
		SET last_sequence = $2, last_hash = $3, updated_at = now()
		WHERE project_id = $1
	`, projectID, e.Sequence, e.Hash)
	if err != nil {
		return nil, fmt.Errorf("failed to update chain state: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE tenant_usage_counters
		SET event_count = event_count + 1, updated_at = now()
		WHERE tenant_id = $1 AND period = $2
	`, tenantID, period)
	if err != nil {
		return nil, fmt.Errorf("failed to increment tenant usage counter: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit event append: %w", err)
	}

	// Notifikasi kuota (#9) — dicek SETELAH commit, di luar transaksi,
	// supaya latency provider notifikasi (kalaupun ada nanti) tidak
	// pernah menahan lock row project_usage_counters. currentCount+1
	// dipakai karena currentCount di atas dibaca SEBELUM increment
	// baris ini sendiri.
	checkQuotaThresholds(projectID, tenantID, currentCount+1, monthlyLimit)

	return e, nil
}

// checkQuotaThresholds mendaftarkan notifikasi email (lewat
// notify.EnqueueQuotaThreshold, no-op kalau notify belum di-Init)
// begitu tenant menyentuh 80% atau 100% dari monthly_event_limit.
// Dedupe per (tenant, template, periode) ada di package notify —
// fungsi ini AMAN dipanggil di setiap Append tanpa perlu tracking
// state tambahan di sini.
func checkQuotaThresholds(projectID, tenantID string, usedCount, monthlyLimit int64) {
	if monthlyLimit <= 0 {
		return
	}

	percent := float64(usedCount) / float64(monthlyLimit) * 100

	data := map[string]string{
		"project_id": projectID,
		"used":       formatInt64(usedCount),
		"limit":      formatInt64(monthlyLimit),
	}

	switch {
	case percent >= 100:
		notify.EnqueueQuotaThreshold(tenantID, projectID, notify.TemplateQuota100, data)
	case percent >= 80:
		notify.EnqueueQuotaThreshold(tenantID, projectID, notify.TemplateQuota80, data)
	}
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *PostgresStore) getByIDTx(ctx context.Context, tx pgx.Tx, id string) (*Event, error) {
	var e Event
	var prevHash *string
	row := tx.QueryRow(ctx, `
		SELECT id, project_id, tenant_id, sequence, event_type, actor_id, payload, prev_hash, hash, occurred_at, created_at
		FROM events WHERE id = $1
	`, id)
	if err := row.Scan(&e.ID, &e.ProjectID, &e.TenantID, &e.Sequence, &e.EventType, &e.ActorID, &e.Payload, &prevHash, &e.Hash, &e.OccurredAt, &e.CreatedAt); err != nil {
		return nil, fmt.Errorf("failed to load event: %w", err)
	}
	if prevHash != nil {
		e.PrevHash = *prevHash
	}
	return &e, nil
}

// ============================================================
// QUERY — cursor pagination berbasis Sequence (stabil, TIDAK
// pakai offset — event baru yang masuk selagi klien paginate
// tidak akan menggeser hasil halaman berikutnya).
// ============================================================

type QueryFilter struct {
	EventType string
	ActorID   string
	Since     *time.Time
	Until     *time.Time
	Cursor    string // opaque, encode dari Sequence terakhir
	Limit     int
}

type QueryResult struct {
	Events     []*Event
	NextCursor string // kosong kalau tidak ada halaman berikutnya
}

func encodeCursor(seq int64) string {
	return base64.URLEncoding.EncodeToString([]byte(formatInt64(seq)))
}

func decodeCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	b, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid_cursor")
	}
	var n int64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid_cursor")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

func (s *PostgresStore) Query(ctx context.Context, projectID string, f QueryFilter) (*QueryResult, error) {
	afterSeq, err := decodeCursor(f.Cursor)
	if err != nil {
		return nil, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	query := `
		SELECT id, project_id, tenant_id, sequence, event_type, actor_id, payload, prev_hash, hash, occurred_at, created_at
		FROM events
		WHERE project_id = $1 AND sequence > $2
	`
	args := []any{projectID, afterSeq}
	argN := 3

	if f.EventType != "" {
		query += fmt.Sprintf(" AND event_type = $%d", argN)
		args = append(args, f.EventType)
		argN++
	}
	if f.ActorID != "" {
		query += fmt.Sprintf(" AND actor_id = $%d", argN)
		args = append(args, f.ActorID)
		argN++
	}
	if f.Since != nil {
		query += fmt.Sprintf(" AND occurred_at >= $%d", argN)
		args = append(args, *f.Since)
		argN++
	}
	if f.Until != nil {
		query += fmt.Sprintf(" AND occurred_at <= $%d", argN)
		args = append(args, *f.Until)
		argN++
	}

	query += fmt.Sprintf(" ORDER BY sequence ASC LIMIT $%d", argN)
	args = append(args, limit+1) // ambil 1 ekstra untuk tahu apakah ada halaman berikutnya

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		var e Event
		var prevHash *string
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.TenantID, &e.Sequence, &e.EventType, &e.ActorID, &e.Payload, &prevHash, &e.Hash, &e.OccurredAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan event row: %w", err)
		}
		if prevHash != nil {
			e.PrevHash = *prevHash
		}
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &QueryResult{}
	if len(out) > limit {
		result.NextCursor = encodeCursor(out[limit-1].Sequence)
		out = out[:limit]
	}
	result.Events = out

	return result, nil
}

// ============================================================
// VERIFY — verifikasi rentang chain.
//
// Mendukung retention:
// - Jika event sebelumnya masih ada, gunakan hash event tersebut.
// - Jika event sebelumnya sudah dipurge, gunakan retention checkpoint.
// - Jika fromSequence berada di dalam rentang yang sudah dipurge,
//   verifikasi ditolak karena tidak ada anchor chain yang valid.
// ============================================================

func (s *PostgresStore) VerifyRange(
	ctx context.Context,
	projectID string,
	fromSequence int64,
) (
	valid bool,
	brokenAtEventID string,
	checkedCount int,
	err error,
) {
	if fromSequence < 1 {
		return false, "", 0, fmt.Errorf("invalid from_sequence: must be >= 1")
	}

	var expectedPrevHash string

	if fromSequence > 1 {
		var prevHash *string

		row := s.pool.QueryRow(ctx, `
			SELECT hash
			FROM events
			WHERE project_id = $1
			  AND sequence = $2
		`, projectID, fromSequence-1)

		scanErr := row.Scan(&prevHash)

		switch {
		case scanErr == nil:
			// Event sebelumnya masih ada.
			if prevHash != nil {
				expectedPrevHash = *prevHash
			}

		case errors.Is(scanErr, pgx.ErrNoRows):
			// Event sebelumnya sudah tidak ada.
			// Kemungkinan besar sudah dipurge oleh retention job.
			var checkpointSequence int64
			var checkpointHash string

			checkpointErr := s.pool.QueryRow(ctx, `
				SELECT
					purged_through_sequence,
					purged_through_hash
				FROM event_retention_checkpoints
				WHERE project_id = $1
			`, projectID).Scan(
				&checkpointSequence,
				&checkpointHash,
			)

			if checkpointErr != nil {
				if errors.Is(checkpointErr, pgx.ErrNoRows) {
					return false, "", 0, fmt.Errorf(
						"event %d is missing and no retention checkpoint exists",
						fromSequence-1,
					)
				}

				return false, "", 0, fmt.Errorf(
					"failed to load retention checkpoint: %w",
					checkpointErr,
				)
			}

			// Checkpoint harus tepat berada pada event
			// sebelum range yang ingin diverifikasi.
			if checkpointSequence != fromSequence-1 {
				return false, "", 0, fmt.Errorf(
					"cannot verify from_sequence=%d: retention checkpoint is at sequence=%d",
					fromSequence,
					checkpointSequence,
				)
			}

			expectedPrevHash = checkpointHash

		default:
			return false, "", 0, fmt.Errorf(
				"failed to load previous event hash: %w",
				scanErr,
			)
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			id,
			project_id,
			tenant_id,
			sequence,
			event_type,
			actor_id,
			payload,
			prev_hash,
			hash,
			occurred_at,
			created_at
		FROM events
		WHERE project_id = $1
		  AND sequence >= $2
		ORDER BY sequence ASC
	`, projectID, fromSequence)

	if err != nil {
		return false, "", 0, fmt.Errorf(
			"failed to query events for verify: %w",
			err,
		)
	}
	defer rows.Close()

	var evts []*Event

	for rows.Next() {
		var e Event
		var prevHash *string

		if err := rows.Scan(
			&e.ID,
			&e.ProjectID,
			&e.TenantID,
			&e.Sequence,
			&e.EventType,
			&e.ActorID,
			&e.Payload,
			&prevHash,
			&e.Hash,
			&e.OccurredAt,
			&e.CreatedAt,
		); err != nil {
			return false, "", 0, fmt.Errorf(
				"failed to scan event row: %w",
				err,
			)
		}

		if prevHash != nil {
			e.PrevHash = *prevHash
		}

		evts = append(evts, &e)
	}

	if err := rows.Err(); err != nil {
		return false, "", 0, err
	}

	valid, brokenAtEventID, err = VerifyChain(
		evts,
		expectedPrevHash,
	)
	if err != nil || !valid {
		// Rantai yang putus di tengah lebih spesifik daripada ekor yang
		// hilang: laporkan itu dulu, lengkap dengan event mana.
		return valid, brokenAtEventID, len(evts), err
	}

	// Sambungannya utuh. Tapi "utuh" tidak berarti "lengkap" — lihat
	// chainTailTruncated di chain_tail.go.
	truncated, truncErr := s.chainTailTruncated(ctx, projectID, evts)
	if truncErr != nil {
		return false, "", len(evts), truncErr
	}
	if truncated {
		// brokenAtEventID kosong: tidak ada event yang bisa ditunjuk,
		// justru karena event itulah yang hilang.
		return false, "", len(evts), nil
	}

	return true, "", len(evts), nil
}

// ============================================================
// TAMBAHKAN method ini ke events/store_postgres.go (paste di akhir
// file). READ-ONLY — tidak mengubah Append() atau logika quota
// check sama sekali. Dibuat supaya portal/dashboard bisa membaca
// project_usage_counters TANPA melakukan write/lock apa pun,
// konsisten dengan sumber kebenaran yang sama persis dipakai
// Append() untuk enforcement (bukan re-hitung dengan cara lain).
// ============================================================

// GetUsageCount mengembalikan event_count untuk satu project pada
// satu periode ('YYYY-MM', pakai currentPeriod() untuk bulan
// berjalan). Mengembalikan 0, nil kalau belum ada baris counter
// sama sekali (project belum pernah ingest event bulan ini) — BUKAN
// error, karena itu kondisi normal untuk project baru.
//
// DEPRECATED untuk keperluan quota/dashboard (2026-09): sejak fix
// tenant-pooled quota, ini TIDAK LAGI merepresentasikan kuota yang
// di-enforce (lihat GetTenantUsageCount). Boleh tetap dipakai untuk
// analitik "usage per project" (breakdown), TAPI JANGAN dipakai lagi
// untuk menghitung sisa kuota/quota bar di dashboard — itu harus baca
// GetTenantUsageCount + tenant_quotas.monthly_event_limit.
func (s *PostgresStore) GetUsageCount(ctx context.Context, projectID, period string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT event_count FROM project_usage_counters
		WHERE project_id = $1 AND period = $2
	`, projectID, period).Scan(&count)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read usage counter: %w", err)
	}
	return count, nil
}

// GetTenantUsageCount mengembalikan event_count TENANT (pooled lintas
// semua project) pada satu periode — ini SUMBER KEBENARAN yang sama
// persis dipakai Append() untuk enforcement kuota. Dashboard/usage.html
// WAJIB pindah ke fungsi ini untuk menampilkan quota bar, bukan
// menjumlahkan GetUsageCount per-project sendiri (rawan drift lagi).
func (s *PostgresStore) GetTenantUsageCount(ctx context.Context, tenantID, period string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT event_count FROM tenant_usage_counters
		WHERE tenant_id = $1 AND period = $2
	`, tenantID, period).Scan(&count)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read tenant usage counter: %w", err)
	}
	return count, nil
}

// GetTenantMonthlyLimit mengembalikan monthly_event_limit tenant saat
// ini dari tenant_quotas — dipakai dashboard untuk menghitung
// persentase/sisa kuota, konsisten dengan angka yang di-enforce Append().
func (s *PostgresStore) GetTenantMonthlyLimit(ctx context.Context, tenantID string) (int64, error) {
	var limit int64
	err := s.pool.QueryRow(ctx, `
		SELECT monthly_event_limit FROM tenant_quotas WHERE tenant_id = $1
	`, tenantID).Scan(&limit)
	if err != nil {
		return 0, fmt.Errorf("failed to read tenant quota: %w", err)
	}
	return limit, nil
}

// CurrentPeriod expose currentPeriod() ke package lain (portal) —
// supaya caller di luar package events tidak menebak format
// 'YYYY-MM' sendiri dan berisiko mismatch dengan yang dipakai
// Append().
func CurrentPeriod() string {
	return currentPeriod()
}

// QueryLatest mengembalikan maksimal n event TERBARU (sequence terbesar)
// yang cocok dengan filter, dikembalikan menaik (urutan hash chain).
// Dipakai export plan Free. Cursor pada f diabaikan.
func (s *PostgresStore) QueryLatest(ctx context.Context, projectID string, f QueryFilter, n int) ([]*Event, error) {
	if n <= 0 {
		return nil, nil
	}
	query := `
		SELECT id, project_id, tenant_id, sequence, event_type, actor_id, payload, prev_hash, hash, occurred_at, created_at
		FROM events
		WHERE project_id = $1
	`
	args := []any{projectID}
	argN := 2

	if f.EventType != "" {
		query += fmt.Sprintf(" AND event_type = $%d", argN)
		args = append(args, f.EventType)
		argN++
	}
	if f.ActorID != "" {
		query += fmt.Sprintf(" AND actor_id = $%d", argN)
		args = append(args, f.ActorID)
		argN++
	}
	if f.Since != nil {
		query += fmt.Sprintf(" AND occurred_at >= $%d", argN)
		args = append(args, *f.Since)
		argN++
	}
	if f.Until != nil {
		query += fmt.Sprintf(" AND occurred_at <= $%d", argN)
		args = append(args, *f.Until)
		argN++
	}
	query += fmt.Sprintf(" ORDER BY sequence DESC LIMIT $%d", argN)
	args = append(args, n)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest events: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		var e Event
		var prevHash *string
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.TenantID, &e.Sequence, &e.EventType, &e.ActorID, &e.Payload, &prevHash, &e.Hash, &e.OccurredAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan event row: %w", err)
		}
		if prevHash != nil {
			e.PrevHash = *prevHash
		}
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// balik ke urutan menaik supaya export tetap terbaca sebagai chain
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
