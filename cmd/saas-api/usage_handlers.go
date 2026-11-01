package main

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/billing"
	"trailseal/middleware"
	"trailseal/tenant"
	"trailseal/usage"
)

// ============================================================
// TENANT USAGE ENDPOINT
// GET /api/tenants/:id/usage
//
// Security chain (harus dipasang di app.go):
//   RequireAuth → RequireTenantScope → RequireCapability("tenant.{tenant_id}.usage.read") → handler
//
// Tenant hanya boleh melihat usage tenant-nya sendiri (dijamin oleh RequireTenantScope).
// ============================================================

// nextMonthlyResetDateForUsage — versi independen, sengaja TIDAK reuse fungsi
// bernama sama di portal_handlers.go: signature fungsi itu belum
// dikonfirmasi (bisa beda tipe return), jadi dibuat baru di sini
// supaya tidak ada resiko gagal compile karena tebakan signature salah.
// Logic: tanggal 1, bulan berikutnya, jam 00:00 UTC.
func nextMonthlyResetDateForUsage(now time.Time) time.Time {
	y, m, _ := now.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
}

// currentMonthStartForUsage — awal bulan berjalan (UTC), dipakai sebagai
// batas bawah untuk hitung "events billed this month". Ditambahkan
// 2026-09-14 bersamaan dengan CountBillableSinceForTenant.
func currentMonthStartForUsage(now time.Time) time.Time {
	y, m, _ := now.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// quotaTierForUsage — sama persis tiga ambang (80/90/100%) seperti
// quotaWarningTier di portal_handlers.go, didefinisikan ulang di sini
// (bukan reuse langsung) supaya file ini tidak punya dependency implisit
// ke portal_handlers.go — kalau salah satu direfactor terpisah, yang
// lain tidak ikut rusak.
func quotaTierForUsage(percent int) string {
	switch {
	case percent >= 100:
		return "exceeded"
	case percent >= 90:
		return "critical"
	case percent >= 80:
		return "warning"
	default:
		return "ok"
	}
}

// TenantUsageHandler handles GET /api/tenants/:id/usage
//
// CHANGED: sekarang menerima tenantStore tambahan untuk membaca
// Tenant.CurrentEventCount / Tenant.MonthlyEventLimit — angka quota
// resmi yang sama dipakai TryConsumeQuota (atomic, akurat sepanjang
// periode berjalan), BUKAN dihitung ulang dari daftar event
// ter-paginasi (yang cuma merefleksikan halaman terakhir, tidak
// akurat untuk total bulanan). Lihat API-CONTRACTS.md bagian Usage
// untuk kronologi kenapa perubahan ini diperlukan.
//
// CHANGED (2026-09-13): tambah field "plan_tier" ke response.
// Sebelumnya plan tenant cuma di-resolve di portal_handlers.go (portal
// lama), tidak pernah diekspos ke endpoint JSON manapun yang dipakai SPA
// baru — SPA plan badge jadi hardcode "Free" terus-menerus walau tenant
// sudah upgrade. Sumber & fallback SAMA PERSIS dengan portal_handlers.go
// (t.PlanTier, fallback string(billing.PlanFree) kalau kosong) supaya
// tidak ada 2 sumber kebenaran plan yang bisa berbeda.
//
// CHANGED (2026-09-14): tambah field "events_billed" — hitungan event MCP
// billable (tools/call, resources/read, prompts/get; SEMUA sudah sukses,
// lihat usage.IsBillableEventType) sejak awal bulan berjalan. Berbeda dari
// "quota_used" (semua attempt, termasuk gagal, dari Tenant.CurrentEventCount)
// — lihat PRD §2 poin 2 dan pricing-and-mcp-decisions-v2.md §4.
func TenantUsageHandler(
	eventStore usage.EventStore,
	tenantStore *tenant.BoltTenantStore,
	quotaSource tenantQuotaReader,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant id is required",
			})
		}

		// Double-check tenant match (sudah dijamin oleh RequireTenantScope)
		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "access denied: tenant mismatch",
			})
		}

		// Parse dan validasi limit
		limit := 50 // default
		if limitStr := c.Query("limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil {
				switch {
				case parsed <= 0:
					limit = 50
				case parsed > 200:
					limit = 200
				default:
					limit = parsed
				}
			}
			// Jika parsing gagal, tetap pakai default 50 (fail closed)
		}

		// Ambil events dari store
		events, err := eventStore.ListForTenant(tenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve usage data",
			})
		}

		// ========================================================
		// QUOTA AGGREGATE — sumbernya Tenant struct (tenant/tenant.go),
		// field yang sama dipakai TryConsumeQuota, bukan hitung ulang dari
		// events di atas.
		// ========================================================
		t, err := tenantStore.Get(tenantID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant_not_found",
			})
		}

		// Nilai bbolt sebagai cadangan. Counter ini HANYA dinaikkan jalur
		// MCP, jadi tidak pernah mencerminkan pemakaian ingest.
		quotaLimit := t.MonthlyEventLimit
		if quotaLimit <= 0 {
			quotaLimit = tenant.DefaultMonthlyEventLimit
		}
		quotaUsed := t.CurrentEventCount

		// Sumber otoritatif: counter Postgres yang sama dipakai ingest untuk
		// menolak dengan quota_exceeded. Tanpa ini, pelanggan yang mengirim
		// event lewat /v1/events melihat pemakaian nol sampai tiba-tiba
		// ditolak 429 — angka di layar tidak pernah berhubungan dengan batas
		// yang sebenarnya menahan mereka.
		//
		// Fail-soft: Postgres tidak terjangkau atau baris kuota belum ada
		// berarti tetap tampilkan nilai cadangan, bukan 500.
		if quotaSource != nil {
			if used, limit, qErr := quotaSource.TenantQuotaUsage(c.UserContext(), tenantID); qErr == nil {
				quotaUsed = used
				if limit > 0 {
					quotaLimit = limit
				}
			}
		}

		percent := 0
		if quotaLimit > 0 {
			percent = int(float64(quotaUsed) / float64(quotaLimit) * 100)
		}

		// Plan tier — sama pola resolve seperti portal_handlers.go (baris
		// ~196-198): t.PlanTier, fallback ke Free kalau kosong (mis. tenant
		// lama yang belum pernah checkout).
		planTier := t.PlanTier
		if planTier == "" {
			planTier = string(billing.PlanFree)
		}

		// Events billed this month — hitung terpisah dari quota_used.
		// Gagal hitung TIDAK menggagalkan seluruh response (fail-soft):
		// lebih baik tampilkan usage tanpa angka ini daripada 500 total.
		eventsBilled := 0
		if bc, bErr := eventStore.CountBillableSinceForTenant(
			tenantID, currentMonthStartForUsage(time.Now()),
		); bErr == nil {
			eventsBilled = bc
		}

		// Response (metadata sudah aman, tidak ada credential)
		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"limit":     limit,
			"events":    events,
			"count":     len(events),

			// Field baru — lihat API-CONTRACTS.md bagian Usage.
			"quota_used":         quotaUsed,
			"quota_limit":        quotaLimit,
			"quota_percent":      percent,
			"quota_warning_tier": quotaTierForUsage(percent),
			"quota_reset_date":   nextMonthlyResetDateForUsage(time.Now()).Format(time.RFC3339),

			// Field baru (2026-09-13) — plan tenant, sebelumnya cuma ada di
			// portal lama.
			"plan_tier": planTier,

			// Field baru (2026-09-14) — events billed this month, TERPISAH
			// dari quota_used. Lihat PRD §2 poin 2.
			"events_billed": eventsBilled,
		})
	}
}

// ============================================================
// ADMIN USAGE ENDPOINT
// GET /api/admin/tenants/:id/usage
//
// Security: RequireRoot SAJA (seperti endpoint admin lainnya)
//
// Root dapat melihat usage tenant mana pun.
// Tenant existence dicek dengan tenantStore.Get().
// ============================================================

// AdminUsageHandler handles GET /api/admin/tenants/:id/usage
func AdminUsageHandler(
	eventStore usage.EventStore,
	tenantStore *tenant.BoltTenantStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant id is required",
			})
		}

		// Verifikasi tenant exists (konsisten dengan bootstrap-owner)
		if _, err := tenantStore.Get(tenantID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}

		// Parse dan validasi limit
		limit := 50 // default
		if limitStr := c.Query("limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil {
				switch {
				case parsed <= 0:
					limit = 50
				case parsed > 200:
					limit = 200
				default:
					limit = parsed
				}
			}
		}

		// Ambil events dari store
		events, err := eventStore.ListForTenant(tenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve usage data",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"limit":     limit,
			"events":    events,
			"count":     len(events),
		})
	}
}
