package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/alerts"
	"trailseal/events"
	"trailseal/project"
	"trailseal/tenant"
)

const (
	auditVerifyCacheTTL       = 1 * time.Minute
	auditVerifyThrottleWindow = 5 * time.Second
)

// ============================================================
// CACHE ROW (tabel tenant_audit_verify_cache, migrasi 000009)
// ============================================================

type auditVerifyCacheRow struct {
	Status               string
	VerifiedAt           time.Time
	EventsChecked        int
	OldestEventAt        *time.Time
	FirstBrokenEventID   *string
	FirstBrokenSequence  *int64
	FirstBrokenTimestamp *time.Time
	LastForcedAt         *time.Time
}

func readAuditVerifyCache(
	ctx *fiber.Ctx,
	pool *pgxpool.Pool,
	tenantID string,
) (*auditVerifyCacheRow, error) {
	var row auditVerifyCacheRow
	err := pool.QueryRow(ctx.Context(), `
		SELECT status, verified_at, events_checked, oldest_event_at,
		       first_broken_event_id, first_broken_sequence, first_broken_timestamp,
		       last_forced_at
		FROM tenant_audit_verify_cache
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&row.Status, &row.VerifiedAt, &row.EventsChecked, &row.OldestEventAt,
		&row.FirstBrokenEventID, &row.FirstBrokenSequence, &row.FirstBrokenTimestamp,
		&row.LastForcedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func cacheRowToResult(tenantID string, row *auditVerifyCacheRow) *events.TenantVerifyResult {
	res := &events.TenantVerifyResult{
		TenantID:      tenantID,
		Status:        row.Status,
		VerifiedAt:    row.VerifiedAt,
		EventsChecked: row.EventsChecked,
		OldestEventAt: row.OldestEventAt,
	}
	if row.FirstBrokenEventID != nil {
		res.FirstBroken = &events.FirstBroken{
			EventID:   *row.FirstBrokenEventID,
			Sequence:  row.FirstBrokenSequence,
			Timestamp: row.FirstBrokenTimestamp,
		}
	}
	return res
}

// writeAuditVerifyCache menimpa status/verified_at/events_checked/
// oldest_event_at/first_broken_*. SENGAJA tidak menyentuh
// last_forced_at — itu murni tanggung jawab tryForceAuditVerify (D9),
// supaya GET yang memicu recompute (cache basi, bukan lewat POST)
// tidak ikut mereset jendela throttle POST.
func writeAuditVerifyCache(
	ctx *fiber.Ctx,
	pool *pgxpool.Pool,
	tenantID string,
	result *events.TenantVerifyResult,
) error {
	var fbID *string
	var fbSeq *int64
	var fbTS *time.Time
	if result.FirstBroken != nil {
		fbID = &result.FirstBroken.EventID
		fbSeq = result.FirstBroken.Sequence
		fbTS = result.FirstBroken.Timestamp
	}

	_, err := pool.Exec(ctx.Context(), `
		INSERT INTO tenant_audit_verify_cache (
			tenant_id, status, verified_at, events_checked, oldest_event_at,
			first_broken_event_id, first_broken_sequence, first_broken_timestamp
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (tenant_id) DO UPDATE SET
			status = EXCLUDED.status,
			verified_at = EXCLUDED.verified_at,
			events_checked = EXCLUDED.events_checked,
			oldest_event_at = EXCLUDED.oldest_event_at,
			first_broken_event_id = EXCLUDED.first_broken_event_id,
			first_broken_sequence = EXCLUDED.first_broken_sequence,
			first_broken_timestamp = EXCLUDED.first_broken_timestamp
	`, tenantID, result.Status, result.VerifiedAt, result.EventsChecked, result.OldestEventAt,
		fbID, fbSeq, fbTS)
	return err
}

// tryForceAuditVerify — throttle POST (D9): 1 request per 5 detik per
// tenant, lewat update kondisional atomik (aman multi-instance, tanpa
// limiter terpisah — lihat keputusan D9).
//
// Kalau tenant belum PERNAH punya baris cache sama sekali (POST
// pertama untuk tenant ini), baris dibuat dengan verified_at EPOCH
// (1970) — sengaja "basi" dari awal, supaya kalau ada GET yang
// bersamaan salah membaca baris placeholder ini, cek TTL di handler
// GET tetap menganggapnya basi dan tetap masuk ke jalur dedupe
// events.VerifyTenant, bukan menyajikan data placeholder ke user.
func tryForceAuditVerify(
	ctx *fiber.Ctx,
	pool *pgxpool.Pool,
	tenantID string,
) (bool, error) {
	tag, err := pool.Exec(ctx.Context(), `
		UPDATE tenant_audit_verify_cache
		SET last_forced_at = now()
		WHERE tenant_id = $1
		  AND (last_forced_at IS NULL OR last_forced_at < now() - interval '5 seconds')
	`, tenantID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}

	insertTag, err := pool.Exec(ctx.Context(), `
		INSERT INTO tenant_audit_verify_cache (tenant_id, status, verified_at, last_forced_at)
		VALUES ($1, 'empty', $2, now())
		ON CONFLICT (tenant_id) DO NOTHING
	`, tenantID, time.Unix(0, 0).UTC())
	if err != nil {
		return false, err
	}
	return insertTag.RowsAffected() > 0, nil
}

// maybeAlertChainBroken — D8: alert kritis HANYA saat hasil baru
// berubah menjadi broken, atau first_broken.event_id berbeda dari
// hasil tersimpan sebelumnya. Tidak dipicu saat melayani dari cache
// (fungsi ini hanya dipanggil setelah recompute, tidak pernah setelah
// cache-hit). alerts.NotifyCritical sendiri punya cooldown tambahan
// (isCooling) sebagai lapis kedua.
func maybeAlertChainBroken(
	tenantID string,
	previous *auditVerifyCacheRow,
	result *events.TenantVerifyResult,
) {
	if result.Status != "broken" {
		return
	}

	prevWasBroken := previous != nil && previous.Status == "broken"
	prevEventID := ""
	if previous != nil && previous.FirstBrokenEventID != nil {
		prevEventID = *previous.FirstBrokenEventID
	}
	newEventID := ""
	if result.FirstBroken != nil {
		newEventID = result.FirstBroken.EventID
	}
	if prevWasBroken && prevEventID == newEventID {
		return
	}

	alerts.NotifyCritical(
		"tenant_chain_broken:"+tenantID+":"+newEventID,
		"Tenant audit chain integrity check failed",
		fmt.Sprintf(
			"Tenant %s: audit verification found a break at event %s.",
			tenantID, newEventID,
		),
		map[string]string{
			"tenant_id":          tenantID,
			"broken_at_event_id": newEventID,
			"events_checked":     strconv.Itoa(result.EventsChecked),
		},
	)
}

// ============================================================
// HANDLER — dipakai untuk GET (forced=false) dan POST (forced=true).
// ============================================================
func AuditVerifyHandler(
	pool *pgxpool.Pool,
	tenantStore *tenant.BoltTenantStore,
	eventsStore *events.PostgresStore,
	projectStore *project.PostgresStore,
	forced bool,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		if _, err := tenantStore.Get(tenantID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant_not_found"})
		}

		previous, err := readAuditVerifyCache(c, pool, tenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "verify_failed"})
		}

		if forced {
			allowed, err := tryForceAuditVerify(c, pool, tenantID)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "verify_failed"})
			}
			if !allowed {
				c.Set("Retry-After", "5")
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error":   "rate_limited",
					"message": "Audit verification was triggered too recently. Try again shortly.",
				})
			}
		} else if previous != nil && time.Since(previous.VerifiedAt) < auditVerifyCacheTTL {
			return c.JSON(cacheRowToResult(tenantID, previous))
		}

		// forced (POST, tombol "Verify now") → full VerifyTenant, seperti semula.
		// !forced (GET, auto-refresh) → VerifyTenantIncremental (§2D): lewati
		// event yang checkpoint-nya sudah maju, jauh lebih cepat untuk tenant
		// besar. Trade-off dicatat di events/verify_incremental.go: baris LAMA
		// yang berubah tidak terdeteksi sampai verifikasi full berikutnya —
		// karena itu POST tetap harus full, bukan sekadar rate-limit tinggi.
		var result *events.TenantVerifyResult
		if forced {
			result, err = events.VerifyTenant(c.Context(), eventsStore, projectStore, tenantID)
		} else {
			result, err = events.VerifyTenantIncremental(c.Context(), eventsStore, projectStore, tenantID)
		}
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "verify_failed"})
		}

		if err := writeAuditVerifyCache(c, pool, tenantID, result); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "verify_failed"})
		}

		maybeAlertChainBroken(tenantID, previous, result)

		return c.JSON(result)
	}
}
