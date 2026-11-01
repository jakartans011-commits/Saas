package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/events"
)

// TestTenantUsage_QuotaFromPostgresCounter mengunci sumber angka kuota di
// dashboard.
//
// Sebelum perbaikan, handler menampilkan Tenant.CurrentEventCount dari bbolt
// — counter yang HANYA dinaikkan jalur MCP. Pelanggan yang mengirim event
// lewat /v1/events melihat pemakaian nol sampai tiba-tiba ditolak 429, karena
// ingest menolak berdasarkan counter Postgres yang sama sekali berbeda.
//
// Angka disemai langsung ke Postgres: yang diuji adalah DARI MANA handler
// membaca, bukan jalur ingest.
func TestTenantUsage_QuotaFromPostgresCounter(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := fmt.Sprintf("tenant-usage-src-%d", time.Now().UnixNano())
	email := tenantID + "@example.test"
	const password = "usage-source-password"

	env.bootstrapTenantOwner(t, tenantID, "Usage Source Tenant", email, password)

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = env.pgPool.Exec(ctx, `DELETE FROM tenant_usage_counters WHERE tenant_id = $1`, tenantID)
		_, _ = env.pgPool.Exec(ctx, `DELETE FROM tenant_quotas WHERE tenant_id = $1`, tenantID)
	})

	const wantUsed = 4242
	const wantLimit = 5000

	if _, err := env.pgPool.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id, monthly_event_limit)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET monthly_event_limit = EXCLUDED.monthly_event_limit
	`, tenantID, wantLimit); err != nil {
		t.Fatalf("seed tenant_quotas: %v", err)
	}

	if _, err := env.pgPool.Exec(ctx, `
		INSERT INTO tenant_usage_counters (tenant_id, period, event_count)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, period) DO UPDATE SET event_count = EXCLUDED.event_count
	`, tenantID, events.CurrentPeriod(), wantUsed); err != nil {
		t.Fatalf("seed tenant_usage_counters: %v", err)
	}

	token := env.login(t, email, password)

	var out struct {
		QuotaUsed  int64 `json:"quota_used"`
		QuotaLimit int64 `json:"quota_limit"`
	}
	status := env.doJSON(t, fiber.MethodGet,
		"/api/tenants/"+tenantID+"/usage", bearer(token), nil, &out)
	if status != fiber.StatusOK {
		t.Fatalf("GET usage: status %d", status)
	}

	if out.QuotaUsed != wantUsed {
		t.Errorf("quota_used = %d, want %d — dashboard masih membaca counter bbolt, bukan counter Postgres yang menahan ingest",
			out.QuotaUsed, wantUsed)
	}
	if out.QuotaLimit != wantLimit {
		t.Errorf("quota_limit = %d, want %d", out.QuotaLimit, wantLimit)
	}
}
