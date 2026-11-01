package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================
// RETENTION TESTS
//
// Test ini memakai PostgreSQL sungguhan karena RetentionJob
// bekerja langsung pada:
//   - projects
//   - events
//   - idempotency_keys
//   - event_retention_checkpoints
//
// Test TIDAK berjalan secara default.
//
// Untuk menjalankannya secara sengaja:
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./events -run Retention -v
//
// Jangan gunakan database production.
// ============================================================

func retentionTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip(
			"TRAILSEAL_TEST_POSTGRES_URL is not set; " +
				"retention integration tests skipped",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping postgres: %v", err)
	}

	// FIX: pool.Close() is registered via t.Cleanup (not `defer` in the
	// caller) so it runs LAST, in LIFO order, AFTER every
	// t.Cleanup-registered DELETE from createRetentionTestProject /
	// cleanupRetentionTestProject. Previously every test did
	// `defer pool.Close()` right after calling this function, which
	// runs BEFORE t.Cleanup callbacks fire on test exit -- so the
	// pool was closed before cleanup queries ever ran, silently
	// leaking a new "retention-test-*" project on every single test
	// run. Those never-deleted projects piled up and made RunOnce()
	// (which iterates ALL projects) time out under load.
	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func retentionTestLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			os.Stdout,
			&slog.HandlerOptions{
				Level: slog.LevelError,
			},
		),
	)
}

// createRetentionTestProject membuat project khusus test.
// Project ini selalu dihapus kembali oleh cleanup.
func createRetentionTestProject(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	retentionDays int,
) string {
	t.Helper()

	var projectID string

	err := pool.QueryRow(ctx, `
		INSERT INTO projects (
			tenant_id,
			name,
			status,
			retention_days
		)
		VALUES ($1, $2, 'active', $3)
		RETURNING id
	`,
		"retention-test-tenant",
		"retention-test-"+time.Now().Format("20060102150405.000000000"),
		retentionDays,
	).Scan(&projectID)

	if err != nil {
		t.Fatalf("failed to create retention test project: %v", err)
	}

	t.Cleanup(func() {
		cleanupRetentionTestProject(
			t,
			context.Background(),
			pool,
			projectID,
		)
	})

	return projectID
}

func cleanupRetentionTestProject(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID string,
) {
	t.Helper()

	// Hapus idempotency_keys lebih dulu karena FK menunjuk events(id).
	_, err := pool.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		t.Logf(
			"cleanup idempotency_keys for project %s: %v",
			projectID,
			err,
		)
	}

	_, err = pool.Exec(ctx, `
		DELETE FROM events
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		t.Logf(
			"cleanup events for project %s: %v",
			projectID,
			err,
		)
	}

	_, err = pool.Exec(ctx, `
		DELETE FROM event_retention_checkpoints
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		t.Logf(
			"cleanup retention checkpoint for project %s: %v",
			projectID,
			err,
		)
	}

	_, err = pool.Exec(ctx, `
		DELETE FROM project_chain_state
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		t.Logf(
			"cleanup chain state for project %s: %v",
			projectID,
			err,
		)
	}

	_, err = pool.Exec(ctx, `
		DELETE FROM project_usage_counters
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		t.Logf(
			"cleanup usage counter for project %s: %v",
			projectID,
			err,
		)
	}

	_, err = pool.Exec(ctx, `
		DELETE FROM projects
		WHERE id = $1
	`, projectID)
	if err != nil {
		t.Logf(
			"cleanup project %s: %v",
			projectID,
			err,
		)
	}
}

// insertRetentionTestEvent memasukkan event test langsung ke DB.
//
// Event dibuat dengan hash chain yang valid:
// seq 1 -> seq 2 -> seq 3
func insertRetentionTestEvent(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	tenantID string,
	sequence int64,
	occurredAt time.Time,
	eventType string,
	actorID string,
	payload json.RawMessage,
	prevHash string,
) (string, string) {
	t.Helper()

	hash := ComputeHash(
		projectID,
		sequence,
		occurredAt,
		eventType,
		actorID,
		payload,
		prevHash,
	)

	var eventID string

	err := tx.QueryRow(ctx, `
		INSERT INTO events (
			project_id,
			tenant_id,
			sequence,
			event_type,
			actor_id,
			payload,
			prev_hash,
			hash,
			occurred_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9
		)
		RETURNING id
	`,
		projectID,
		tenantID,
		sequence,
		eventType,
		actorID,
		payload,
		nullIfEmpty(prevHash),
		hash,
		occurredAt,
	).Scan(&eventID)

	if err != nil {
		t.Fatalf(
			"failed to insert test event sequence %d: %v",
			sequence,
			err,
		)
	}

	return eventID, hash
}

