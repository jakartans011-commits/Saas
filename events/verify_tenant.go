package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"trailseal/notify"
	"trailseal/project"
)

// ============================================================
// GetByID — versi PUBLIC dari getByIDTx yang sudah ada di
// store_postgres.go (private, butuh tx aktif). Dipakai audit-verify
// (D7) untuk lookup satu event by ID TANPA transaksi aktif.
// getByIDTx yang lama TIDAK disentuh.
// ============================================================
func (s *PostgresStore) GetByID(ctx context.Context, id string) (*Event, error) {
	var e Event
	var prevHash *string

	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, tenant_id, sequence, event_type, actor_id, payload, prev_hash, hash, occurred_at, created_at
		FROM events WHERE id = $1
	`, id)

	if err := row.Scan(
		&e.ID, &e.ProjectID, &e.TenantID, &e.Sequence, &e.EventType,
		&e.ActorID, &e.Payload, &prevHash, &e.Hash, &e.OccurredAt, &e.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to load event: %w", err)
	}
	if prevHash != nil {
		e.PrevHash = *prevHash
	}
	return &e, nil
}

// firstExistingEvent mengembalikan sequence dan occurred_at event
// PERTAMA yang masih ada di project (query baca baru — roadmap P0-8
// eksplisit mengizinkan ini). Dipakai untuk D2 (from_sequence ke
// VerifyRange) sekaligus D6 (oldest_event_at), satu query untuk
// keduanya. found=false kalau project tidak punya event sama sekali
// (BUKAN error — project dilewati, §2B poin 1).
func (s *PostgresStore) firstExistingEvent(
	ctx context.Context,
	projectID string,
) (sequence int64, occurredAt time.Time, found bool, err error) {
	row := s.pool.QueryRow(ctx, `
		SELECT sequence, occurred_at
		FROM events
		WHERE project_id = $1
		ORDER BY sequence ASC
		LIMIT 1
	`, projectID)

	if scanErr := row.Scan(&sequence, &occurredAt); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return 0, time.Time{}, false, nil
		}
		return 0, time.Time{}, false, fmt.Errorf("find first existing event: %w", scanErr)
	}
	return sequence, occurredAt, true, nil
}

// ============================================================
// TenantVerifyResult — bentuk PERSIS kontrak frontend (lib/integrity.ts,
// interface ChainIntegrity). JANGAN ubah nama/struktur field ini tanpa
// mengubah frontend juga (BACKEND-DASHBOARD-SPEC.md §1 mengikat).
// ============================================================
type FirstBroken struct {
	EventID   string     `json:"event_id"`
	Sequence  *int64     `json:"sequence,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

type TenantVerifyResult struct {
	TenantID      string       `json:"tenant_id"`
	Status        string       `json:"status"` // "valid" | "broken" | "empty"
	VerifiedAt    time.Time    `json:"verified_at"`
	EventsChecked int          `json:"events_checked"`
	OldestEventAt *time.Time   `json:"oldest_event_at"`
	FirstBroken   *FirstBroken `json:"first_broken"`
}

// ============================================================
// CONCURRENCY GUARD (§2B poin 4)
//   - Dedupe manual (bukan cuma mutex serialize): dua pemanggil
//     VerifyTenant untuk tenant YANG SAMA pada saat bersamaan berbagi
//     SATU perhitungan dan SATU hasil. Repo belum punya
//     golang.org/x/sync/singleflight, jadi ditulis manual di sini
//     tanpa dependency baru.
//   - verifySemaphore: batas verifikasi BERBEDA TENANT yang berjalan
//     bersamaan di seluruh proses, mulai dari 2 (VerifyRange memuat
//     seluruh event ke memori — beberapa tenant besar sekaligus bisa
//     menghabiskan memori).
//
// ============================================================
type verifyCall struct {
	wg     sync.WaitGroup
	result *TenantVerifyResult
	err    error
}

var (
	verifyCalls     sync.Map // map[string]*verifyCall
	verifySemaphore = make(chan struct{}, 2)
)

func VerifyTenant(
	ctx context.Context,
	store *PostgresStore,
	projectStore *project.PostgresStore,
	tenantID string,
) (*TenantVerifyResult, error) {
	call := &verifyCall{}
	call.wg.Add(1)

	actual, loaded := verifyCalls.LoadOrStore(tenantID, call)
	sharedCall := actual.(*verifyCall)

	if loaded {
		// Perhitungan untuk tenant ini sedang berjalan di goroutine lain
		// — tunggu hasilnya, JANGAN hitung ulang (§2B poin 4).
		sharedCall.wg.Wait()
		return sharedCall.result, sharedCall.err
	}

	defer func() {
		verifyCalls.Delete(tenantID)
		call.wg.Done()
	}()

	select {
	case verifySemaphore <- struct{}{}:
		defer func() { <-verifySemaphore }()
	case <-ctx.Done():
		call.err = ctx.Err()
		return nil, call.err
	}

	call.result, call.err = doVerifyTenant(ctx, store, projectStore, tenantID)
	return call.result, call.err
}

// doVerifyTenant — logika inti verifikasi (§2B poin 1–3), dipanggil
// HANYA lewat VerifyTenant di atas supaya dedupe+semaphore selalu aktif.
func doVerifyTenant(
	ctx context.Context,
	store *PostgresStore,
	projectStore *project.PostgresStore,
	tenantID string,
) (*TenantVerifyResult, error) {
	projects, err := projectStore.ListByTenantIncludingDeleted(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list projects for verify: %w", err)
	}

	result := &TenantVerifyResult{
		TenantID:   tenantID,
		Status:     "empty",
		VerifiedAt: time.Now().UTC(),
	}

	var oldest *time.Time

	for _, p := range projects {
		fromSeq, firstOccurredAt, found, err := store.firstExistingEvent(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("find first event for project %s: %w", p.ID, err)
		}
		if !found {
			continue
		}

		valid, brokenAtEventID, checkedCount, err := store.VerifyRange(ctx, p.ID, fromSeq)
		if err != nil {
			return nil, fmt.Errorf("verify_failed: %w", err)
		}

		result.EventsChecked += checkedCount

		if oldest == nil || firstOccurredAt.Before(*oldest) {
			t := firstOccurredAt
			oldest = &t
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
			notify.EnqueueChainBroken(p.ID, brokenAtEventID)
			result.OldestEventAt = oldest
			return result, nil
		}

		if result.Status == "empty" {
			result.Status = "valid"
		}
	}

	result.OldestEventAt = oldest
	return result, nil
}
