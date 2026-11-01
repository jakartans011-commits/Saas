package events

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/alerts"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/middleware"
	"trailseal/tenant"
	"trailseal/usage"
)

// recordFirstValueEvidence — dipanggil SETELAH minimal 1 event
// berhasil di-ingest via API key. Menulis usage + audit trail
// (supaya customer lihat "usage/audit evidence" di dashboard
// Overview sesuai DoD Phase 2B) dan menandai onboarding selesai
// (2B.4). Best-effort: kegagalan di sini TIDAK BOLEH menggagalkan
// response ingest yang event-nya sudah tersimpan valid di chain —
// hanya di-log, konsisten dengan komentar usage.RecordEvent ("caller
// tidak boleh gagalkan request bisnis hanya karena metering gagal").
func recordFirstValueEvidence(usageStore usage.EventStore, auditStore audit.Store, tenantStore tenant.TenantStore, key *apikey.APIKey) {
	if err := usage.RecordEvent(usageStore, key.TenantID, usage.EventTypeAPIRequest, map[string]string{
		"source":     "events_ingest",
		"project_id": key.ProjectID,
	}); err != nil {
		log.Printf("usage.RecordEvent failed for tenant %s: %v", key.TenantID, err)
	}

	if err := audit.RecordEvent(
		auditStore,
		key.TenantID,
		"apikey:"+key.ID,
		"event.ingest",
		"project:"+key.ProjectID,
		audit.OutcomeSuccess,
		map[string]string{"api_key_id": key.ID},
	); err != nil {
		log.Printf("audit.RecordEvent failed for tenant %s: %v", key.TenantID, err)
	}

	if tenantStore != nil {
		if err := tenant.MarkOnboardingCompleteIfUnset(tenantStore, key.TenantID); err != nil {
			log.Printf("MarkOnboardingCompleteIfUnset failed for tenant %s: %v", key.TenantID, err)
		}
	}
}

func parseRFC3339(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// ============================================================
// HANDLERS
//
// Semua handler di sini mengambil ProjectID/TenantID dari API key
// yang sudah divalidasi RequireAPIKey — TIDAK PERNAH dari body atau
// query param. Ini yang menjamin isolasi antar-project (ingest key
// project A tidak bisa dipakai untuk apapun di project B, bahkan
// kalau ID project B dikirim manual di body).
// ============================================================

const maxBatchSize = 500

// ============================================================
// POST /v1/events — single event
// ============================================================

func IngestHandler(store *PostgresStore, usageStore usage.EventStore, auditStore audit.Store, tenantStore tenant.TenantStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := middleware.APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		var req IngestRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		idempotencyKey := c.Get("Idempotency-Key")

		event, err := store.Append(c.Context(), key.ProjectID, key.TenantID, &req, idempotencyKey)
		if err != nil {
			return handleAppendError(c, err, key.ProjectID, key.TenantID)
		}

		recordFirstValueEvidence(usageStore, auditStore, tenantStore, key)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"event": event,
		})
	}
}

// ============================================================
// POST /v1/events/batch — batch ingest
//
// SEMANTIK: setiap event di batch di-append SATU PER SATU, bukan
// dalam satu transaksi raksasa — karena hash chain memerlukan
// sequence berurutan yang dikunci per-event (lihat store_postgres.go
// Append). Kalau event ke-N di tengah batch gagal (mis. quota habis
// di tengah jalan), event 1..N-1 SUDAH tersimpan valid di chain —
// response melaporkan mana yang berhasil dan mana yang gagal,
// bukan all-or-nothing.
// ============================================================

type BatchIngestRequest struct {
	Events []IngestRequest `json:"events"`
}

type BatchEventResult struct {
	Index int    `json:"index"`
	Event *Event `json:"event,omitempty"`
	Error string `json:"error,omitempty"`
}

func BatchIngestHandler(store *PostgresStore, usageStore usage.EventStore, auditStore audit.Store, tenantStore tenant.TenantStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := middleware.APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		var req BatchIngestRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		if len(req.Events) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "events_array_empty",
			})
		}
		if len(req.Events) > maxBatchSize {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":     "batch_too_large",
				"max_batch": maxBatchSize,
				"received":  len(req.Events),
			})
		}

		results := make([]BatchEventResult, len(req.Events))
		successCount := 0

		for i := range req.Events {
			// Idempotency key per-item di batch (opsional): kalau
			// client kirim field khusus, pakai itu; kalau tidak,
			// batch ini tidak idempotent per-item.
			event, err := store.Append(c.Context(), key.ProjectID, key.TenantID, &req.Events[i], "")
			if err != nil {
				results[i] = BatchEventResult{Index: i, Error: appendErrorCode(err)}
				if errors.Is(err, ErrQuotaExceeded) {
					notifyQuotaExceeded(key.ProjectID, key.TenantID)
				}
				continue
			}
			results[i] = BatchEventResult{Index: i, Event: event}
			successCount++
		}

		if successCount > 0 {
			recordFirstValueEvidence(usageStore, auditStore, tenantStore, key)
		}

		status := fiber.StatusCreated
		if successCount == 0 {
			status = fiber.StatusBadRequest
		} else if successCount < len(req.Events) {
			status = fiber.StatusMultiStatus
		}

		return c.Status(status).JSON(fiber.Map{
			"results":       results,
			"success_count": successCount,
			"total_count":   len(req.Events),
		})
	}
}

func appendErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrQuotaExceeded):
		return "quota_exceeded"
	case errors.Is(err, ErrEventTypeRequired):
		return "event_type_required"
	default:
		return "ingest_failed"
	}
}

func handleAppendError(c *fiber.Ctx, err error, projectID, tenantID string) error {
	switch {
	case errors.Is(err, ErrQuotaExceeded):
		notifyQuotaExceeded(projectID, tenantID)
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "quota_exceeded",
		})
	case errors.Is(err, ErrEventTypeRequired):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "event_type_required",
		})
	default:
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "ingest_failed",
		})
	}
}

// notifyQuotaExceeded — alert kritikal (#5). dedupeKey per project
// supaya satu tenant yang terus mengirim event setelah kuotanya
// habis (ekspektasi normal: retry client yang tidak berhenti) tidak
// membanjiri Slack/Discord — lihat cooldown di alerts.NotifyCritical.
func notifyQuotaExceeded(projectID, tenantID string) {
	alerts.NotifyCritical(
		"quota_exceeded:"+projectID,
		"Monthly event quota exceeded",
		fmt.Sprintf("Project %s (tenant %s) has hit its monthly event ingest quota. New events are being rejected.", projectID, tenantID),
		map[string]string{
			"project_id": projectID,
			"tenant_id":  tenantID,
		},
	)
}

// ============================================================
// GET /v1/events — query + cursor pagination
// ============================================================

func QueryHandler(store *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := middleware.APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		filter := QueryFilter{
			EventType: c.Query("event_type"),
			ActorID:   c.Query("actor_id"),
			Cursor:    c.Query("cursor"),
			Limit:     c.QueryInt("limit", 100),
		}

		if since := c.Query("since"); since != "" {
			t, err := parseRFC3339(since)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_since_format",
				})
			}
			filter.Since = &t
		}
		if until := c.Query("until"); until != "" {
			t, err := parseRFC3339(until)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_until_format",
				})
			}
			filter.Until = &t
		}

		result, err := store.Query(c.Context(), key.ProjectID, filter)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "query_failed",
			})
		}

		return c.JSON(fiber.Map{
			"events":      result.Events,
			"next_cursor": result.NextCursor,
		})
	}
}

// ============================================================
// POST /v1/events/verify — verifikasi hash chain
// ============================================================

type VerifyRequest struct {
	// FromSequence opsional — kalau 0/kosong, verifikasi dari
	// event pertama project (seluruh chain). Kalau diisi, verifikasi
	// mulai dari checkpoint itu (lebih murah untuk chain besar).
	FromSequence int64 `json:"from_sequence"`
}

func VerifyHandler(store *PostgresStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := middleware.APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		// Body kosong itu VALID di sini (berarti "verifikasi seluruh
		// chain") — hanya body yang ADA tapi malformed yang ditolak.
		var req VerifyRequest
		if len(c.Body()) > 0 {
			if err := c.BodyParser(&req); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_body",
				})
			}
		}

		fromSeq := req.FromSequence
		if fromSeq <= 0 {
			fromSeq = 1
		}

		valid, brokenAtEventID, checkedCount, err := store.VerifyRange(c.Context(), key.ProjectID, fromSeq)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "verify_failed",
			})
		}

		resp := fiber.Map{
			"valid":         valid,
			"checked_count": checkedCount,
			"from_sequence": fromSeq,
		}
		if !valid {
			resp["broken_at_event_id"] = brokenAtEventID

			// Hash chain rusak = insiden integritas produk INTI
			// TrailSeal — selalu alert, TIDAK di-dedupe agresif
			// seperti quota_exceeded, karena ini seharusnya sangat
			// jarang terjadi dan tiap kejadian layak diselidiki.
			alerts.NotifyCritical(
				"chain_broken:"+key.ProjectID+":"+brokenAtEventID,
				"Event hash chain integrity check failed",
				fmt.Sprintf(
					"Project %s: chain verification from sequence %d found a break at event %s. This may indicate tampering or data corruption.",
					key.ProjectID, fromSeq, brokenAtEventID,
				),
				map[string]string{
					"project_id":         key.ProjectID,
					"broken_at_event_id": brokenAtEventID,
					"from_sequence":      strconv.FormatInt(fromSeq, 10),
					"checked_count":      strconv.Itoa(checkedCount),
				},
			)
		}

		return c.JSON(resp)
	}
}

// ExportCapResolver mengembalikan batas jumlah event export per tenant.
// 0 = tanpa batas.
type ExportCapResolver func(tenantID string) int

