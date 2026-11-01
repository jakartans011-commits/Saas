package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================
// RETENTION JOB
// ============================================================
//
// RetentionJob melakukan hard-delete event yang sudah melewati
// retention_days setiap project.
//
// Hash chain tetap dapat diverifikasi setelah purge karena hash
// event terakhir yang dihapus disimpan sebagai checkpoint.
//
// Job ini sengaja standalone dan run-once.
// Tidak bergantung pada core/scheduler.
//

type RetentionJob struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewRetentionJob(
	pool *pgxpool.Pool,
	logger *slog.Logger,
) *RetentionJob {
	if logger == nil {
		logger = slog.Default()
	}

	return &RetentionJob{
		pool:   pool,
		logger: logger,
	}
}

// RunOnce menjalankan retention untuk seluruh project aktif.
//
// Satu project gagal tidak menghentikan project lainnya.
// Setelah seluruh project diproses, error agregat dikembalikan
// jika ada project yang gagal.
func (j *RetentionJob) RunOnce(ctx context.Context) error {
	if j == nil || j.pool == nil {
		return errors.New("retention job: nil postgres pool")
	}

	projectIDs, err := j.listProjectIDs(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	var failed int

	for _, projectID := range projectIDs {
		if err := j.purgeProject(ctx, projectID); err != nil {
			j.logger.Error(
				"retention purge failed",
				"project_id", projectID,
				"error", err,
			)

			failed++
			continue
		}
	}

	if failed > 0 {
		return fmt.Errorf(
			"retention purge failed for %d project(s)",
			failed,
		)
	}

	return nil
}

// listProjectIDs mengambil project yang belum di-soft-delete.
func (j *RetentionJob) listProjectIDs(
	ctx context.Context,
) ([]string, error) {
	rows, err := j.pool.Query(ctx, `
		SELECT id
		FROM projects
		WHERE deleted_at IS NULL
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

// purgeProject melakukan purge satu project dalam satu transaksi:
//
// 1. baca retention_days
// 2. tentukan boundary event yang boleh dihapus
// 3. simpan sequence + hash boundary sebagai checkpoint
// 4. hapus idempotency_keys
// 5. hapus events
// 6. commit
//
// Semua langkah berada dalam transaksi yang sama.
func (j *RetentionJob) purgeProject(
	ctx context.Context,
	projectID string,
) error {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var retentionDays int

	err = tx.QueryRow(ctx, `
		SELECT retention_days
		FROM projects
		WHERE id = $1
	`, projectID).Scan(&retentionDays)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("project not found")
		}

		return fmt.Errorf(
			"load retention_days: %w",
			err,
		)
	}

	if retentionDays <= 0 {
		return fmt.Errorf(
			"invalid retention_days=%d",
			retentionDays,
		)
	}

	cutoff := time.Now().
		UTC().
		AddDate(0, 0, -retentionDays)

	// Cari event TERAKHIR yang masih berada di sisi
	// "boleh dipurge".
	//
	// Karena sequence monotonic per project, event dengan
	// sequence <= boundary inilah yang akan hilang.
	var boundarySequence int64
	var boundaryHash string

	err = tx.QueryRow(ctx, `
		SELECT sequence, hash
		FROM events
		WHERE project_id = $1
		  AND occurred_at < $2
		ORDER BY sequence DESC
		LIMIT 1
	`, projectID, cutoff).Scan(
		&boundarySequence,
		&boundaryHash,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		// Tidak ada event yang melewati retention.
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf(
				"commit no-op retention: %w",
				err,
			)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf(
			"find purge boundary: %w",
			err,
		)
	}

	// =========================================================
	// CHECKPOINT
	// =========================================================
	//
	// Checkpoint ditulis SEBELUM delete, tetapi tetap dalam
	// transaksi yang sama.
	//
	// Jika delete gagal, seluruh transaksi rollback sehingga
	// checkpoint palsu tidak akan tertinggal.

	_, err = tx.Exec(ctx, `
		INSERT INTO event_retention_checkpoints (
			project_id,
			purged_through_sequence,
			purged_through_hash,
			updated_at
		)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (project_id)
		DO UPDATE SET
			purged_through_sequence = EXCLUDED.purged_through_sequence,
			purged_through_hash = EXCLUDED.purged_through_hash,
			updated_at = EXCLUDED.updated_at
		WHERE EXCLUDED.purged_through_sequence >
		      event_retention_checkpoints.purged_through_sequence
	`, projectID, boundarySequence, boundaryHash)

	if err != nil {
		return fmt.Errorf(
			"update retention checkpoint: %w",
			err,
		)
	}

	// =========================================================
	// IDEMPOTENCY KEYS
	// =========================================================
	//
	// FK idempotency_keys.event_id -> events.id tidak memakai
	// ON DELETE CASCADE pada schema saat ini.
	//
	// Karena itu idempotency_keys HARUS dihapus lebih dulu.

	idemTag, err := tx.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE project_id = $1
		  AND event_id IN (
			  SELECT id
			  FROM events
			  WHERE project_id = $1
			    AND occurred_at < $2
		  )
	`, projectID, cutoff)

	if err != nil {
		return fmt.Errorf(
			"delete idempotency keys: %w",
			err,
		)
	}

	// =========================================================
	// EVENTS
	// =========================================================

	eventTag, err := tx.Exec(ctx, `
		DELETE FROM events
		WHERE project_id = $1
		  AND occurred_at < $2
	`, projectID, cutoff)

	if err != nil {
		return fmt.Errorf(
			"delete old events: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit retention: %w",
			err,
		)
	}

	if eventTag.RowsAffected() > 0 {
		j.logger.Info(
			"events retention purge completed",
			"project_id", projectID,
			"deleted_events", eventTag.RowsAffected(),
			"deleted_idempotency_keys", idemTag.RowsAffected(),
			"checkpoint_sequence", boundarySequence,
			"cutoff", cutoff,
		)
	}

	return nil
}
