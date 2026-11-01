// events/verify_incremental.go
//
// Method verifikasi INKREMENTAL untuk §2D — dipicu karena VerifyRange
// mentah pada 1 juta event makan ~74.7 detik & +582 MB (jauh dari
// target ≤±2 detik, lihat pengukuran cmd/perf-verify-test). Sesuai
// keputusan §2D: tambah method BARU, JANGAN ubah VerifyRange. File
// ini sepenuhnya aditif — tidak menyentuh verify_tenant.go,
// VerifyRange, VerifyChain, atau ComputeHash.
//
// Tabel checkpoint (audit_verify_checkpoints) dibuat lewat migrasi
// migrations/000010_audit_verify_checkpoints.up.sql, konsisten dengan
// 000001-000009 — BUKAN dibuat inline dari kode Go (dikonfirmasi tidak
// ada pola CREATE TABLE inline di repo ini).
//
// TRADE-OFF (dicatat eksplisit di roadmap §2D): verifikasi
// inkremental melewati event yang SUDAH pernah diperiksa, jadi
// perubahan pada baris LAMA tidak terdeteksi sampai verifikasi penuh
// berikutnya. Karena itu: GET (auto-refresh) sebaiknya pakai
// VerifyTenantIncremental, POST (manual, tombol "Verify now") tetap
// pakai VerifyTenant (full) yang sudah ada. File ini TIDAK mengubah
// handler GET/POST — itu langkah terpisah menyusul.
package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"trailseal/project"
)

type verifyCheckpoint struct {
	lastSequence int64
	lastHash     string
}