// ============================================================
// TEST 1
//
// Event yang lebih tua dari retention:
// - dihapus
// - idempotency_key ikut dihapus
// - checkpoint dibuat
// - event baru TIDAK dihapus
// ============================================================

func TestRetentionJob_PurgesOldEventsAndCreatesCheckpoint(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	projectID := createRetentionTestProject(
		t,
		ctx,
		pool,
		90,
	)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin fixture transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC().Truncate(time.Microsecond)

	oldTime1 := now.AddDate(0, 0, -120)
	oldTime2 := now.AddDate(0, 0, -100)

	newTime := now.AddDate(0, 0, -10)

	payload1 := json.RawMessage(`{"test":"old-1"}`)
	payload2 := json.RawMessage(`{"test":"old-2"}`)
	payload3 := json.RawMessage(`{"test":"new"}`)

	_, hash1 := insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		1,
		oldTime1,
		"retention.test.old",
		"tester",
		payload1,
		"",
	)

	event2ID, hash2 := insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		2,
		oldTime2,
		"retention.test.old",
		"tester",
		payload2,
		hash1,
	)

	_, _ = insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		3,
		newTime,
		"retention.test.new",
		"tester",
		payload3,
		hash2,
	)

	// Idempotency key sengaja dibuat untuk event lama.
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (
			project_id,
			idempotency_key,
			event_id
		)
		VALUES ($1, $2, $3)
	`,
		projectID,
		"retention-test-idempotency",
		event2ID,
	)
	if err != nil {
		t.Fatalf(
			"failed to insert idempotency fixture: %v",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit fixtures: %v", err)
	}

	job := NewRetentionJob(
		pool,
		retentionTestLogger(),
	)

	if err := job.RunOnce(ctx); err != nil {
		t.Fatalf("retention job failed: %v", err)
	}

	// --------------------------------------------------------
	// Event 1 dan 2 harus sudah dihapus.
	// --------------------------------------------------------

	var oldCount int

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM events
		WHERE project_id = $1
		  AND sequence <= 2
	`, projectID).Scan(&oldCount)

	if err != nil {
		t.Fatalf("failed to count purged events: %v", err)
	}

	if oldCount != 0 {
		t.Fatalf(
			"expected old events to be deleted, found %d",
			oldCount,
		)
	}

	// --------------------------------------------------------
	// Event 3 masih harus ada.
	// --------------------------------------------------------

	var newCount int

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM events
		WHERE project_id = $1
		  AND sequence = 3
	`, projectID).Scan(&newCount)

	if err != nil {
		t.Fatalf("failed to check new event: %v", err)
	}

	if newCount != 1 {
		t.Fatalf(
			"expected new event to remain, found %d",
			newCount,
		)
	}

	// --------------------------------------------------------
	// Idempotency key event lama harus ikut terhapus.
	// --------------------------------------------------------

	var idempotencyCount int

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM idempotency_keys
		WHERE project_id = $1
		  AND idempotency_key = $2
	`,
		projectID,
		"retention-test-idempotency",
	).Scan(&idempotencyCount)

	if err != nil {
		t.Fatalf(
			"failed to check idempotency key: %v",
			err,
		)
	}

	if idempotencyCount != 0 {
		t.Fatalf(
			"expected old idempotency key to be deleted, found %d",
			idempotencyCount,
		)
	}

	// --------------------------------------------------------
	// Checkpoint harus menunjuk event 2.
	// --------------------------------------------------------

	var checkpointSequence int64
	var checkpointHash string

	err = pool.QueryRow(ctx, `
		SELECT
			purged_through_sequence,
			purged_through_hash
		FROM event_retention_checkpoints
		WHERE project_id = $1
	`, projectID).Scan(
		&checkpointSequence,
		&checkpointHash,
	)

	if err != nil {
		t.Fatalf(
			"failed to load retention checkpoint: %v",
			err,
		)
	}

	if checkpointSequence != 2 {
		t.Fatalf(
			"expected checkpoint sequence 2, got %d",
			checkpointSequence,
		)
	}

	if checkpointHash != hash2 {
		t.Fatalf(
			"checkpoint hash mismatch: expected %s, got %s",
			hash2,
			checkpointHash,
		)
	}
}

// ============================================================
// TEST 2
//
// Kalau belum ada event yang melewati retention boundary,
// retention tidak boleh membuat checkpoint palsu.
// ============================================================

