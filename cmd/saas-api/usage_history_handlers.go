package main

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
	"trailseal/project"
	"trailseal/tenant"
	"trailseal/usage"
)

// ============================================================
// CACHE — §3D: pengukuran performa (1 juta event campuran, days=30)
// menunjukkan waktu ~3.2 detik, memori kecil (+1.5 MB, aman). Memori
// bukan masalah, tapi 3+ detik tiap buka dashboard terlalu lambat.
// Sesuai keputusan §3D: "langkah pertama adalah cache dalam memori
// berumur singkat (±60 detik) per tenant dan days" — BUKAN tabel
// agregat harian (itu cuma kalau cache terbukti tidak cukup, belum
// diukur sama sekali).
//
// Cache in-process saja (beda dari §2A/D10 yang di Postgres): ini
// murni percepatan baca, bukan sumber kebenaran. Instance berbeda di
// belakang load balancer boleh punya cache terpisah/kadaluwarsa
// sedikit beda — bukan bug untuk cache berumur sependek ini.
// ============================================================

const usageHistoryCacheTTL = 60 * time.Second

type usageHistoryCacheEntry struct {
	body      fiber.Map
	expiresAt time.Time
}

var (
	usageHistoryCacheMu sync.RWMutex
	usageHistoryCache   = map[string]usageHistoryCacheEntry{}
)

func usageHistoryCacheKey(tenantID string, days int) string {
	return fmt.Sprintf("%s:%d", tenantID, days)
}

type usageBreakdownItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Billed int    `json:"billed"`
}

// sortBreakdown — billed menurun, tie-break stabil lewat key menaik
// (deterministic, bukan urutan map Go yang acak). Maksimal 10, [] bila
// kosong (sesuai 3B poin 6 — tidak pernah nil).
func sortBreakdown(totals map[string]int, labels map[string]string) []usageBreakdownItem {
	out := make([]usageBreakdownItem, 0, len(totals))
	for k, v := range totals {
		label := labels[k]
		if label == "" {
			label = k
		}
		out = append(out, usageBreakdownItem{Key: k, Label: label, Billed: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Billed != out[j].Billed {
			return out[i].Billed > out[j].Billed
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// TenantUsageHistoryHandler — GET /api/tenants/:id/usage/history?days=N
// (Phase 3, ROADMAP §8). Guard MENIRU TenantUsageHandler, TANPA
// middleware.UsageMetering (3B poin 8 — endpoint ini sendiri men-scan
// bucket usage_events; menambah UsageMetering di sini akan memperberat
// scan berikutnya).
func TenantUsageHistoryHandler(
	eventStore *usage.BoltEventStore,
	tenantStore *tenant.BoltTenantStore,
	projectStore *project.PostgresStore,
	apiKeyStore *apikey.PostgresStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		if _, err := tenantStore.Get(tenantID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant_not_found"})
		}

		days := c.QueryInt("days", 14)
		if days < 1 {
			days = 1
		} else if days > 30 {
			days = 30
		}

		cacheKey := usageHistoryCacheKey(tenantID, days)

		usageHistoryCacheMu.RLock()
		if entry, ok := usageHistoryCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
			usageHistoryCacheMu.RUnlock()
			return c.JSON(entry.body)
		}
		usageHistoryCacheMu.RUnlock()

		result, err := eventStore.UsageHistoryForTenant(tenantID, days)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "history_scan_failed"})
		}

		ctx := c.Context()

		// D11: petakan api_key -> project lewat ListByTenant + ListByProject.
		// Project soft-deleted TIDAK ikut (ListByTenant biasa, bukan
		// IncludingDeleted) — key dari project yang sudah dihapus otomatis
		// jatuh ke Unattributed, sesuai D11.
		projects, err := projectStore.ListByTenant(ctx, tenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "history_scan_failed"})
		}

		apiKeyToProject := map[string]struct{ id, name string }{}
		for _, p := range projects {
			keys, err := apiKeyStore.ListByProject(ctx, tenantID, p.ID)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "history_scan_failed"})
			}
			for _, k := range keys {
				apiKeyToProject[k.ID] = struct{ id, name string }{p.ID, p.Name}
			}
		}

		projectTotals := map[string]int{}
		projectLabels := map[string]string{"": "Unattributed"}
		for apiKeyID, count := range result.ByAPIKey {
			if apiKeyID == "" {
				projectTotals[""] += count
				continue
			}
			info, ok := apiKeyToProject[apiKeyID]
			if !ok {
				projectTotals[""] += count
				continue
			}
			projectTotals[info.id] += count
			projectLabels[info.id] = info.name
		}

		eventTypeTotals := map[string]int{}
		for et, count := range result.ByEventType {
			eventTypeTotals[et] = count
		}

		response := fiber.Map{
			"tenant_id": tenantID,
			"from":      result.From,
			"to":        result.To,
			"timezone":  "UTC",
			"series":    result.Series,
			"breakdowns": fiber.Map{
				"project":    sortBreakdown(projectTotals, projectLabels),
				"event_type": sortBreakdown(eventTypeTotals, nil),
			},
		}

		usageHistoryCacheMu.Lock()
		usageHistoryCache[cacheKey] = usageHistoryCacheEntry{
			body:      response,
			expiresAt: time.Now().Add(usageHistoryCacheTTL),
		}
		usageHistoryCacheMu.Unlock()

		return c.JSON(response)
	}
}