// ============================================================
// GET /v1/events/export — export seluruh (atau sebagian, lewat
// filter yang sama seperti QueryHandler) event project sebagai
// JSON atau CSV. (#8)
//
// Dipanggil dengan API key yang sama dengan /v1/events (ScopeCanRead),
// scoped ke project dari key — TIDAK ADA project_id di URL/body,
// konsisten dengan seluruh /v1/events/* lainnya.
//
// Query params:
//   format     — "json" (default) atau "csv"
//   event_type, actor_id, since, until — sama seperti GET /v1/events
//   max        — batas jumlah event yang di-export, default 10000,
//                hard cap 100000 (export dibaca dalam memori, BUKAN
//                streaming per-row dari DB — cap ini yang mencegah
//                satu request export menghabiskan memori server)
// ============================================================

const (
	defaultExportMax = 10000
	maxExportMax     = 100000
	exportPageSize   = 500 // sama dengan batas maksimum QueryFilter.Limit
)

func ExportHandler(store *PostgresStore, capResolver ...ExportCapResolver) fiber.Handler {
	var resolveCap ExportCapResolver
	if len(capResolver) > 0 {
		resolveCap = capResolver[0]
	}
	return func(c *fiber.Ctx) error {
		key, ok := middleware.APIKeyFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		format := c.Query("format", "json")
		if format != "json" && format != "csv" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_format: must be 'json' or 'csv'",
			})
		}

		maxEvents := c.QueryInt("max", defaultExportMax)
		if maxEvents <= 0 || maxEvents > maxExportMax {
			maxEvents = defaultExportMax
		}

		filter := QueryFilter{
			EventType: c.Query("event_type"),
			ActorID:   c.Query("actor_id"),
			Limit:     exportPageSize,
		}

		if since := c.Query("since"); since != "" {
			t, err := parseRFC3339(since)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_since_format",
				})
			}
			filter.Since = &t
		}
		if until := c.Query("until"); until != "" {
			t, err := parseRFC3339(until)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_until_format",
				})
			}
			filter.Until = &t
		}

		// Paginate lewat store.Query (cursor pagination yang sama
		// dengan GET /v1/events) sampai maxEvents tercapai atau
		// halaman habis — BUKAN query export terpisah, supaya
		// filter/behavior export selalu konsisten dengan query biasa.
		exportCap := 0
		if resolveCap != nil {
			exportCap = resolveCap(key.TenantID)
		}

		var all []*Event
		if exportCap > 0 {
			n := maxEvents
			if n > exportCap {
				n = exportCap
			}
			latest, err := store.QueryLatest(c.Context(), key.ProjectID, filter, n)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "export_query_failed",
				})
			}
			all = latest
			c.Set("X-Export-Capped", "true")
			c.Set("X-Export-Plan-Cap", strconv.Itoa(exportCap))
		} else {
			for {
				result, err := store.Query(c.Context(), key.ProjectID, filter)
				if err != nil {
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error": "export_query_failed",
					})
				}
				all = append(all, result.Events...)
				if result.NextCursor == "" || len(all) >= maxEvents {
					break
				}
				filter.Cursor = result.NextCursor
			}
			if len(all) > maxEvents {
				all = all[:maxEvents]
			}
		}

		timestamp := time.Now().UTC().Format("20060102T150405Z")
		filename := fmt.Sprintf("trailseal_events_%s_%s.%s", key.ProjectID, timestamp, format)
		c.Set("Content-Disposition", "attachment; filename="+filename)

		if format == "csv" {
			return writeEventsCSV(c, all)
		}
		return writeEventsJSON(c, all, key.ProjectID)
	}
}

func writeEventsJSON(c *fiber.Ctx, events []*Event, projectID string) error {
	c.Set("Content-Type", "application/json")
	return c.JSON(fiber.Map{
		"project_id":   projectID,
		"exported_at":  time.Now().UTC().Format(time.RFC3339),
		"total_events": len(events),
		"events":       events,
	})
}

var csvHeader = []string{
	"id", "project_id", "tenant_id", "sequence", "event_type",
	"actor_id", "payload", "prev_hash", "hash", "occurred_at", "created_at",
}

func writeEventsCSV(c *fiber.Ctx, events []*Event) error {
	c.Set("Content-Type", "text/csv")

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	if err := w.Write(csvHeader); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "export_write_failed",
		})
	}

	for _, e := range events {
		payload, err := json.Marshal(e.Payload)
		if err != nil {
			payload = []byte("{}")
		}
		row := []string{
			e.ID,
			e.ProjectID,
			e.TenantID,
			strconv.FormatInt(e.Sequence, 10),
			e.EventType,
			e.ActorID,
			string(payload),
			e.PrevHash,
			e.Hash,
			e.OccurredAt.UTC().Format(time.RFC3339Nano),
			e.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if err := w.Write(row); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "export_write_failed",
			})
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "export_write_failed",
		})
	}

	return c.Status(fiber.StatusOK).Send(buf.Bytes())
}

// scopeCanIngest / scopeCanRead — dipakai dengan middleware.RequireScope
// saat pendaftaran route (lihat app.go).
func ScopeCanIngest(s apikey.Scope) bool { return s.CanIngest() }
func ScopeCanRead(s apikey.Scope) bool   { return s.CanRead() }