func TestRetentionJob_DoesNothingWhenNothingExpired(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	projectID := createRetentionTestProject(
		t,
		ctx,
		pool,
		90,
	)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin fixture transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC().Truncate(time.Microsecond)
	recentTime := now.AddDate(0, 0, -10)

	payload := json.RawMessage(`{"test":"recent"}`)

	_, _ = insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		1,
		recentTime,
		"retention.test.recent",
		"tester",
		payload,
		"",
	)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit fixture: %v", err)
	}

	job := NewRetentionJob(
		pool,
		retentionTestLogger(),
	)

	if err := job.RunOnce(ctx); err != nil {
		t.Fatalf("retention job failed: %v", err)
	}

	// Event tetap ada.
	var eventCount int

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM events
		WHERE project_id = $1
	`, projectID).Scan(&eventCount)

	if err != nil {
		t.Fatalf("failed to count events: %v", err)
	}

	if eventCount != 1 {
		t.Fatalf(
			"expected 1 event to remain, got %d",
			eventCount,
		)
	}

	// Checkpoint belum boleh dibuat.
	var checkpointCount int

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM event_retention_checkpoints
		WHERE project_id = $1
	`, projectID).Scan(&checkpointCount)

	if err != nil {
		t.Fatalf(
			"failed to check checkpoint: %v",
			err,
		)
	}

	if checkpointCount != 0 {
		t.Fatalf(
			"expected no checkpoint, found %d",
			checkpointCount,
		)
	}
}

// ============================================================
// TEST 3
//
// Setelah purge, VerifyRange dari event pertama yang masih ada
// harus tetap valid menggunakan retention checkpoint.
//
// Contoh:
//
//   seq 1 ─┐
//   seq 2 ─┤ PURGED
//          └── checkpoint = hash(seq 2)
//
//   seq 3 → seq 4 → seq 5
//
// VerifyRange(..., 3) harus tetap valid.
// ============================================================

func TestRetentionJob_VerifyRangeAfterPurge(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	projectID := createRetentionTestProject(
		t,
		ctx,
		pool,
		90,
	)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin fixture transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC().Truncate(time.Microsecond)

	oldTime1 := now.AddDate(0, 0, -120)
	oldTime2 := now.AddDate(0, 0, -100)

	recentTime1 := now.AddDate(0, 0, -10)
	recentTime2 := now.AddDate(0, 0, -5)
	recentTime3 := now.AddDate(0, 0, -1)

	payload1 := json.RawMessage(`{"seq":1}`)
	payload2 := json.RawMessage(`{"seq":2}`)
	payload3 := json.RawMessage(`{"seq":3}`)
	payload4 := json.RawMessage(`{"seq":4}`)
	payload5 := json.RawMessage(`{"seq":5}`)

	_, hash1 := insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		1,
		oldTime1,
		"retention.chain",
		"tester",
		payload1,
		"",
	)

	_, hash2 := insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		2,
		oldTime2,
		"retention.chain",
		"tester",
		payload2,
		hash1,
	)

	_, hash3 := insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		3,
		recentTime1,
		"retention.chain",
		"tester",
		payload3,
		hash2,
	)

	_, hash4 := insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		4,
		recentTime2,
		"retention.chain",
		"tester",
		payload4,
		hash3,
	)

	_, _ = insertRetentionTestEvent(
		t,
		ctx,
		tx,
		projectID,
		"retention-test-tenant",
		5,
		recentTime3,
		"retention.chain",
		"tester",
		payload5,
		hash4,
	)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit fixtures: %v", err)
	}

	job := NewRetentionJob(
		pool,
		retentionTestLogger(),
	)

	if err := job.RunOnce(ctx); err != nil {
		t.Fatalf("retention job failed: %v", err)
	}

	store := NewPostgresStore(pool)

	valid, brokenID, checkedCount, err := store.VerifyRange(
		ctx,
		projectID,
		3,
	)

	if err != nil {
		t.Fatalf(
			"VerifyRange failed after retention: %v",
			err,
		)
	}

	if !valid {
		t.Fatalf(
			"expected chain to remain valid after purge; broken event=%s",
			brokenID,
		)
	}

	if brokenID != "" {
		t.Fatalf(
			"expected no broken event, got %s",
			brokenID,
		)
	}

	if checkedCount != 3 {
		t.Fatalf(
			"expected 3 remaining events to be checked, got %d",
			checkedCount,
		)
	}
}