func (s *PostgresStore) getVerifyCheckpoint(ctx context.Context, projectID string) (*verifyCheckpoint, error) {
	var cp verifyCheckpoint
	row := s.pool.QueryRow(ctx, `
		SELECT last_verified_sequence, last_verified_hash
		FROM audit_verify_checkpoints WHERE project_id = $1
	`, projectID)
	if err := row.Scan(&cp.lastSequence, &cp.lastHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &cp, nil
}

func (s *PostgresStore) saveVerifyCheckpoint(ctx context.Context, projectID string, sequence int64, hash string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_verify_checkpoints (project_id, last_verified_sequence, last_verified_hash, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id) DO UPDATE SET
			last_verified_sequence = EXCLUDED.last_verified_sequence,
			last_verified_hash     = EXCLUDED.last_verified_hash,
			updated_at             = EXCLUDED.updated_at
	`, projectID, sequence, hash, time.Now().UTC())
	return err
}

// lastEventForProject — sequence + hash event TERAKHIR (bukan
// pertama, beda dari firstExistingEvent di verify_tenant.go). Query
// baca baru, sama semangatnya dengan firstExistingEvent (P0-8).
func (s *PostgresStore) lastEventForProject(ctx context.Context, projectID string) (sequence int64, hash string, found bool, err error) {
	row := s.pool.QueryRow(ctx, `
		SELECT sequence, hash FROM events
		WHERE project_id = $1
		ORDER BY sequence DESC
		LIMIT 1
	`, projectID)
	if scanErr := row.Scan(&sequence, &hash); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return 0, "", false, nil
		}
		return 0, "", false, fmt.Errorf("find last event: %w", scanErr)
	}
	return sequence, hash, true, nil
}

// VerifyRangeIncremental — per PROJECT. Lewati event yang sudah
// tercatat di checkpoint; VerifyRange (TIDAK DIUBAH) dipanggil apa
// adanya hanya untuk bagian yang belum pernah diperiksa.
//
// hasEvents membedakan "project benar-benar tidak pernah punya event"
// (false) dari "project punya event tapi checkpoint sudah mutakhir,
// tidak ada yang baru untuk diperiksa" (true, checkedCount=0) — dua
// kasus yang SEBELUMNYA tidak bisa dibedakan lewat checkedCount saja,
// dan menyebabkan tenant dengan checkpoint mutakhir bisa salah
// dilaporkan status "empty" alih-alih "valid" oleh caller di bawah.
func (s *PostgresStore) VerifyRangeIncremental(ctx context.Context, projectID string) (valid bool, brokenAtEventID string, checkedCount int, hasEvents bool, err error) {
	lastSeq, lastHash, found, err := s.lastEventForProject(ctx, projectID)
	if err != nil {
		return false, "", 0, false, err
	}
	if !found {
		return true, "", 0, false, nil // project benar-benar kosong
	}

	cp, err := s.getVerifyCheckpoint(ctx, projectID)
	if err != nil {
		return false, "", 0, true, err
	}
	if cp != nil && cp.lastSequence >= lastSeq {
		return true, "", 0, true, nil // ada event, tapi tidak ada yang baru sejak checkpoint terakhir
	}

	var fromSeq int64
	if cp != nil {
		fromSeq = cp.lastSequence + 1
	} else {
		fromSeq, _, _, err = s.firstExistingEvent(ctx, projectID)
		if err != nil {
			return false, "", 0, true, err
		}
	}

	valid, brokenAtEventID, checkedCount, err = s.VerifyRange(ctx, projectID, fromSeq)
	if err != nil {
		return false, "", 0, true, err // verify_failed: checkpoint TIDAK diupdate
	}
	if !valid {
		return false, brokenAtEventID, checkedCount, true, nil // broken: checkpoint TIDAK maju, diperiksa ulang dari titik ini
	}

	if err := s.saveVerifyCheckpoint(ctx, projectID, lastSeq, lastHash); err != nil {
		return false, "", 0, true, fmt.Errorf("verify_failed: gagal simpan checkpoint: %w", err)
	}
	return true, "", checkedCount, true, nil
}

// ============================================================
// VerifyTenantIncremental — orkestrasi per-tenant, SALINAN pola
// dedupe+semaphore dari VerifyTenant di verify_tenant.go (§2B poin 4),
// map/semaphore TERPISAH supaya panggilan full (VerifyTenant) dan
// inkremental tidak saling memblokir. Reuse TenantVerifyResult/
// FirstBroken yang sudah ada (sama-sama package events).
// ============================================================

type incrementalVerifyCall struct {
	wg     sync.WaitGroup
	result *TenantVerifyResult
	err    error
}

var (
	incrementalVerifyCalls     sync.Map
	incrementalVerifySemaphore = make(chan struct{}, 2)
)

func VerifyTenantIncremental(
	ctx context.Context,
	store *PostgresStore,
	projectStore *project.PostgresStore,
	tenantID string,
) (*TenantVerifyResult, error) {
	call := &incrementalVerifyCall{}
	call.wg.Add(1)

	actual, loaded := incrementalVerifyCalls.LoadOrStore(tenantID, call)
	sharedCall := actual.(*incrementalVerifyCall)
	if loaded {
		sharedCall.wg.Wait()
		return sharedCall.result, sharedCall.err
	}
	defer func() {
		incrementalVerifyCalls.Delete(tenantID)
		call.wg.Done()
	}()

	select {
	case incrementalVerifySemaphore <- struct{}{}:
		defer func() { <-incrementalVerifySemaphore }()
	case <-ctx.Done():
		call.err = ctx.Err()
		return nil, call.err
	}

	call.result, call.err = doVerifyTenantIncremental(ctx, store, projectStore, tenantID)
	return call.result, call.err
}

func doVerifyTenantIncremental(
	ctx context.Context,
	store *PostgresStore,
	projectStore *project.PostgresStore,
	tenantID string,
) (*TenantVerifyResult, error) {
	projects, err := projectStore.ListByTenantIncludingDeleted(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list projects for incremental verify: %w", err)
	}

	result := &TenantVerifyResult{
		TenantID:   tenantID,
		Status:     "empty",
		VerifiedAt: time.Now().UTC(),
	}

	anyEvents := false

	for _, p := range projects {
		valid, brokenAtEventID, checkedCount, hasEvents, err := store.VerifyRangeIncremental(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("verify_failed: %w", err)
		}
		result.EventsChecked += checkedCount
		if hasEvents {
			anyEvents = true
		}

		if !valid {
			result.Status = "broken"
			fb := &FirstBroken{EventID: brokenAtEventID}
			if evt, lookupErr := store.GetByID(ctx, brokenAtEventID); lookupErr == nil {
				seq := evt.Sequence
				ts := evt.OccurredAt
				fb.Sequence = &seq
				fb.Timestamp = &ts
			}
			result.FirstBroken = fb
			return result, nil
		}
	}

	if anyEvents {
		result.Status = "valid"
	}
	// else: tetap "empty" — benar untuk tenant tanpa project, atau
	// semua project-nya tidak pernah punya event sama sekali. Beda
	// dari sebelumnya (bug lama menyamakan ini dengan "checkpoint
	// sudah mutakhir, 0 event baru" — dua kasus berbeda).
	return result, nil
}
